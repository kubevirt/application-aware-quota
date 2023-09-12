package arq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"k8s.io/utils/clock"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	arq_controller "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-gate-controller"
	built_in_usage_calculators "kubevirt.io/applications-aware-quota/pkg/aaq-controller/built-in-usage-calculators"
	aaq_controller "kubevirt.io/applications-aware-quota/pkg/aaq-controller/quota-utils"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/utils"
	"kubevirt.io/applications-aware-quota/pkg/client"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/controller"
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
	aaqCli           client.AAQClient
	// A lister/getter of resource quota objects
	arqInformer cache.SharedIndexInformer
	// A list of functions that return true when their caches have synced
	informerSyncedFuncs []cache.InformerSynced
	arqQueue            workqueue.RateLimitingInterface
	missingUsageQueue   workqueue.RateLimitingInterface
	enqueueAllQueue     workqueue.RateLimitingInterface
	// Controls full recalculation of quota usage
	resyncPeriod pkgcontroller.ResyncPeriodFunc

	// knows how to calculate usage
	eval *aaq_evaluator.AaqEvaluator

	// controls the workers that process quotas
	// this lock is acquired to control write access to the monitors and ensures that all
	// monitors are synced before the controller can process quotas.
	workerLock sync.RWMutex

	recorder     record.EventRecorder
	aaqEvaluator v12.Evaluator
	syncHandler  func(key string) error
}

func NewArqController(clientSet client.AAQClient,
	podInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
	aaqjqcInformer cache.SharedIndexInformer,
) *ArqController {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: clientSet.CoreV1().Events(v1.NamespaceAll)})
	//todo: make this generic for now we will try only launcher calculator
	InformerFactory := informers.NewSharedInformerFactoryWithOptions(clientSet, 1*time.Hour)
	calcRegistry := aaq_evaluator.NewAaqCalculatorsRegistry(10, clock.RealClock{}).AddCalculator(built_in_usage_calculators.NewVirtLauncherCalculator())
	listerFuncForResource := generic.ListerFuncForResourceFunc(InformerFactory.ForResource)
	discoveryFunction := discovery.NewDiscoveryClient(clientSet.RestClient()).ServerPreferredNamespacedResources

	ctrl := &ArqController{
		aaqCli:              clientSet,
		arqInformer:         arqInformer,
		informerSyncedFuncs: []cache.InformerSynced{arqInformer.HasSynced},
		podInformer:         podInformer,
		aaqjqcInformer:      aaqjqcInformer,
		arqQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_primary"),
		missingUsageQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_priority"),
		enqueueAllQueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq_enqueue_all"),
		resyncPeriod:        pkgcontroller.StaticResyncPeriodFunc(metav1.Duration{Duration: 5 * time.Minute}.Duration),
		recorder:            eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: utils.ControllerPodName}),
		eval:                aaq_evaluator.NewAaqEvaluator(listerFuncForResource, podInformer, calcRegistry, clock.RealClock{}),
	}
	ctrl.syncHandler = ctrl.syncResourceQuotaFromKey

	logger := klog.FromContext(context.Background())

	arqInformer.AddEventHandlerWithResyncPeriod(
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
		AddFunc:    ctrl.AddPod,
		DeleteFunc: ctrl.DeletePod,
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

	// do initial quota monitor setup.  If we have a discovery failure here, it's ok. We'll discover more resources when a later sync happens.
	resources, err := quotacontroller.GetQuotableResources(discoveryFunction)
	podResourceMap := make(map[schema.GroupVersionResource]struct{})
	for resource := range resources {
		if resource.Resource == "pods" {
			podResourceMap[resource] = struct{}{}
		}
	}

	if discovery.IsGroupDiscoveryFailedError(err) {
		utilruntime.HandleError(fmt.Errorf("initial discovery check failure, continuing and counting on future sync update: %v", err))
	} else if err != nil {
		panic("NewArqController: something is wrong")
	}

	ctrl.informerSyncedFuncs = ctrl.informerSyncedFuncs

	return ctrl
}

// When a ApplicationsResourceQuotaaqjqc.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *ArqController) updateAaqjqc(old, cur interface{}) {
	aaqjqc := cur.(*v1alpha12.AAQJobQueueConfig)
	if aaqjqc.Status.PodsInJobQueue != nil && len(aaqjqc.Status.PodsInJobQueue) > 0 {
		ctrl.addAllArqsInNamespace(aaqjqc.Namespace)
	}
	return
}

func (ctrl *ArqController) addAllArqsInNamespace(ns string) {
	arqObjs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		log.Log.Infof("AaqGateController: Error failed to list pod from podInformer")
	}
	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationsResourceQuota)
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
		if err != nil {
			return
		}
		ctrl.enqueueAllQueue.Add(key)
	}
}

// enqueueAll is called at the fullResyncPeriod interval to force a full recalculation of quota usage statistics
func (ctrl *ArqController) enqueueAll() {
	arqObjs := ctrl.arqInformer.GetIndexer().List()
	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationsResourceQuota)
		key, err := controller.KeyFunc(arqObj.(*v1alpha12.ApplicationsResourceQuota))
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", arq, err))
			continue
		}
		ctrl.arqQueue.Add(key)
	}
}

