package acrq_controller

import (
	"context"
	"fmt"
	quotav1 "github.com/openshift/api/quota/v1"
	"github.com/openshift/library-go/pkg/quota/quotautil"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kutilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	quota "k8s.io/apiserver/pkg/quota/v1"
	utilquota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	aaq_evaluator "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-evaluator"
	arq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-gate-controller"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/clusterquotamapping"
	crq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/crq-controller"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"

	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"strings"
	"sync"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
)

type AcrqController struct {
	podInformer    cache.SharedIndexInformer
	aaqjqcInformer cache.SharedIndexInformer
	aaqCli         client.AAQClient

	AcrqInformer    cache.SharedIndexInformer
	crqInformer     cache.SharedIndexInformer
	namespaceLister v12.NamespaceLister

	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper
	resyncPeriod       time.Duration

	// queue tracks which clusterquotas to update along with a list of namespaces for that clusterquota
	queue util.BucketingWorkQueue

	// nsQueue tracks changes in Aaqjqcs
	nsQueue workqueue.RateLimitingInterface

	// knows how to calculate usage
	registry utilquota.Registry
	// controls the workers that process quotas
	// this lock is acquired to control write access to the monitors and ensures that all
	// monitors are synced before the controller can process quotas.
	workerLock      sync.RWMutex
	stop            <-chan struct{}
	collectCrqsData bool
}

type workItem struct {
	namespaceName      string
	forceRecalculation bool
}

func NewAcrqController(aaqCli client.AAQClient,
	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper,
	AcrqInformer cache.SharedIndexInformer,
	crqInformer cache.SharedIndexInformer,
	podInformer cache.SharedIndexInformer,
	aaqjqcInformer cache.SharedIndexInformer,
	calcRegistry *aaq_evaluator.AaqEvaluatorRegistry,
	namespaceLister v12.NamespaceLister,
	stop <-chan struct{},
	collectCrqsData bool,
) *AcrqController {
	ctrl := &AcrqController{
		AcrqInformer:       AcrqInformer,
		clusterQuotaMapper: clusterQuotaMapper,
		aaqCli:             aaqCli,
		aaqjqcInformer:     aaqjqcInformer,
		crqInformer:        crqInformer,
		podInformer:        podInformer,
		resyncPeriod:       metav1.Duration{Duration: 5 * time.Minute}.Duration,
		namespaceLister:    namespaceLister,
		registry:           generic.NewRegistry([]quota.Evaluator{aaq_evaluator.NewAaqEvaluator(v12.NewPodLister(podInformer.GetIndexer()), calcRegistry, clock.RealClock{})}),
		queue:              util.NewBucketingWorkQueue("controller_clusterquotareconcilationcontroller"),
		nsQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ns_queue"),
		stop:               stop,
		collectCrqsData:    collectCrqsData,
	}

	AcrqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addAcrq,
		UpdateFunc: ctrl.updateAcrq,
	})

	_, err := ctrl.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updatePod,
		AddFunc:    ctrl.addPod,
		DeleteFunc: ctrl.deletePod,
	})
	if err != nil {
		panic("something is wrong")
	}
	_, err = ctrl.aaqjqcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateAaqjqc,
		AddFunc:    ctrl.addAaqjqc,
	})
	if err != nil {
		panic("something is wrong")
	}
	if collectCrqsData {
		_, err = ctrl.crqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: ctrl.updateCRQ,
			AddFunc:    ctrl.addCRQ,
			DeleteFunc: ctrl.deleteCRQ,
		})
		if err != nil {
			panic("something is wrong")
		}
	}
	return ctrl
}

// Run begins quota controller using the specified number of workers
func (c *AcrqController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting acrq controller")
	defer klog.Info("Shutting acrq Controller")
	defer utilruntime.HandleCrash()

	klog.Infof("Starting the cluster quota reconciliation controller")

	// the workers that chug through the quota calculation backlog
	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, c.stop)
		go wait.Until(c.runGateWatcherWorker, time.Second, c.stop)

	}

	// the timer for how often we do a full recalculation across all quotas
	go wait.Until(func() { c.calculateAll(c.queue) }, c.resyncPeriod, ctx.Done())

	<-ctx.Done()
	klog.Infof("Shutting down ClusterQuotaReconcilationController")
	c.queue.ShutDown()
}

