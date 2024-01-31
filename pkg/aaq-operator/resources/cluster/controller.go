package cluster

import (
	rbacv1 "k8s.io/api/rbac/v1"
	utils2 "kubevirt.io/applications-aware-quota/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createStaticControllerResources(args *FactoryArgs) []client.Object {
	return []client.Object{
		createControllerClusterRole(),
		createControllerClusterRoleBinding(args.Namespace),
	}
}

func createControllerClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	return utils2.ResourceBuilder.CreateClusterRoleBinding(utils2.ControllerServiceAccountName, utils2.ControllerClusterRoleName, utils2.ControllerServiceAccountName, namespace)
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
				"update",
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
				"namespaces",
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
				"create",
				"delete",
			},
		},
		{
			APIGroups: []string{
				"quota.openshift.io",
			},
			Resources: []string{
				"clusterresourcequotas",
			},
			Verbs: []string{
				"list",
				"watch",
				"create",
				"delete",
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"aaqjobqueueconfigs",
			},
			Verbs: []string{
				"get",
				"watch",
				"list",
				"create",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"aaqjobqueueconfigs/status",
			},
			Verbs: []string{
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationawareresourcequotas",
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
				"applicationawareclusterresourcequotas",
			},
			Verbs: []string{
				"get",
				"watch",
				"list",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationawareclusterresourcequotas/finalizers",
			},
			Verbs: []string{
				"create",
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationawareresourcequotas/finalizers",
			},
			Verbs: []string{
				"create",
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationawareresourcequotas/status",
			},
			Verbs: []string{
				"update",
			},
		},
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"applicationawareclusterresourcequotas/status",
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
				"get",
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
		{
			APIGroups: []string{
				"aaq.kubevirt.io",
			},
			Resources: []string{
				"aaqs",
			},
			Verbs: []string{
				"list",
				"watch",
			},
		},
	}
}

func createControllerClusterRole() *rbacv1.ClusterRole {
	return utils2.ResourceBuilder.CreateClusterRole(utils2.ControllerClusterRoleName, getControllerClusterPolicyRules())
}
