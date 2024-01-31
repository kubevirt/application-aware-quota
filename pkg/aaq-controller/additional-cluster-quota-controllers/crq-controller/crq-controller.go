package crq_controller

import (
	"context"
	"fmt"
	v12 "github.com/openshift/api/quota/v1"
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
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"reflect"
	"strings"
	"time"
)

type enqueueState string

const (
	Immediate enqueueState = "Immediate"
	Forget    enqueueState = "Forget"
	BackOff   enqueueState = "BackOff"
	CRQSuffix string       = "-non-schedulable-resources-managed-crq-x"
)

type CRQController struct {
	acrqInformer cache.SharedIndexInformer
	crqInformer  cache.SharedIndexInformer
	acrqQueue    workqueue.RateLimitingInterface
	aaqCli       client.AAQClient
	stop         <-chan struct{}
}

func NewCRQController(aaqCli client.AAQClient,
	crqInformer cache.SharedIndexInformer,
	acrqInformer cache.SharedIndexInformer,
	stop <-chan struct{},
) *CRQController {
	ctrl := CRQController{
		crqInformer:  crqInformer,
		aaqCli:       aaqCli,
		acrqInformer: acrqInformer,
		acrqQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "acrq-queue-for-crq-contorller"),
		stop:         stop,
	}

	_, err := ctrl.crqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateCRQ,
		DeleteFunc: ctrl.deleteCRQ,
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

// When a ApplicationAwareResourceQuota is deleted, enqueue all gated pods for revaluation
func (ctrl *CRQController) deleteAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
	if err != nil {
		return
	}
	ctrl.acrqQueue.Add(key)
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *CRQController) addAcrq(obj interface{}) {
	acrq := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
	if err != nil {
		return
	}
	ctrl.acrqQueue.Add(key)
	return
}

// When a ApplicationAwareResourceQuota is updated, enqueue all gated pods for revaluation
func (ctrl *CRQController) updateAcrq(old, cur interface{}) {
	curArq := cur.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	oldArq := old.(*v1alpha12.ApplicationAwareClusterResourceQuota)

	if !quota.Equals(curArq.Spec.Quota.Hard, oldArq.Spec.Quota.Hard) || !reflect.DeepEqual(curArq.Spec.Selector, oldArq.Spec.Selector) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curArq)
		if err != nil {
			return
		}
		ctrl.acrqQueue.Add(key)
	}

	return
}

func (ctrl *CRQController) deleteCRQ(obj interface{}) {
	crq := obj.(*v12.ClusterResourceQuota)
	acrq := &v1alpha12.ApplicationAwareClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(crq.Name, CRQSuffix), Namespace: crq.Namespace},
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
	if err != nil {
		return
	}

	ctrl.acrqQueue.Add(key)
	return
}

func (ctrl *CRQController) updateCRQ(old, curr interface{}) {
	curRq := curr.(*v12.ClusterResourceQuota)
	oldRq := old.(*v12.ClusterResourceQuota)
	if !quota.Equals(curRq.Spec.Quota.Hard, oldRq.Spec.Quota.Hard) || !labels.Equals(curRq.Labels, oldRq.Labels) || !reflect.DeepEqual(curRq.Spec.Selector, oldRq.Spec.Selector) {
		acrq := &v1alpha12.ApplicationAwareClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSuffix(curRq.Name, CRQSuffix), Namespace: curRq.Namespace},
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(acrq)
		if err != nil {
			return
		}
		ctrl.acrqQueue.Add(key)
	}
	return
}

func (ctrl *CRQController) runWorker() {
	for ctrl.Execute() {
	}
}

func (ctrl *CRQController) Execute() bool {
	key, quit := ctrl.acrqQueue.Get()
	if quit {
		return false
	}
	defer ctrl.acrqQueue.Done(key)

	err, enqueueState := ctrl.execute(key.(string))
	if err != nil {
		log.Log.Infof(fmt.Sprintf("CRQController: Error with key: %v err: %v", key, err))
	}
	switch enqueueState {
	case BackOff:
		ctrl.acrqQueue.AddRateLimited(key)
	case Forget:
		ctrl.acrqQueue.Forget(key)
	case Immediate:
		ctrl.acrqQueue.Add(key)
	}

	return true
}

