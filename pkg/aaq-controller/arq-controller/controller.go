package arq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	quota "k8s.io/apiserver/pkg/quota/v1"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	v14 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	pkgcontroller "k8s.io/kubernetes/pkg/controller"
	quotacontroller "k8s.io/kubernetes/pkg/controller/resourcequota"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	built_in_usage_calculators "kubevirt.io/applications-aware-quota/pkg/aaq-controller/built-in-usage-calculators"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/utils"
	v1alpha13 "kubevirt.io/applications-aware-quota/pkg/generated/clientset/versioned/typed/core/v1alpha1"
	v1alpha15 "kubevirt.io/applications-aware-quota/pkg/generated/informers/externalversions/core/v1alpha1"
	v1alpha14 "kubevirt.io/applications-aware-quota/pkg/generated/listers/core/v1alpha1"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/controller"
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

type ArqController struct {
	InformersStarted <-chan struct{}
	aaqNs            string
	podInformer      cache.SharedIndexInformer
	aaqjqcInformer   cache.SharedIndexInformer
	virtCli          kubecli.KubevirtClient
	aaqCli           v1alpha13.AaqV1alpha1Client
	// A lister/getter of resource quota objects
	arqLister v1alpha14.ApplicationsResourceQuotaLister
	// A list of functions that return true when their caches have synced
	informerSyncedFuncs []cache.InformerSynced
	arqQueue            workqueue.RateLimitingInterface
	missingUsageQueue   workqueue.RateLimitingInterface
	enqueueAllQueue     workqueue.RateLimitingInterface
	// Controls full recalculation of quota usage
	resyncPeriod pkgcontroller.ResyncPeriodFunc

	// knows how to calculate usage
	registry quota.Registry
	// knows how to monitor all the resources tracked by quota and trigger replenishment
	quotaMonitor *QuotaMonitor
	// controls the workers that process quotas
	// this lock is acquired to control write access to the monitors and ensures that all
	// monitors are synced before the controller can process quotas.
	workerLock sync.RWMutex

	recorder     record.EventRecorder
	podEvaluator v12.Evaluator
	syncHandler  func(ctx context.Context, key string) error
}

