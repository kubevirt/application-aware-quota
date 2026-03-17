package recordingrules

import "github.com/rhobs/operator-observability-toolkit/pkg/operatorrules"

// Register registers all AAQ recording rules.
func Register() error {
	return operatorrules.RegisterRecordingRules(
		aaqRecordingRules,
	)
}
