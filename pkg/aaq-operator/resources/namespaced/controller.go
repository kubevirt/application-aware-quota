package namespaced

import (
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utils2 "kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

func createAAQControllerResources(args *FactoryArgs) []client.Object {
	cr, _ := utils2.GetActiveAAQ(args.Client)
	if cr == nil {
		return nil
	}
	return []client.Object{
		createAAQControllerServiceAccount(),
		createControllerRoleBinding(),
		createControllerRole(),
		createAAQControllerDeployment(args.ControllerImage, args.Verbosity, args.PullPolicy, args.ImagePullSecrets, args.PriorityClassName, args.InfraNodePlacement, cr.Spec.Configuration.AllowApplicationAwareClusterResourceQuota, args.OnOpenshift, cr.Spec.Configuration.VmiCalculatorConfiguration.ConfigName, args.Client),
	}
}
func createControllerRoleBinding() *rbacv1.RoleBinding {
	return utils2.ResourceBuilder.CreateRoleBinding(utils2.ControllerResourceName, utils2.ControllerResourceName, utils2.ControllerServiceAccountName, "")
}
func createControllerRole() *rbacv1.Role {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"configmaps",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"endpoints",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
				"create",
				"update",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"secrets",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"coordination.k8s.io",
			},
			Resources: []string{
				"leases",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
				"delete",
				"update",
				"create",
				"patch",
			},
		},
	}
	return utils2.ResourceBuilder.CreateRole(utils2.ControllerResourceName, rules)
}

func createAAQControllerServiceAccount() *corev1.ServiceAccount {
	return utils2.ResourceBuilder.CreateServiceAccount(utils2.ControllerResourceName)
}

func createAAQControllerDeployment(image, verbosity, pullPolicy string, imagePullSecrets []corev1.LocalObjectReference, priorityClassName string, infraNodePlacement *sdkapi.NodePlacement, enableClusterQuota bool, onOpenshift bool, configName v1alpha1.VmiCalcConfigName, c client.Client) *appsv1.Deployment {
	defaultMode := corev1.ConfigMapVolumeSourceDefaultMode
	deployment := utils2.CreateDeployment(utils2.ControllerResourceName, utils2.AAQLabel, utils2.ControllerResourceName, utils2.ControllerResourceName, imagePullSecrets, 2, infraNodePlacement)
	if priorityClassName != "" {
		deployment.Spec.Template.Spec.PriorityClassName = priorityClassName
	}
	desiredMaxUnavailable := intstr.FromInt(1)
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &desiredMaxUnavailable,
		},
	}
	container := utils2.CreateContainer(utils2.ControllerResourceName, image, verbosity, pullPolicy)
	labels := mergeLabels(deployment.Spec.Template.GetLabels(), map[string]string{utils2.PrometheusLabelKey: utils2.PrometheusLabelValue})
	//Add label for pod affinity
	deployment.SetLabels(labels)
	deployment.Spec.Template.SetLabels(labels)
	if configName != "" {
		container.Args = append(container.Args, []string{"--" + utils2.VMICalculatorConfiguration, string(configName)}...)
	}
	if enableClusterQuota {
		container.Args = append(container.Args, []string{"--" + utils2.EnableClusterQuota, "true"}...)
		if onOpenshift {
			container.Args = append(container.Args, []string{"--" + utils2.IsOnOpenshift, "true"}...)
		}
	}
	container.Ports = createAAQControllerPorts()
	container.Env = []corev1.EnvVar{
		{
			Name: utils2.InstallerPartOfLabel,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  fmt.Sprintf("metadata.labels['%s']", utils2.AppKubernetesPartOfLabel),
				},
			},
		},
		{
			Name: utils2.InstallerVersionLabel,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  fmt.Sprintf("metadata.labels['%s']", utils2.AppKubernetesVersionLabel),
				},
			},
		},
	}
	container.ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTPS,
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8443,
				},
				Path: "/leader",
			},
		},
		InitialDelaySeconds: 15,
		TimeoutSeconds:      10,
	}
	container.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("150Mi"),
		},
	}
	container.VolumeMounts = []corev1.VolumeMount{
		{
			Name:      utils2.VolumeMountName,
			MountPath: utils2.SocketsSharedDirectory,
		},
	}

	cr, _ := utils2.GetActiveAAQ(c)
	var Containers []corev1.Container
	if cr != nil {
		container.Args = append(container.Args, []string{"--" + utils2.SidecarEvaluatorsNumberFlag, strconv.Itoa(len(cr.Spec.Configuration.SidecarEvaluators))}...)
		Containers = cr.Spec.Configuration.SidecarEvaluators
	}
	Containers = append(Containers, container)
	deployment.Spec.Template.Spec.Containers = Containers
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "server-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: utils2.SecretResourceName,
					Items: []corev1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "tls.crt",
						},
						{
							Key:  "tls.key",
							Path: "tls.key",
						},
					},
					DefaultMode: &defaultMode,
				},
			},
		},
		{
			Name: utils2.VolumeMountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	if infraNodePlacement == nil {
		deployment.Spec.Template.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{utils2.AAQLabel: utils2.ControllerResourceName},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
						Weight: 100,
					},
				},
			},
		}
	}
	return deployment
}

func createAAQControllerPorts() []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{
			Name:          "metrics",
			ContainerPort: 8443,
			Protocol:      "TCP",
		},
	}
}

// mergeLabels adds source labels to destination (does not change existing ones)
func mergeLabels(src, dest map[string]string) map[string]string {
	if dest == nil {
		dest = map[string]string{}
	}

	for k, v := range src {
		dest[k] = v
	}

	return dest
}