func NewArqController(virtCli kubecli.KubevirtClient,
	aaqNs string, aaqCli v1alpha13.AaqV1alpha1Client,
	podInformer cache.SharedIndexInformer,
	arqInformer v1alpha15.ApplicationsResourceQuotaInformer,
	aaqjqcInformer cache.SharedIndexInformer,
	stop <-chan struct{},
	InformersStarted <-chan struct{},
) *ArqController {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: virtCli.CoreV1().Events(v1.NamespaceAll)})
	//todo: make this generic for now we will try only launcher calculator
	InformerFactory := informers.NewSharedInformerFactoryWithOptions(virtCli, 1*time.Hour)
	calcRegistry := aaq_evaluator.NewAaqCalculatorsRegistry(10, clock.RealClock{}).AddCalculator(built_in_usage_calculators.NewVirtLauncherCalculator(stop))
	listerFuncForResource := generic.ListerFuncForResourceFunc(InformerFactory.ForResource)
	discoveryFunction := discovery.NewDiscoveryClient(virtCli.RestClient()).ServerPreferredNamespacedResources

	ctrl := &ArqController{
		InformersStarted:    InformersStarted,
		aaqNs:               aaqNs,
		virtCli:             virtCli,
		aaqCli:              aaqCli,
		arqLister:           arqInformer.Lister(),
		informerSyncedFuncs: []cache.InformerSynced{arqInformer.Informer().HasSynced},
		podInformer:         podInformer,
		aaqjqcInformer:      aaqjqcInformer,
		arqQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_primary"),
		missingUsageQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_priority"),
		enqueueAllQueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_enqueue_all"),
		resyncPeriod:        pkgcontroller.StaticResyncPeriodFunc(metav1.Duration{Duration: 5 * time.Minute}.Duration),
		recorder:            eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: utils.ControllerPodName}),
		podEvaluator:        core.NewPodEvaluator(nil, clock.RealClock{}),
		registry:            generic.NewRegistry([]v12.Evaluator{aaq_evaluator.NewAaqEvaluator(listerFuncForResource, calcRegistry, clock.RealClock{})}),
	}
	ctrl.syncHandler = ctrl.syncResourceQuotaFromKey

	logger := klog.FromContext(context.Background())

	arqInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				ctrl.addQuota(logger, obj)
			},
			UpdateFunc: func(old, cur interface{}) {
				// We are only interested in observing updates to quota.spec to drive updates to quota.status.
				// We ignore all updates to quota.Status because they are all driven by this controller.
				// IMPORTANT:
				// We do not use this function to queue up a full quota recalculation.  To do so, would require
				// us to enqueue all quota.Status updates, and since quota.Status updates involve additional queries
				// that cannot be backed by a cache and result in a full query of a namespace's content, we do not
				// want to pay the price on spurious status updates.  As a result, we have a separate routine that is
				// responsible for enqueue of all resource quotas when doing a full resync (enqueueAll)
				oldArq := old.(*v1alpha12.ApplicationsResourceQuota)
				curArq := cur.(*v1alpha12.ApplicationsResourceQuota)
				if quota.Equals(oldArq.Spec.Hard, curArq.Spec.Hard) {
					return
				}
				ctrl.addQuota(logger, curArq)
			},
			// This will enter the sync loop and no-op, because the controller has been deleted from the store.
			// Note that deleting a controller immediately after scaling it to 0 will not work. The recommended
			// way of achieving this is by performing a `stop` operation on the controller.
			DeleteFunc: func(obj interface{}) {
				ctrl.enqueueArq(logger, obj)
			},
		},
		ctrl.resyncPeriod(),
	)
	_, err := ctrl.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updatePod,
	})
	if err != nil {
		panic("something is wrong")
	}
	_, err = ctrl.aaqjqcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateAaqjqc,
	})
	if err != nil {
		panic("something is wrong")
	}
	qm := &QuotaMonitor{
		informersStarted:  InformersStarted,
		informerFactory:   InformerFactory,
		ignoredResources:  make(map[schema.GroupResource]struct{}),
		resourceChanges:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "resource_quota_controller_resource_changes"),
		resyncPeriod:      func() time.Duration { return metav1.Duration{Duration: 5 * time.Minute}.Duration },
		replenishmentFunc: ctrl.replenishQuota,
		registry:          ctrl.registry,
		updateFilter:      DefaultUpdateFilter(),
	}
	ctrl.quotaMonitor = qm

	// do initial quota monitor setup.  If we have a discovery failure here, it's ok. We'll discover more resources when a later sync happens.
	resources, err := quotacontroller.GetQuotableResources(discoveryFunction)
	if discovery.IsGroupDiscoveryFailedError(err) {
		utilruntime.HandleError(fmt.Errorf("initial discovery check failure, continuing and counting on future sync update: %v", err))
	} else if err != nil {
		panic("NewArqController: something is wrong")
	}

	if err = qm.SyncMonitors(context.Background(), resources); err != nil {
		utilruntime.HandleError(fmt.Errorf("initial monitor sync has error: %v", err))
	}

	// only start quota once all informers synced
	ctrl.informerSyncedFuncs = append(ctrl.informerSyncedFuncs, func() bool {
		return qm.IsSynced(context.Background())
	})

	return ctrl
}

// When a ApplicationsResourceQuota is added, enqueue fake pod to namespace to trigger the reconciliation loop
func (ctrl *ArqController) addArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.addAllArqsInNamespace(arq.Namespace, &ctrl.arqQueue)
	return
}

