package configuration_controller

import (
	"context"
	"fmt"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	_ "kubevirt.io/api/core/v1"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/pkg/log"
	"kubevirt.io/applications-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
)

type AaqConfigurationController struct {
	aaqInformer                  cache.SharedIndexInformer
	aaqQueue                     workqueue.RateLimitingInterface
	aaqCli                       client.AAQClient
	calcRegistry                 *aaq_evaluator.AaqCalculatorsRegistry
	stop                         <-chan struct{}
	enqueueAllArgControllerChan  chan<- struct{}
	enqueueAllGateControllerChan chan<- struct{}
}

func NewAaqConfigurationController(aaqCli client.AAQClient,
	aaqInformer cache.SharedIndexInformer,
	calcRegistry *aaq_evaluator.AaqCalculatorsRegistry,
	stop <-chan struct{},
	enqueueAllArgControllerChan chan<- struct{},
	enqueueAllGateControllerChan chan<- struct{},
) *AaqConfigurationController {

	ctrl := AaqConfigurationController{
		aaqCli:                       aaqCli,
		aaqInformer:                  aaqInformer,
		aaqQueue:                     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aaq-queue"),
		calcRegistry:                 calcRegistry,
		stop:                         stop,
		enqueueAllArgControllerChan:  enqueueAllArgControllerChan,
		enqueueAllGateControllerChan: enqueueAllGateControllerChan,
	}

	_, err := ctrl.aaqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateAaq,
	})
	if err != nil {
		panic("something is wrong")

	}

	return &ctrl
}

func (ctrl *AaqConfigurationController) updateAaq(old, cur interface{}) {
	oldAaq := old.(*v1alpha12.AAQ)
	curAaq := cur.(*v1alpha12.AAQ)
	if oldAaq.Spec.Configuration.VmiCalculatorConfiguration != curAaq.Spec.Configuration.VmiCalculatorConfiguration {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curAaq)
		if err != nil {
			return
		}
		ctrl.aaqQueue.Add(key)
	}
}

func (ctrl *AaqConfigurationController) Run(_ context.Context, threadiness int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting config controller")
	defer klog.Info("Shutting down config controller")
	defer ctrl.aaqQueue.ShutDown()

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}
	<-ctrl.stop
}

func (ctrl *AaqConfigurationController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *AaqConfigurationController) Execute() bool {
	key, quit := ctrl.aaqQueue.Get()
	if quit {
		return false
	}
	defer ctrl.aaqQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		klog.Errorf(fmt.Sprintf("AaqConfigurationController: Error with key: %v err: %v", key, err))
	}
	switch enqueueState {
	case BackOff:
		ctrl.aaqQueue.AddRateLimited(key)
	case Forget:
		ctrl.aaqQueue.Forget(key)
	case Immediate:
		ctrl.aaqQueue.Add(key)
	}

	return true
}

func (ctrl *AaqConfigurationController) execute(_ string) (error, enqueueState) {
	objs := ctrl.aaqInformer.GetIndexer().List()
	if len(objs) != 1 {
		log.Log.Error("Single AAQ object should exist in the cluster.")
		return nil, BackOff
	}
	aaq := (objs[0]).(*v1alpha12.AAQ)

	ctrl.calcRegistry.ReplaceBuiltInCalculatorConfig(util.LauncherConfig, string(aaq.Spec.Configuration.VmiCalculatorConfiguration.ConfigName))

	ctrl.enqueueAllArgControllerChan <- struct{}{}
	ctrl.enqueueAllGateControllerChan <- struct{}{}

	return nil, Forget
}
