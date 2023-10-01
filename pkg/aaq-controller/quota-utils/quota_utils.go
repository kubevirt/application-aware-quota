package quota_utils

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	resourcequotaapi "k8s.io/apiserver/pkg/admission/plugin/resourcequota/apis/resourcequota"
	quota "k8s.io/apiserver/pkg/quota/v1"
	v1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"sort"
	"strings"
)

// CalculateUsage calculates and returns the requested ResourceList usage.
// If an error is returned, usage only contains the resources which encountered no calculation errors.
func CalculateUsage(namespaceName string, scopes []corev1.ResourceQuotaScope, hardLimits corev1.ResourceList, evaluator *aaq_evaluator.AaqEvaluator, scopeSelector *corev1.ScopeSelector) (corev1.ResourceList, error) {
	// find the intersection between the hard resources on the quota
	// and the resources this controller can track to know what we can
	// look to measure updated usage stats for
	hardResources := v1.ResourceNames(hardLimits)
	potentialResources := []corev1.ResourceName{}
	potentialResources = append(potentialResources, evaluator.MatchingResources(hardResources)...)

	// NOTE: the intersection just removes duplicates since the evaluator match intersects with hard
	matchedResources := v1.Intersection(hardResources, potentialResources)

	errors := []error{}
	// sum the observed usage from each evaluator
	newUsage := corev1.ResourceList{}
	// only trigger the evaluator if it matches a resource in the quota, otherwise, skip calculating anything
	intersection := evaluator.MatchingResources(matchedResources)
	if len(intersection) != 0 {
		usageStatsOptions := v1.UsageStatsOptions{Namespace: namespaceName, Scopes: scopes, Resources: intersection, ScopeSelector: scopeSelector}
		stats, err := evaluator.UsageStats(usageStatsOptions)
		if err != nil {
			// remember the error
			errors = append(errors, err)
			// exclude resources which encountered calculation errors
			matchedResources = v1.Difference(matchedResources, intersection)
		} else {
			newUsage = v1.Add(newUsage, stats.Used)
		}
	}
	// mask the observed usage to only the set of resources tracked by this quota
	// merge our observed usage with the quota usage status
	// if the new usage is different than the last usage, we will need to do an update
	newUsage = v1.Mask(newUsage, matchedResources)
	return newUsage, utilerrors.NewAggregate(errors)
}

