package cluster

import (
	"fmt"
	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utils "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/resources"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FactoryArgs contains the required parameters to generate all cluster-scoped resources
type FactoryArgs struct {
	Namespace   string
	Client      client.Client
	Logger      logr.Logger
	OnOpenshift bool
}

type factoryFunc func(*FactoryArgs) []client.Object

type factoryFuncMap map[string]factoryFunc

var staticFactoryFunctions = factoryFuncMap{
	"aaq-server-rbac": createStaticAAQLockResources,
	"controller-rbac": createStaticControllerResources,
	"crd-resources":   createCRDResources,
}

var dynamicFactoryFunctions = factoryFuncMap{
	"webhooks-server-registrations": createDynamicMutatingGatingServerResources,
}

// CreateAllStaticResources creates all static cluster-wide resources
func CreateAllStaticResources(args *FactoryArgs) ([]client.Object, error) {
	return createAllResources(staticFactoryFunctions, args)
}

// CreateAllDynamicResources creates all dynamic cluster-wide resources
func CreateAllDynamicResources(args *FactoryArgs) ([]client.Object, error) {
	return createAllResources(dynamicFactoryFunctions, args)
}

func createAllResources(funcMap factoryFuncMap, args *FactoryArgs) ([]client.Object, error) {
	var resources []client.Object
	for group := range funcMap {
		rs, err := createResourceGroup(funcMap, group, args)
		if err != nil {
			return nil, err
		}
		resources = append(resources, rs...)
	}
	return resources, nil
}

func createResourceGroup(funcMap factoryFuncMap, group string, args *FactoryArgs) ([]client.Object, error) {
	f, ok := funcMap[group]
	if !ok {
		return nil, fmt.Errorf("group %s does not exist", group)
	}
	resources := f(args)
	for _, r := range resources {
		utils.ValidateGVKs([]runtime.Object{r})
	}
	return resources, nil
}

func createCRDResources(args *FactoryArgs) []client.Object {
	objs := []client.Object{
		createApplicationsResourceQuotaCRD(),
		createAaqJobQueueConfigsCRD(),
	}
	if args.OnOpenshift {
		objs = append(objs, createClusterAppsResourceQuotaCRD())
	}
	return objs
}

// GetClusterRolePolicyRules returns all cluster PolicyRules
func GetClusterRolePolicyRules() []rbacv1.PolicyRule {
	result := getControllerClusterPolicyRules()
	result = append(result, getAaqServerClusterPolicyRules()...)
	return result
}
