package arq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	aaq_evaluator "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-evaluator"
	arq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-gate-controller"
	rq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"strings"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
)

type ArqController struct {
	podInformer     cache.SharedIndexInformer
	aaqjqcInformer  cache.SharedIndexInformer
	aaqCli          client.AAQClient
	arqInformer     cache.SharedIndexInformer
	rqInformer      cache.SharedIndexInformer
	namespaceLister v12.NamespaceLister
	// A list of functions that return true when their caches have synced
	arqQueue          workqueue.RateLimitingInterface
	missingUsageQueue workqueue.RateLimitingInterface
	nsQueue           workqueue.RateLimitingInterface
	// Controls full recalculation of quota usage
	resyncPeriod time.Duration
	// knows how to calculate usage
	evalRegistry quota.Registry
	recorder     record.EventRecorder
	syncHandler  func(key string) error
	logger       klog.Logger
	stop         <-chan struct{}
}

func NewArqController(clientSet client.AAQClient,
	podInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
	rqInformer cache.SharedIndexInformer,
	aaqjqcInformer cache.SharedIndexInformer,
	calcRegistry *aaq_evaluator.AaqEvaluatorRegistry,
	namespaceLister v12.NamespaceLister,
	stop <-chan struct{},
) *ArqController {
	ctrl := &ArqController{
		aaqCli:            clientSet,
		arqInformer:       arqInformer,
		rqInformer:        rqInformer,
		podInformer:       podInformer,
		aaqjqcInformer:    aaqjqcInformer,
		arqQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_primary"),
		missingUsageQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_priority"),
		nsQueue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ns_queue"),
		resyncPeriod:      metav1.Duration{Duration: 5 * time.Minute}.Duration,
		evalRegistry:      generic.NewRegistry([]quota.Evaluator{aaq_evaluator.NewAaqEvaluator(v12.NewPodLister(podInformer.GetIndexer()), calcRegistry, clock.RealClock{})}),
		namespaceLister:   namespaceLister,
		logger:            klog.FromContext(context.Background()),
		stop:              stop,
	}
	ctrl.syncHandler = ctrl.syncResourceQuotaFromKey

	arqInformer.AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    ctrl.addArq,
			UpdateFunc: ctrl.updateArq,
			DeleteFunc: ctrl.deleteArq,
		},
		ctrl.resyncPeriod,
	)
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
	_, err = ctrl.rqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateRQ,
		AddFunc:    ctrl.addRQ,
		DeleteFunc: ctrl.deleteRQ,
	})
	if err != nil {
		panic("something is wrong")
	}

	return ctrl
}
func (ctrl *ArqController) updateRQ(old, curr interface{}) {
	curRq := curr.(*v1.ResourceQuota)
	oldRq := old.(*v1.ResourceQuota)
	if !quota.Equals(curRq.Status.Hard, oldRq.Status.Hard) || !quota.Equals(curRq.Status.Used, oldRq.Status.Used) {
		arq := &v1alpha12.ApplicationAwareResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(curRq.Name, rq_controller.RQSuffix), Namespace: curRq.Namespace},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
		if err != nil {
			return
		}

		ctrl.arqQueue.Add(key)
	}
	return
}
func (ctrl *ArqController) deleteRQ(obj interface{}) {
	rq := obj.(*v1.ResourceQuota)
	arq := &v1alpha12.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(rq.Name, rq_controller.RQSuffix), Namespace: rq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
	if err != nil {
		return
	}
	ctrl.arqQueue.Add(key)
	return
}

func (ctrl *ArqController) addRQ(obj interface{}) {
	rq := obj.(*v1.ResourceQuota)
	arq := &v1alpha12.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(rq.Name, rq_controller.RQSuffix), Namespace: rq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
	if err != nil {
		return
	}
	ctrl.arqQueue.Add(key)
	return
}

// When a ApplicationAwareResourceQuotAaqjqc.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *ArqController) updateAaqjqc(old, cur interface{}) {
	aaqjqc := cur.(*v1alpha12.AAQJobQueueConfig)
	if aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareResourceQuotaLockName] {
		ctrl.nsQueue.Add(aaqjqc.Namespace)
	}
	return
}

// When a ApplicationAwareResourceQuotAaqjqc.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *ArqController) addAaqjqc(obj interface{}) {
	aaqjqc := obj.(*v1alpha12.AAQJobQueueConfig)
	if aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareResourceQuotaLockName] {
		ctrl.nsQueue.Add(aaqjqc.Namespace)
	}
	return
}

// enqueueAll is called at the fullResyncPeriod interval to force a full recalculation of quota usage statistics
func (ctrl *ArqController) enqueueAll() {
	arqObjs := ctrl.arqInformer.GetIndexer().List()
	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationAwareResourceQuota)
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arqObj.(*v1alpha12.ApplicationAwareResourceQuota))
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", arq, err))
			continue
		}
		ctrl.arqQueue.Add(key)
	}
}
func (ctrl *ArqController) updateArq(_, curr interface{}) {
	ctrl.addQuota(ctrl.logger, curr.(*v1alpha12.ApplicationAwareResourceQuota))
}