// CheckRequest is a static version of quotaEvaluator.checkRequest, possible to be called from outside.
func CheckRequest(quotas []corev1.ResourceQuota, a admission.Attributes, evaluator *aaq_evaluator.AaqEvaluator,
	limited []resourcequotaapi.LimitedResource) ([]corev1.ResourceQuota, error) {
	if !evaluator.Handles(a) {
		return quotas, nil
	}

	// if we have limited resources enabled for this resource, always calculate usage
	inputObject := a.GetObject()

	// Check if object matches AdmissionConfiguration matchScopes
	limitedScopes, err := getMatchedLimitedScopes(evaluator, inputObject, limited)
	if err != nil {
		return quotas, nil
	}

	// determine the set of resource names that must exist in a covering quota
	limitedResourceNames := []corev1.ResourceName{}
	limitedResources := filterLimitedResourcesByGroupResource(limited, a.GetResource().GroupResource())
	if len(limitedResources) > 0 {
		deltaUsage, err := evaluator.Usage(inputObject, nil)
		if err != nil {
			return quotas, err
		}
		limitedResourceNames = limitedByDefault(deltaUsage, limitedResources)
	}
	limitedResourceNamesSet := quota.ToSet(limitedResourceNames)

	// find the set of quotas that are pertinent to this request
	// reject if we match the quota, but usage is not calculated yet
	// reject if the input object does not satisfy quota constraints
	// if there are no pertinent quotas, we can just return
	interestingQuotaIndexes := []int{}
	// track the cumulative set of resources that were required across all quotas
	// this is needed to know if we have satisfied any constraints where consumption
	// was limited by default.
	restrictedResourcesSet := sets.String{}
	restrictedScopes := []corev1.ScopedResourceSelectorRequirement{}
	for i := range quotas {
		resourceQuota := quotas[i]
		scopeSelectors := getScopeSelectorsFromQuota(resourceQuota)
		localRestrictedScopes, err := evaluator.MatchingScopes(inputObject, scopeSelectors)
		if err != nil {
			return nil, fmt.Errorf("error matching scopes of quota %s, err: %v", resourceQuota.Name, err)
		}
		restrictedScopes = append(restrictedScopes, localRestrictedScopes...)

		match, err := evaluator.Matches(&resourceQuota, inputObject)
		if err != nil {
			klog.ErrorS(err, "Error occurred while matching resource quota against input object",
				"resourceQuota", resourceQuota)
			return quotas, err
		}
		if !match {
			continue
		}

		hardResources := quota.ResourceNames(resourceQuota.Status.Hard)
		restrictedResources := evaluator.MatchingResources(hardResources)
		if err := evaluator.Constraints(restrictedResources, inputObject); err != nil {
			return nil, admission.NewForbidden(a, fmt.Errorf("failed quota: %s: %v", resourceQuota.Name, err))
		}
		if !hasUsageStats(&resourceQuota, restrictedResources) {
			return nil, admission.NewForbidden(a, fmt.Errorf("status unknown for quota: %s, resources: %s", resourceQuota.Name, prettyPrintResourceNames(restrictedResources)))
		}
		interestingQuotaIndexes = append(interestingQuotaIndexes, i)
		localRestrictedResourcesSet := quota.ToSet(restrictedResources)
		restrictedResourcesSet.Insert(localRestrictedResourcesSet.List()...)
	}

	// Usage of some resources cannot be counted in isolation. For example, when
	// the resource represents a number of unique references to external
	// resource. In such a case an evaluator needs to process other objects in
	// the same namespace which needs to be known.
	namespace := a.GetNamespace()
	if accessor, err := meta.Accessor(inputObject); namespace != "" && err == nil {
		if accessor.GetNamespace() == "" {
			accessor.SetNamespace(namespace)
		}
	}
	// there is at least one quota that definitely matches our object
	// as a result, we need to measure the usage of this object for quota
	// on updates, we need to subtract the previous measured usage
	// if usage shows no change, just return since it has no impact on quota
	deltaUsage, err := evaluator.Usage(inputObject, nil)
	if err != nil {
		return quotas, err
	}

	// ensure that usage for input object is never negative (this would mean a resource made a negative resource requirement)
	if negativeUsage := quota.IsNegative(deltaUsage); len(negativeUsage) > 0 {
		return nil, admission.NewForbidden(a, fmt.Errorf("quota usage is negative for resource(s): %s", prettyPrintResourceNames(negativeUsage)))
	}

	if admission.Update == a.GetOperation() {
		prevItem := a.GetOldObject()
		if prevItem == nil {
			return nil, admission.NewForbidden(a, fmt.Errorf("unable to get previous usage since prior version of object was not found"))
		}

		// if we can definitively determine that this is not a case of "create on update",
		// then charge based on the delta.  Otherwise, bill the maximum
		metadata, err := meta.Accessor(prevItem)
		if err == nil && len(metadata.GetResourceVersion()) > 0 {
			prevUsage, innerErr := evaluator.Usage(prevItem, nil)
			if innerErr != nil {
				return quotas, innerErr
			}
			deltaUsage = quota.SubtractWithNonNegativeResult(deltaUsage, prevUsage)
		}
	}

	// ignore items in deltaUsage with zero usage
	deltaUsage = quota.RemoveZeros(deltaUsage)
	// if there is no remaining non-zero usage, short-circuit and return
	if len(deltaUsage) == 0 {
		return quotas, nil
	}

	// verify that for every resource that had limited by default consumption
	// enabled that there was a corresponding quota that covered its use.
	// if not, we reject the request.
	hasNoCoveringQuota := limitedResourceNamesSet.Difference(restrictedResourcesSet)
	if len(hasNoCoveringQuota) > 0 {
		return quotas, admission.NewForbidden(a, fmt.Errorf("insufficient quota to consume: %v", strings.Join(hasNoCoveringQuota.List(), ",")))
	}

	// verify that for every scope that had limited access enabled
	// that there was a corresponding quota that covered it.
	// if not, we reject the request.
	scopesHasNoCoveringQuota, err := evaluator.UncoveredQuotaScopes(limitedScopes, restrictedScopes)
	if err != nil {
		return quotas, err
	}
	if len(scopesHasNoCoveringQuota) > 0 {
		return quotas, fmt.Errorf("insufficient quota to match these scopes: %v", scopesHasNoCoveringQuota)
	}

	if len(interestingQuotaIndexes) == 0 {
		return quotas, nil
	}

	outQuotas, err := copyQuotas(quotas)
	if err != nil {
		return nil, err
	}

	for _, index := range interestingQuotaIndexes {
		resourceQuota := outQuotas[index]

		hardResources := quota.ResourceNames(resourceQuota.Status.Hard)
		requestedUsage := quota.Mask(deltaUsage, hardResources)
		newUsage := quota.Add(resourceQuota.Status.Used, requestedUsage)
		maskedNewUsage := quota.Mask(newUsage, quota.ResourceNames(requestedUsage))

		if allowed, exceeded := quota.LessThanOrEqual(maskedNewUsage, resourceQuota.Status.Hard); !allowed {
			failedRequestedUsage := quota.Mask(requestedUsage, exceeded)
			failedUsed := quota.Mask(resourceQuota.Status.Used, exceeded)
			failedHard := quota.Mask(resourceQuota.Status.Hard, exceeded)
			return nil, admission.NewForbidden(a,
				fmt.Errorf("exceeded quota: %s, requested: %s, used: %s, limited: %s",
					resourceQuota.Name,
					prettyPrint(failedRequestedUsage),
					prettyPrint(failedUsed),
					prettyPrint(failedHard)))
		}

		// update to the new usage number
		outQuotas[index].Status.Used = newUsage
	}

	return outQuotas, nil
}