func (ctrl *ArqController) updatePod(old, curr interface{}) {
	currPod := curr.(*v1.Pod)
	if len(currPod.Spec.SchedulingGates) == 0 {
		ctrl.addAllArqsInNamespace(currPod.Namespace)
	}
}
func (ctrl *ArqController) AddPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if len(pod.Spec.SchedulingGates) == 0 {
		ctrl.addAllArqsInNamespace(pod.Namespace)
	}
}
func (ctrl *ArqController) DeletePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if len(pod.Spec.SchedulingGates) == 0 {
		ctrl.addAllArqsInNamespace(pod.Namespace)
	}
}

func (ctrl *ArqController) runGateWatcherWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *ArqController) Execute() bool {
	key, quit := ctrl.enqueueAllQueue.Get()
	if quit {
		return false
	}
	defer ctrl.enqueueAllQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("ArqController: Error with key: %v", key))
	}
	switch enqueueState {
	case BackOff:
		ctrl.enqueueAllQueue.AddRateLimited(key)
	case Forget:
		ctrl.enqueueAllQueue.Forget(key)
	case Immediate:
		ctrl.enqueueAllQueue.Add(key)
	}

	return true
}

func (ctrl *ArqController) execute(key string) (error, enqueueState) {
	ns, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err, BackOff
	}
	aaqjqc, err := ctrl.aaqCli.AAQJobQueueConfigs(ns).Get(context.Background(), arq_controller.AaqjqcName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		aaqjqc, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).Create(context.Background(),
			&v1alpha12.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: arq_controller.AaqjqcName}, Spec: v1alpha12.AAQJobQueueConfigSpec{}},
			metav1.CreateOptions{})
		if err != nil {
			return err, Immediate
		}
	} else if err != nil {
		return err, Immediate
	}

	arqObjs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return err, Immediate
	}

	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationsResourceQuota)
		err := ctrl.syncResourceQuota(arq)
		if err != nil {
			return err, Immediate
		}
	}

	for _, podName := range aaqjqc.Status.PodsInJobQueue {
		obj, exists, err := ctrl.podInformer.GetIndexer().GetByKey(ns + "/" + podName)
		if err != nil {
			return err, Immediate
		}
		if !exists {
			continue
		}
		pod := obj.(*v1.Pod)
		if len(pod.Spec.SchedulingGates) > 0 {
			return nil, Immediate
		}
	}
	aaqjqc.Status.PodsInJobQueue = []string{}
	_, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).UpdateStatus(context.Background(), aaqjqc, metav1.UpdateOptions{})
	if err != nil {
		return err, Immediate
	}
	return nil, Forget
}

func (ctrl *ArqController) Run(ctx context.Context, workers int, stop <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer ctrl.arqQueue.ShutDown()
	defer ctrl.missingUsageQueue.ShutDown()

	logger := klog.FromContext(ctx)
	if !cache.WaitForNamedCacheSync("resource quota", ctx.Done(), ctrl.informerSyncedFuncs...) {
		return
	}

	// the workers that chug through the quota calculation backlog
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.worker(ctrl.arqQueue), time.Second, stop)
		go wait.Until(ctrl.worker(ctrl.missingUsageQueue), time.Second, stop)
		go wait.Until(ctrl.runGateWatcherWorker, time.Second, stop)
	}
	// the timer for how often we do a full recalculation across all quotas
	if ctrl.resyncPeriod() > 0 {
		go wait.Until(ctrl.enqueueAll, ctrl.resyncPeriod(), stop)
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

		ctrl.workerLock.RLock()
		defer ctrl.workerLock.RUnlock()

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
			if intersection := ctrl.eval.MatchingResources(matchedResources); len(intersection) > 0 {
				ctrl.missingUsageQueue.Add(key)
				return
			}
		}
	}

	// no special priority, go in normal recalc queue
	ctrl.arqQueue.Add(key)
}

// replenishQuota is a replenishment function invoked by a controller to notify that a quota should be recalculated
func (ctrl *ArqController) replenishQuota(ctx context.Context, groupResource schema.GroupResource, namespace string) {

	// check if this namespace even has a quota...
	arqObjs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, namespace)
	if errors.IsNotFound(err) {
		utilruntime.HandleError(fmt.Errorf("quota controller could not find ApplicationsResourceQuotas associated with namespace: %s, could take up to %v before a quota replenishes", namespace, ctrl.resyncPeriod()))
		return
	}
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error checking to see if namespace %s has any ApplicationsResourceQuotas associated with it: %v", namespace, err))
		return
	}
	if len(arqObjs) == 0 {
		return
	}

	logger := klog.FromContext(ctx)

	// only queue those quotas that are tracking a resource associated with this kind.
	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationsResourceQuota)
		resourceQuotaResources := quota.ResourceNames(arq.Status.Hard)
		if intersection := ctrl.eval.MatchingResources(resourceQuotaResources); len(intersection) > 0 {
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
	arq := arqObj.(*v1alpha12.ApplicationsResourceQuota)
	return ctrl.syncResourceQuota(arq)
}

// syncResourceQuota runs a complete sync of resource quota status across all known kinds
func (ctrl *ArqController) syncResourceQuota(appResourceQuota *v1alpha12.ApplicationsResourceQuota) (err error) {
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
	newUsage, err := aaq_controller.CalculateUsage(appResourceQuota.Namespace, appResourceQuota.Spec.Scopes, hardLimits, ctrl.eval, appResourceQuota.Spec.ScopeSelector)
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
		_, err = ctrl.aaqCli.ApplicationsResourceQuotas(usage.Namespace).UpdateStatus(context.Background(), usage, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}
