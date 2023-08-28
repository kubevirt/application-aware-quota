package tests

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
)

func getCurrLauncherUsage(podToCreate *v1.Pod) (v1.ResourceList, error) {
	podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
	usage, err := podEvaluator.Usage(podToCreate)
	if err != nil {
		return usage, err
	}
	return usage, nil
}
