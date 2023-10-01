package util

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	aaqv1alpha1 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	utils "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/resources"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AAQLabel is the labe applied to all non operator resources
	AAQLabel = "aaq.kubevirt.io"
	// AAQPriorityClass is the priority class for all AAQ pods.
	AAQPriorityClass = "kubevirt-cluster-critical"
	// AppKubernetesManagedByLabel is the Kubernetes recommended managed-by label
	AppKubernetesManagedByLabel = "app.kubernetes.io/managed-by"
	// AppKubernetesComponentLabel is the Kubernetes recommended component label
	AppKubernetesComponentLabel = "app.kubernetes.io/component"
	// PrometheusLabelKey provides the label to indicate prometheus metrics are available in the pods.
	PrometheusLabelKey = "prometheus.aaq.kubevirt.io"
	// PrometheusLabelValue provides the label value which shouldn't be empty to avoid a prometheus WIP issue.
	PrometheusLabelValue = "true"
	// AppKubernetesPartOfLabel is the Kubernetes recommended part-of label
	AppKubernetesPartOfLabel = "app.kubernetes.io/part-of"
	// AppKubernetesVersionLabel is the Kubernetes recommended version label
	AppKubernetesVersionLabel = "app.kubernetes.io/version"
	// ControllerPodName is the label applied to aaq-controller resources
	ControllerPodName = "aaq-controller"
	// AaqServerPodName is the name of the server pods
	AaqServerPodName = "aaq-server"
	// ControllerServiceAccountName is the name of the AAQ controller service account
	ControllerServiceAccountName = ControllerPodName

	// InstallerPartOfLabel provides a constant to capture our env variable "INSTALLER_PART_OF_LABEL"
	InstallerPartOfLabel = "INSTALLER_PART_OF_LABEL"
	// InstallerVersionLabel provides a constant to capture our env variable "INSTALLER_VERSION_LABEL"
	InstallerVersionLabel = "INSTALLER_VERSION_LABEL"
	// InstallerVersionLabel provides a constant to capture our env variable "INSTALLER_VERSION_LABEL"
	KubevirtInstallNamespace = "KUBEVIRT_INSTALL_NAMESPACE"
	// TlsLabel provides a constant to capture our env variable "TLS"
	TlsLabel = "TLS"
	// ConfigMapName is the name of the aaq configmap that own aaq resources
	ConfigMapName                                                     = "aaq-config"
	OperatorServiceAccountName                                        = "aaq-operator"
	AAQGate                                                           = "ApplicationsAwareQuotaGate"
	ControllerResourceName                                            = ControllerPodName
	SecretResourceName                                                = "aaq-server-cert"
	AaqServerResourceName                                             = "aaq-server"
	ControllerClusterRoleName                                         = ControllerPodName
	DefaultLauncherConfig      aaqv1alpha1.VmiCalculatorConfiguration = aaqv1alpha1.VmiPodUsage
	LauncherConfig                                                    = "launcherConfig"
)

var commonLabels = map[string]string{
	AAQLabel:                    "",
	AppKubernetesManagedByLabel: "aaq-operator",
	AppKubernetesComponentLabel: "multi-tenant",
}

var operatorLabels = map[string]string{
	"operator.aaq.kubevirt.io": "",
}

// ResourceBuilder helps in creating k8s resources
var ResourceBuilder = utils.NewResourceBuilder(commonLabels, operatorLabels)

// CreateContainer creates container
func CreateContainer(name, image, verbosity, pullPolicy string) corev1.Container {
	container := ResourceBuilder.CreateContainer(name, image, pullPolicy)
	container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	container.TerminationMessagePath = corev1.TerminationMessagePathDefault
	container.Args = []string{"-v=" + verbosity}
	container.SecurityContext = &corev1.SecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		AllowPrivilegeEscalation: pointer.Bool(false),
		RunAsNonRoot:             pointer.Bool(true),
	}
	return *container
}

