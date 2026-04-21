package main

import (
	"fmt"
	"os"

	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"

	aaq_controller "kubevirt.io/application-aware-quota/pkg/monitoring/metrics/aaq-controller"
	"kubevirt.io/application-aware-quota/pkg/monitoring/rules"

	"github.com/rhobs/operator-observability-toolkit/pkg/docs"
)

const tpl = `# Application Aware Quota metrics

{{- range . }}

{{ $deprecatedVersion := "" -}}
{{- with index .ExtraFields "DeprecatedVersion" -}}
    {{- $deprecatedVersion = printf " in %s" . -}}
{{- end -}}

{{- $stabilityLevel := "" -}}
{{- if and (.ExtraFields.StabilityLevel) (ne .ExtraFields.StabilityLevel "STABLE") -}}
	{{- $stabilityLevel = printf "[%s%s] " .ExtraFields.StabilityLevel $deprecatedVersion -}}
{{- end -}}

### {{ .Name }}
{{ print $stabilityLevel }}{{ .Help }} Type: {{ .Type -}}.

{{- end }}

## Developing new metrics

All metrics documented here are auto-generated and reflect exactly what is being
exposed. After developing new metrics or changing old ones please regenerate
this document.
`

func main() {
	if err := aaq_controller.SetupMetrics(nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register metrics: %v\n", err)
		os.Exit(1)
	}
	if err := rules.SetupRules(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register rules: %v\n", err)
		os.Exit(1)
	}

	metricsList := operatormetrics.ListMetrics()
	rulesList := rules.ListRecordingRules()

	docsString := docs.BuildMetricsDocsWithCustomTemplate(metricsList, rulesList, tpl)
	fmt.Print(docsString)
}
