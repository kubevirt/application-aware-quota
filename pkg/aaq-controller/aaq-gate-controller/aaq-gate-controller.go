package arq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	v14 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	_ "kubevirt.io/api/core/v1"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/applications-aware-quota/pkg/aaq-controller/namespace-lock-utils"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/utils"
	v1alpha13 "kubevirt.io/applications-aware-quota/pkg/generated/clientset/versioned/typed/core/v1alpha1"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"
	"kubevirt.io/containerized-data-importer/pkg/util/cert/fetcher"
	"kubevirt.io/kubevirt/pkg/virt-controller/services"
	"strings"
	"sync"
	"time"
)

type enqueueState string

const (
	Immediate  enqueueState = "Immediate"
	Forget     enqueueState = "Forget"
	BackOff    enqueueState = "BackOff"
	aaqjqcName string       = "aaqjqc"
)

type AaqGateController struct {
	aaqNs               string
	stop                <-chan struct{}
	nsCache             *namespace_lock_utils.NamespaceCache
	internalLock        *sync.Mutex
	nsLockMap           *namespace_lock_utils.NamespaceLockMap
	podInformer         cache.SharedIndexInformer
	migrationInformer   cache.SharedIndexInformer
	arqInformer         cache.SharedIndexInformer
	vmiInformer         cache.SharedIndexInformer
	kubeVirtInformer    cache.SharedIndexInformer
	crdInformer         cache.SharedIndexInformer
	pvcInformer         cache.SharedIndexInformer
	podQueue            workqueue.RateLimitingInterface
	virtCli             kubecli.KubevirtClient
	aaqCli              v1alpha13.AaqV1alpha1Client
	recorder            record.EventRecorder
	aaqEvaluator        v12.Evaluator
	serverBundleFetcher *fetcher.ConfigMapCertBundleFetcher
	templateSvc         services.TemplateService
}

func NewAaqGateController(virtCli kubecli.KubevirtClient,
	aaqNs string, aaqCli v1alpha13.AaqV1alpha1Client,
	vmiInformer cache.SharedIndexInformer,
	migrationInformer cache.SharedIndexInformer,
	podInformer cache.SharedIndexInformer,
	kubeVirtInformer cache.SharedIndexInformer,
	crdInformer cache.SharedIndexInformer,
	pvcInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
) *AaqGateController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: virtCli.CoreV1().Events(v1.NamespaceAll)})
	listerFuncForResource := generic.ListerFuncForResourceFunc(informers.NewSharedInformerFactoryWithOptions(virtCli, 0).ForResource)

	ctrl := AaqGateController{
		aaqNs:               aaqNs,
		virtCli:             virtCli,
		aaqCli:              aaqCli,
		migrationInformer:   migrationInformer,
		podInformer:         podInformer,
		arqInformer:         arqInformer,
		vmiInformer:         vmiInformer,
		kubeVirtInformer:    kubeVirtInformer,
		crdInformer:         crdInformer,
		pvcInformer:         pvcInformer,
		serverBundleFetcher: &fetcher.ConfigMapCertBundleFetcher{Name: "aaq-server-signer-bundle", Client: virtCli.CoreV1().ConfigMaps(aaqNs)},
		podQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pod-queue"),
		internalLock:        &sync.Mutex{},
		nsLockMap:           &namespace_lock_utils.NamespaceLockMap{M: make(map[string]*sync.Mutex), Mutex: &sync.Mutex{}},
		nsCache:             namespace_lock_utils.NewNamespaceCache(),
		recorder:            eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: utils.ControllerPodName}),
		aaqEvaluator:        aaq_evaluator.NewAaqEvaluator(listerFuncForResource, aaq_evaluator.NewAaqAppUsageCalculator(), clock.RealClock{}),
		templateSvc:         nil,
	}

	return &ctrl
}

func (ctrl *AaqGateController) addAllGatedPodsInNamespace(ns string) {
	objs, err := ctrl.podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		log.Log.Infof("AaqGateController: Error failed to list pod from podInformer")
	}
	for _, obj := range objs {
		pod := obj.(*v1.Pod)
		if pod.Spec.SchedulingGates != nil &&
			len(pod.Spec.SchedulingGates) == 1 &&
			pod.Spec.SchedulingGates[0].Name == "ApplicationsAwareQuotaGate" {

			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(pod)
			if err != nil {
				return
			}
			ctrl.podQueue.Add(key) //todo: enqeue all arqs in namespace
		}
	}
}

// When a ApplicationsResourceQuota is deleted, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) deleteArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.addAllGatedPodsInNamespace(arq.Namespace)
	return
}