func (c *AcrqController) calculate(quotaName string, namespaceNames ...string) {
	if len(namespaceNames) == 0 {
		klog.V(2).Infof("no namespace is passed for quota %s", quotaName)
		return
	}
	items := make([]interface{}, 0, len(namespaceNames))
	for _, name := range namespaceNames {
		items = append(items, workItem{namespaceName: name, forceRecalculation: false})
	}

	klog.V(2).Infof("calculating items for quota %s with namespaces %v", quotaName, items)
	c.queue.AddWithData(quotaName, items...)
}

func (c *AcrqController) forceCalculation(quotaName string, namespaceNames ...string) {
	if len(namespaceNames) == 0 {
		return
	}
	items := make([]interface{}, 0, len(namespaceNames))
	for _, name := range namespaceNames {
		items = append(items, workItem{namespaceName: name, forceRecalculation: true})
	}

	klog.V(2).Infof("force calculating items for quota %s with namespaces %v", quotaName, items)
	c.queue.AddWithData(quotaName, items...)
}

func (ctrl *AcrqController) calculateAll(queue util.BucketingWorkQueue) {
	quotaObjs := ctrl.AcrqInformer.GetIndexer().List()

	for _, quotaObj := range quotaObjs {
		quota := quotaObj.(*v1alpha1.ApplicationAwareClusterResourceQuota)
		// If we have namespaces we map to, force calculating those namespaces
		namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(quota.Name)
		if len(namespaces) > 0 {
			klog.V(2).Infof("syncing quota %s with namespaces %v", quota.Name, namespaces)
			ctrl.forceCalculation(quota.Name, namespaces...)
			continue
		}

		// If the quota status has namespaces when our mapper doesn't think it should,
		// add it directly to the queue without any work items
		if len(quota.Status.Namespaces) > 0 {
			queue.AddWithData(quota.Name)
			continue
		}
	}
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *AcrqController) worker() {
	workFunc := func() bool {
		uncastKey, uncastData, quit := c.queue.GetWithData()
		if quit {
			return true
		}
		defer c.queue.Done(uncastKey)

		c.workerLock.RLock()
		defer c.workerLock.RUnlock()

		quotaName := uncastKey.(string)

		quotaObj, exists, err := c.AcrqInformer.GetIndexer().GetByKey(quotaName)

		if !exists || apierrors.IsNotFound(err) {
			klog.V(4).Infof("queued quota %s not found in quota lister", quotaName)
			c.queue.Forget(uncastKey)
			return false
		}
		if err != nil {
			utilruntime.HandleError(err)
			c.queue.AddWithDataRateLimited(uncastKey, uncastData...)
			return false
		}

		workItems := make([]workItem, 0, len(uncastData))
		for _, dataElement := range uncastData {
			workItems = append(workItems, dataElement.(workItem))
		}
		err, retryItems := c.syncQuotaForNamespaces(quotaObj.(*v1alpha1.ApplicationAwareClusterResourceQuota), workItems)
		if err == nil {
			c.queue.Forget(uncastKey)
			return false
		}
		utilruntime.HandleError(err)

		items := make([]interface{}, 0, len(retryItems))
		for _, item := range retryItems {
			items = append(items, item)
		}
		c.queue.AddWithDataRateLimited(uncastKey, items...)
		return false
	}

	for {
		if quit := workFunc(); quit {
			klog.Infof("resource quota controller worker shutting down")
			return
		}
	}
}

