package rq_controller

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	_ "kubevirt.io/api/core/v1"
	"kubevirt.io/applications-aware-quota/pkg/client"
	v1alpha12 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/log"
	"strings"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
	RQSuffix  string       = "-non-schedulable-resources-managed-rq-x"
	RQLabel   string       = "aaq.managed.rq"
)

type RQController struct {
	arqInformer cache.SharedIndexInformer
	rqInformer  cache.SharedIndexInformer
	arqQueue    workqueue.RateLimitingInterface
	aaqCli      client.AAQClient
	stop        <-chan struct{}
}

func NewRQController(aaqCli client.AAQClient,
	rqInformer cache.SharedIndexInformer,
	arqInformer cache.SharedIndexInformer,
	stop <-chan struct{},
) *RQController {
	ctrl := RQController{
		rqInformer:  rqInformer,
		aaqCli:      aaqCli,
		arqInformer: arqInformer,
		arqQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "arq-queue-for-rq-contorller"),
		stop:        stop,
	}

	_, err := ctrl.rqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateRQ,
		DeleteFunc: ctrl.deleteRQ,
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
	}

	return &ctrl
}

// When a ApplicationsResourceQuotas is deleted, enqueue all gated pods for revaluation
func (ctrl *RQController) deleteArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
	if err != nil {
		return
	}
	ctrl.arqQueue.Add(key)
	return
}

// When a ApplicationsResourceQuotas is updated, enqueue all gated pods for revaluation
func (ctrl *RQController) addArq(obj interface{}) {
	arq := obj.(*v1alpha12.ApplicationsResourceQuota)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
	if err != nil {
		return
	}
	ctrl.arqQueue.Add(key)
	return
}

// When a ApplicationsResourceQuotas is updated, enqueue all gated pods for revaluation
func (ctrl *RQController) updateArq(old, cur interface{}) {
	curArq := cur.(*v1alpha12.ApplicationsResourceQuota)
	oldArq := old.(*v1alpha12.ApplicationsResourceQuota)

	if !quota.Equals(curArq.Spec.Hard, oldArq.Spec.Hard) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curArq)
		if err != nil {
			return
		}
		ctrl.arqQueue.Add(key)
	}

	return
}

func (ctrl *RQController) deleteRQ(obj interface{}) {
	rq := obj.(*v1.ResourceQuota)
	arq := &v1alpha12.ApplicationsResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(rq.Name, RQSuffix), Namespace: rq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
	if err != nil {
		return
	}

	ctrl.arqQueue.Add(key)
	return
}
func (ctrl *RQController) updateRQ(old, curr interface{}) {
	curRq := curr.(*v1.ResourceQuota)
	oldRq := old.(*v1.ResourceQuota)
	if !quota.Equals(curRq.Spec.Hard, oldRq.Spec.Hard) || !labels.Equals(curRq.Labels, oldRq.Labels) {
		arq := &v1alpha12.ApplicationsResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(curRq.Name, RQSuffix), Namespace: curRq.Namespace},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
		if err != nil {
			return
		}
		ctrl.arqQueue.Add(key)
	}
	return
}

func (ctrl *RQController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *RQController) Execute() bool {
	key, quit := ctrl.arqQueue.Get()
	if quit {
		return false
	}
	defer ctrl.arqQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		log.Log.Infof(fmt.Sprintf("RQController: Error with key: %v err: %v", key, err))
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

func (ctrl *RQController) execute(key string) (error, enqueueState) {
	arqNS, arqName, err := cache.SplitMetaNamespaceKey(key)
	arqObj, exists, err := ctrl.arqInformer.GetIndexer().GetByKey(arqNS + "/" + arqName)
	if err != nil {
		return err, Immediate
	} else if !exists {
		err = ctrl.aaqCli.CoreV1().ResourceQuotas(arqNS).Delete(context.Background(), arqName+RQSuffix, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	arq := arqObj.(*v1alpha12.ApplicationsResourceQuota).DeepCopy()
	nonSchedulableResourcesLimitations := FilterNonScheduableResources(arq.Spec.Hard)
	if len(nonSchedulableResourcesLimitations) == 0 {
		err = ctrl.aaqCli.CoreV1().ResourceQuotas(arqNS).Delete(context.Background(), arqName+RQSuffix, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	rqObj, exists, err := ctrl.rqInformer.GetIndexer().GetByKey(arq.Namespace + "/" + arq.Name + RQSuffix)
	if err != nil {
		return err, Immediate
	} else if !exists {
		rq := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: arq.Name + RQSuffix,
				Labels: map[string]string{
					RQLabel: "true",
				},
			},
			Spec: v1.ResourceQuotaSpec{
				Hard: nonSchedulableResourcesLimitations,
			},
		}
		rq, err = ctrl.aaqCli.CoreV1().ResourceQuotas(arqNS).Create(context.Background(), rq, metav1.CreateOptions{})
		if err != nil {
			return err, Immediate
		} else {
			return err, Forget
		}
	}
	rq := rqObj.(*v1.ResourceQuota).DeepCopy()

	dirty := !quota.Equals(rq.Spec.Hard, nonSchedulableResourcesLimitations)

	if rq.Labels == nil {
		rq.Labels = map[string]string{
			RQLabel: "true",
		}
		dirty = true
	}

	_, ok := rq.Labels[RQLabel]
	if !ok {
		rq.Labels[RQLabel] = "true"
		dirty = true
	}

	if !dirty {
		return nil, Forget
	}

	rq.Spec.Hard = nonSchedulableResourcesLimitations
	_, err = ctrl.aaqCli.CoreV1().ResourceQuotas(arqNS).Update(context.Background(), rq, metav1.UpdateOptions{})
	if err != nil {
		return err, Immediate
	}

	return nil, Forget
}

func FilterNonScheduableResources(resourceList v1.ResourceList) v1.ResourceList {
	rlCopy := resourceList.DeepCopy()
	scheduableResources := getSchedulableResources()
	for _, resourceName := range scheduableResources {
		delete(rlCopy, resourceName)
	}
	return rlCopy
}

// getSchedulableResources returns a list of resource names that are not counted in the resource quota.
func getSchedulableResources() []v1.ResourceName {
	// Add the resource names that should not be counted in the quota here
	return []v1.ResourceName{
		v1.ResourcePods,
		v1.ResourceCPU,
		v1.ResourceMemory,
		v1.ResourceEphemeralStorage,
		v1.ResourceRequestsCPU,
		v1.ResourceRequestsMemory,
		v1.ResourceRequestsEphemeralStorage,
		v1.ResourceLimitsCPU,
		v1.ResourceLimitsMemory,
		v1.ResourceLimitsEphemeralStorage,
	}
}

func (ctrl *RQController) Run(threadiness int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting Arq controller")
	defer klog.Info("Shutting down Arq controller")
	defer ctrl.arqQueue.ShutDown()

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}

	<-ctrl.stop
}
