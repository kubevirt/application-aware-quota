package arq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/api/equality"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	k8sadmission "k8s.io/apiserver/pkg/admission"
	resourcequota2 "k8s.io/apiserver/pkg/admission/plugin/resourcequota"
	"k8s.io/apiserver/pkg/admission/plugin/resourcequota/apis/resourcequota"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	_ "kubevirt.io/api/core/v1"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/applications-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/clusterquotamapping"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/pkg/generated/aaq/listers/core/v1alpha1"
	"kubevirt.io/applications-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

type enqueueState string

const (
	Immediate                                    enqueueState = "Immediate"
	Forget                                       enqueueState = "Forget"
	BackOff                                      enqueueState = "BackOff"
	AaqjqcName                                                = "aaqjqc"
	ApplicationAwareResourceQuotaLockName                     = "application-aware-resource-quota-lock"
	ApplicationAwareClusterResourceQuotaLockName              = "application-aware-cluster-resource-quota-lock"
)

var locksNames = []string{
	ApplicationAwareResourceQuotaLockName,
	ApplicationAwareClusterResourceQuotaLockName,
}

type AaqGateController struct {
	podInformer         cache.SharedIndexInformer
	arqInformer         cache.SharedIndexInformer
	acrqInformer        cache.SharedIndexInformer
	aaqjqcInformer      cache.SharedIndexInformer
	nsQueue             workqueue.RateLimitingInterface
	aaqCli              client.AAQClient
	aaqEvaluator        *aaq_evaluator.AaqEvaluator
	clusterQuotaEnabled bool
	clusterQuotaLister  v1alpha1.ApplicationAwareClusterResourceQuotaLister
	namespaceLister     v12.NamespaceLister
	clusterQuotaMapper  clusterquotamapping.ClusterQuotaMapper
	stop                <-chan struct{}
}

func NewAaqGateController(aaqCli client.AAQClient,
	podInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
	aaqjqcInformer cache.SharedIndexInformer,
	acrqInformer cache.SharedIndexInformer,
	calcRegistry *aaq_evaluator.AaqCalculatorsRegistry,
	clusterQuotaLister v1alpha1.ApplicationAwareClusterResourceQuotaLister,
	namespaceLister v12.NamespaceLister,
	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper,
	clusterQuotaEnabled bool,
	stop <-chan struct{},
) *AaqGateController {
	ctrl := AaqGateController{
		aaqCli:              aaqCli,
		aaqjqcInformer:      aaqjqcInformer,
		podInformer:         podInformer,
		arqInformer:         arqInformer,
		acrqInformer:        acrqInformer,
		nsQueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ns-queue"),
		aaqEvaluator:        aaq_evaluator.NewAaqEvaluator(podInformer, calcRegistry, clock.RealClock{}),
		clusterQuotaLister:  clusterQuotaLister,
		namespaceLister:     namespaceLister,
		clusterQuotaMapper:  clusterQuotaMapper,
		clusterQuotaEnabled: clusterQuotaEnabled,
		stop:                stop,
	}

	_, err := ctrl.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addPod,
		UpdateFunc: ctrl.updatePod,
	})
	if err != nil {
		panic("something is wrong")

	}
	_, err = ctrl.arqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: ctrl.deleteArq,
		UpdateFunc: ctrl.updateArq,
		AddFunc:    ctrl.addArq,
	})
	if err != nil {
		panic("something is wrong")
	}
	if clusterQuotaEnabled {
		_, err = ctrl.acrqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			DeleteFunc: ctrl.deleteAcrq,
			UpdateFunc: ctrl.updateAcrq,
			AddFunc:    ctrl.addAcrq,
		})
		if err != nil {
			panic("something is wrong")
		}
	}
	_, err = ctrl.aaqjqcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: ctrl.deleteAaqjqc,
		UpdateFunc: ctrl.updateAaqjqc,
	})
	if err != nil {
		panic("something is wrong")
	}

	return &ctrl
}

// enqueueAll is called at the fullResyncPeriod interval to force a full recalculation of quota usage statistics
func (ctrl *AaqGateController) enqueueAll() {
	arqObjs := ctrl.arqInformer.GetIndexer().List()
	for _, arqObj := range arqObjs {
		arq := arqObj.(*v1alpha12.ApplicationAwareResourceQuota)
		ctrl.nsQueue.Add(arq.Namespace)
	}
	if ctrl.clusterQuotaEnabled {
		acrqObjs := ctrl.acrqInformer.GetIndexer().List()
		for _, acrqObj := range acrqObjs {
			acrq := acrqObj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
			namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
			for _, ns := range namespaces {
				ctrl.nsQueue.Add(ns)
			}
		}
	}
}

// When a ApplicationAwareResourceQuota is deleted, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) deleteArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) addArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) updateArq(old, cur interface{}) {
	arq := cur.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
	return
}

