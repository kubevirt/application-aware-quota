package aaq_evaluator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	quota "k8s.io/apiserver/pkg/quota/v1"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	"kubevirt.io/applications-aware-quota/pkg/log"
	"kubevirt.io/applications-aware-quota/pkg/util"
)

// NewAaqEvaluator returns an evaluator that can evaluate pods with apps consideration
func NewAaqEvaluator(podInformer cache.SharedIndexInformer, aaqAppUsageCalculator *AaqCalculatorsRegistry, clock clock.Clock) *AaqEvaluator {
	podEvaluator := core.NewPodEvaluator(nil, clock)
	return &AaqEvaluator{
		podEvaluator:                  podEvaluator,
		podInformer:                   podInformer,
		aaqAppUsageCalculatorRegistry: aaqAppUsageCalculator,
	}
}

type AaqEvaluator struct {
	podEvaluator                  v12.Evaluator
	aaqAppUsageCalculatorRegistry *AaqCalculatorsRegistry
	// knows how to list pods
	podInformer cache.SharedIndexInformer
}

func (aaqe *AaqEvaluator) Constraints(_ []corev1.ResourceName, _ runtime.Object) error {
	//let's not repeat kubernetes mistake: https://github.com/kubernetes/kubernetes/blob/46835f8792dfb4a17345e592d1325bf63bc054e4/pkg/quota/v1/evaluator/core/pods.go#L125
	return nil
}

func (aaqe *AaqEvaluator) GroupResource() schema.GroupResource {
	return aaqe.podEvaluator.GroupResource()
}

func (aaqe *AaqEvaluator) Handles(operation admission.Attributes) bool {
	return aaqe.podEvaluator.Handles(operation)
}

func (aaqe *AaqEvaluator) Matches(resourceQuota *corev1.ResourceQuota, item runtime.Object) (bool, error) {
	return aaqe.podEvaluator.Matches(resourceQuota, item)
}

func (aaqe *AaqEvaluator) MatchingScopes(item runtime.Object, scopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	return aaqe.podEvaluator.MatchingScopes(item, scopes)
}

func (aaqe *AaqEvaluator) UncoveredQuotaScopes(limitedScopes []corev1.ScopedResourceSelectorRequirement, matchedQuotaScopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	return aaqe.podEvaluator.UncoveredQuotaScopes(limitedScopes, matchedQuotaScopes)
}

func (aaqe *AaqEvaluator) MatchingResources(input []corev1.ResourceName) []corev1.ResourceName {
	return aaqe.podEvaluator.MatchingResources(input)
}

func (aaqe *AaqEvaluator) Usage(item runtime.Object) (corev1.ResourceList, error) {
	pod, err := util.ToExternalPodOrError(item)
	if err != nil {
		return corev1.ResourceList{}, err
	} else if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) > 0 {
		return corev1.ResourceList{}, nil
	}
	podObjs, err := aaqe.podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, pod.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list content: %v", err)
	}
	var runtimeObjects []runtime.Object
	for _, obj := range podObjs {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			log.Log.Infof("Failed to type assert to *v1.Pod")
			continue
		}
		runtimeObjects = append(runtimeObjects, pod.DeepCopy())
	}

	rl, err := aaqe.aaqAppUsageCalculatorRegistry.Usage(item, runtimeObjects)
	if err != nil {
		return aaqe.podEvaluator.Usage(item)
	}
	return rl, err
}

func (aaqe *AaqEvaluator) CalculatorUsage(item runtime.Object, items []runtime.Object) (corev1.ResourceList, error) {
	pod, err := util.ToExternalPodOrError(item)
	if err != nil {
		return corev1.ResourceList{}, err
	} else if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) > 0 {
		return corev1.ResourceList{}, nil
	}

	rl, err := aaqe.aaqAppUsageCalculatorRegistry.Usage(item, items)
	if err != nil {
		return aaqe.podEvaluator.Usage(item)
	}
	return rl, err
}

