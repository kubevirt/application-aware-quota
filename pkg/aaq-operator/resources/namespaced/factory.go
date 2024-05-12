package namespaced

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	utils "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/resources"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FactoryArgs contains the required parameters to generate all namespaced resources
type FactoryArgs struct {
	OperatorVersion                           string `required:"true" split_words:"true"`
	ControllerImage                           string `required:"true" split_words:"true"`
	DeployClusterResources                    string `required:"true" split_words:"true"`
	AaqServerImage                            string `required:"true" split_words:"true"`
	Verbosity                                 string `required:"true"`
	PullPolicy                                string `required:"true" split_words:"true"`
	ImagePullSecrets                          []corev1.LocalObjectReference
	PriorityClassName                         string
	Namespace                                 string
	InfraNodePlacement                        *sdkapi.NodePlacement
	OnOpenshift                               bool
	Client                                    client.Client
	AllowApplicationAwareClusterResourceQuota bool
	VmiCalcConfigName                         v1alpha1.VmiCalcConfigName
}

type factoryFunc func(*FactoryArgs) []client.Object

type namespaceHaver interface {
	SetNamespace(string)
	GetNamespace() string
}

var factoryFunctions = map[string]factoryFunc{
	"aaqServer":  createAAQServerResources,
	"controller": createAAQControllerResources,
}

// CreateAllResources creates all namespaced resources
func CreateAllResources(args *FactoryArgs) ([]client.Object, error) {
	var resources []client.Object
	for group := range factoryFunctions {
		rs, err := CreateResourceGroup(group, args)
		if err != nil {
			return nil, err
		}
		resources = append(resources, rs...)
	}
	return resources, nil
}

// CreateResourceGroup creates namespaced resources for a specific group/component
func CreateResourceGroup(group string, args *FactoryArgs) ([]client.Object, error) {
	f, ok := factoryFunctions[group]
	if !ok {
		return nil, fmt.Errorf("group %s does not exist", group)
	}
	resources := f(args)
	for _, resource := range resources {
		utils.ValidateGVKs([]runtime.Object{resource})
		assignNamspaceIfMissing(resource, args.Namespace)
	}
	return resources, nil
}

func assignNamspaceIfMissing(resource client.Object, namespace string) {
	obj, ok := resource.(namespaceHaver)
	if !ok {
		return
	}

	if obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}
}