// When a ApplicationsResourceQuotaaqjqc.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *ArqController) updateAaqjqc(old, cur interface{}) {
	aaqjqc := cur.(*v1alpha12.AAQJobQueueConfig)
	if aaqjqc.Status.PodsInJobQueue != nil && len(aaqjqc.Status.PodsInJobQueue) > 0 {
		ctrl.addAllArqsInNamespace(aaqjqc.Namespace, &ctrl.enqueueAllQueue)
	}
	return
}

func (ctrl *ArqController) addAllArqsInNamespace(ns string, queue *workqueue.RateLimitingInterface) {
	arq, err := ctrl.arqLister.List(labels.Everything())
	if err != nil {
		log.Log.Infof("AaqGateController: Error failed to list pod from podInformer")
	}
	for _, arq := range arq {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
		if err != nil {
			return
		}
		(*queue).Add(key)
	}
}

// enqueueAll is called at the fullResyncPeriod interval to force a full recalculation of quota usage statistics
func (ctrl *ArqController) enqueueAll(ctx context.Context) {
	logger := klog.FromContext(ctx)
	defer logger.V(4).Info("Arq controller queued all resource quota for full calculation of usage")
	arqs, err := ctrl.arqLister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to enqueue all - error listing resource quotas: %v", err))
		return
	}
	for i := range arqs {
		key, err := controller.KeyFunc(arqs[i])
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", arqs[i], err))
			continue
		}
		ctrl.arqQueue.Add(key)
	}
}

func (ctrl *ArqController) updatePod(old, curr interface{}) {
	currPod := curr.(*v1.Pod)
	oldPod := old.(*v1.Pod)
	if !isTerminalState(currPod) && oldPod.Spec.SchedulingGates != nil &&
		len(oldPod.Spec.SchedulingGates) == 1 &&
		oldPod.Spec.SchedulingGates[0].Name == "ApplicationsAwareQuotaGate" &&
		(currPod.Spec.SchedulingGates == nil || len(currPod.Spec.SchedulingGates) == 0) {
		ctrl.addAllArqsInNamespace(currPod.Namespace, &ctrl.enqueueAllQueue)
	}
}

func isTerminalState(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

func (ctrl *ArqController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *ArqController) Execute() bool {
	key, quit := ctrl.arqQueue.Get()
	if quit {
		return false
	}
	defer ctrl.arqQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("ArqController: Error with key: %v", key))
	}
	switch enqueueState {
	case BackOff:
		ctrl.arqQueue.AddRateLimited(key)
	case Forget:
		ctrl.arqQueue.Forget(key)
	case Immediate:
		ctrl.arqQueue.Add(key)
	}

	return true
}

func (ctrl *ArqController) execute(key string) (error, enqueueState) {
	_, _, err := ctrl.podInformer.GetStore().GetByKey(key)
	if err != nil {
		return err, BackOff
	}
	//todo: implement
	_, _, _ = parseKey(key)
	return nil, Forget
}

func (ctrl *ArqController) Run(ctx context.Context, workers int, stop <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer ctrl.arqQueue.ShutDown()
	defer ctrl.missingUsageQueue.ShutDown()

	logger := klog.FromContext(ctx)

	if ctrl.quotaMonitor != nil {
		go ctrl.quotaMonitor.Run(ctx)
	}

	if !cache.WaitForNamedCacheSync("resource quota", ctx.Done(), ctrl.informerSyncedFuncs...) {
		return
	}

	// the workers that chug through the quota calculation backlog
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, ctrl.worker(ctrl.arqQueue), time.Second)
		go wait.UntilWithContext(ctx, ctrl.worker(ctrl.missingUsageQueue), time.Second)
	}
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.runWorker, time.Second, stop)
	}
	// the timer for how often we do a full recalculation across all quotas
	if ctrl.resyncPeriod() > 0 {
		go wait.UntilWithContext(ctx, ctrl.enqueueAll, ctrl.resyncPeriod())
	} else {
		logger.Info("periodic quota controller resync disabled")
	}
	<-ctx.Done()

}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
func (ctrl *ArqController) worker(queue workqueue.RateLimitingInterface) func(context.Context) {
	workFunc := func(ctx context.Context) bool {
		key, quit := queue.Get()
		if quit {
			return true
		}
		defer queue.Done(key)

		ctrl.workerLock.RLock()
		defer ctrl.workerLock.RUnlock()

		logger := klog.FromContext(ctx)
		logger = klog.LoggerWithValues(logger, "queueKey", key)
		ctx = klog.NewContext(ctx, logger)

		err := ctrl.syncHandler(ctx, key.(string))
		if err == nil {
			queue.Forget(key)
			return false
		}

		utilruntime.HandleError(err)
		queue.AddRateLimited(key)

		return false
	}

	return func(ctx context.Context) {
		for {
			if quit := workFunc(ctx); quit {
				klog.FromContext(ctx).Info("resource quota controller worker shutting down")
				return
			}
		}
	}
}