// When a ApplicationAwareResourceQuota is deleted, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) deleteAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	for _, ns := range namespaces {
		ctrl.nsQueue.Add(ns)
	}
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) addAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	for _, ns := range namespaces {
		ctrl.nsQueue.Add(ns)
	}
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) updateAcrq(old, cur interface{}) {
	acrq := cur.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	namespaces, _ := ctrl.clusterQuotaMapper.GetNamespacesFor(acrq.Name)
	for _, ns := range namespaces {
		ctrl.nsQueue.Add(ns)
	}
	return
}

// When a ApplicationAwareResourceQuotAaqjqc.Status.PodsInJobQueuea is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) updateAaqjqc(old, cur interface{}) {
	aaqjqc := cur.(*v1alpha12.AAQJobQueueConfig)
	ctrl.nsQueue.Add(aaqjqc.Namespace)
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) deleteAaqjqc(obj interface{}) {
	aaqjqc := obj.(*v1alpha12.AAQJobQueueConfig)
	ctrl.nsQueue.Add(aaqjqc.Namespace)
	return
}

func (ctrl *AaqGateController) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) == 1 &&
		pod.Spec.SchedulingGates[0].Name == util.AAQGate {
		ctrl.nsQueue.Add(pod.Namespace)
	}
}

func (ctrl *AaqGateController) updatePod(old, curr interface{}) {
	pod := curr.(*v1.Pod)

	if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) == 1 &&
		pod.Spec.SchedulingGates[0].Name == util.AAQGate {
		ctrl.nsQueue.Add(pod.Namespace)
	}
}

func (ctrl *AaqGateController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *AaqGateController) Execute() bool {
	key, quit := ctrl.nsQueue.Get()
	if quit {
		return false
	}
	defer ctrl.nsQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("AaqGateController: Error with key: %v err: %v", key, err))
	}
	switch enqueueState {
	case BackOff:
		ctrl.nsQueue.AddRateLimited(key)
	case Forget:
		ctrl.nsQueue.Forget(key)
	case Immediate:
		ctrl.nsQueue.Add(key)
	}

	return true
}

func (ctrl *AaqGateController) execute(ns string) (error, enqueueState) {
	aaqjqc, err := ctrl.createAndGetAaqjqc(ns)
	if err != nil {
		return err, Immediate
	}

	if aaqjqc.Status.ControllerLock != nil && !ctrl.aaqjqcProcessed(aaqjqc) { //check if controller already processed and unlocked
		err := ctrl.releasePods(aaqjqc.Status.PodsInJobQueue, ns)
		return err, Immediate //wait until for controllers to process changes
	}

	aaqjqc.Status.PodsInJobQueue = []string{}
	rqs, err := ctrl.getArtificialRqsForGateController(ns)
	if err != nil {
		return err, Immediate
	}
	podObjs, err := ctrl.podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return err, Immediate
	}
	for _, podObj := range podObjs {
		pod := podObj.(*v1.Pod)
		if pod.Spec.SchedulingGates != nil &&
			len(pod.Spec.SchedulingGates) == 1 &&
			pod.Spec.SchedulingGates[0].Name == util.AAQGate {
			podCopy := pod.DeepCopy()
			podCopy.Spec.SchedulingGates = []v1.PodSchedulingGate{}

			podToCreateAttr := k8sadmission.NewAttributesRecord(podCopy, nil,
				apiextensions.Kind("Pod").WithVersion("version"), podCopy.Namespace, podCopy.Name,
				v1alpha12.Resource("pods").WithVersion("version"), "", k8sadmission.Create,
				&metav1.CreateOptions{}, false, nil)

			currPodLimitedResource, err := getCurrLimitedResource(ctrl.aaqEvaluator, podCopy)
			if err != nil {
				return nil, Immediate
			}

			newRq, err := resourcequota2.CheckRequest(rqs, podToCreateAttr, ctrl.aaqEvaluator, []resourcequota.LimitedResource{currPodLimitedResource})
			if err == nil {
				rqs = newRq
				aaqjqc.Status.PodsInJobQueue = append(aaqjqc.Status.PodsInJobQueue, pod.Name)
			} //todo: create an event if we are blocked for a while
		}
	}

	if len(aaqjqc.Status.PodsInJobQueue) > 0 {
		aaqjqc.Status.ControllerLock = map[string]bool{}
		for _, lockName := range locksNames {
			if lockName == ApplicationAwareClusterResourceQuotaLockName && !ctrl.clusterQuotaEnabled {
				continue
			}
			aaqjqc.Status.ControllerLock[lockName] = true // after releasing we should wait for controllers to process and unlock
		}
		aaqjqc, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).UpdateStatus(context.Background(), aaqjqc, metav1.UpdateOptions{})
		if err != nil {
			return err, Immediate
		}
	}

	err = ctrl.releasePods(aaqjqc.Status.PodsInJobQueue, ns)
	if err != nil {
		return err, Immediate
	}
	return nil, Forget
}

