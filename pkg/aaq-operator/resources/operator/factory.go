package operator

import (
	"fmt"

	csvv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/namespaced"
	utils "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/resources"
)

// FactoryArgs contains the required parameters to generate all cluster-scoped resources
type FactoryArgs struct {
	NamespacedArgs namespaced.FactoryArgs
	Image          string
}

type factoryFunc func(*FactoryArgs) []client.Object

func aggregateFactoryFunc(funcs ...factoryFunc) factoryFunc {
	return func(args *FactoryArgs) []client.Object {
		var result []client.Object
		for _, f := range funcs {
			result = append(result, f(args)...)
		}
		return result
	}
}

var operatorFactoryFunctions = map[string]factoryFunc{
	"operator-cluster-rbac": createClusterRBAC,
	"operator-rbac":         createNamespacedRBAC,
	"operator-deployment":   createDeployment,
	"operator-crd":          createCRD,
	"everything":            aggregateFactoryFunc(createCRD, createClusterRBAC, createNamespacedRBAC, createDeployment),
}

// ClusterServiceVersionData - Data arguments used to create AAQ's CSV manifest
type ClusterServiceVersionData struct {
	CsvVersion         string
	ReplacesCsvVersion string
	Namespace          string
	ImagePullPolicy    string
	ImagePullSecrets   []corev1.LocalObjectReference
	IconBase64         string
	Verbosity          string

	OperatorVersion string

	ControllerImage    string
	WebhookServerImage string
	OperatorImage      string
}

// CreateOperatorResourceGroup creates all cluster resources from a specific group/component
func CreateOperatorResourceGroup(group string, args *FactoryArgs) ([]client.Object, error) {
	f, ok := operatorFactoryFunctions[group]
	if !ok {
		return nil, fmt.Errorf("group %s does not exist", group)
	}
	resources := f(args)
	for _, r := range resources {
		utils.ValidateGVKs([]runtime.Object{r})
	}
	return resources, nil
}

// NewAaqCrd - provides AAQ CRD
func NewAaqCrd() *extv1.CustomResourceDefinition {
	return createAAQListCRD()
}

// NewClusterServiceVersion - generates CSV for CDI
func NewClusterServiceVersion(data *ClusterServiceVersionData) (*csvv1.ClusterServiceVersion, error) {
	return createClusterServiceVersion(data)
}
