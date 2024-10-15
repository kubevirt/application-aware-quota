package built_in_usage_calculators

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

type MetadataFilterCalculator struct {
	Labels      map[string]string
	Annotations map[string]string
	MyConfigs   []v1alpha1.VmiCalcConfigName
	CalcConfig  v1alpha1.VmiCalcConfigName
}

func NewMetadataFilterCalculator(labels, annotations map[string]string, myConfigs []v1alpha1.VmiCalcConfigName, calcConfig v1alpha1.VmiCalcConfigName) *MetadataFilterCalculator {
	return &MetadataFilterCalculator{
		Labels:      labels,
		Annotations: annotations,
		MyConfigs:   myConfigs,
		CalcConfig:  calcConfig,
	}
}

var cdiLabels = map[string]string{"app": "containerized-data-importer"}

var cdiConfigs = []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources, v1alpha1.DedicatedVirtualResources}

func NewCDIFilterCalculator(calcConfig v1alpha1.VmiCalcConfigName) *MetadataFilterCalculator {
	return &MetadataFilterCalculator{
		Labels:     cdiLabels,
		MyConfigs:  cdiConfigs,
		CalcConfig: calcConfig,
	}
}

func (filtercalc *MetadataFilterCalculator) PodUsageFunc(pod *corev1.Pod, _ []*corev1.Pod) (corev1.ResourceList, error, bool) {
	if !hasMatchingKeyAndValue(filtercalc.Labels, pod.Labels) &&
		!hasMatchingKeyAndValue(filtercalc.Annotations, pod.Annotations) {
		return corev1.ResourceList{}, nil, false
	}

	if !validConfig(filtercalc.CalcConfig, filtercalc.MyConfigs) {
		podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
		rl, err := podEvaluator.Usage(pod)
		if err != nil {
			return corev1.ResourceList{}, err, true
		}
		return rl, nil, true
	}

	// got this far so filtering the Pod
	return corev1.ResourceList{}, nil, true
}

func hasMatchingKeyAndValue(desired, actual map[string]string) bool {
	for key, value := range desired {
		actualValue, hasKey := actual[key]
		if hasKey && actualValue == value {
			return true
		}
	}
	return false
}