func (ctrl *ArqController) addQuota(logger klog.Logger, obj interface{}) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		logger.Error(err, "Couldn't get key", "object", obj)
		return
	}

	arq := obj.(*v1alpha12.ApplicationsResourceQuota)

	// if we declared an intent that is not yet captured in status (prioritize it)
	if !apiequality.Semantic.DeepEqual(arq.Spec.Hard, arq.Status.Hard) {
		ctrl.missingUsageQueue.Add(key)
		return
	}

	// if we declared a constraint that has no usage (which this controller can calculate, prioritize it)
	for constraint := range arq.Status.Hard {
		if _, usageFound := arq.Status.Used[constraint]; !usageFound {
			matchedResources := []v1.ResourceName{constraint}
			for _, evaluator := range ctrl.registry.List() {
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

// replenishQuota is a replenishment function invoked by a controller to notify that a quota should be recalculated
func (ctrl *ArqController) replenishQuota(ctx context.Context, groupResource schema.GroupResource, namespace string) {
	// check if the quota controller can evaluate this groupResource, if not, ignore it altogether...
	evaluator := ctrl.registry.Get(groupResource)
	if evaluator == nil {
		return
	}

	// check if this namespace even has a quota...
	arqs, err := ctrl.arqLister.ApplicationsResourceQuotas(namespace).List(labels.Everything())
	if errors.IsNotFound(err) {
		utilruntime.HandleError(fmt.Errorf("quota controller could not find ApplicationsResourceQuotas associated with namespace: %s, could take up to %v before a quota replenishes", namespace, ctrl.resyncPeriod()))
		return
	}
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error checking to see if namespace %s has any ApplicationsResourceQuotas associated with it: %v", namespace, err))
		return
	}
	if len(arqs) == 0 {
		return
	}

	logger := klog.FromContext(ctx)

	// only queue those quotas that are tracking a resource associated with this kind.
	for i := range arqs {
		arq := arqs[i]
		resourceQuotaResources := quota.ResourceNames(arq.Status.Hard)
		if intersection := evaluator.MatchingResources(resourceQuotaResources); len(intersection) > 0 {
			ctrl.enqueueArq(logger, arq)
		}
	}
}

// obj could be an *v1.ResourceQuota, or a DeletionFinalStateUnknown marker item.
func (ctrl *ArqController) enqueueArq(logger klog.Logger, obj interface{}) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		logger.Error(err, "Couldn't get key", "object", obj)
		return
	}
	ctrl.arqQueue.Add(key)
}