// syncResourceQuotaFromKey syncs a quota key
func (ctrl *AcrqController) syncQuotaForNamespaces(originalQuota *v1alpha1.ApplicationAwareClusterResourceQuota, workItems []workItem) (error, []workItem /* to retry */) {
	quota := originalQuota.DeepCopy()

	// get the list of namespaces that match this cluster quota
	matchingNamespaceNamesList, quotaSelector := ctrl.clusterQuotaMapper.GetNamespacesFor(quota.Name)
	if !equality.Semantic.DeepEqual(quotaSelector, quota.Spec.Selector) {
		return fmt.Errorf("mapping not up to date, have=%v need=%v", quotaSelector, quota.Spec.Selector), workItems
	}
	matchingNamespaceNames := sets.NewString(matchingNamespaceNamesList...)
	klog.V(2).Infof("syncing for quota %s with set of namespaces %v", quota.Name, matchingNamespaceNames)

	reconcilationErrors := []error{}
	retryItems := []workItem{}
	for _, item := range workItems {
		namespaceName := item.namespaceName
		namespaceTotals, namespaceLoaded := quotautil.GetResourceQuotasStatusByNamespace(quota.Status.Namespaces, namespaceName)
		if !matchingNamespaceNames.Has(namespaceName) {
			if namespaceLoaded {
				// remove this item from all totals
				quota.Status.Total.Used = utilquota.Subtract(quota.Status.Total.Used, namespaceTotals.Used)
				quotautil.RemoveResourceQuotasStatusByNamespace(&quota.Status.Namespaces, namespaceName)
			}
			continue
		}

		// if there's no work for us to do, do nothing
		if !item.forceRecalculation && namespaceLoaded && equality.Semantic.DeepEqual(namespaceTotals.Hard, quota.Spec.Quota.Hard) {
			continue
		}

		actualUsage, err := quotaUsageCalculationFunc(namespaceName, quota.Spec.Quota.Scopes, quota.Spec.Quota.Hard, ctrl.registry, quota.Spec.Quota.ScopeSelector)
		if err != nil {
			// tally up errors, but calculate everything you can
			reconcilationErrors = append(reconcilationErrors, err)
			retryItems = append(retryItems, item)
			continue
		}
		if ctrl.collectCrqsData {
			var crq *quotav1.ClusterResourceQuota
			crqObj, exists, err := ctrl.crqInformer.GetIndexer().GetByKey(quota.Name + crq_controller.CRQSuffix)
			if err != nil {
				reconcilationErrors = append(reconcilationErrors, err)
			} else if exists {
				crq = crqObj.(*quotav1.ClusterResourceQuota).DeepCopy()
			}
			if exists && crq.Status.Total.Hard != nil && quota.Status.Total.Hard != nil {
				actualUsage = includeUsageFromClusterResourceQuota(actualUsage, crq, namespaceName)
			}
		}

		recalculatedStatus := corev1.ResourceQuotaStatus{
			Used: actualUsage,
			Hard: quota.Spec.Quota.Hard,
		}

		// subtract old usage, add new usage
		quota.Status.Total.Used = utilquota.Subtract(quota.Status.Total.Used, namespaceTotals.Used)
		quota.Status.Total.Used = utilquota.Add(quota.Status.Total.Used, recalculatedStatus.Used)
		quotautil.InsertResourceQuotasStatus(&quota.Status.Namespaces, quotav1.ResourceQuotaStatusByNamespace{
			Namespace: namespaceName,
			Status:    recalculatedStatus,
		})
	}

	// Remove any namespaces from quota.status that no longer match.
	// Needed because we will never get workitems for namespaces that no longer exist if we missed the delete event (e.g. on startup)
	// range on a copy so that we don't mutate our original
	statusCopy := quota.Status.Namespaces.DeepCopy()
	for _, namespaceTotals := range statusCopy {
		namespaceName := namespaceTotals.Namespace
		if !matchingNamespaceNames.Has(namespaceName) {
			quota.Status.Total.Used = utilquota.Subtract(quota.Status.Total.Used, namespaceTotals.Status.Used)
			quotautil.RemoveResourceQuotasStatusByNamespace(&quota.Status.Namespaces, namespaceName)
		}
	}

	quota.Status.Total.Hard = quota.Spec.Quota.Hard

	// if there's no change, no update, return early.  NewAggregate returns nil on empty input
	if equality.Semantic.DeepEqual(quota, originalQuota) {
		return kutilerrors.NewAggregate(reconcilationErrors), retryItems
	}

	if _, err := ctrl.aaqCli.ApplicationAwareClusterResourceQuotas().UpdateStatus(context.TODO(), quota, metav1.UpdateOptions{}); err != nil {
		return kutilerrors.NewAggregate(append(reconcilationErrors, err)), workItems
	}

	return kutilerrors.NewAggregate(reconcilationErrors), retryItems
}

