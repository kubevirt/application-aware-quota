package arq_controller

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sadmission "k8s.io/apiserver/pkg/admission"
	quotaplugin "k8s.io/apiserver/pkg/admission/plugin/resourcequota"
	"k8s.io/apiserver/pkg/admission/plugin/resourcequota/apis/resourcequota"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/kubernetes/scheme"
	v14 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	v1alpha1 "kubevirt.io/api/core/v1"
	"kubevirt.io/applications-aware-quota/pkg/aaq-controller/namespace-lock-utils"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/utils"
	v1alpha13 "kubevirt.io/applications-aware-quota/pkg/generated/clientset/versioned/typed/core/v1alpha1"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/containerized-data-importer/pkg/util/cert/fetcher"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
	"kubevirt.io/kubevirt/pkg/virt-controller/services"
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
	podEvaluator        v12.Evaluator
	serverBundleFetcher *fetcher.ConfigMapCertBundleFetcher
	templateSvc         services.TemplateService
}

func NewArqController(virtCli kubecli.KubevirtClient,
	aaqNs string, aaqCli v1alpha13.AaqV1alpha1Client,
	vmiInformer cache.SharedIndexInformer,
	migrationInformer cache.SharedIndexInformer,
	podInformer cache.SharedIndexInformer,
	kubeVirtInformer cache.SharedIndexInformer,
	crdInformer cache.SharedIndexInformer,
	pvcInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
) *ArqController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: virtCli.CoreV1().Events(v1.NamespaceAll)})

	ctrl := ArqController{
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
		podEvaluator:        core.NewPodEvaluator(nil, clock.RealClock{}),
		templateSvc:         nil,
	}

	return &ctrl
}

// When a ApplicationsResourceQuota is added, enqueue fake pod to namespace to trigger the reconciliation loop
func (ctrl *ArqController) addArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.podQueue.Add(arq.Namespace + "/fake")
	return
} // When a ApplicationsResourceQuota is deleted, enqueue fake pod to namespace to trigger the reconciliation loop
func (ctrl *ArqController) deleteArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.podQueue.Add(arq.Namespace + "/fake")
	return
}

// When a ApplicationsResourceQuota is updated, enqueue fake pod to namespace to trigger the reconciliation loop
func (ctrl *ArqController) updateArq(old, cur interface{}) {
	arq := cur.(*v1alpha12.ApplicationsResourceQuota)
	ctrl.podQueue.Add(arq.Namespace + "/fake")
	return
}

func (ctrl *ArqController) deletePod(obj interface{}) {
	ctrl.enqueuePod(obj)
}
func (ctrl *ArqController) addPod(obj interface{}) {
	ctrl.enqueuePod(obj)
}
func (ctrl *ArqController) updatePod(old, curr interface{}) {
	currPod := curr.(*v1.Pod)
	oldPod := old.(*v1.Pod)
	if !isTerminalState(oldPod) && isTerminalState(currPod) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(currPod)
		if err != nil {
			return
		}
		ctrl.enqueuePod(key)
	}
}
func isTerminalState(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}
func (ctrl *ArqController) enqueuePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(pod)
	if err != nil {
		return
	}
	ctrl.podQueue.Add(key)
}

func (ctrl *ArqController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *ArqController) Execute() bool {
	key, quit := ctrl.podQueue.Get()
	if quit {
		return false
	}
	defer ctrl.podQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("ArqController: Error with key: %v", key))
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

func (ctrl *ArqController) execute(key string) (error, enqueueState) {
	_, _, err := ctrl.podInformer.GetStore().GetByKey(key)
	if err != nil {
		return err, BackOff
	}
	//todo: implement
	_, _, _ = parseKey(key)
	return nil, Forget
}

