package aaq_evaluator

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	namespace_lock_utils "kubevirt.io/applications-aware-quota/pkg/aaq-controller/namespace-lock-utils"
	v1alpha13 "kubevirt.io/applications-aware-quota/pkg/generated/clientset/versioned/typed/core/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/containerized-data-importer/pkg/util/cert/fetcher"
	"kubevirt.io/kubevirt/pkg/virt-controller/services"
	"sync"
)

// Evaluator knows how to evaluate quota usage for a particular group resource
type Evaluator interface {
	// Constraints ensures that each required resource is present on item
	Constraints(required []corev1.ResourceName, item runtime.Object) error
	// GroupResource returns the groupResource that this object knows how to evaluate
	GroupResource() schema.GroupResource
	// Handles determines if quota could be impacted by the specified attribute.
	// If true, admission control must perform quota processing for the operation, otherwise it is safe to ignore quota.
	Handles(operation admission.Attributes) bool
	// Matches returns true if the specified quota matches the input item
	Matches(resourceQuota *corev1.ResourceQuota, item runtime.Object) (bool, error)
	// MatchingScopes takes the input specified list of scopes and input object and returns the set of scopes that matches input object.
	MatchingScopes(item runtime.Object, scopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error)
	// UncoveredQuotaScopes takes the input matched scopes which are limited by configuration and the matched quota scopes. It returns the scopes which are in limited scopes but don't have a corresponding covering quota scope
	UncoveredQuotaScopes(limitedScopes []corev1.ScopedResourceSelectorRequirement, matchedQuotaScopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error)
	// MatchingResources takes the input specified list of resources and returns the set of resources evaluator matches.
	MatchingResources(input []corev1.ResourceName) []corev1.ResourceName
	// Usage returns the resource usage for the specified object
	Usage(item runtime.Object) (corev1.ResourceList, error)
	// UsageStats calculates latest observed usage stats for all objects
	UsageStats(options UsageStatsOptions) (UsageStats, error)
}

type AaqEva struct {
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