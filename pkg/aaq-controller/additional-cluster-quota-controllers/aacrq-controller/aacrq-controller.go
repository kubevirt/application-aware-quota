package aacrq_controller

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	_ "kubevirt.io/api/core/v1"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
)

type AacrqController struct {
	acrqInformer  cache.SharedIndexInformer
	aacrqInformer cache.SharedIndexInformer
	aacrqQueue    workqueue.RateLimitingInterface
	aaqCli        client.AAQClient
	stop          <-chan struct{}
}

func NewAacrqController(aaqCli client.AAQClient,
	aacrqInformer cache.SharedIndexInformer,
	acrqInformer cache.SharedIndexInformer,
	stop <-chan struct{},
) *AacrqController {
	ctrl := AacrqController{
		aacrqInformer: aacrqInformer,
		aaqCli:        aaqCli,
		acrqInformer:  acrqInformer,
		aacrqQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aacrq-queue-for-aacrq-contorller"),
		stop:          stop,
	}

	_, err := ctrl.aacrqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateAacrq,
		DeleteFunc: ctrl.deleteAacrq,
	})
	if err != nil {
		panic("something is wrong")
	}
	_, err = ctrl.acrqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: ctrl.deleteAcrq,
		UpdateFunc: ctrl.updateAcrq,
		AddFunc:    ctrl.addAcrq,
	})
	if err != nil {
	}

	return &ctrl
}

// When a ApplicationAwareClusterResourceQuota is deleted, enqueue all gated pods for revaluation
func (ctrl *AacrqController) deleteAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	if acrq.Status.Namespaces == nil {
		return
	}
	for _, ns := range acrq.Status.Namespaces {
		aacrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: acrq.Name, Namespace: ns.Namespace},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(aacrq)
		if err != nil {
			continue
		}
		ctrl.aacrqQueue.Add(key)
	}
	return
}

// When a ApplicationAwareClusterResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AacrqController) addAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	if acrq.Status.Namespaces == nil {
		return
	}
	for _, ns := range acrq.Status.Namespaces {
		aacrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: acrq.Name, Namespace: ns.Namespace},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(aacrq)
		if err != nil {
			continue
		}
		ctrl.aacrqQueue.Add(key)
	}
	return
}

// When a ApplicationAwareClusterResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *AacrqController) updateAcrq(old, cur interface{}) {
	curacrq := cur.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	oldacrq := old.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	// Create a set of namespaces from both the current and old acrq
	nsSet := make(map[string]bool)
	for _, ns := range append(curacrq.Status.Namespaces, oldacrq.Status.Namespaces...) {
		nsSet[ns.Namespace] = true
	}
	// Enqueue all the namespaces in the set
	for ns := range nsSet {
		aacrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: curacrq.Name, Namespace: ns},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(aacrq)
		if err != nil {
			continue
		}
		ctrl.aacrqQueue.Add(key)
	}
	return
}

func (ctrl *AacrqController) deleteAacrq(obj interface{}) {
	aacrq := obj.(*v1alpha12.ApplicationAwareAppliedClusterResourceQuota)
	acrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: aacrq.Name, Namespace: aacrq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
	if err != nil {
		return
	}

	ctrl.aacrqQueue.Add(key)
	return
}

func (ctrl *AacrqController) updateAacrq(old, curr interface{}) {
	curAppliedAcrq := curr.(*v1alpha12.ApplicationAwareAppliedClusterResourceQuota)
	acrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: curAppliedAcrq.Name, Namespace: curAppliedAcrq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
	if err != nil {
		return
	}
	ctrl.aacrqQueue.Add(key)
	return
}

func (ctrl *AacrqController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *AacrqController) Execute() bool {
	key, quit := ctrl.aacrqQueue.Get()
	if quit {
		return false
	}
	defer ctrl.aacrqQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		log.Log.Infof(fmt.Sprintf("AacrqController: Error with key: %v err: %v", key, err))
	}
	switch enqueueState {
	case BackOff:
		ctrl.aacrqQueue.AddRateLimited(key)
	case Forget:
		ctrl.aacrqQueue.Forget(key)
	case Immediate:
		ctrl.aacrqQueue.Add(key)
	}

	return true
}

func (ctrl *AacrqController) execute(key string) (error, enqueueState) {
	aacrqNamespace, aacrqName, err := cache.SplitMetaNamespaceKey(key)
	acrqObj, exists, err := ctrl.acrqInformer.GetIndexer().GetByKey(aacrqName)
	if err != nil {
		return err, Immediate
	} else if !exists {
		err = ctrl.aaqCli.ApplicationAwareAppliedClusterResourceQuotas(aacrqNamespace).Delete(context.Background(), aacrqName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	acrq := acrqObj.(*v1alpha12.ApplicationAwareClusterResourceQuota).DeepCopy()
	found := false
	for _, ns := range acrq.Status.Namespaces {
		if ns.Namespace == aacrqNamespace {
			found = true
			break
		}
	}
	if !found {
		err = ctrl.aaqCli.ApplicationAwareAppliedClusterResourceQuotas(aacrqNamespace).Delete(context.Background(), aacrqName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	aacrqObj, exists, err := ctrl.aacrqInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return err, Immediate
	} else if !exists {
		aacrq := &v1alpha12.ApplicationAwareAppliedClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: acrq.Name,
				Labels: map[string]string{
					util.AAQLabel: "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(acrq, v1alpha12.ApplicationAwareClusterResourceQuotaGroupVersionKind),
				},
			},
			Spec:   acrq.Spec,
			Status: acrq.Status,
		}
		aacrq, err = ctrl.aaqCli.ApplicationAwareAppliedClusterResourceQuotas(aacrqNamespace).Create(context.Background(), aacrq, metav1.CreateOptions{})
		if err != nil {
			return err, Immediate
		} else {
			return err, Forget
		}
	}
	aacrq := aacrqObj.(*v1alpha12.ApplicationAwareAppliedClusterResourceQuota).DeepCopy()

	if aacrq.Labels == nil {
		aacrq.Labels = map[string]string{
			util.AAQLabel: "true",
		}
	}

	_, ok := aacrq.Labels[util.AAQLabel]
	if !ok {
		aacrq.Labels[util.AAQLabel] = "true"
	}

	aacrq.Spec = acrq.Spec
	aacrq.Status = acrq.Status
	_, err = ctrl.aaqCli.ApplicationAwareAppliedClusterResourceQuotas(aacrqNamespace).Update(context.Background(), aacrq, metav1.UpdateOptions{})
	if err != nil {
		return err, Immediate
	}

	return nil, Forget
}

func (ctrl *AacrqController) Run(threadiness int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting AACRQ controller")
	defer klog.Info("Shutting down AACRQ controller")
	defer ctrl.aacrqQueue.ShutDown()

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}

	<-ctrl.stop
}
