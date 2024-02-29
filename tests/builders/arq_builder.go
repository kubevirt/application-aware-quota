package builders

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// ArqBuilder is a builder for creating a ApplicationAwareResourceQuota.
type ArqBuilder struct {
	arq *v1alpha1.ApplicationAwareResourceQuota
}

// NewArqBuilder creates a new instance of ArqBuilder.
func NewArqBuilder() *ArqBuilder {
	return &ArqBuilder{
		arq: &v1alpha1.ApplicationAwareResourceQuota{},
	}
}

// WithNamespace sets the namespace for the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) WithNamespace(namespace string) *ArqBuilder {
	qb.arq.ObjectMeta.Namespace = namespace
	return qb
}

// WithName sets the name for the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) WithName(name string) *ArqBuilder {
	qb.arq.ObjectMeta.Name = name
	return qb
}

// WithRequestsMemory sets  requests/limits for the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) WithResource(resourceName v1.ResourceName, val resource.Quantity) *ArqBuilder {
	if qb.arq.Spec.Hard == nil {
		qb.arq.Spec.Hard = make(v1.ResourceList)
	}
	qb.arq.Spec.Hard[resourceName] = val
	return qb
}

// WithScopes sets scopes for the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) WithScopes(scopes []v1.ResourceQuotaScope) *ArqBuilder {
	qb.arq.Spec.Scopes = []v1.ResourceQuotaScope{}
	qb.arq.Spec.Scopes = scopes
	return qb
}

// WithName sets the name for the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) WithSyncStatusHardEmptyStatusUsed() *ArqBuilder {
	if qb.arq.Spec.Hard == nil {
		qb.arq.Spec.Hard = make(v1.ResourceList)
	}
	if qb.arq.Status.Hard == nil {
		qb.arq.Status.Hard = make(v1.ResourceList)
	}
	if qb.arq.Status.Used == nil {
		qb.arq.Status.Used = make(v1.ResourceList)
	}
	for rqResourceName, q := range qb.arq.Spec.Hard {
		qb.arq.Status.Hard[rqResourceName] = q
		qb.arq.Status.Used[rqResourceName] = resource.MustParse("0")
	}
	return qb
}

// Build creates and returns the ApplicationAwareResourceQuota.
func (qb *ArqBuilder) Build() *v1alpha1.ApplicationAwareResourceQuota {
	return qb.arq
}
