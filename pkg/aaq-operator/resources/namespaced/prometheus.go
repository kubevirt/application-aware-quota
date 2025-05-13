package namespaced

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"kubevirt.io/application-aware-quota/pkg/util"
	"os"
)

const (
	rbacName            = "aaq-monitoring"
	monitorName         = "service-monitor-aaq"
	defaultMonitoringNs = "monitoring"
)

func getPrometheusNamespacedRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"services",
				"endpoints",
				"pods",
			},
			Verbs: []string{
				"get", "list", "watch",
			},
		},
	}
}

func NewPrometheusServiceMonitor(namespace string) *promv1.ServiceMonitor {
	return &promv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      monitorName,
			Labels: map[string]string{
				util.AAQLabel:                     "",
				"openshift.io/cluster-monitoring": "",
				util.PrometheusLabelKey:           util.PrometheusLabelValue,
			},
		},
		Spec: promv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					util.PrometheusLabelKey: util.PrometheusLabelValue,
				},
			},
			NamespaceSelector: promv1.NamespaceSelector{
				MatchNames: []string{namespace},
			},
			Endpoints: []promv1.Endpoint{
				{
					HonorLabels: true,
					Port:        "metrics",
					Scheme:      "https",
					TLSConfig: &promv1.TLSConfig{
						SafeTLSConfig: promv1.SafeTLSConfig{
							InsecureSkipVerify: ptr.To(true),
						},
					},
				},
			},
		},
	}
}

func NewPrometheusRole(namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbacName,
			Namespace: namespace,
			Labels: map[string]string{
				util.AAQLabel:           "",
				util.PrometheusLabelKey: util.PrometheusLabelValue,
			},
		},
		Rules: getPrometheusNamespacedRules(),
	}
}

func NewPrometheusRoleBinding(namespace string) *rbacv1.RoleBinding {
	monitoringNamespace := getMonitoringNamespace()

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbacName,
			Namespace: namespace,
			Labels: map[string]string{
				util.AAQLabel:           "",
				util.PrometheusLabelKey: util.PrometheusLabelValue,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     rbacName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: monitoringNamespace,
				Name:      "prometheus-k8s",
			},
		},
	}
}

func getMonitoringNamespace() string {
	if ns := os.Getenv("MONITORING_NAMESPACE"); ns != "" {
		return ns
	}

	return defaultMonitoringNs
}

func NewPrometheusService(namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.PrometheusServiceName,
			Namespace: namespace,
			Labels: map[string]string{
				util.AAQLabel:           "",
				util.PrometheusLabelKey: util.PrometheusLabelValue,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "metrics",
					Port: 443,
					TargetPort: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "metrics",
					},
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{util.PrometheusLabelKey: util.PrometheusLabelValue},
		},
	}
}
