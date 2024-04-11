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
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/pkg/util/patch"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"net/http"
)

const (
	allowPodRequest               = "Pod has successfully gated"
	allowArqRequest               = "ApplicationAwareResourceQuota request is valid"
	allowAcrqRequest              = "ApplicationAwareClusterResourceQuota request is valid"
	validatingResourceQuotaPrefix = "aaq-validating-rq-"
	validPodUpdate                = "Pod update did not remove AAQGate"
	aaqControllerPodUpdate        = "AAQ controller has permission to remove gate from pods"
	invalidPodUpdate              = "Only AAQ controller has permission to remove " + util.AAQGate + " gate from pods"
	onlySingleAAQInstaceIsAllowed = "only a single AAQ CR instance is allowed"
)

type Handler struct {
	request       *admissionv1.AdmissionRequest
	aaqCli        client.AAQClient
	aaqNS         string
	isOnOpenshift bool
}

func NewHandler(Request *admissionv1.AdmissionRequest, aaqCli client.AAQClient, aaqNS string, isOnOpenshift bool) *Handler {
	return &Handler{
		request:       Request,
		aaqCli:        aaqCli,
		aaqNS:         aaqNS,
		isOnOpenshift: isOnOpenshift,
	}
}

func (v Handler) Handle() (*admissionv1.AdmissionReview, error) {
	if v.shouldMutate() {
		return v.mutatePod()
	}

	switch v.request.Kind.Kind {
	case "Pod":
		return v.validatePodUpdate()
	case "ApplicationAwareResourceQuota":
		return v.validateApplicationAwareResourceQuota()
	case "ApplicationAwareClusterResourceQuota":
		return v.validateApplicationAwareClusterResourceQuota()
	case "AAQ":
		return reviewResponse(v.request.UID, false, http.StatusForbidden, onlySingleAAQInstaceIsAllowed), nil
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

	patches := []patch.PatchOperation{
		{
			Op:    patch.PatchAddOp,
			Path:  "/spec/schedulingGates",
			Value: schedulingGates,
		},
	}

	patches = append(patches, generatePatchesForPodWithNodeName(pod)...)

	patchBytes, err := patch.GeneratePatchPayload(patches...)
	if err != nil {
		return nil, err
	}

	return reviewResponseWithPatch(v.request.UID, true, http.StatusAccepted, allowPodRequest, patchBytes), nil
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

func (v Handler) validateApplicationAwareResourceQuota() (*admissionv1.AdmissionReview, error) {
	arq := v1alpha1.ApplicationAwareResourceQuota{}
	if err := json.Unmarshal(v.request.Object.Raw, &arq); err != nil {
		return nil, err
	}
	rq := &v1.ResourceQuota{}
	rq.Namespace = arq.Namespace
	rq.Name = createRQName()
	rq.Spec.Hard = arq.Spec.Hard
	rq.Spec.ScopeSelector = arq.Spec.ScopeSelector
	rq.Spec.Scopes = arq.Spec.Scopes
	_, err := v.aaqCli.CoreV1().ResourceQuotas(arq.Namespace).Create(context.Background(), rq, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return reviewResponse(v.request.UID, false, http.StatusForbidden, util.IgnoreRqErr(err.Error())), nil
	}
	return reviewResponse(v.request.UID, true, http.StatusAccepted, allowArqRequest), nil

}

func (v Handler) validateApplicationAwareClusterResourceQuota() (*admissionv1.AdmissionReview, error) {
	acrq := v1alpha1.ApplicationAwareClusterResourceQuota{}
	if err := json.Unmarshal(v.request.Object.Raw, &acrq); err != nil {
		return nil, err
	}
	rq := &v1.ResourceQuota{}
	rq.Name = createRQName()
	rq.Spec.Hard = acrq.Spec.Quota.Hard
	rq.Spec.ScopeSelector = acrq.Spec.Quota.ScopeSelector
	rq.Spec.Scopes = acrq.Spec.Quota.Scopes
	_, err := v.aaqCli.CoreV1().ResourceQuotas(v1.NamespaceDefault).Create(context.Background(), rq, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return reviewResponse(v.request.UID, false, http.StatusForbidden, util.IgnoreRqErr(err.Error())), nil
	}
	nonSchedulableResourcesHard := util.FilterNonScheduableResources(acrq.Spec.Quota.Hard)
	if !v.isOnOpenshift && len(nonSchedulableResourcesHard) > 0 {
		errMsg := fmt.Sprintf("ApplicationAwareClusterResourceQuota without clusterResourceQuota support, operator cannot handle non scheduable resources :%v", getResourcesNames(nonSchedulableResourcesHard))
		return reviewResponse(v.request.UID, false, http.StatusForbidden, errMsg), nil
	}
	return reviewResponse(v.request.UID, true, http.StatusAccepted, allowAcrqRequest), nil
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

func isAAQControllerServiceAccount(serviceAccount string, aaqNS string) bool {
	prefix := fmt.Sprintf("system:serviceaccount:%s", aaqNS)
	return serviceAccount == fmt.Sprintf("%s:%s", prefix, util.ControllerResourceName)
}

func getResourcesNames(resourceList v1.ResourceList) []v1.ResourceName {
	keys := make([]v1.ResourceName, 0, len(resourceList))
	for key := range resourceList {
		keys = append(keys, key)
	}
	return keys
}

func generatePatchesForPodWithNodeName(pod v1.Pod) []patch.PatchOperation {
	if pod.Spec.NodeName == "" {
		return nil
	}

	affinity := pod.Spec.Affinity
	affinityPatchOp := patch.PatchReplaceOp

	if affinity == nil {
		affinity = &v1.Affinity{}
		affinityPatchOp = patch.PatchAddOp
	}
	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &v1.NodeAffinity{}
	}
	if affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &v1.NodeSelector{}
	}
	affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms,
		v1.NodeSelectorTerm{
			MatchFields: []v1.NodeSelectorRequirement{
				{
					Key:      "metadata.name",
					Operator: v1.NodeSelectorOpIn,
					Values:   []string{pod.Spec.NodeName},
				},
			},
		},
	)

	tolerations := pod.Spec.Tolerations
	if tolerations == nil {
		tolerations = []v1.Toleration{}
	}
	tolerations = append(tolerations, v1.Toleration{
		Key:      "", // i.e. any key
		Operator: v1.TolerationOpExists,
		Effect:   v1.TaintEffectNoSchedule,
	})

	return []patch.PatchOperation{
		{
			Op:    affinityPatchOp,
			Path:  "/spec/affinity",
			Value: affinity,
		},
		{
			Op:    patch.PatchReplaceOp,
			Path:  "/spec/nodeName",
			Value: "",
		},
		{
			Op:    patch.PatchAddOp,
			Path:  "/spec/tolerations",
			Value: tolerations,
		},
	}
}