func (ctrl *CRQController) execute(key string) (error, enqueueState) {
	_, arqName, err := cache.SplitMetaNamespaceKey(key)
	acrqObj, exists, err := ctrl.acrqInformer.GetIndexer().GetByKey(arqName)
	if err != nil {
		return err, Immediate
	} else if !exists {
		err = ctrl.aaqCli.CRQClient().QuotaV1().ClusterResourceQuotas().Delete(context.Background(), arqName+CRQSuffix, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	acrq := acrqObj.(*v1alpha12.ApplicationAwareClusterResourceQuota).DeepCopy()
	nonSchedulableResourcesLimitations := util.FilterNonScheduableResources(acrq.Spec.Quota.Hard)
	if len(nonSchedulableResourcesLimitations) == 0 {
		err = ctrl.aaqCli.CRQClient().QuotaV1().ClusterResourceQuotas().Delete(context.Background(), arqName+CRQSuffix, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err, Immediate
		} else {
			return nil, Forget
		}
	}

	crqObj, exists, err := ctrl.crqInformer.GetIndexer().GetByKey(acrq.Name + CRQSuffix)
	if err != nil {
		return err, Immediate
	} else if !exists {
		crq := &v12.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: acrq.Name + CRQSuffix,
				Labels: map[string]string{
					util.AAQLabel: "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(acrq, v1alpha12.ApplicationAwareClusterResourceQuotaGroupVersionKind),
				},
			},
			Spec: v12.ClusterResourceQuotaSpec{
				Quota: v1.ResourceQuotaSpec{
					Hard:          nonSchedulableResourcesLimitations,
					Scopes:        acrq.Spec.Quota.Scopes,
					ScopeSelector: acrq.Spec.Quota.ScopeSelector,
				},
				Selector: acrq.Spec.Selector,
			},
		}
		crq, err = ctrl.aaqCli.CRQClient().QuotaV1().ClusterResourceQuotas().Create(context.Background(), crq, metav1.CreateOptions{})
		if err != nil {
			return err, Immediate
		} else {
			return err, Forget
		}
	}
	crq := crqObj.(*v12.ClusterResourceQuota).DeepCopy()

	dirty := !quota.Equals(crq.Spec.Quota.Hard, nonSchedulableResourcesLimitations) ||
		!reflect.DeepEqual(crq.Spec.Quota.ScopeSelector, acrq.Spec.Quota.ScopeSelector) ||
		!reflect.DeepEqual(crq.Spec.Quota.Scopes, acrq.Spec.Quota.Scopes) ||
		!reflect.DeepEqual(crq.Spec.Selector, acrq.Spec.Selector)

	if crq.Labels == nil {
		crq.Labels = map[string]string{
			util.AAQLabel: "true",
		}
		dirty = true
	}

	_, ok := crq.Labels[util.AAQLabel]
	if !ok {
		crq.Labels[util.AAQLabel] = "true"
		dirty = true
	}

	if !dirty {
		return nil, Forget
	}

	crq.Spec = v12.ClusterResourceQuotaSpec{
		Quota: v1.ResourceQuotaSpec{
			Hard:          nonSchedulableResourcesLimitations,
			Scopes:        acrq.Spec.Quota.Scopes,
			ScopeSelector: acrq.Spec.Quota.ScopeSelector,
		},
		Selector: acrq.Spec.Selector,
	}

	_, err = ctrl.aaqCli.CRQClient().QuotaV1().ClusterResourceQuotas().Update(context.Background(), crq, metav1.UpdateOptions{})
	if err != nil {
		return err, Immediate
	}

	return nil, Forget
}

func (ctrl *CRQController) Run(threadiness int) {
	defer utilruntime.HandleCrash()
	klog.Info("Starting CRQ controller")
	defer klog.Info("Shutting down CRQ controller")
	defer ctrl.acrqQueue.ShutDown()

	for i := 0; i < threadiness; i++ {
		go wait.Until(ctrl.runWorker, time.Second, ctrl.stop)
	}

	<-ctrl.stop
}
