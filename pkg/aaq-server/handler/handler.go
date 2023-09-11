package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"net/http"
	"strings"
)

const (
	allowPodRequest               = "Pod has successfully gated"
	allowArqRequest               = "ApplicationResourceQuota request is valid"
	validatingResourceQuotaPrefix = "arq-validating-rq-"
)

type Handler struct {
	request *admissionv1.AdmissionRequest
	aaqCli  client.AAQClient
}

func NewHandler(Request *admissionv1.AdmissionRequest, aaqCli client.AAQClient) *Handler {
	return &Handler{
		request: Request,
		aaqCli:  aaqCli,
	}
}

func (v Handler) Handle() (*admissionv1.AdmissionReview, error) {
	switch v.request.Kind.Kind {
	case "Pod":
		return v.mutatePod()
	case "ApplicationsResourceQuota":
		return v.validateApplicationsResourceQuota()
	}
	return nil, fmt.Errorf("AAQ webhook doesn't recongnize request: %+v", v.request)
}

func (v Handler) mutatePod() (*admissionv1.AdmissionReview, error) { //todo: check that we don't replace all schedulingGates with our's
	patch := `[{"op": "add", "path": "/spec/schedulingGates", "value": [{"name": "ApplicationsAwareQuotaGate"}]}]`
	return reviewResponseWithPatch(v.request.UID, true, http.StatusAccepted, allowPodRequest, []byte(patch)), nil
}

func reviewResponseWithPatch(uid types.UID, allowed bool, httpCode int32,
	reason string, patch []byte) *admissionv1.AdmissionReview {
	rr := reviewResponse(uid, allowed, httpCode, reason)
	patchType := admissionv1.PatchTypeJSONPatch
	rr.Response.PatchType = &patchType
	rr.Response.Patch = patch
	return rr
}

func reviewResponse(uid types.UID, allowed bool, httpCode int32,
	reason string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: allowed,
			Result: &metav1.Status{
				Code:    httpCode,
				Message: reason,
			},
		},
	}
}

func (v Handler) validateApplicationsResourceQuota() (*admissionv1.AdmissionReview, error) {
	arq := v1alpha1.ApplicationsResourceQuota{}
	if err := json.Unmarshal(v.request.Object.Raw, &arq); err != nil {
		return nil, err
	}
	rq := &v1.ResourceQuota{}
	rq.Namespace = arq.Namespace
	rq.Name = createRQName()
	rq.Spec.Hard = arq.Spec.Hard
	//todo: make sure we don't let users define services and pvcs in quota spec
	_, err := v.aaqCli.CoreV1().ResourceQuotas(arq.Namespace).Create(context.Background(), rq, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return reviewResponse(v.request.UID, false, http.StatusForbidden, ignoreRqErr(err.Error())), nil
	}
	return reviewResponse(v.request.UID, true, http.StatusAccepted, allowArqRequest), nil

}

func createRQName() string {
	suffix := make([]byte, 10)
	rand.Read(suffix)
	return fmt.Sprintf("%s%x", validatingResourceQuotaPrefix, suffix)
}

func ignoreRqErr(err string) string {
	return strings.TrimPrefix(err, strings.Split(err, ":")[0]+": ")
}