func (ctrl *ArqController) addArq(obj interface{}) {
	ctrl.addQuota(ctrl.logger, obj)
}

func (ctrl *ArqController) deleteArq(obj interface{}) {
	ctrl.enqueueArq(ctrl.logger, obj)
}

func (ctrl *ArqController) updatePod(old, curr interface{}) {
	currPod := curr.(*v1.Pod)
	oldPod := old.(*v1.Pod)
	if len(oldPod.Spec.SchedulingGates) == 0 && len(currPod.Spec.SchedulingGates) == 0 {
		ctrl.nsQueue.Add(currPod.Namespace)
	}
}

func (ctrl *ArqController) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	ctrl.nsQueue.Add(pod.Namespace)
}

func (ctrl *ArqController) deletePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	ctrl.nsQueue.Add(pod.Namespace)
}

func (ctrl *ArqController) runGateWatcherWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *ArqController) Execute() bool {
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

func (ctrl *ArqController) execute(ns string) (error, enqueueState) {
	var aaqjqc *v1alpha12.AAQJobQueueConfig
	namespace, err := ctrl.namespaceLister.Get(ns)
	if errors.IsNotFound(err) || namespace.Status.Phase == v1.NamespaceTerminating {
		return nil, Forget
	}
	aaqjqcObj, exists, err := ctrl.aaqjqcInformer.GetIndexer().GetByKey(ns + "/" + arq_controller.AaqjqcName)
	if err != nil {
		return err, Immediate
	} else if exists {
		aaqjqc = aaqjqcObj.(*v1alpha12.AAQJobQueueConfig).DeepCopy()
	}

	if aaqjqc != nil && aaqjqc.Status.ControllerLock != nil && aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareResourceQuotaLockName] {
		if res, err := util.VerifyPodsWithOutSchedulingGates(ctrl.aaqCli, ctrl.podInformer, ns, aaqjqc.Status.PodsInJobQueue); err != nil || !res {
			return err, Immediate //wait until gate controller remove the scheduling gates
		}
	}

	arqObjs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return err, Immediate
	}

	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationAwareResourceQuota).DeepCopy()
		err := ctrl.syncResourceQuota(arq)
		if err != nil {
			return err, Immediate
		}
	}

	if aaqjqc != nil && aaqjqc.Status.ControllerLock != nil && aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareResourceQuotaLockName] {
		aaqjqc.Status.ControllerLock[arq_controller.ApplicationAwareResourceQuotaLockName] = false
		_, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).UpdateStatus(context.Background(), aaqjqc, metav1.UpdateOptions{})
		if err != nil {
			return err, Immediate
		}
	}
	return nil, Forget
}

func (ctrl *ArqController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()
	defer ctrl.arqQueue.ShutDown()
	defer ctrl.missingUsageQueue.ShutDown()
	logger := klog.FromContext(ctx)
	klog.Info("Starting ARQ controller")
	defer klog.Info("Shutting ARQ Controller")

	// the workers that chug through the quota calculation backlog
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.worker(ctrl.arqQueue), time.Second, ctrl.stop)
		go wait.Until(ctrl.worker(ctrl.missingUsageQueue), time.Second, ctrl.stop)
		go wait.Until(ctrl.runGateWatcherWorker, time.Second, ctrl.stop)
	}
	// the timer for how often we do a full recalculation across all quotas
	if ctrl.resyncPeriod > 0 {
		go wait.Until(ctrl.enqueueAll, ctrl.resyncPeriod, ctrl.stop)
	} else {
		logger.Info("periodic quota controller resync disabled")
	}
	<-ctx.Done()

}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
func (ctrl *ArqController) worker(queue workqueue.RateLimitingInterface) func() {
	workFunc := func(ctx context.Context) bool {
		key, quit := queue.Get()
		if quit {
			return true
		}
		defer queue.Done(key)

		logger := klog.FromContext(ctx)
		logger = klog.LoggerWithValues(logger, "queueKey", key)
		ctx = klog.NewContext(ctx, logger)
		err := ctrl.syncHandler(key.(string))
		if err == nil {
			queue.Forget(key)
			return false
		} else {
			log.Log.Infof("ERROR: %v", err)
		}

		utilruntime.HandleError(err)
		queue.AddRateLimited(key)

		return false
	}

	return func() {
		for {
			if quit := workFunc(context.Background()); quit {
				klog.FromContext(context.Background()).Info("resource quota controller worker shutting down")
				return
			}
		}
	}
}

