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
	api "k8s.io/kubernetes/pkg/apis/core"
	k8s_api_v1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	"kubevirt.io/client-go/log"
)

// UsageCalculator knows how to evaluate quota usage for a particular app pods
type UsageCalculator interface {
	// Usage returns the resource usage for the specified object
	PodUsageFunc(item runtime.Object, items []runtime.Object, clock clock.Clock) (corev1.ResourceList, error, bool)
}

func NewAaqCalculatorsRegistry(retriesOnMatchFailure int, clock clock.Clock) *AaqCalculatorsRegistry {
	return &AaqCalculatorsRegistry{
		retriesOnMatchFailure: retriesOnMatchFailure,
		clock:                 clock,
	}
}

type AaqCalculatorsRegistry struct {
	usageCalculators []UsageCalculator
	// used to track time
	clock                 clock.Clock
	retriesOnMatchFailure int
}

func (aaqe *AaqCalculatorsRegistry) AddCalculator(usageCalculator UsageCalculator) *AaqCalculatorsRegistry {
	aaqe.usageCalculators = append(aaqe.usageCalculators, usageCalculator)
	return aaqe
}

func (aaqe *AaqCalculatorsRegistry) Usage(item runtime.Object, items []runtime.Object) (rlToRet corev1.ResourceList, acceptedErr error) {
	accepted := false
	for _, calculator := range aaqe.usageCalculators {
		for retries := 0; retries < aaqe.retriesOnMatchFailure; retries++ {
			rl, err, match := calculator.PodUsageFunc(item, items, aaqe.clock)
			if !match && err == nil {
				break
			} else if err == nil {
				accepted = true
				rlToRet = quota.Add(rlToRet, rl)
				break
			} else {
				log.Log.Infof(fmt.Sprintf("Retries: %v Error: %v ", retries, err))
			}
		}
	}
	if !accepted {
		acceptedErr = fmt.Errorf("pod didn't match any usageFunc")
	}
	return rlToRet, acceptedErr
}

// NewAaqEvaluator returns an evaluator that can evaluate pods with apps consideration
func NewAaqEvaluator(f v12.ListerForResourceFunc, podInformer cache.SharedIndexInformer, aaqAppUsageCalculator *AaqCalculatorsRegistry, clock clock.Clock) *AaqEvaluator {
	podEvaluator := core.NewPodEvaluator(f, clock)
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

func (aaqe *AaqEvaluator) Constraints(required []corev1.ResourceName, item runtime.Object) error {
	return aaqe.podEvaluator.Constraints(required, item)
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

func (aaqe *AaqEvaluator) Usage(item runtime.Object, items []runtime.Object) (corev1.ResourceList, error) {
	pod, err := ToExternalPodOrError(item)
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
		runtimeObjects = append(runtimeObjects, pod)
	}

	for _, item := range runtimeObjects {
		// need to verify that the item matches the set of scopes
		matchesScopes := true
		for _, scope := range options.Scopes {
			innerMatch, err := podMatchesScopeFunc(corev1.ScopedResourceSelectorRequirement{ScopeName: scope}, item)
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
			usage, err := aaqe.Usage(item, runtimeObjects)
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
	pod, err := ToExternalPodOrError(object)
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

func ToExternalPodOrError(obj runtime.Object) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	switch t := obj.(type) {
	case *corev1.Pod:
		pod = t
	case *api.Pod:
		if err := k8s_api_v1.Convert_core_Pod_To_v1_Pod(t, pod, nil); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("expect *api.Pod or *v1.Pod, got %v", t)
	}
	return pod, nil
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