func getMatchedLimitedScopes(evaluator *aaq_evaluator.AaqEvaluator, inputObject runtime.Object, limitedResources []resourcequotaapi.LimitedResource) ([]corev1.ScopedResourceSelectorRequirement, error) {
	scopes := []corev1.ScopedResourceSelectorRequirement{}
	for _, limitedResource := range limitedResources {
		matched, err := evaluator.MatchingScopes(inputObject, limitedResource.MatchScopes)
		if err != nil {
			klog.ErrorS(err, "Error while matching limited Scopes")
			return []corev1.ScopedResourceSelectorRequirement{}, err
		}
		scopes = append(scopes, matched...)
	}
	return scopes, nil
}

// filterLimitedResourcesByGroupResource filters the input that match the specified groupResource
func filterLimitedResourcesByGroupResource(input []resourcequotaapi.LimitedResource, groupResource schema.GroupResource) []resourcequotaapi.LimitedResource {
	result := []resourcequotaapi.LimitedResource{}
	for i := range input {
		limitedResource := input[i]
		limitedGroupResource := schema.GroupResource{Group: limitedResource.APIGroup, Resource: limitedResource.Resource}
		if limitedGroupResource == groupResource {
			result = append(result, limitedResource)
		}
	}
	return result
}

// limitedByDefault determines from the specified usage and limitedResources the set of resources names
// that must be present in a covering quota.  It returns empty set if it was unable to determine if
// a resource was not limited by default.
func limitedByDefault(usage corev1.ResourceList, limitedResources []resourcequotaapi.LimitedResource) []corev1.ResourceName {
	result := []corev1.ResourceName{}
	for _, limitedResource := range limitedResources {
		for k, v := range usage {
			// if a resource is consumed, we need to check if it matches on the limited resource list.
			if v.Sign() == 1 {
				// if we get a match, we add it to limited set
				for _, matchContain := range limitedResource.MatchContains {
					if strings.Contains(string(k), matchContain) {
						result = append(result, k)
						break
					}
				}
			}
		}
	}
	return result
}

func getScopeSelectorsFromQuota(quota corev1.ResourceQuota) []corev1.ScopedResourceSelectorRequirement {
	selectors := []corev1.ScopedResourceSelectorRequirement{}
	for _, scope := range quota.Spec.Scopes {
		selectors = append(selectors, corev1.ScopedResourceSelectorRequirement{
			ScopeName: scope,
			Operator:  corev1.ScopeSelectorOpExists})
	}
	if quota.Spec.ScopeSelector != nil {
		selectors = append(selectors, quota.Spec.ScopeSelector.MatchExpressions...)
	}
	return selectors
}

// hasUsageStats returns true if for each hard constraint in interestingResources there is a value for its current usage
func hasUsageStats(resourceQuota *corev1.ResourceQuota, interestingResources []corev1.ResourceName) bool {
	interestingSet := quota.ToSet(interestingResources)
	for resourceName := range resourceQuota.Status.Hard {
		if !interestingSet.Has(string(resourceName)) {
			continue
		}
		if _, found := resourceQuota.Status.Used[resourceName]; !found {
			return false
		}
	}
	return true
}

func prettyPrintResourceNames(a []corev1.ResourceName) string {
	values := []string{}
	for _, value := range a {
		values = append(values, string(value))
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func copyQuotas(in []corev1.ResourceQuota) ([]corev1.ResourceQuota, error) {
	out := make([]corev1.ResourceQuota, 0, len(in))
	for _, quota := range in {
		out = append(out, *quota.DeepCopy())
	}

	return out, nil
}

// prettyPrint formats a resource list for usage in errors
// it outputs resources sorted in increasing order
func prettyPrint(item corev1.ResourceList) string {
	parts := []string{}
	keys := []string{}
	for key := range item {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := item[corev1.ResourceName(key)]
		constraint := key + "=" + value.String()
		parts = append(parts, constraint)
	}
	return strings.Join(parts, ",")
}
