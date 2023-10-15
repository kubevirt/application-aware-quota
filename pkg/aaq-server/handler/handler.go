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
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"net/http"
	"strings"
)

const (
	allowPodRequest               = "Pod has successfully gated"
	allowArqRequest               = "ApplicationResourceQuota request is valid"
	validatingResourceQuotaPrefix = "arq-validating-rq-"
	validPodUpdate                = "Pod update did not remove AAQGate"
	aaqControllerPodUpdate        = "AAQ controller has permission to remove gate from pods"
	invalidPodUpdate              = "Only AAQ controller has permission to remove " + util.AAQGate + " gate from pods"
)

type Handler struct {
	request *admissionv1.AdmissionRequest
	aaqCli  kubernetes.Interface
	aaqNS   string
}

func NewHandler(Request *admissionv1.AdmissionRequest, aaqCli kubernetes.Interface, aaqNS string) *Handler {
	return &Handler{
		request: Request,
		aaqCli:  aaqCli,
		aaqNS:   aaqNS,
	}
}

func (v Handler) Handle() (*admissionv1.AdmissionReview, error) {
	if v.shouldMutate() {
		return v.mutatePod()
	}

	switch v.request.Kind.Kind {
	case "Pod":
		return v.validatePodUpdate()
	case "ApplicationsResourceQuota":
		return v.validateApplicationsResourceQuota()
	}
	return nil, fmt.Errorf("AAQ webhook doesn't recongnize request: %+v", v.request)
}

func (v Handler) shouldMutate() bool {
	return v.request.Kind.Kind == "Pod" && v.request.Operation == admissionv1.Create
}

func (v Handler) mutatePod() (*admissionv1.AdmissionReview, error) {
	pod := v1.Pod{}
	if err := json.Unmarshal(v.request.Object.Raw, &pod); err != nil {
		return nil, err
	}
	schedulingGates := pod.Spec.SchedulingGates
	if schedulingGates == nil {
		schedulingGates = []v1.PodSchedulingGate{}
	}
	schedulingGates = append(schedulingGates, v1.PodSchedulingGate{Name: util.AAQGate})

	schedulingGatesBytes, err := json.Marshal(schedulingGates)
	if err != nil {
		return nil, err
	}

	patch := fmt.Sprintf(`[{"op": "add", "path": "/spec/schedulingGates", "value": %s}]`, string(schedulingGatesBytes))
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
	_, err := v.aaqCli.CoreV1().ResourceQuotas(arq.Namespace).Create(context.Background(), rq, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return reviewResponse(v.request.UID, false, http.StatusForbidden, ignoreRqErr(err.Error())), nil
	}
	return reviewResponse(v.request.UID, true, http.StatusAccepted, allowArqRequest), nil

}

func (v Handler) validatePodUpdate() (*admissionv1.AdmissionReview, error) {
	oldPod := v1.Pod{}
	if err := json.Unmarshal(v.request.OldObject.Raw, &oldPod); err != nil {
		return nil, err
	}

	if !hasAAQGate(oldPod.Spec.SchedulingGates) {
		return reviewResponse(v.request.UID, true, http.StatusAccepted, validPodUpdate), nil
	}

	currentPod := v1.Pod{}
	if err := json.Unmarshal(v.request.Object.Raw, &currentPod); err != nil {
		return nil, err
	}

	if hasAAQGate(currentPod.Spec.SchedulingGates) {
		return reviewResponse(v.request.UID, true, http.StatusAccepted, validPodUpdate), nil
	}

	if isAAQControllerServiceAccount(v.request.UserInfo.Username, v.aaqNS) {
		return reviewResponse(v.request.UID, true, http.StatusAccepted, aaqControllerPodUpdate), nil
	}

	return reviewResponse(v.request.UID, false, http.StatusForbidden, invalidPodUpdate), nil

}

func hasAAQGate(psgs []v1.PodSchedulingGate) bool {
	if psgs == nil {
		return false
	}
	for _, sg := range psgs {
		if sg.Name == util.AAQGate {
			return true
		}
	}
	return false
}

func createRQName() string {
	suffix := make([]byte, 10)
	rand.Read(suffix)
	return fmt.Sprintf("%s%x", validatingResourceQuotaPrefix, suffix)
}

func ignoreRqErr(err string) string {
	return strings.TrimPrefix(err, strings.Split(err, ":")[0]+": ")
}

func isAAQControllerServiceAccount(serviceAccount string, aaqNS string) bool {
	prefix := fmt.Sprintf("system:serviceaccount:%s", aaqNS)
	return serviceAccount == fmt.Sprintf("%s:%s", prefix, util.ControllerResourceName)
}
