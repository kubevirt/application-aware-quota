package builders

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// AcrqBuilder is a builder for creating a ApplicationAwareClusterResourceQuota.
type AcrqBuilder struct {
	acrq *v1alpha1.ApplicationAwareClusterResourceQuota
}

// NewQuotaBuilder creates a new instance of ApplicationAwareClusterResourceQuota.
func NewAcrqBuilder() *AcrqBuilder {
	return &AcrqBuilder{
		acrq: &v1alpha1.ApplicationAwareClusterResourceQuota{},
	}
}

// WithNamespace sets the namespace for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithLabelSelector(selector *v13.LabelSelector) *AcrqBuilder {
	qb.acrq.Spec.Selector.LabelSelector = selector
	return qb
}

// WithNamespace sets the namespace for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithAnnotationSelector(annotationSelector map[string]string) *AcrqBuilder {
	qb.acrq.Spec.Selector.AnnotationSelector = annotationSelector
	return qb
}

// WithName sets the name for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithName(name string) *AcrqBuilder {
	qb.acrq.ObjectMeta.Name = name
	return qb
}

// WithRequestsMemory sets  requests/limits for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithResource(resourceName v1.ResourceName, val resource.Quantity) *AcrqBuilder {
	if qb.acrq.Spec.Quota.Hard == nil {
		qb.acrq.Spec.Quota.Hard = make(v1.ResourceList)
	}
	qb.acrq.Spec.Quota.Hard[resourceName] = val
	return qb
}

// WithScopes sets scopes for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithScopes(scopes []v1.ResourceQuotaScope) *AcrqBuilder {
	qb.acrq.Spec.Quota.Scopes = []v1.ResourceQuotaScope{}
	qb.acrq.Spec.Quota.Scopes = scopes
	return qb
}

// WithScopesSelector sets scopeSelector for the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) WithScopesSelector(scopeSelector *v1.ScopeSelector) *AcrqBuilder {
	qb.acrq.Spec.Quota.ScopeSelector = &v1.ScopeSelector{}
	qb.acrq.Spec.Quota.ScopeSelector = scopeSelector
	return qb
}

// Build creates and returns the ApplicationAwareClusterResourceQuota.
func (qb *AcrqBuilder) Build() *v1alpha1.ApplicationAwareClusterResourceQuota {
	return qb.acrq
}
