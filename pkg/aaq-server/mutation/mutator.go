package mutation

import (
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
)

const (
	allowRequest = "Pod has successfully gated"
)

type Mutator struct {
	Request *admissionv1.AdmissionRequest
}

func (v Mutator) Mutate() (*admissionv1.AdmissionReview, error) {
	switch v.Request.Kind.Kind {
	case "Pod":
		return v.mutatePod()
	}
	return nil, fmt.Errorf("AAQ webhook doesn't recongnize request: %+v", v.Request)
}

func (v Mutator) mutatePod() (*admissionv1.AdmissionReview, error) {
	patch := `[{"op": "add", "path": "/spec/schedulingGates", "value": [{"name": "ApplicationsAwareQuotaGate"}]}]`
	return reviewResponse(v.Request.UID, true, http.StatusAccepted, allowRequest, []byte(patch)), nil
}

func reviewResponse(uid types.UID, allowed bool, httpCode int32,
	reason string, patch []byte) *admissionv1.AdmissionReview {
	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},

		Response: &admissionv1.AdmissionResponse{
			PatchType: &patchType,
			Patch:     patch,
			UID:       uid,
			Allowed:   allowed,
			Result: &metav1.Status{
				Code:    httpCode,
				Message: reason,
			},
		},
	}
}