func (ctrl *AcrqController) addAllAcrqsAppliedToNamespace(namespace string) {
	quotaNames, _ := ctrl.clusterQuotaMapper.GetClusterQuotasFor(namespace)
	if len(quotaNames) > 0 {
		klog.V(2).Infof("replenish quotas %v for namespace %s", quotaNames, namespace)
	}
	for _, quotaName := range quotaNames {
		ctrl.forceCalculation(quotaName, namespace)
	}
}

func (ctrl *AcrqController) updateCRQ(old, curr interface{}) {
	crq := curr.(*quotav1.ClusterResourceQuota)
	acrq := &v1alpha1.ApplicationAwareClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(crq.Name, crq_controller.CRQSuffix), Namespace: crq.Namespace},
	}
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	ctrl.forceCalculation(acrq.Name, namespaces...)
}

func (ctrl *AcrqController) deleteCRQ(obj interface{}) {
	crq := obj.(*quotav1.ClusterResourceQuota)
	acrq := &v1alpha1.ApplicationAwareClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(crq.Name, crq_controller.CRQSuffix), Namespace: crq.Namespace},
	}
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	ctrl.forceCalculation(acrq.Name, namespaces...)
}

func (ctrl *AcrqController) addCRQ(obj interface{}) {
	crq := obj.(*quotav1.ClusterResourceQuota)
	acrq := &v1alpha1.ApplicationAwareClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(crq.Name, crq_controller.CRQSuffix), Namespace: crq.Namespace},
	}
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	ctrl.forceCalculation(acrq.Name, namespaces...)
}

// When a ApplicationAwareResourceQuota.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *AcrqController) updateAaqjqc(old, cur interface{}) {
	aaqjqc := cur.(*v1alpha1.AAQJobQueueConfig)
	if aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareClusterResourceQuotaLockName] {
		ctrl.nsQueue.Add(aaqjqc.Namespace)
	}
	return
}

// When a ApplicationAwareResourceQuota.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *AcrqController) addAaqjqc(obj interface{}) {
	aaqjqc := obj.(*v1alpha1.AAQJobQueueConfig)
	if aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareClusterResourceQuotaLockName] {
		ctrl.nsQueue.Add(aaqjqc.Namespace)
	}
	return
}

func (ctrl *AcrqController) updatePod(old, curr interface{}) {
	currPod := curr.(*v1.Pod)
	oldPod := old.(*v1.Pod)
	if len(oldPod.Spec.SchedulingGates) == 0 && len(currPod.Spec.SchedulingGates) == 0 {
		ctrl.addAllAcrqsAppliedToNamespace(currPod.Namespace)
	}
}

func (ctrl *AcrqController) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	ctrl.addAllAcrqsAppliedToNamespace(pod.Namespace)
}

func (ctrl *AcrqController) deletePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	ctrl.addAllAcrqsAppliedToNamespace(pod.Namespace)
}

func (ctrl *AcrqController) addAcrq(cur interface{}) {
	ctrl.enqueueClusterQuota(cur)
}

func (ctrl *AcrqController) updateAcrq(old, cur interface{}) {
	ctrl.enqueueClusterQuota(cur)
}

