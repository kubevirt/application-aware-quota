package main

import (
	flag "github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	"kubevirt.io/application-aware-quota-api/libsidecar"
)

const (
	// Double Calculate double amount of usage for pod
	Double configName = "double"
	// Triple Calculate triple amount of usage for pod
	Triple        configName = "triple"
	labelAppLabel            = "label-app"
)

type configName string

type labelCalculator struct {
	evaluatorConfig configName
}

var _ = libsidecar.SidecarCalculator(&labelCalculator{})

func (lc *labelCalculator) PodUsageFunc(podToEvaluate *corev1.Pod, _ []*corev1.Pod) (corev1.ResourceList, bool, error) {
	if !core.QuotaV1Pod(podToEvaluate, clock.RealClock{}) {
		return corev1.ResourceList{}, false, nil
	}
	for key := range podToEvaluate.Labels {
		if key == labelAppLabel {
			podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
			r, err := podEvaluator.Usage(podToEvaluate)
			if err != nil {
				return corev1.ResourceList{}, true, nil
			}
			switch lc.evaluatorConfig {
			case Double:
				return v12.Add(r, r), true, nil
			case Triple:
				return v12.Add(r, v12.Add(r, r)), true, nil
			}
		}
	}
	return corev1.ResourceList{}, false, nil
}

func main() {
	var evaluatorConfig string
	flag.StringVar(&evaluatorConfig, "config", "", "Config to configure Evaluator resource allocation for label apps pods")
	flag.Parse()
	libsidecar.RunServer(&labelCalculator{configName(evaluatorConfig)})
}
