package libaaq

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/flags"
)

type PlugablePolicyName string

const (
	LabelSidecar      PlugablePolicyName = "label-sidecar"
	AnnotationSidecar PlugablePolicyName = "annotation-sidecar"
	// Double Calculate double amount of usage for pod
	Double configName = "double"
	// Triple Calculate triple amount of usage for pod
	Triple                  configName = "triple"
	LabelAppLabel                      = "label-app"
	AnnotationAppAnnotation            = "annotation-app"
)

type configName string

func AllowApplicationAwareClusterResourceQuota(aaq *v1alpha1.AAQ) *v1alpha1.AAQ {
	aaq.Spec.Configuration.AllowApplicationAwareClusterResourceQuota = true
	return aaq
}

func AddPlugablePolicy(aaq *v1alpha1.AAQ, pp PlugablePolicyName, c configName) *v1alpha1.AAQ {
	aaq.Spec.Configuration.SidecarEvaluators = append(aaq.Spec.Configuration.SidecarEvaluators, corev1.Container{
		Name: string(pp),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      util.VolumeMountName,
				MountPath: util.SocketsSharedDirectory,
			},
		},
		Image: plugablePolicyFor(pp),
		Args:  []string{"--config", string(c)},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			AllowPrivilegeEscalation: pointer.Bool(false),
			RunAsNonRoot:             pointer.Bool(true),
		},
	})

	return aaq
}

// CheckIfPlugablePolicyExistInAAQ checks if a PlugablePolicy with a specific config exists in the AAQ.
func CheckIfPlugablePolicyExistInAAQ(aaq *v1alpha1.AAQ, sidecarName PlugablePolicyName, c configName) bool {
	for _, sidecar := range aaq.Spec.Configuration.SidecarEvaluators {
		if sidecar.Name != string(sidecarName) {
			continue
		}
		for _, arg := range sidecar.Args {
			if arg == string(c) {
				return true
			}
		}
	}
	return false
}

func plugablePolicyFor(name PlugablePolicyName) string {
	return plugablePolicyFromRegistryFor(flags.KubeVirtUtilityRepoPrefix, name)
}

func plugablePolicyFromRegistryFor(registry string, name PlugablePolicyName) string {
	switch name {
	case LabelSidecar, AnnotationSidecar:
		return fmt.Sprintf("%s/%s:%s", registry, name, flags.KubeVirtUtilityVersionTag)
	}
	panic(fmt.Sprintf("Unsupported registry disk %s", name))
}

func AaqControllerReady(clientset *kubernetes.Clientset, aaqInstallNs string) bool {
	deployment, err := clientset.AppsV1().Deployments(aaqInstallNs).Get(context.TODO(), util.ControllerPodName, v12.GetOptions{})
	if err != nil {
		panic(err)
	}
	if *deployment.Spec.Replicas != deployment.Status.ReadyReplicas {
		return false
	}
	return true
}
