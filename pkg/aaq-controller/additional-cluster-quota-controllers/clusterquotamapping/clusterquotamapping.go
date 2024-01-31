package clusterquotamapping

import (
	"fmt"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/listers/core/v1alpha1"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// NewClusterQuotaMappingController builds a mapping between namespaces and ApplicationAwareClusterResourceQuotas
func NewClusterQuotaMappingController(namespaceInformer cache.SharedIndexInformer, quotaInformer cache.SharedIndexInformer, stop <-chan struct{}) *ClusterQuotaMappingController {
	c := newClusterQuotaMappingController(namespaceInformer, quotaInformer, stop)
	c.namespaceLister = v1NamespaceLister{lister: corev1listers.NewNamespaceLister(namespaceInformer.GetIndexer())}
	return c
}

type namespaceLister interface {
	Each(label labels.Selector, fn func(metav1.Object) bool) error
	Get(name string) (metav1.Object, error)
}

type v1NamespaceLister struct {
	lister corev1listers.NamespaceLister
}

func (l v1NamespaceLister) Each(label labels.Selector, fn func(metav1.Object) bool) error {
	results, err := l.lister.List(label)
	if err != nil {
		return err
	}
	for i := range results {
		if !fn(results[i]) {
			return nil
		}
	}
	return nil
}
func (l v1NamespaceLister) Get(name string) (metav1.Object, error) {
	return l.lister.Get(name)
}

func newClusterQuotaMappingController(namespaceInformer cache.SharedIndexInformer, quotaInformer cache.SharedIndexInformer, stop <-chan struct{}) *ClusterQuotaMappingController {
	c := &ClusterQuotaMappingController{
		namespaceQueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controller_clusterquotamappingcontroller_namespaces"),
		quotaQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controller_clusterquotamappingcontroller_clusterquotas"),
		clusterQuotaMapper: NewClusterQuotaMapper(),
		stop:               stop,
	}
	namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addNamespace,
		UpdateFunc: c.updateNamespace,
		DeleteFunc: c.deleteNamespace,
	})
	c.namespacesSynced = namespaceInformer.HasSynced

	quotaInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addQuota,
		UpdateFunc: c.updateQuota,
		DeleteFunc: c.deleteQuota,
	})

	c.quotaLister = v1alpha1.NewApplicationAwareClusterResourceQuotaLister(quotaInformer.GetIndexer())
	c.quotasSynced = quotaInformer.HasSynced

	return c
}

type ClusterQuotaMappingController struct {
	namespaceQueue   workqueue.RateLimitingInterface
	namespaceLister  namespaceLister
	namespacesSynced func() bool

	quotaQueue   workqueue.RateLimitingInterface
	quotaLister  v1alpha1.ApplicationAwareClusterResourceQuotaLister
	quotasSynced func() bool

	clusterQuotaMapper *clusterQuotaMapper
	stop               <-chan struct{}
}

func (c *ClusterQuotaMappingController) GetClusterQuotaMapper() ClusterQuotaMapper {
	return c.clusterQuotaMapper
}