// When a ApplicationsResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AaqGateController) updateArq(old, cur interface{}) {
	arq := cur.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.addAllGatedPodsInNamespace(arq.Namespace)
	return
}

func (ctrl *AaqGateController) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) == 1 &&
		pod.Spec.SchedulingGates[0].Name == "ApplicationsAwareQuotaGate" {

		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(pod)
		if err != nil {
			return
		}
		ctrl.podQueue.Add(key)
	}
}

func (ctrl *AaqGateController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *AaqGateController) Execute() bool {
	key, quit := ctrl.podQueue.Get()
	if quit {
		return false
	}
	defer ctrl.podQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("AaqGateController: Error with key: %v", key))
	}
	switch enqueueState {
	case BackOff:
		ctrl.podQueue.AddRateLimited(key)
	case Forget:
		ctrl.podQueue.Forget(key)
	case Immediate:
		ctrl.podQueue.Add(key)
	}

	return true
}

func (ctrl *AaqGateController) execute(key string) (error, enqueueState) {
	arqObj, _, err := ctrl.podInformer.GetStore().GetByKey(key)
	if err != nil {
		return err, BackOff
	}
	arq := arqObj.(*v1alpha12.AAQJobQueueConfig)
	aaqjqc, err := ctrl.aaqCli.AAQJobQueueConfigs(arq.Namespace).Get(context.Background(), aaqjqcName, v13.GetOptions{})
	if !errors.IsNotFound(err) {
		aaqjqc, err = ctrl.aaqCli.AAQJobQueueConfigs(arq.Namespace).Create(context.Background(),
			&v1alpha12.AAQJobQueueConfig{ObjectMeta: v13.ObjectMeta{Name: aaqjqcName}},
			v13.CreateOptions{})
		if err != nil {
			return err, Immediate
		}
	}
	if len(aaqjqc.Status.PodsInJobQueue) != 0 {
		return nil, Immediate //wait for the calculator to process changes
	}
	arqs, err := ctrl.arqInformer.GetIndexer().ByIndex(cache.NamespaceIndex, arq.Namespace)
	if err != nil {
		return err, Immediate
	}

	ctrl.podEvaluator
	_, _, _ = parseKey(key)
	return nil, Forget
}

func (ctrl *AaqGateController) Run(threadiness int, stop <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	_, err := ctrl.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ctrl.addPod,
	})
	if err != nil {
		return err
	}
	_, err = ctrl.arqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: ctrl.deleteArq,
		UpdateFunc: ctrl.updateArq,
	})
	if err != nil {
		return err
	}

	klog.Info("Starting Arq controller")
	defer klog.Info("Shutting down Arq controller")

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, stop)
	}

	<-stop
	return nil

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

func LauncherPodUsageFunc(obj runtime.Object, clock clock.Clock, vmiInformer cache.SharedIndexInformer) (corev1.ResourceList, error) {

	pods, err := getPodsByLabel(string(vmi.GetUID()), v1.CreatedByLabel, vmi.Namespace)

	pod, err := toExternalPodOrError(obj)
	if err != nil {
		return corev1.ResourceList{}, err
	}

	// always quota the object count (even if the pod is end of life)
	// object count quotas track all objects that are in storage.
	// where "pods" tracks all pods that have not reached a terminal state,
	// count/pods tracks all pods independent of state.
	result := corev1.ResourceList{
		podObjectCountName: *(resource.NewQuantity(1, resource.DecimalSI)),
	}

	// by convention, we do not quota compute resources that have reached end-of life
	// note: the "pods" resource is considered a compute resource since it is tied to life-cycle.
	if !QuotaV1Pod(pod, clock) {
		return result, nil
	}

	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	// TODO: ideally, we have pod level requests and limits in the future.
	for i := range pod.Spec.Containers {
		requests = quota.Add(requests, pod.Spec.Containers[i].Resources.Requests)
		limits = quota.Add(limits, pod.Spec.Containers[i].Resources.Limits)
	}
	// InitContainers are run sequentially before other containers start, so the highest
	// init container resource is compared against the sum of app containers to determine
	// the effective usage for both requests and limits.
	for i := range pod.Spec.InitContainers {
		requests = quota.Max(requests, pod.Spec.InitContainers[i].Resources.Requests)
		limits = quota.Max(limits, pod.Spec.InitContainers[i].Resources.Limits)
	}

	requests = quota.Add(requests, pod.Spec.Overhead)
	limits = quota.Add(limits, pod.Spec.Overhead)

	result = quota.Add(result, podComputeUsageHelper(requests, limits))
	return result, nil
}
