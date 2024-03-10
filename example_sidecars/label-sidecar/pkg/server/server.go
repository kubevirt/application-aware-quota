package server

import (
	"encoding/json"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
)

const (
	// Double Calculate double amount of usage for pod
	Double configName = "double"
	// Triple Calculate triple amount of usage for pod
	Triple        configName = "triple"
	labelAppLabel            = "label-app"
)

type configName string

var MyConfigs = []configName{Double, Triple}

type Server struct {
	labelCalculator *LabelCalculator
}

func (s *Server) PodUsageFunc(_ context.Context, request *PodUsageRequest) (*PodUsageResponse, error) {
	pod := &corev1.Pod{}
	var podsState []corev1.Pod
	err := json.Unmarshal(request.Pod.GetPodJson(), pod)
	if err != nil {
		return nil, err
	}
	for _, pItem := range request.GetPodsState() {
		currPod := &corev1.Pod{}
		err := json.Unmarshal(pItem.GetPodJson(), currPod)
		if err != nil {
			return nil, err
		}
		podsState = append(podsState, *currPod)
	}

	rl, err, match := s.labelCalculator.PodUsageFunc(*pod, podsState, clock.RealClock{})

	rlData, err := json.Marshal(rl)
	if err != nil {
		return nil, err
	}
	podUsageResponse := &PodUsageResponse{Error: &Error{false, ""}, Match: match, ResourceList: &ResourceList{rlData}}
	if err != nil {
		podUsageResponse.Error.Error = true
		podUsageResponse.Error.ErrorMessage = err.Error()
	}

	return podUsageResponse, nil
}

func (s *Server) HealthCheck(_ context.Context, _ *HealthCheckRequest) (*HealthCheckResponse, error) {
	return &HealthCheckResponse{true}, nil
}

func NewLabelCalculator(config string) *LabelCalculator {
	if !validConfig(config) {
		return &LabelCalculator{Double}
	}
	return &LabelCalculator{configName(config)}
}

type LabelCalculator struct {
	config configName
}

func (labelcalc *LabelCalculator) PodUsageFunc(pod corev1.Pod, _ []corev1.Pod, clock clock.Clock) (corev1.ResourceList, error, bool) {
	if !core.QuotaV1Pod(&pod, clock) {
		return corev1.ResourceList{}, nil, false
	}
	for key := range pod.Labels {
		if key == labelAppLabel {
			podEvaluator := core.NewPodEvaluator(nil, clock)
			r, err := podEvaluator.Usage(&pod)
			if err != nil {
				return corev1.ResourceList{}, err, true
			}
			switch labelcalc.config {
			case Double:
				return v12.Add(r, r), nil, true
			case Triple:
				return v12.Add(r, v12.Add(r, r)), nil, true
			}
		}
	}
	return corev1.ResourceList{}, nil, false
}

func validConfig(target string) bool {
	for _, item := range MyConfigs {
		if string(item) == target {
			return true
		}
	}
	return false
}