// CreateDeployment creates deployment
func CreateDeployment(name, matchKey, matchValue, serviceAccountName string, imagePullSecrets []corev1.LocalObjectReference, replicas int32, infraNodePlacement *sdkapi.NodePlacement) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: &[]bool{true}[0],
		},
		ImagePullSecrets: imagePullSecrets,
	}
	deployment := ResourceBuilder.CreateDeployment(name, "", matchKey, matchValue, serviceAccountName, replicas, podSpec, infraNodePlacement)
	return deployment
}

// CreateOperatorDeployment creates operator deployment
func CreateOperatorDeployment(name, namespace, matchKey, matchValue, serviceAccount string, imagePullSecrets []corev1.LocalObjectReference, numReplicas int32) *appsv1.Deployment {

	podSpec := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: &[]bool{true}[0],
		},
		ImagePullSecrets: imagePullSecrets,
		NodeSelector:     map[string]string{"kubernetes.io/os": "linux"},
		Tolerations: []corev1.Toleration{
			{
				Key:      "CriticalAddonsOnly",
				Operator: corev1.TolerationOpExists,
			},
		},
	}
	deployment := ResourceBuilder.CreateOperatorDeployment(name, namespace, matchKey, matchValue, serviceAccount, numReplicas, podSpec)
	labels := MergeLabels(deployment.Spec.Template.GetLabels(), map[string]string{PrometheusLabelKey: PrometheusLabelValue})
	deployment.SetLabels(labels)
	deployment.Spec.Template.SetLabels(labels)
	return deployment
}

// MergeLabels adds source labels to destination (does not change existing ones)
func MergeLabels(src, dest map[string]string) map[string]string {
	if dest == nil {
		dest = map[string]string{}
	}

	for k, v := range src {
		dest[k] = v
	}

	return dest
}

// GetActiveAAQ returns the active AAQ CR
func GetActiveAAQ(c client.Client) (*aaqv1alpha1.AAQ, error) {
	crList := &aaqv1alpha1.AAQList{}
	if err := c.List(context.TODO(), crList, &client.ListOptions{}); err != nil {
		return nil, err
	}

	var activeResources []aaqv1alpha1.AAQ
	for _, cr := range crList.Items {
		if cr.Status.Phase != sdkapi.PhaseError {
			activeResources = append(activeResources, cr)
		}
	}

	if len(activeResources) == 0 {
		return nil, nil
	}

	if len(activeResources) > 1 {
		return nil, fmt.Errorf("number of active AAQ CRs > 1")
	}

	return &activeResources[0], nil
}

func GetDeployment(c client.Client, deploymentName string, deploymentNS string) (*appsv1.Deployment, error) {
	d := &appsv1.Deployment{}
	key := client.ObjectKey{Name: deploymentName, Namespace: deploymentNS}

	if err := c.Get(context.TODO(), key, d); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return d, nil
}

// GetRecommendedInstallerLabelsFromCr returns the recommended labels to set on AAQ resources
func GetRecommendedInstallerLabelsFromCr(cr *aaqv1alpha1.AAQ) map[string]string {
	labels := map[string]string{}

	// In non-standalone installs, we fetch labels that were set on the AAQ CR by the installer
	for k, v := range cr.GetLabels() {
		if k == AppKubernetesPartOfLabel || k == AppKubernetesVersionLabel {
			labels[k] = v
		}
	}

	return labels
}

// SetRecommendedLabels sets the recommended labels on AAQ resources (does not get rid of existing ones)
func SetRecommendedLabels(obj metav1.Object, installerLabels map[string]string, controllerName string) {
	staticLabels := map[string]string{
		AppKubernetesManagedByLabel: controllerName,
		AppKubernetesComponentLabel: "multi-tenant",
	}

	// Merge static & existing labels
	mergedLabels := MergeLabels(staticLabels, obj.GetLabels())
	// Add installer dynamic labels as well (/version, /part-of)
	mergedLabels = MergeLabels(installerLabels, mergedLabels)

	obj.SetLabels(mergedLabels)
}
