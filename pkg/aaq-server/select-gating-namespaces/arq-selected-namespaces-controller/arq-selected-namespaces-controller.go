package arq_selected_namespaces_controller

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/listers/core/v1alpha1"
	"kubevirt.io/application-aware-quota/pkg/log"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"sync"
	"time"
)

type NamespaceSet struct {
	m sync.Map
}

func (s *NamespaceSet) Add(ns string) {
	s.m.Store(ns, true)
}

func (s *NamespaceSet) Remove(ns string) {
	s.m.Delete(ns)
}

func (s *NamespaceSet) Contains(ns string) bool {
	_, ok := s.m.Load(ns)
	return ok
}

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
)

type ArqSelectedNamespacesController struct {
	nsSet     NamespaceSet
	arqLister v1alpha1.ApplicationAwareResourceQuotaLister
	nsQueue   workqueue.RateLimitingInterface
	stop      <-chan struct{}
}

func NewArqSelectedNamespacesController(
	arqInformer cache.SharedIndexInformer,
	stop <-chan struct{},
) *ArqSelectedNamespacesController {
	ctrl := &ArqSelectedNamespacesController{
		arqLister: v1alpha1.NewApplicationAwareResourceQuotaLister(arqInformer.GetIndexer()),
		nsQueue:   workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), workqueue.RateLimitingQueueConfig{Name: "ns_queue"}),
		stop:      stop,
	}

	arqInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    ctrl.addArq,
			UpdateFunc: ctrl.updateArq,
			DeleteFunc: ctrl.deleteArq,
		},
	)

	return ctrl
}

func (ctrl *ArqSelectedNamespacesController) updateArq(_, curr interface{}) {
	arq := curr.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
}

func (ctrl *ArqSelectedNamespacesController) addArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
}

func (ctrl *ArqSelectedNamespacesController) deleteArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationAwareResourceQuota)
	ctrl.nsQueue.Add(arq.Namespace)
}

func (ctrl *ArqSelectedNamespacesController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *ArqSelectedNamespacesController) Execute() bool {
	ns, quit := ctrl.nsQueue.Get()
	if quit {
		return false
	}
	defer ctrl.nsQueue.Done(ns)
	enqueueState, err := ctrl.execute(ns.(string))

	if err != nil {
		log.Log.Infof(fmt.Sprintf("ArqSelectedNamespacesController: Error with key: %v err: %v", ns, err))
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

func (ctrl *ArqSelectedNamespacesController) execute(ns string) (enqueueState, error) {
	arqs, err := ctrl.arqLister.ApplicationAwareResourceQuotas(ns).List(labels.Everything())
	if err != nil {
		return Immediate, err
	}
	if len(arqs) > 0 {
		ctrl.nsSet.Add(ns)
		return Forget, nil
	}

	ctrl.nsSet.Remove(ns)
	return Forget, nil
}

func (ctrl *ArqSelectedNamespacesController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting ARQ controller")
	defer klog.Info("Shutting ARQ Controller")

	arqs, _ := ctrl.arqLister.List(labels.Everything())
	for _, arq := range arqs {
		ctrl.nsQueue.Add(arq.Namespace)
	}

	// the workers that chug through the quota calculation backlog
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}

	<-ctx.Done()
}

func (ctrl *ArqSelectedNamespacesController) GetNSSet() *NamespaceSet {
	return &ctrl.nsSet
}
