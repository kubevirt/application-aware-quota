package cluster

import (
	rbacv1 "k8s.io/api/rbac/v1"
	utils2 "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	controllerServiceAccountName = utils2.ControllerPodName
	controllerClusterRoleName    = utils2.ControllerPodName
)

func createStaticControllerResources(args *FactoryArgs) []client.Object {
	return []client.Object{
		createControllerClusterRole(),
		createControllerClusterRoleBinding(args.Namespace),
	}
}

func createControllerClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	return utils2.ResourceBuilder.CreateClusterRoleBinding(controllerServiceAccountName, controllerClusterRoleName, controllerServiceAccountName, namespace)
}

func getControllerClusterPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"events",
			},
			Verbs: []string{
				"create",
				"patch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"pods",
			},
			Verbs: []string{
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"persistentvolumeclaims",
			},
			Verbs: []string{
				"list",
				"watch",
				"get",
			},
		},
		{
			APIGroups: []string{
				"apiextensions.k8s.io",
			},
			Resources: []string{
				"customresourcedefinitions",
			},
			Verbs: []string{
				"list",
				"watch",
				"get",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"resourcequotas",
			},
			Verbs: []string{
				"list",
				"watch",
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationsresourcequotas",
			},
			Verbs: []string{
				"get",
				"update",
				"watch",
				"list",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationsresourcequotas/status",
			},
			Verbs: []string{
				"update",
			},
		},
		{
			APIGroups: []string{
				"kubevirt.io",
			},
			Resources: []string{
				"kubevirts",
			},
			Verbs: []string{
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"kubevirt.io",
			},
			Resources: []string{
				"virtualmachineinstances",
				"virtualmachineinstancemigrations",
			},
			Verbs: []string{
				"watch",
				"list",
			},
		},
		{
			APIGroups: []string{
				"admissionregistration.k8s.io",
			},
			Resources: []string{
				"validatingwebhookconfigurations",
			},
			Verbs: []string{
				"create",
				"get",
				"delete",
			},
		},
	}
}

func createControllerClusterRole() *rbacv1.ClusterRole {
	return utils2.ResourceBuilder.CreateClusterRole(controllerClusterRoleName, getControllerClusterPolicyRules())
}