func (c *AcrqController) enqueueClusterQuota(obj interface{}) {
	quota, ok := obj.(*v1alpha1.ApplicationAwareClusterResourceQuota)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("not a ApplicationAwareClusterResourceQuota %v", obj))
		return
	}

	namespaces, _ := c.clusterQuotaMapper.GetNamespacesFor(quota.Name)
	c.calculate(quota.Name, namespaces...)
}

func (c *AcrqController) AddMapping(quotaName, namespaceName string) {
	c.calculate(quotaName, namespaceName)

}
func (c *AcrqController) RemoveMapping(quotaName, namespaceName string) {
	c.calculate(quotaName, namespaceName)
}

// quotaUsageCalculationFunc is a function to calculate quota usage.  It is only configurable for easy unit testing
// NEVER CHANGE THIS OUTSIDE A TEST
var quotaUsageCalculationFunc = utilquota.CalculateUsage

func includeUsageFromClusterResourceQuota(rl corev1.ResourceList, crq *quotav1.ClusterResourceQuota, namespace string) corev1.ResourceList {
	result := corev1.ResourceList{}
	for key, val := range rl {
		result[key] = val
	}
	for _, crqns := range crq.Status.Namespaces {
		if crqns.Namespace == namespace {
			for key, value := range crqns.Status.Used {
				result[key] = value
			}
			break
		}
	}
	return result
}

func (ctrl *AcrqController) runGateWatcherWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *AcrqController) Execute() bool {
	ns, quit := ctrl.nsQueue.Get()
	if quit {
		return false
	}
	defer ctrl.nsQueue.Done(ns)
	err, enqueueState := ctrl.execute(ns.(string))

	if err != nil {
		log.Log.Infof(fmt.Sprintf("ArqController: Error with key: %v err: %v", ns, err))
	}
	switch enqueueState {
	case BackOff:
		ctrl.nsQueue.AddRateLimited(ns)
	case Forget:
		ctrl.nsQueue.Forget(ns)
	case Immediate:
		ctrl.nsQueue.Add(ns)
	}

	return true
}

func (ctrl *AcrqController) execute(ns string) (error, enqueueState) {
	var aaqjqc *v1alpha1.AAQJobQueueConfig
	_, err := ctrl.namespaceLister.Get(ns)
	if apierrors.IsNotFound(err) {
		return nil, Forget
	}

	aaqjqcObj, exists, err := ctrl.aaqjqcInformer.GetIndexer().GetByKey(ns + "/" + arq_controller.AaqjqcName)
	if err != nil {
		return err, Immediate
	} else if exists {
		aaqjqc = aaqjqcObj.(*v1alpha1.AAQJobQueueConfig).DeepCopy()
	}

	if aaqjqc != nil && aaqjqc.Status.ControllerLock != nil && aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareClusterResourceQuotaLockName] {
		if res, err := util.VerifyPodsWithOutSchedulingGates(ctrl.aaqCli, ctrl.podInformer, ns, aaqjqc.Status.PodsInJobQueue); err != nil || !res {
			return err, Immediate //wait until gate controller remove the scheduling gates
		}

	}

	acrqs, _ := ctrl.clusterQuotaMapper.GetClusterQuotasFor(ns)
	for _, acrq := range acrqs {
		quotaObj, exists, err := ctrl.AcrqInformer.GetIndexer().GetByKey(acrq)
		if !exists || err != nil {
			return err, Immediate
		}
		err, retryItems := ctrl.syncQuotaForNamespaces(quotaObj.(*v1alpha1.ApplicationAwareClusterResourceQuota), []workItem{{namespaceName: ns, forceRecalculation: true}})
		if err != nil || len(retryItems) != 0 {
			return err, Immediate
		}
	}

	if aaqjqc != nil && aaqjqc.Status.ControllerLock != nil && aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareClusterResourceQuotaLockName] {
		aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareClusterResourceQuotaLockName] = false
		_, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).UpdateStatus(context.Background(), aaqjqc, metav1.UpdateOptions{})
		if err != nil {
			return err, Immediate
		}
	}
	return nil, Forget
}