func (ctrl *AaqGateController) releasePods(podsToRelease []string, ns string) error {
	for _, podName := range podsToRelease {
		obj, exists, err := ctrl.podInformer.GetIndexer().GetByKey(ns + "/" + podName)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		pod := obj.(*v1.Pod).DeepCopy()
		if pod.Spec.SchedulingGates != nil && len(pod.Spec.SchedulingGates) == 1 && pod.Spec.SchedulingGates[0].Name == util.AAQGate {
			pod.Spec.SchedulingGates = []v1.PodSchedulingGate{}
			_, err = ctrl.aaqCli.CoreV1().Pods(ns).Update(context.Background(), pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}
	return nil

}

func (ctrl *AaqGateController) createAndGetAaqjqc(ns string) (*v1alpha12.AAQJobQueueConfig, error) {
	var aaqjqc *v1alpha12.AAQJobQueueConfig
	aaqjqcObj, exists, err := ctrl.aaqjqcInformer.GetIndexer().GetByKey(ns + "/" + AaqjqcName)
	if err != nil {
		return nil, err
	} else if !exists {
		aaqjqc = &v1alpha12.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName}, Spec: v1alpha12.AAQJobQueueConfigSpec{}}
		aaqjqc, err = ctrl.aaqCli.AAQJobQueueConfigs(ns).Create(context.Background(),
			aaqjqc,
			metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	} else {
		aaqjqc = aaqjqcObj.(*v1alpha12.AAQJobQueueConfig).DeepCopy()
	}

	return aaqjqc, nil
}

func (ctrl *AaqGateController) getArtificialRqsForGateController(ns string) ([]v1.ResourceQuota, error) {
	arqsObjs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return nil, err
	}
	var rqs []v1.ResourceQuota
	for _, arqObj := range arqsObjs {
		arq := arqObj.(*v1alpha12.ApplicationAwareResourceQuota)
		rq := v1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: arq.Name, Namespace: ns},
			Spec:   v1.ResourceQuotaSpec{Hard: arq.Spec.Hard},
			Status: v1.ResourceQuotaStatus{Hard: arq.Status.Hard, Used: arq.Status.Used},
		}
		rqs = append(rqs, rq)
	}
	if !ctrl.clusterQuotaEnabled {
		return rqs, nil
	}

	clusterQuotaNames, err := ctrl.waitForReadyClusterQuotaNames(ns)
	if err != nil {
		return nil, err
	}

	for _, clusterQuotaName := range clusterQuotaNames {
		clusterQuota, err := ctrl.clusterQuotaLister.Get(clusterQuotaName)
		if kapierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}

		// now convert to a ResourceQuota
		convertedQuota := v1.ResourceQuota{}
		convertedQuota.ObjectMeta = clusterQuota.ObjectMeta
		convertedQuota.Namespace = ns
		convertedQuota.Spec = clusterQuota.Spec.Quota
		convertedQuota.Status = clusterQuota.Status.Total
		rqs = append(rqs, convertedQuota)

	}

	return rqs, nil
}

func (ctrl *AaqGateController) waitForReadyClusterQuotaNames(namespaceName string) ([]string, error) {
	var clusterQuotaNames []string
	// wait for a valid mapping cache.  The overall response can be delayed for up to 10 seconds.
	err := utilwait.PollImmediate(100*time.Millisecond, 8*time.Second, func() (done bool, err error) {
		var namespaceSelectionFields clusterquotamapping.SelectionFields
		clusterQuotaNames, namespaceSelectionFields = ctrl.clusterQuotaMapper.GetClusterQuotasFor(namespaceName)
		namespace, err := ctrl.namespaceLister.Get(namespaceName)
		// if we can't find the namespace yet, just wait for the cache to update.  Requests to non-existent namespaces
		// may hang, but those people are doing something wrong and namespacelifecycle should reject them.
		if kapierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if equality.Semantic.DeepEqual(namespaceSelectionFields, clusterquotamapping.GetSelectionFields(namespace)) {
			return true, nil
		}
		return false, nil
	})
	return clusterQuotaNames, err
}

func (ctrl *AaqGateController) Run(ctx context.Context, threadiness int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting Aaq Gate controller")
	defer klog.Info("Shutting down Aaq Gate controller")
	defer ctrl.nsQueue.ShutDown()

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}

	<-ctrl.stop

}

func getCurrLimitedResource(podEvaluator *aaq_evaluator.AaqEvaluator, podToCreate *v1.Pod) (resourcequota.LimitedResource, error) {
	launcherLimitedResource := resourcequota.LimitedResource{
		Resource:      "pods",
		MatchContains: []string{},
	}
	usage, err := podEvaluator.Usage(podToCreate)
	if err != nil {
		return launcherLimitedResource, err
	}
	for k, _ := range usage {
		launcherLimitedResource.MatchContains = append(launcherLimitedResource.MatchContains, string(k))
	}
	return launcherLimitedResource, nil
}

func (ctrl *AaqGateController) aaqjqcProcessed(aaqjqc *v1alpha12.AAQJobQueueConfig) bool {
	for rqName, locked := range aaqjqc.Status.ControllerLock {
		if rqName == ApplicationAwareClusterResourceQuotaLockName && !ctrl.clusterQuotaEnabled {
			continue
		}
		if locked {
			return false
		}
	}
	return true
}