func (ctrl *ArqController) addQuota(logger klog.Logger, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		logger.Error(err, "Couldn't get key", "object", obj)
		return
	}

	arq := obj.(*v1alpha12.ApplicationAwareResourceQuota)

	// if we declared an intent that is not yet captured in status (prioritize it)
	if !apiequality.Semantic.DeepEqual(arq.Spec.Hard, arq.Status.Hard) {
		ctrl.missingUsageQueue.Add(key)
		return
	}

	// if we declared a constraint that has no usage (which this controller can calculate, prioritize it)
	for constraint := range arq.Status.Hard {
		if _, usageFound := arq.Status.Used[constraint]; !usageFound {
			matchedResources := []v1.ResourceName{constraint}
			for _, evaluator := range ctrl.evalRegistry.List() {
				if intersection := evaluator.MatchingResources(matchedResources); len(intersection) > 0 {
					ctrl.missingUsageQueue.Add(key)
					return
				}
			}
		}
	}

	// no special priority, go in normal recalc queue
	ctrl.arqQueue.Add(key)
}

// obj could be an *v1.ResourceQuota, or a DeletionFinalStateUnknown marker item.
func (ctrl *ArqController) enqueueArq(logger klog.Logger, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		logger.Error(err, "Couldn't get key", "object", obj)
		return
	}
	ctrl.arqQueue.Add(key)
}

// syncResourceQuotaFromKey syncs a quota key
func (ctrl *ArqController) syncResourceQuotaFromKey(key string) (err error) {
	startTime := time.Now()

	logger := klog.FromContext(context.Background())
	logger = klog.LoggerWithValues(logger, "key", key)

	defer func() {
		logger.V(4).Info("Finished syncing resource quota", "key", key, "duration", time.Since(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	arqObj, exist, err := ctrl.arqInformer.GetIndexer().GetByKey(namespace + "/" + name)
	if !exist {
		logger.Info("Resource quota has been deleted", "key", key)
		return nil
	}
	if err != nil {
		logger.Error(err, "Unable to retrieve resource quota from store", "key", key)
		return err
	}
	arq := arqObj.(*v1alpha12.ApplicationAwareResourceQuota).DeepCopy()
	return ctrl.syncResourceQuota(arq)
}

// syncResourceQuota runs a complete sync of resource quota status across all known kinds
func (ctrl *ArqController) syncResourceQuota(arq *v1alpha12.ApplicationAwareResourceQuota) (err error) {
	// quota is dirty if any part of spec hard limits differs from the status hard limits
	statusLimitsDirty := !apiequality.Semantic.DeepEqual(arq.Spec.Hard, arq.Status.Hard)

	// dirty tracks if the usage status differs from the previous sync,
	// if so, we send a new usage with latest status
	// if this is our first sync, it will be dirty by default, since we need track usage
	dirty := statusLimitsDirty || arq.Status.Hard == nil || arq.Status.Used == nil

	used := v1.ResourceList{}
	if arq.Status.Used != nil {
		used = quota.Add(v1.ResourceList{}, arq.Status.Used)
	}
	hardLimits := quota.Add(v1.ResourceList{}, arq.Spec.Hard)

	var errs []error
	newUsage, err := quota.CalculateUsage(arq.Namespace, arq.Spec.Scopes, hardLimits, ctrl.evalRegistry, arq.Spec.ScopeSelector)
	if err != nil {
		// if err is non-nil, remember it to return, but continue updating status with any resources in newUsage
		errs = append(errs, err)
	}

	var rq *v1.ResourceQuota
	rqObj, exists, err := ctrl.rqInformer.GetIndexer().GetByKey(arq.Namespace + "/" + arq.Name + rq_controller.RQSuffix)
	if err != nil {
		errs = append(errs, err)
	} else if exists {
		rq = rqObj.(*v1.ResourceQuota).DeepCopy()
	}

	if exists && rq.Status.Hard != nil && arq.Status.Hard != nil {
		updateUsageFromResourceQuota(arq, rq, newUsage)
	}

	for key, value := range newUsage {
		used[key] = value
	}

	// ensure set of used values match those that have hard constraints
	hardResources := quota.ResourceNames(hardLimits)
	used = quota.Mask(used, hardResources)

	// Create a usage object that is based on the quota resource version that will handle updates
	// by default, we preserve the past usage observation, and set hard to the current spec
	usage := arq.DeepCopy()
	usage.Status = v1alpha12.ApplicationAwareResourceQuotaStatus{}
	usage.Status.Hard = hardLimits
	usage.Status.Used = used

	dirty = dirty || !quota.Equals(usage.Status.Used, arq.Status.Used)

	// there was a change observed by this controller that requires we update quota
	if dirty {
		_, err = ctrl.aaqCli.ApplicationAwareResourceQuotas(usage.Namespace).UpdateStatus(context.Background(), usage, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func updateUsageFromResourceQuota(arq *v1alpha12.ApplicationAwareResourceQuota, rq *v1.ResourceQuota, newUsage map[v1.ResourceName]resource.Quantity) {
	nonSchedulableResourcesHard := util.FilterNonScheduableResources(arq.Status.Hard)
	if quota.Equals(rq.Spec.Hard, nonSchedulableResourcesHard) && rq.Status.Used != nil {
		for key, value := range rq.Status.Used {
			newUsage[key] = value
		}
	}
}
