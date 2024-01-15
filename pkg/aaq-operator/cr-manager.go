package aaq_operator

import (
	"context"
	"kubevirt.io/applications-aware-quota/pkg/util"

	aaqcluster "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/cluster"
	aaqnamespaced "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/namespaced"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	aaqv1 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk"
)

// Status provides AAQ status sub-resource
func (r *ReconcileAAQ) Status(cr client.Object) *sdkapi.Status {
	return &cr.(*aaqv1.AAQ).Status.Status
}

// Create creates new AAQ resource
func (r *ReconcileAAQ) Create() client.Object {
	return &aaqv1.AAQ{}
}

// GetDependantResourcesListObjects provides slice of List resources corresponding to AAQ-dependant resource types
func (r *ReconcileAAQ) GetDependantResourcesListObjects() []client.ObjectList {
	return []client.ObjectList{
		&extv1.CustomResourceDefinitionList{},
		&rbacv1.ClusterRoleBindingList{},
		&rbacv1.ClusterRoleList{},
		&appsv1.DeploymentList{},
		&corev1.ServiceList{},
		&rbacv1.RoleBindingList{},
		&rbacv1.RoleList{},
		&corev1.ServiceAccountList{},
	}
}

// IsCreating checks whether operator config is missing (which means it is create-type reconciliation)
func (r *ReconcileAAQ) IsCreating(_ client.Object) (bool, error) {
	configMap, err := r.getConfigMap()
	if err != nil {
		return true, nil
	}
	return configMap == nil, nil
}

func (r *ReconcileAAQ) getNamespacedArgs(cr *aaqv1.AAQ) *aaqnamespaced.FactoryArgs {
	result := *r.namespacedArgs

	if cr != nil {
		if cr.Spec.ImagePullPolicy != "" {
			result.PullPolicy = string(cr.Spec.ImagePullPolicy)
		}
		if cr.Spec.PriorityClass != nil && string(*cr.Spec.PriorityClass) != "" {
			result.PriorityClassName = string(*cr.Spec.PriorityClass)
		} else {
			result.PriorityClassName = util.AAQPriorityClass
		}
		// Verify the priority class name exists.
		priorityClass := &schedulingv1.PriorityClass{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: result.PriorityClassName}, priorityClass); err != nil {
			// Any error we cannot determine if priority class exists.
			result.PriorityClassName = ""
		}
		result.InfraNodePlacement = &cr.Spec.Infra
		result.Client = r.client
	}

	return &result
}

// GetAllResources provides slice of resources AAQ depends on
func (r *ReconcileAAQ) GetAllResources(crObject client.Object) ([]client.Object, error) {
	cr := crObject.(*aaqv1.AAQ)
	var resources []client.Object

	if sdk.DeployClusterResources() {
		crs, err := aaqcluster.CreateAllStaticResources(r.clusterArgs)
		if err != nil {
			sdk.MarkCrFailedHealing(cr, r.Status(cr), "CreateResources", "Unable to create all resources", r.recorder)
			return nil, err
		}

		resources = append(resources, crs...)
	}

	nsrs, err := aaqnamespaced.CreateAllResources(r.getNamespacedArgs(cr))
	if err != nil {
		sdk.MarkCrFailedHealing(cr, r.Status(cr), "CreateNamespaceResources", "Unable to create all namespaced resources", r.recorder)
		return nil, err
	}

	resources = append(resources, nsrs...)

	drs, err := aaqcluster.CreateAllDynamicResources(r.clusterArgs)
	if err != nil {
		sdk.MarkCrFailedHealing(cr, r.Status(cr), "CreateDynamicResources", "Unable to create all dynamic resources", r.recorder)
		return nil, err
	}

	resources = append(resources, drs...)

	certs := r.getCertificateDefinitions(cr)
	for _, cert := range certs {
		if cert.SignerSecret != nil {
			resources = append(resources, cert.SignerSecret)
		}

		if cert.CertBundleConfigmap != nil {
			resources = append(resources, cert.CertBundleConfigmap)
		}

		if cert.TargetSecret != nil {
			resources = append(resources, cert.TargetSecret)
		}
	}

	return resources, nil
}