func (ctrl *ArqController) Run(threadiness int, stop <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	_, err := ctrl.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addPod,
		DeleteFunc: ctrl.deletePod,
		UpdateFunc: ctrl.updatePod,
	})
	if err != nil {
		return err
	}
	_, err = ctrl.arqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addArq,
		DeleteFunc: ctrl.deleteArq,
		UpdateFunc: ctrl.updateArq,
	})
	if err != nil {
		return err
	}

	objs := ctrl.kubeVirtInformer.GetIndexer().List()
	if len(objs) != 1 {
		return fmt.Errorf("single KV object should exist in the cluster")
	}
	kv := (objs[0]).(*v1alpha1.KubeVirt)

	clusterConfig, err := virtconfig.NewClusterConfig(ctrl.crdInformer, ctrl.kubeVirtInformer, kv.Namespace)
	if err != nil {
		return err
	}

	fakeVal := "NotImportantWeJustNeedTheTargetResources"
	ctrl.templateSvc = services.NewTemplateService(fakeVal,
		240,
		fakeVal,
		fakeVal,
		fakeVal,
		fakeVal,
		fakeVal,
		fakeVal,
		ctrl.pvcInformer.GetStore(),
		ctrl.virtCli,
		clusterConfig,
		107,
		fakeVal,
	)
	klog.Info("Starting Arq controller")
	defer klog.Info("Shutting down Arq controller")

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, stop)
	}

	<-stop
	return nil

}

func listMatchingTargetPods(migration *v1alpha1.VirtualMachineInstanceMigration, vmi *v1alpha1.VirtualMachineInstance, podInformer cache.SharedIndexInformer) ([]*v1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.CreatedByLabel:    string(vmi.UID),
			v1alpha1.AppLabel:          "virt-launcher",
			v1alpha1.MigrationJobLabel: string(migration.UID),
		},
	})
	if err != nil {
		return nil, err
	}

	objs, err := podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, migration.Namespace)
	if err != nil {
		return nil, err
	}

	pods := []*v1.Pod{}
	for _, obj := range objs {
		pod := obj.(*v1.Pod)
		if selector.Matches(labels.Set(pod.ObjectMeta.Labels)) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

func admitPodToQuota(resourceQuota *v1.ResourceQuota, attributes k8sadmission.Attributes, evaluator v12.Evaluator, limitedResource resourcequota.LimitedResource) ([]v1.ResourceQuota, error) {
	return quotaplugin.CheckRequest([]v1.ResourceQuota{*resourceQuota}, attributes, evaluator, []resourcequota.LimitedResource{limitedResource})
}

func shouldUpdateArq(currArq *v1alpha12.ApplicationsResourceQuota, prevArq *v1alpha12.ApplicationsResourceQuota) bool {
	return !areResourceMapsEqual(currArq.Status.Hard, prevArq.Status.Hard)
}

func addResourcesToRQ(rq v1.ResourceQuota, rl *v1.ResourceList) *v1.ResourceQuota {
	newRQ := &v1.ResourceQuota{
		Spec: v1.ResourceQuotaSpec{
			Hard: rq.Spec.Hard,
		},
		Status: v1.ResourceQuotaStatus{
			Hard: rq.Spec.Hard,
			Used: rq.Status.Used,
		},
	}
	for rqResourceName, currQuantity := range newRQ.Spec.Hard {
		for resourceName, quantityToAdd := range *rl {
			if rqResourceName == resourceName {
				currQuantity.Add(quantityToAdd)
				newRQ.Spec.Hard[rqResourceName] = currQuantity
				newRQ.Status.Hard[rqResourceName] = currQuantity
			}
		}
	}
	return newRQ
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

func areResourceMapsEqual(map1, map2 map[v1.ResourceName]resource.Quantity) bool {
	if len(map1) != len(map2) {
		return false
	}

	for key, value1 := range map1 {
		value2, exists := map2[key]
		if !exists {
			return false
		}

		if !value1.Equal(value2) {
			return false
		}
	}

	return true
}

func getCurrLauncherLimitedResource(podEvaluator v12.Evaluator, podToCreate *v1.Pod) (resourcequota.LimitedResource, error) {
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