// UsageStats calculates aggregate usage for the object.
func (aaqe *AaqEvaluator) UsageStats(options v12.UsageStatsOptions) (v12.UsageStats, error) {
	result := quota.UsageStats{Used: corev1.ResourceList{}}
	for _, resourceName := range options.Resources {
		result.Used[resourceName] = resource.Quantity{Format: resource.DecimalSI}
	}
	arqObjs, err := aaqe.podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, options.Namespace)
	if err != nil {
		return result, fmt.Errorf("failed to list content: %v", err)
	}
	var runtimeObjects []runtime.Object
	for _, obj := range arqObjs {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			fmt.Printf("Failed to type assert to *v1.Pod\n")
			continue
		}
		runtimeObjects = append(runtimeObjects, pod.DeepCopy())
	}

	for _, item := range runtimeObjects {
		// need to verify that the item matches the set of scopes
		matchesScopes := true
		for _, scope := range options.Scopes {
			innerMatch, err := podMatchesScopeFunc(corev1.ScopedResourceSelectorRequirement{ScopeName: scope, Operator: corev1.ScopeSelectorOpExists}, item)
			if err != nil {
				return result, nil
			}
			if !innerMatch {
				matchesScopes = false
			}
		}
		if options.ScopeSelector != nil {
			for _, selector := range options.ScopeSelector.MatchExpressions {
				innerMatch, err := podMatchesScopeFunc(selector, item)
				if err != nil {
					return result, nil
				}
				matchesScopes = matchesScopes && innerMatch
			}
		}
		// only count usage if there was a match
		if matchesScopes {
			usage, err := aaqe.CalculatorUsage(item, runtimeObjects)
			if err != nil {
				return result, err
			}
			result.Used = quota.Add(result.Used, usage)
		}
	}
	return result, nil
}

// todo: ask kubernetes to make this funcs global and remove all this code
// podMatchesScopeFunc is a function that knows how to evaluate if a pod matches a scope
func podMatchesScopeFunc(selector corev1.ScopedResourceSelectorRequirement, object runtime.Object) (bool, error) {
	pod, err := util.ToExternalPodOrError(object)
	if err != nil {
		return false, err
	}
	switch selector.ScopeName {
	case corev1.ResourceQuotaScopeTerminating:
		return isTerminating(pod), nil
	case corev1.ResourceQuotaScopeNotTerminating:
		return !isTerminating(pod), nil
	case corev1.ResourceQuotaScopeBestEffort:
		return isBestEffort(pod), nil
	case corev1.ResourceQuotaScopeNotBestEffort:
		return !isBestEffort(pod), nil
	case corev1.ResourceQuotaScopePriorityClass:
		if selector.Operator == corev1.ScopeSelectorOpExists {
			// This is just checking for existence of a priorityClass on the pod,
			// no need to take the overhead of selector parsing/evaluation.
			return len(pod.Spec.PriorityClassName) != 0, nil
		}
		return podMatchesSelector(pod, selector)
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return usesCrossNamespacePodAffinity(pod), nil
	}
	return false, nil
}

func isTerminating(pod *corev1.Pod) bool {
	if pod.Spec.ActiveDeadlineSeconds != nil && *pod.Spec.ActiveDeadlineSeconds >= int64(0) {
		return true
	}
	return false
}

func isBestEffort(pod *corev1.Pod) bool {
	return qos.GetPodQOS(pod) == corev1.PodQOSBestEffort
}

func podMatchesSelector(pod *corev1.Pod, selector corev1.ScopedResourceSelectorRequirement) (bool, error) {
	labelSelector, err := helper.ScopedResourceSelectorRequirementsAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("failed to parse and convert selector: %v", err)
	}
	var m map[string]string
	if len(pod.Spec.PriorityClassName) != 0 {
		m = map[string]string{string(corev1.ResourceQuotaScopePriorityClass): pod.Spec.PriorityClassName}
	}
	if labelSelector.Matches(labels.Set(m)) {
		return true, nil
	}
	return false, nil
}

func usesCrossNamespacePodAffinity(pod *corev1.Pod) bool {
	if pod == nil || pod.Spec.Affinity == nil {
		return false
	}

	affinity := pod.Spec.Affinity.PodAffinity
	if affinity != nil {
		if crossNamespacePodAffinityTerms(affinity.RequiredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
		if crossNamespaceWeightedPodAffinityTerms(affinity.PreferredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
	}

	antiAffinity := pod.Spec.Affinity.PodAntiAffinity
	if antiAffinity != nil {
		if crossNamespacePodAffinityTerms(antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
		if crossNamespaceWeightedPodAffinityTerms(antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
			return true
		}
	}

	return false
}

func crossNamespacePodAffinityTerms(terms []corev1.PodAffinityTerm) bool {
	for _, t := range terms {
		if crossNamespacePodAffinityTerm(&t) {
			return true
		}
	}
	return false
}

func crossNamespacePodAffinityTerm(term *corev1.PodAffinityTerm) bool {
	return len(term.Namespaces) != 0 || term.NamespaceSelector != nil
}

func crossNamespaceWeightedPodAffinityTerms(terms []corev1.WeightedPodAffinityTerm) bool {
	for _, t := range terms {
		if crossNamespacePodAffinityTerm(&t.PodAffinityTerm) {
			return true
		}
	}
	return false
}