func (c *ClusterQuotaMappingController) Run(workers int) {
	defer utilruntime.HandleCrash()
	defer c.namespaceQueue.ShutDown()
	defer c.quotaQueue.ShutDown()

	klog.Infof("Starting ClusterQuotaMappingController controller")
	defer klog.Infof("Shutting down ClusterQuotaMappingController controller")

	if !cache.WaitForCacheSync(c.stop, c.namespacesSynced, c.quotasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	klog.V(4).Infof("Starting workers for quota mapping controller workers")
	for i := 0; i < workers; i++ {
		go wait.Until(c.namespaceWorker, time.Second, c.stop)
		go wait.Until(c.quotaWorker, time.Second, c.stop)
	}

	<-c.stop
}

func (c *ClusterQuotaMappingController) syncQuota(quota *v1alpha12.ApplicationAwareClusterResourceQuota) error {
	matcherFunc, err := GetObjectMatcher(quota.Spec.Selector)
	if err != nil {
		return err
	}

	if err := c.namespaceLister.Each(labels.Everything(), func(obj metav1.Object) bool {
		// attempt to set the mapping. The quotas never collide with each other (same quota is never processed twice in parallel)
		// so this means that the project we have is out of date, pull a more recent copy from the cache and retest
		for {
			matches, err := matcherFunc(obj)
			if err != nil {
				utilruntime.HandleError(err)
				break
			}
			success, quotaMatches, _ := c.clusterQuotaMapper.setMapping(quota, obj, !matches)
			if success {
				break
			}

			// if the quota is mismatched, then someone has updated the quota or has deleted the entry entirely.
			// if we've been updated, we'll be rekicked, if we've been deleted we should stop.  Either way, this
			// execution is finished
			if !quotaMatches {
				return false
			}
			newer, err := c.namespaceLister.Get(obj.GetName())
			if kapierrors.IsNotFound(err) {
				// if the namespace is gone, then the deleteNamespace path will be called, just continue
				break
			}
			if err != nil {
				utilruntime.HandleError(err)
				break
			}
			obj = newer
		}
		return true
	}); err != nil {
		return err
	}

	c.clusterQuotaMapper.completeQuota(quota)
	return nil
}

func (c *ClusterQuotaMappingController) syncNamespace(namespace metav1.Object) error {
	allQuotas, err1 := c.quotaLister.List(labels.Everything())
	if err1 != nil {
		return err1
	}
	for i := range allQuotas {
		quota := allQuotas[i]

		for {
			matcherFunc, err := GetObjectMatcher(quota.Spec.Selector)
			if err != nil {
				utilruntime.HandleError(err)
				break
			}

			// attempt to set the mapping. The namespaces never collide with each other (same namespace is never processed twice in parallel)
			// so this means that the quota we have is out of date, pull a more recent copy from the cache and retest
			matches, err := matcherFunc(namespace)
			if err != nil {
				utilruntime.HandleError(err)
				break
			}
			success, _, namespaceMatches := c.clusterQuotaMapper.setMapping(quota, namespace, !matches)
			if success {
				break
			}

			// if the namespace is mismatched, then someone has updated the namespace or has deleted the entry entirely.
			// if we've been updated, we'll be rekicked, if we've been deleted we should stop.  Either way, this
			// execution is finished
			if !namespaceMatches {
				return nil
			}

			quota, err = c.quotaLister.Get(quota.Name)
			if kapierrors.IsNotFound(err) {
				// if the quota is gone, then the deleteQuota path will be called, just continue
				break
			}
			if err != nil {
				utilruntime.HandleError(err)
				break
			}
		}
	}

	c.clusterQuotaMapper.completeNamespace(namespace)
	return nil
}

func (c *ClusterQuotaMappingController) quotaWork() bool {
	key, quit := c.quotaQueue.Get()
	if quit {
		return true
	}
	defer c.quotaQueue.Done(key)

	quota, err := c.quotaLister.Get(key.(string))
	if err != nil {
		if errors.IsNotFound(err) {
			c.quotaQueue.Forget(key)
			return false
		}
		utilruntime.HandleError(err)
		return false
	}

	err = c.syncQuota(quota)
	outOfRetries := c.quotaQueue.NumRequeues(key) > 5
	switch {
	case err != nil && outOfRetries:
		utilruntime.HandleError(err)
		c.quotaQueue.Forget(key)

	case err != nil && !outOfRetries:
		c.quotaQueue.AddRateLimited(key)

	default:
		c.quotaQueue.Forget(key)
	}

	return false
}

func (c *ClusterQuotaMappingController) quotaWorker() {
	for {
		if quit := c.quotaWork(); quit {
			return
		}
	}
}

func (c *ClusterQuotaMappingController) namespaceWork() bool {
	key, quit := c.namespaceQueue.Get()
	if quit {
		return true
	}
	defer c.namespaceQueue.Done(key)

	namespace, err := c.namespaceLister.Get(key.(string))
	if kapierrors.IsNotFound(err) {
		c.namespaceQueue.Forget(key)
		return false
	}
	if err != nil {
		utilruntime.HandleError(err)
		return false
	}

	err = c.syncNamespace(namespace)
	outOfRetries := c.namespaceQueue.NumRequeues(key) > 5
	switch {
	case err != nil && outOfRetries:
		utilruntime.HandleError(err)
		c.namespaceQueue.Forget(key)

	case err != nil && !outOfRetries:
		c.namespaceQueue.AddRateLimited(key)

	default:
		c.namespaceQueue.Forget(key)
	}

	return false
}

func (c *ClusterQuotaMappingController) namespaceWorker() {
	for {
		if quit := c.namespaceWork(); quit {
			return
		}
	}
}

func (c *ClusterQuotaMappingController) deleteNamespace(obj interface{}) {
	var name string
	switch ns := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		switch nested := ns.Obj.(type) {
		case *corev1.Namespace:
			name = nested.Name
		default:
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Namespace %T", ns.Obj))
			return
		}
	case *corev1.Namespace:
		name = ns.Name
	default:
		utilruntime.HandleError(fmt.Errorf("not a Namespace %v", obj))
		return
	}
	c.clusterQuotaMapper.removeNamespace(name)
}

func (c *ClusterQuotaMappingController) addNamespace(cur interface{}) {
	c.enqueueNamespace(cur)
}
func (c *ClusterQuotaMappingController) updateNamespace(old, cur interface{}) {
	c.enqueueNamespace(cur)
}
func (c *ClusterQuotaMappingController) enqueueNamespace(obj interface{}) {
	switch ns := obj.(type) {
	case *corev1.Namespace:
		if !c.clusterQuotaMapper.requireNamespace(ns) {
			return
		}
	default:
		utilruntime.HandleError(fmt.Errorf("not a Namespace %v", obj))
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.namespaceQueue.Add(key)
}

func (c *ClusterQuotaMappingController) deleteQuota(obj interface{}) {
	quota, ok1 := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	if !ok1 {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %v", obj))
			return
		}
		quota, ok = tombstone.Obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Quota %v", obj))
			return
		}
	}

	c.clusterQuotaMapper.removeQuota(quota.Name)
}

func (c *ClusterQuotaMappingController) addQuota(cur interface{}) {
	c.enqueueQuota(cur)
}
func (c *ClusterQuotaMappingController) updateQuota(old, cur interface{}) {
	c.enqueueQuota(cur)
}
func (c *ClusterQuotaMappingController) enqueueQuota(obj interface{}) {
	quota, ok := obj.(*v1alpha12.ApplicationAwareClusterResourceQuota)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("not a Quota %v", obj))
		return
	}
	if !c.clusterQuotaMapper.requireQuota(quota) {
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(quota)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.quotaQueue.Add(key)
}