// syncResourceQuotaFromKey syncs a quota key
func (ctrl *ArqController) syncResourceQuotaFromKey(ctx context.Context, key string) (err error) {
	startTime := time.Now()

	logger := klog.FromContext(ctx)
	logger = klog.LoggerWithValues(logger, "key", key)

	defer func() {
		logger.V(4).Info("Finished syncing resource quota", "key", key, "duration", time.Since(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	resourceQuota, err := ctrl.arqLister.ApplicationsResourceQuotas(namespace).Get(name)
	if errors.IsNotFound(err) {
		logger.Info("Resource quota has been deleted", "key", key)
		return nil
	}
	if err != nil {
		logger.Error(err, "Unable to retrieve resource quota from store", "key", key)
		return err
	}
	return ctrl.syncResourceQuota(ctx, resourceQuota)
}

// syncResourceQuota runs a complete sync of resource quota status across all known kinds
func (ctrl *ArqController) syncResourceQuota(ctx context.Context, appResourceQuota *v1alpha12.ApplicationsResourceQuota) (err error) {
	// quota is dirty if any part of spec hard limits differs from the status hard limits
	statusLimitsDirty := !apiequality.Semantic.DeepEqual(appResourceQuota.Spec.Hard, appResourceQuota.Status.Hard)

	// dirty tracks if the usage status differs from the previous sync,
	// if so, we send a new usage with latest status
	// if this is our first sync, it will be dirty by default, since we need track usage
	dirty := statusLimitsDirty || appResourceQuota.Status.Hard == nil || appResourceQuota.Status.Used == nil

	used := v1.ResourceList{}
	if appResourceQuota.Status.Used != nil {
		used = quota.Add(v1.ResourceList{}, appResourceQuota.Status.Used)
	}
	hardLimits := quota.Add(v1.ResourceList{}, appResourceQuota.Spec.Hard)

	var errs []error

	newUsage, err := quota.CalculateUsage(appResourceQuota.Namespace, appResourceQuota.Spec.Scopes, hardLimits, ctrl.registry, appResourceQuota.Spec.ScopeSelector)
	if err != nil {
		// if err is non-nil, remember it to return, but continue updating status with any resources in newUsage
		errs = append(errs, err)
	}
	for key, value := range newUsage {
		used[key] = value
	}

	// ensure set of used values match those that have hard constraints
	hardResources := quota.ResourceNames(hardLimits)
	used = quota.Mask(used, hardResources)

	// Create a usage object that is based on the quota resource version that will handle updates
	// by default, we preserve the past usage observation, and set hard to the current spec
	usage := appResourceQuota.DeepCopy()
	usage.Status = v1.ResourceQuotaStatus{
		Hard: hardLimits,
		Used: used,
	}

	dirty = dirty || !quota.Equals(usage.Status.Used, appResourceQuota.Status.Used)

	// there was a change observed by this controller that requires we update quota
	if dirty {
		_, err = ctrl.aaqCli.ApplicationsResourceQuotas(usage.Namespace).UpdateStatus(ctx, usage, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func parseKey(key string) (string, string, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("error: input key: %v is not in the expected format", key)
	}
	migartionNS := parts[0]
	migrationName := parts[1]
	return migartionNS, migrationName, nil
}

// function from kubernetes
// DefaultUpdateFilter returns the default update filter for resource update events for consideration for quota.
func DefaultUpdateFilter() func(resource schema.GroupVersionResource, oldObj, newObj interface{}) bool {
	return func(resource schema.GroupVersionResource, oldObj, newObj interface{}) bool {
		switch resource.GroupResource() {
		case schema.GroupResource{Resource: "pods"}:
			oldPod := oldObj.(*v1.Pod)
			newPod := newObj.(*v1.Pod)
			return core.QuotaV1Pod(oldPod, clock.RealClock{}) && !core.QuotaV1Pod(newPod, clock.RealClock{})
		case schema.GroupResource{Resource: "services"}:
			oldService := oldObj.(*v1.Service)
			newService := newObj.(*v1.Service)
			return core.GetQuotaServiceType(oldService) != core.GetQuotaServiceType(newService)
		case schema.GroupResource{Resource: "persistentvolumeclaims"}:
			oldPVC := oldObj.(*v1.PersistentVolumeClaim)
			newPVC := newObj.(*v1.PersistentVolumeClaim)
			return core.RequiresQuotaReplenish(newPVC, oldPVC)
		}

		return false
	}
}
