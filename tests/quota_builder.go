package tests

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// QuotaBuilder is a builder for creating a ResourceQuota.
type QuotaBuilder struct {
	resourceQuota *v1.ResourceQuota
}

// NewQuotaBuilder creates a new instance of QuotaBuilder.
func NewQuotaBuilder() *QuotaBuilder {
	return &QuotaBuilder{
		resourceQuota: &v1.ResourceQuota{},
	}
}

// WithNamespace sets the namespace for the ResourceQuota.
func (qb *QuotaBuilder) WithNamespace(namespace string) *QuotaBuilder {
	qb.resourceQuota.ObjectMeta.Namespace = namespace
	return qb
}

// WithName sets the name for the ResourceQuota.
func (qb *QuotaBuilder) WithName(name string) *QuotaBuilder {
	qb.resourceQuota.ObjectMeta.Name = name
	return qb
}

func (qb *QuotaBuilder) WithLabel(key string, val string) *QuotaBuilder {
	if qb.resourceQuota.ObjectMeta.Labels == nil {
		qb.resourceQuota.ObjectMeta.Labels = map[string]string{}
	}
	qb.resourceQuota.ObjectMeta.Labels[key] = val
	return qb
}

// WithRequestsMemory sets  requests/limits for the ResourceQuota.
func (qb *QuotaBuilder) WithResource(resourceName v1.ResourceName, requestMemory resource.Quantity) *QuotaBuilder {
	if qb.resourceQuota.Spec.Hard == nil {
		qb.resourceQuota.Spec.Hard = make(v1.ResourceList)
	}
	qb.resourceQuota.Spec.Hard[resourceName] = requestMemory
	return qb
}

// WithName sets the name for the ResourceQuota.
func (qb *QuotaBuilder) WithZeroUsage() *QuotaBuilder {
	if qb.resourceQuota.Spec.Hard == nil {
		qb.resourceQuota.Spec.Hard = make(v1.ResourceList)
	}
	if qb.resourceQuota.Status.Used == nil {
		qb.resourceQuota.Status.Used = make(v1.ResourceList)
	}
	for rqResourceName := range qb.resourceQuota.Spec.Hard {
		qb.resourceQuota.Status.Used[rqResourceName] = *resource.NewQuantity(0, resource.DecimalSI)
	}
	return qb
}

// WithName sets the name for the ResourceQuota.
func (qb *QuotaBuilder) WithSyncStatusHard() *QuotaBuilder {
	if qb.resourceQuota.Spec.Hard == nil {
		qb.resourceQuota.Spec.Hard = make(v1.ResourceList)
	}
	if qb.resourceQuota.Status.Hard == nil {
		qb.resourceQuota.Status.Hard = make(v1.ResourceList)
	}
	for rqResourceName, q := range qb.resourceQuota.Spec.Hard {
		qb.resourceQuota.Status.Hard[rqResourceName] = q
	}
	return qb
}

// Build creates and returns the ResourceQuota.
func (qb *QuotaBuilder) Build() *v1.ResourceQuota {
	return qb.resourceQuota
}
