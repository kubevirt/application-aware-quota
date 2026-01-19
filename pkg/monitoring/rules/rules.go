package rules

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/operator-observability-toolkit/pkg/operatorrules"

	"kubevirt.io/application-aware-quota/pkg/monitoring/rules/recordingrules"
)

const (
	aaqPrometheusRuleName = "prometheus-aaq-rules"
)

// SetupRules registers AAQ recording rules.
func SetupRules() error {
	return recordingrules.Register()
}

// BuildPrometheusRule builds the PrometheusRule object containing the registered
// recording rules and any registered alerts. It sets labels compatible with the
// cluster Prometheus selectors.
func BuildPrometheusRule(namespace string) (*promv1.PrometheusRule, error) {
	return operatorrules.BuildPrometheusRule(
		aaqPrometheusRuleName,
		namespace,
		map[string]string{
			"prometheus":      "k8s",
			"role":            "alert-rules",
			"aaq.kubevirt.io": "",
		},
	)
}

// ListRecordingRules returns the registered recording rules.
func ListRecordingRules() []operatorrules.RecordingRule {
	return operatorrules.ListRecordingRules()
}
