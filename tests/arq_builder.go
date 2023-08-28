package tests

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
)

// QuotaBuilder is a builder for creating a ResourceQuota.
type ArqBuilder struct {
	arq *v1alpha1.ApplicationsResourceQuota
}

// NewQuotaBuilder creates a new instance of QuotaBuilder.
func NewArqBuilder() *ArqBuilder {
	return &ArqBuilder{
		arq: &v1alpha1.ApplicationsResourceQuota{},
	}
}

// WithNamespace sets the namespace for the ResourceQuota.
func (qb *ArqBuilder) WithNamespace(namespace string) *ArqBuilder {
	qb.arq.ObjectMeta.Namespace = namespace
	return qb
}

// WithName sets the name for the ResourceQuota.
func (qb *ArqBuilder) WithName(name string) *ArqBuilder {
	qb.arq.ObjectMeta.Name = name
	return qb
}

// WithRequestsMemory sets  requests/limits for the ResourceQuota.
func (qb *ArqBuilder) WithResource(resourceName v1.ResourceName, requestMemory resource.Quantity) *ArqBuilder {
	if qb.arq.Spec.Hard == nil {
		qb.arq.Spec.Hard = make(v1.ResourceList)
	}
	qb.arq.Spec.Hard[resourceName] = requestMemory
	return qb
}

// Build creates and returns the ResourceQuota.
func (qb *ArqBuilder) Build() *v1alpha1.ApplicationsResourceQuota {
	return qb.arq
}
