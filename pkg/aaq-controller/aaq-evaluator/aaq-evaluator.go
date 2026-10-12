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
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// NewAaqEvaluator returns an evaluator that can evaluate pods with apps consideration
func NewAaqEvaluator(podLister v1.PodLister, aaqEvalRegistery Registry, clock clock.Clock) *AaqEvaluator {
	podEvaluator := core.NewPodEvaluator(nil, clock)
	return &AaqEvaluator{
		podEvaluator:     podEvaluator,
		podLister:        podLister,
		aaqEvalRegistery: aaqEvalRegistery,
	}
}

type AaqEvaluator struct {
	podEvaluator     v12.Evaluator
	aaqEvalRegistery Registry
	podLister        v1.PodLister
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
	matchResource := len(aaqe.MatchingResources(quota.ResourceNames(resourceQuota.Status.Hard))) > 0
	matchScope := true
	for _, scope := range getScopeSelectorsFromQuota(resourceQuota) {
		innerMatch, err := aaqe.podMatchesScopeFunc(scope, item)
		if err != nil {
			return false, err
		}
		matchScope = matchScope && innerMatch
	}
	return matchResource && matchScope, nil
}

func getScopeSelectorsFromQuota(rq *corev1.ResourceQuota) []corev1.ScopedResourceSelectorRequirement {
	var selectors []corev1.ScopedResourceSelectorRequirement
	for _, scope := range rq.Spec.Scopes {
		selectors = append(selectors, corev1.ScopedResourceSelectorRequirement{
			ScopeName: scope, Operator: corev1.ScopeSelectorOpExists})
	}
	if rq.Spec.ScopeSelector != nil {
		selectors = append(selectors, rq.Spec.ScopeSelector.MatchExpressions...)
	}
	return selectors
}

func (aaqe *AaqEvaluator) MatchingScopes(item runtime.Object, scopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	var matched []corev1.ScopedResourceSelectorRequirement
	for _, scope := range scopes {
		innerMatch, err := aaqe.podMatchesScopeFunc(scope, item)
		if err != nil {
			return nil, err
		}
		if innerMatch {
			matched = append(matched, scope)
		}
	}
	return matched, nil
}

func (aaqe *AaqEvaluator) UncoveredQuotaScopes(limitedScopes []corev1.ScopedResourceSelectorRequirement, matchedQuotaScopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	return aaqe.podEvaluator.UncoveredQuotaScopes(limitedScopes, matchedQuotaScopes)
}

func (aaqe *AaqEvaluator) MatchingResources(input []corev1.ResourceName) []corev1.ResourceName {
	return input
}

func (aaqe *AaqEvaluator) SourceCalculatorUsage(pod *corev1.Pod, existingPods []*corev1.Pod) (corev1.ResourceList, error) {
	if len(pod.Spec.SchedulingGates) > 0 {
		return corev1.ResourceList{}, nil
	}
	rl, err := aaqe.aaqEvalRegistery.SourceUsage(pod, existingPods)
	if err != nil {
		return aaqe.podEvaluator.Usage(pod)
	}
	return rl, err
}

func (aaqe *AaqEvaluator) Usage(item runtime.Object) (corev1.ResourceList, error) {
	pod, err := util.ToExternalPodOrError(item)
	if err != nil {
		return corev1.ResourceList{}, err
	} else if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) > 0 {
		return corev1.ResourceList{}, nil
	}
	existingPods, err := aaqe.podLister.Pods(pod.Namespace).List(labels.Everything())
	if err != nil {
		return corev1.ResourceList{}, fmt.Errorf("failed to list content: %v", err)
	}
	rl, err := aaqe.aaqEvalRegistery.Usage(pod, existingPods)
	if err != nil {
		return aaqe.podEvaluator.Usage(item)
	}
	return rl, err
}

func (aaqe *AaqEvaluator) CalculatorUsage(pod *corev1.Pod, existingPods []*corev1.Pod) (corev1.ResourceList, error) {
	if pod.Spec.SchedulingGates != nil &&
		len(pod.Spec.SchedulingGates) > 0 {
		return corev1.ResourceList{}, nil
	}
	rl, err := aaqe.aaqEvalRegistery.Usage(pod, existingPods)
	if err != nil {
		return aaqe.podEvaluator.Usage(pod)
	}
	return rl, err
}

// UsageStats calculates aggregate usage for the object.
func (aaqe *AaqEvaluator) UsageStats(options v12.UsageStatsOptions) (v12.UsageStats, error) {
	result := quota.UsageStats{Used: corev1.ResourceList{}}
	for _, resourceName := range options.Resources {
		result.Used[resourceName] = resource.Quantity{Format: resource.DecimalSI}
	}
	existingPods, err := aaqe.podLister.Pods(options.Namespace).List(labels.Everything())
	if err != nil {
		return result, fmt.Errorf("failed to list content: %v", err)
	}

	hasVmiScope := hasVmiScopes(options.Scopes, options.ScopeSelector)

	for _, pod := range existingPods {
		matchesScopes := true
		for _, scope := range options.Scopes {
			innerMatch, err := aaqe.podMatchesScopeFunc(corev1.ScopedResourceSelectorRequirement{ScopeName: scope, Operator: corev1.ScopeSelectorOpExists}, pod)
			if err != nil {
				return result, nil
			}
			if !innerMatch {
				matchesScopes = false
			}
		}
		if options.ScopeSelector != nil {
			for _, selector := range options.ScopeSelector.MatchExpressions {
				innerMatch, err := aaqe.podMatchesScopeFunc(selector, pod)
				if err != nil {
					return result, nil
				}
				matchesScopes = matchesScopes && innerMatch
			}
		}
		if matchesScopes {
			var usage corev1.ResourceList
			var usageErr error
			if hasVmiScope {
				usage, usageErr = aaqe.SourceCalculatorUsage(pod, existingPods)
			} else {
				usage, usageErr = aaqe.CalculatorUsage(pod, existingPods)
			}
			if usageErr != nil {
				return result, usageErr
			}
			result.Used = quota.Add(result.Used, usage)
		}
	}
	return result, nil
}

var aaqVmiScopes = map[corev1.ResourceQuotaScope]bool{
	v1alpha1.VmiStarting:  true,
	v1alpha1.VmiMigrating: true,
}

func hasVmiScopes(scopes []corev1.ResourceQuotaScope, scopeSelector *corev1.ScopeSelector) bool {
	for _, scope := range scopes {
		if aaqVmiScopes[scope] {
			return true
		}
	}
	if scopeSelector != nil {
		for _, expr := range scopeSelector.MatchExpressions {
			if aaqVmiScopes[expr.ScopeName] && expr.Operator == corev1.ScopeSelectorOpExists {
				return true
			}
		}
	}
	return false
}

func (aaqe *AaqEvaluator) podMatchesScopeFunc(selector corev1.ScopedResourceSelectorRequirement, object runtime.Object) (bool, error) {
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
			return len(pod.Spec.PriorityClassName) != 0, nil
		}
		return podMatchesSelector(pod, selector)
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return usesCrossNamespacePodAffinity(pod), nil
	default:
		if matched, handled := aaqe.aaqEvalRegistery.MatchesScope(pod, selector.ScopeName); handled {
			return matched, nil
		}
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
