package patch

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

const (
	PatchReplaceOp = "replace"
	PatchTestOp    = "test"
	PatchAddOp     = "add"
	PatchRemoveOp  = "remove"
)

func GeneratePatchPayload(patches ...PatchOperation) ([]byte, error) {
	if len(patches) == 0 {
		return nil, fmt.Errorf("list of patches is empty")
	}

	payloadBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}

	return payloadBytes, nil
}

func GenerateTestReplacePatch(path string, oldValue, newValue interface{}) ([]byte, error) {
	return GeneratePatchPayload(
		PatchOperation{
			Op:    PatchTestOp,
			Path:  path,
			Value: oldValue,
		},
		PatchOperation{
			Op:    PatchReplaceOp,
			Path:  path,
			Value: newValue,
		},
	)
}

func UnmarshalPatch(patch []byte) ([]PatchOperation, error) {
	var p []PatchOperation
	err := json.Unmarshal(patch, &p)

	return p, err
}

func EscapeJSONPointer(ptr string) string {
	s := strings.ReplaceAll(ptr, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}
