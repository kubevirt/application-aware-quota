package util

import (
	"context"
	"crypto/tls"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"
	api "k8s.io/kubernetes/pkg/apis/core"
	k8s_api_v1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/utils/pointer"
	v1 "kubevirt.io/api/core/v1"
	aaqv1alpha1 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	utils "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/resources"
	"os"
	"runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

var (
	cipherSuites         = tls.CipherSuites()
	insecureCipherSuites = tls.InsecureCipherSuites()
)

const (
	noSrvCertMessage = "No server certificate, server is not yet ready to receive traffic"
	// Default port that api listens on.
	DefaultPort = 8443
	// Default address api listens on.
	DefaultHost  = "0.0.0.0"
	DefaultAaqNs = "aaq"
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
	// TlsLabel provides a constant to capture our env variable "TLS"
	TlsLabel = "TLS"
	// ConfigMapName is the name of the aaq configmap that own aaq resources
	ConfigMapName                                            = "aaq-config"
	OperatorServiceAccountName                               = "aaq-operator"
	AAQGate                                                  = "ApplicationsAwareQuotaGate"
	ControllerResourceName                                   = ControllerPodName
	SecretResourceName                                       = "aaq-server-cert"
	AaqServerResourceName                                    = "aaq-server"
	ControllerClusterRoleName                                = ControllerPodName
	DefaultLauncherConfig      aaqv1alpha1.VmiCalcConfigName = aaqv1alpha1.VmiPodUsage
	LauncherConfig                                           = "launcherConfig"
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

func PrintVersion() {
	klog.Infof(fmt.Sprintf("Go Version: %s", runtime.Version()))
	klog.Infof(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}
func getNamespace(path string) string {
	if data, err := os.ReadFile(path); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "aaq"
}

// GetNamespace returns the namespace the pod is executing in
func GetNamespace() string {
	return getNamespace("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
}

// TLSVersion converts from human-readable TLS version (for example "1.1")
// to the values accepted by tls.Config (for example 0x301).
func TLSVersion(version v1.TLSProtocolVersion) uint16 {
	switch version {
	case v1.VersionTLS10:
		return tls.VersionTLS10
	case v1.VersionTLS11:
		return tls.VersionTLS11
	case v1.VersionTLS12:
		return tls.VersionTLS12
	case v1.VersionTLS13:
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

func CipherSuiteNameMap() map[string]uint16 {
	var idByName = map[string]uint16{}
	for _, cipherSuite := range cipherSuites {
		idByName[cipherSuite.Name] = cipherSuite.ID
	}
	for _, cipherSuite := range insecureCipherSuites {
		idByName[cipherSuite.Name] = cipherSuite.ID
	}
	return idByName
}

func CipherSuiteIds(names []string) []uint16 {
	var idByName = CipherSuiteNameMap()
	var ids []uint16
	for _, name := range names {
		if id, ok := idByName[name]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func SetupTLS(certManager certificate.Manager) *tls.Config {
	tlsConfig := &tls.Config{
		GetCertificate: func(info *tls.ClientHelloInfo) (certificate *tls.Certificate, err error) {
			cert := certManager.Current()
			if cert == nil {
				return nil, fmt.Errorf(noSrvCertMessage)
			}
			return cert, nil
		},
		GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
			crt := certManager.Current()
			if crt == nil {
				klog.Error(noSrvCertMessage)
				return nil, fmt.Errorf(noSrvCertMessage)
			}
			tlsConfig := &v1.TLSConfiguration{ //maybe we will want to add config in AAQ CR in the future
				MinTLSVersion: v1.VersionTLS12,
				Ciphers:       nil,
			}
			ciphers := CipherSuiteIds(tlsConfig.Ciphers)
			minTLSVersion := TLSVersion(tlsConfig.MinTLSVersion)
			config := &tls.Config{
				CipherSuites: ciphers,
				MinVersion:   minTLSVersion,
				Certificates: []tls.Certificate{*crt},
				ClientAuth:   tls.VerifyClientCertIfGiven,
			}

			config.BuildNameToCertificate()
			return config, nil
		},
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig
}

// todo: ask kubernetes to make this funcs global and remove all this code:
func ToExternalPodOrError(obj k8sruntime.Object) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	switch t := obj.(type) {
	case *corev1.Pod:
		pod = t
	case *api.Pod:
		if err := k8s_api_v1.Convert_core_Pod_To_v1_Pod(t, pod, nil); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("expect *api.Pod or *v1.Pod, got %v", t)
	}
	return pod, nil
}
