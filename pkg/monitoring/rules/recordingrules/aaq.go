package recordingrules

import (
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"
	"github.com/rhobs/operator-observability-toolkit/pkg/operatorrules"
)

// aaqRecordingRules contains recording rules owned by AAQ.
var aaqRecordingRules = []operatorrules.RecordingRule{
	{
		MetricsOpts: operatormetrics.MetricOpts{
			Name: "kube_application_aware_resourcequota_creation_timestamp",
			Help: "[Deprecated] Backwards compatible alias for kube_application_aware_resourcequota_creation_timestamp_seconds",
		},
		MetricType: operatormetrics.GaugeType,
		Expr:       intstr.FromString("kube_application_aware_resourcequota_creation_timestamp_seconds"),
	},
}
