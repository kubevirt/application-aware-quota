package aaq_evaluator

import (
	"fmt"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/apiserver/pkg/util/feature"
	v1 "k8s.io/client-go/listers/core/v1"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/kubernetes/pkg/util/node"
	testingclock "k8s.io/utils/clock/testing"
	fakeinformers "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"time"
)

var _ = Describe("AaqEvaluator", func() {
	Context("Test usage func", func() {
		var eval *AaqEvaluator
		var deletionTimestampPastGracePeriod metav1.Time
		var deletionTimestampNotPastGracePeriod metav1.Time
		var terminationGracePeriodSeconds int64

		BeforeEach(func() {
			podInformer := fakeinformers.NewFakeSharedIndexInformer([]metav1.Object{})
			fakeClock := testingclock.NewFakeClock(time.Now())
			now := fakeClock.Now()
			terminationGracePeriodSeconds = int64(30)
			deletionTimestampPastGracePeriod = metav1.NewTime(now.Add(time.Duration(terminationGracePeriodSeconds) * time.Second * time.Duration(-2)))
			deletionTimestampNotPastGracePeriod = metav1.NewTime(fakeClock.Now())
			eval = NewAaqEvaluator(v1.NewPodLister(podInformer.GetIndexer()), newAaqEvaluatorsRegistry(1, "/fakeSocketSharedDirectory"), fakeClock)
		})
		DescribeTable("Test pod Usage when ", func(pod *api.Pod, expectedUsage corev1.ResourceList) {
			actual, err := eval.Usage(pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(quota.Equals(expectedUsage, actual)).To(BeTrue())
		},
			Entry("init container CPU", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceCPU: resource.MustParse("1m")},
							Limits:   api.ResourceList{api.ResourceCPU: resource.MustParse("2m")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1m"),
				corev1.ResourceLimitsCPU:   resource.MustParse("2m"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("1m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("init container MEM", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceMemory: resource.MustParse("1m")},
							Limits:   api.ResourceList{api.ResourceMemory: resource.MustParse("2m")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("1m"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2m"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceMemory:         resource.MustParse("1m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("init container local ephemeral storage", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceEphemeralStorage: resource.MustParse("32Mi")},
							Limits:   api.ResourceList{api.ResourceEphemeralStorage: resource.MustParse("64Mi")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceEphemeralStorage:         resource.MustParse("32Mi"),
				corev1.ResourceRequestsEphemeralStorage: resource.MustParse("32Mi"),
				corev1.ResourceLimitsEphemeralStorage:   resource.MustParse("64Mi"),
				corev1.ResourcePods:                     resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("init container hugepages", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceName(api.ResourceHugePagesPrefix + "2Mi"): resource.MustParse("100Mi")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceName(corev1.ResourceHugePagesPrefix + "2Mi"):         resource.MustParse("100Mi"),
				corev1.ResourceName(corev1.ResourceRequestsHugePagesPrefix + "2Mi"): resource.MustParse("100Mi"),
				corev1.ResourcePods: resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("init container extended resources", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
							Limits:   api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceName("requests.example.com/dongle"): resource.MustParse("3"),
				corev1.ResourcePods: resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("container CPU", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceCPU: resource.MustParse("1m")},
							Limits:   api.ResourceList{api.ResourceCPU: resource.MustParse("2m")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1m"),
				corev1.ResourceLimitsCPU:   resource.MustParse("2m"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("1m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("container MEM", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceMemory: resource.MustParse("1m")},
							Limits:   api.ResourceList{api.ResourceMemory: resource.MustParse("2m")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("1m"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2m"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceMemory:         resource.MustParse("1m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("container local ephemeral storage", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceEphemeralStorage: resource.MustParse("32Mi")},
							Limits:   api.ResourceList{api.ResourceEphemeralStorage: resource.MustParse("64Mi")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceEphemeralStorage:         resource.MustParse("32Mi"),
				corev1.ResourceRequestsEphemeralStorage: resource.MustParse("32Mi"),
				corev1.ResourceLimitsEphemeralStorage:   resource.MustParse("64Mi"),
				corev1.ResourcePods:                     resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("container hugepages", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceName(api.ResourceHugePagesPrefix + "2Mi"): resource.MustParse("100Mi")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceName(api.ResourceHugePagesPrefix + "2Mi"):         resource.MustParse("100Mi"),
				corev1.ResourceName(api.ResourceRequestsHugePagesPrefix + "2Mi"): resource.MustParse("100Mi"),
				corev1.ResourcePods: resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("container extended resources", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
							Limits:   api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
						},
					}},
				},
			}, corev1.ResourceList{
				corev1.ResourceName("requests.example.com/dongle"): resource.MustParse("3"),
				corev1.ResourcePods: resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("terminated generic count still appears", &api.Pod{
				Spec: api.PodSpec{
					Containers: []api.Container{{
						Resources: api.ResourceRequirements{
							Requests: api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
							Limits:   api.ResourceList{api.ResourceName("example.com/dongle"): resource.MustParse("3")},
						},
					}},
				},
				Status: api.PodStatus{
					Phase: api.PodSucceeded,
				},
			}, corev1.ResourceList{
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("init container maximums override sum of containers", &api.Pod{
				Spec: api.PodSpec{
					InitContainers: []api.Container{
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("4"),
									api.ResourceMemory:                     resource.MustParse("100M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("4"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("8"),
									api.ResourceMemory:                     resource.MustParse("200M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("4"),
								},
							},
						},
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("1"),
									api.ResourceMemory:                     resource.MustParse("50M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("2"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("2"),
									api.ResourceMemory:                     resource.MustParse("100M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("2"),
								},
							},
						},
					},
					Containers: []api.Container{
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("1"),
									api.ResourceMemory:                     resource.MustParse("50M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("1"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("2"),
									api.ResourceMemory:                     resource.MustParse("100M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("1"),
								},
							},
						},
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("2"),
									api.ResourceMemory:                     resource.MustParse("25M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("2"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU:                        resource.MustParse("5"),
									api.ResourceMemory:                     resource.MustParse("50M"),
									api.ResourceName("example.com/dongle"): resource.MustParse("2"),
								},
							},
						},
					},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsCPU:                         resource.MustParse("4"),
				corev1.ResourceRequestsMemory:                      resource.MustParse("100M"),
				corev1.ResourceLimitsCPU:                           resource.MustParse("8"),
				corev1.ResourceLimitsMemory:                        resource.MustParse("200M"),
				corev1.ResourcePods:                                resource.MustParse("1"),
				corev1.ResourceCPU:                                 resource.MustParse("4"),
				corev1.ResourceMemory:                              resource.MustParse("100M"),
				corev1.ResourceName("requests.example.com/dongle"): resource.MustParse("4"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("pod deletion timestamp exceeded", &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp:          &deletionTimestampPastGracePeriod,
					DeletionGracePeriodSeconds: &terminationGracePeriodSeconds,
				},
				Status: api.PodStatus{
					Reason: node.NodeUnreachablePodReason,
				},
				Spec: api.PodSpec{
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Containers: []api.Container{
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU:    resource.MustParse("1"),
									api.ResourceMemory: resource.MustParse("50M"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU:    resource.MustParse("2"),
									api.ResourceMemory: resource.MustParse("100M"),
								},
							},
						},
					},
				},
			}, corev1.ResourceList{
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("pod deletion timestamp not exceeded", &api.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp:          &deletionTimestampNotPastGracePeriod,
					DeletionGracePeriodSeconds: &terminationGracePeriodSeconds,
				},
				Status: api.PodStatus{
					Reason: node.NodeUnreachablePodReason,
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU: resource.MustParse("1"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU: resource.MustParse("2"),
								},
							},
						},
					},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
				corev1.ResourceLimitsCPU:   resource.MustParse("2"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("1"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
			Entry("count pod overhead as usage", &api.Pod{
				Spec: api.PodSpec{
					Overhead: api.ResourceList{
						api.ResourceCPU: resource.MustParse("1"),
					},
					Containers: []api.Container{
						{
							Resources: api.ResourceRequirements{
								Requests: api.ResourceList{
									api.ResourceCPU: resource.MustParse("1"),
								},
								Limits: api.ResourceList{
									api.ResourceCPU: resource.MustParse("2"),
								},
							},
						},
					},
				},
			}, corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("2"),
				corev1.ResourceLimitsCPU:   resource.MustParse("3"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("2"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			}),
		)
	})

	Context("Test UsageStats func", func() {
		cpu1 := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
		DescribeTable("Test pod UsageStats when ", func(objs []metav1.Object, expectedUsage corev1.ResourceList,
			quotaScopes []corev1.ResourceQuotaScope, quotaScopeSelector *corev1.ScopeSelector) {
			fakeClock := testingclock.NewFakeClock(time.Now())
			podInformer := fakeinformers.NewFakeSharedIndexInformer(objs)
			evaluator := NewAaqEvaluator(v1.NewPodLister(podInformer.GetIndexer()), newAaqEvaluatorsRegistry(1, "/fakeSocketSharedDirectory"), fakeClock)
			usageStatsOption := quota.UsageStatsOptions{
				Scopes:        quotaScopes,
				ScopeSelector: quotaScopeSelector,
			}
			actual, err := evaluator.UsageStats(usageStatsOption)
			Expect(err).ToNot(HaveOccurred())
			Expect(quota.Equals(expectedUsage, actual.Used)).To(BeTrue())
		}, Entry("all pods in running state", []metav1.Object{
			makePod("p1", "", cpu1, corev1.PodRunning),
			makePod("p2", "", cpu1, corev1.PodRunning),
		}, corev1.ResourceList{
			corev1.ResourcePods:               resource.MustParse("2"),
			corev1.ResourceName("count/pods"): resource.MustParse("2"),
			corev1.ResourceCPU:                resource.MustParse("2"),
			corev1.ResourceRequestsCPU:        resource.MustParse("2"),
			corev1.ResourceLimitsCPU:          resource.MustParse("2"),
		}, nil, nil),
			Entry("one pods in terminal state", []metav1.Object{
				makePod("p1", "", cpu1, corev1.PodRunning),
				makePod("p2", "", cpu1, corev1.PodSucceeded),
			}, corev1.ResourceList{
				corev1.ResourcePods:               resource.MustParse("1"),
				corev1.ResourceName("count/pods"): resource.MustParse("2"),
				corev1.ResourceCPU:                resource.MustParse("1"),
				corev1.ResourceRequestsCPU:        resource.MustParse("1"),
				corev1.ResourceLimitsCPU:          resource.MustParse("1"),
			}, nil, nil),
			Entry("partial pods matching quotaScopeSelector", []metav1.Object{
				makePod("p1", "high-priority", cpu1, corev1.PodRunning),
				makePod("p2", "high-priority", cpu1, corev1.PodSucceeded),
				makePod("p3", "low-priority", cpu1, corev1.PodRunning),
			}, corev1.ResourceList{
				corev1.ResourcePods:               resource.MustParse("1"),
				corev1.ResourceName("count/pods"): resource.MustParse("2"),
				corev1.ResourceCPU:                resource.MustParse("1"),
				corev1.ResourceRequestsCPU:        resource.MustParse("1"),
				corev1.ResourceLimitsCPU:          resource.MustParse("1"),
			}, nil,
				&corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpIn,
							Values:    []string{"high-priority"},
						},
					},
				}),
			Entry("partial pods matching quotaScopeSelector - w/ scopeName specified", []metav1.Object{
				makePod("p1", "high-priority", cpu1, corev1.PodRunning),
				makePod("p2", "high-priority", cpu1, corev1.PodSucceeded),
				makePod("p3", "low-priority", cpu1, corev1.PodRunning),
			}, corev1.ResourceList{
				corev1.ResourcePods:               resource.MustParse("1"),
				corev1.ResourceName("count/pods"): resource.MustParse("2"),
				corev1.ResourceCPU:                resource.MustParse("1"),
				corev1.ResourceRequestsCPU:        resource.MustParse("1"),
				corev1.ResourceLimitsCPU:          resource.MustParse("1"),
			}, []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopePriorityClass,
			}, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
					{
						ScopeName: corev1.ResourceQuotaScopePriorityClass,
						Operator:  corev1.ScopeSelectorOpIn,
						Values:    []string{"high-priority"},
					},
				},
			}),
			Entry("partial pods matching quotaScopeSelector - w/ multiple scopeNames specified", []metav1.Object{
				makePod("p1", "high-priority", cpu1, corev1.PodRunning),
				makePod("p2", "high-priority", cpu1, corev1.PodSucceeded),
				makePod("p3", "low-priority", cpu1, corev1.PodRunning),
				makePod("p4", "high-priority", nil, corev1.PodFailed),
			}, corev1.ResourceList{
				corev1.ResourceName("count/pods"): resource.MustParse("1"),
			}, []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopePriorityClass,
				corev1.ResourceQuotaScopeBestEffort,
			}, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
					{
						ScopeName: corev1.ResourceQuotaScopePriorityClass,
						Operator:  corev1.ScopeSelectorOpIn,
						Values:    []string{"high-priority"},
					},
				},
			}),
		)
	})

	Context("Test MatchingScopes func", func() {
		var eval *AaqEvaluator
		var activeDeadlineSeconds int64
		BeforeEach(func() {
			podInformer := fakeinformers.NewFakeSharedIndexInformer([]metav1.Object{})
			fakeClock := testingclock.NewFakeClock(time.Now())
			eval = NewAaqEvaluator(v1.NewPodLister(podInformer.GetIndexer()), newAaqEvaluatorsRegistry(1, "/fakeSocketSharedDirectory"), fakeClock)
			activeDeadlineSeconds = int64(30)

		})
		DescribeTable("Test pod MatchingScopes when ", func(pod *corev1.Pod, wantSelectors []corev1.ScopedResourceSelectorRequirement,
			selectors []corev1.ScopedResourceSelectorRequirement) {
			if selectors == nil {
				selectors = []corev1.ScopedResourceSelectorRequirement{
					{ScopeName: corev1.ResourceQuotaScopeTerminating},
					{ScopeName: corev1.ResourceQuotaScopeNotTerminating},
					{ScopeName: corev1.ResourceQuotaScopeBestEffort},
					{ScopeName: corev1.ResourceQuotaScopeNotBestEffort},
					{ScopeName: corev1.ResourceQuotaScopePriorityClass, Operator: corev1.ScopeSelectorOpIn, Values: []string{"class1"}},
					{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
				}
			}
			gotSelectors, err := eval.MatchingScopes(pod, selectors)
			Expect(err).ToNot(HaveOccurred())
			diff := cmp.Diff(wantSelectors, gotSelectors)
			Expect(diff).To(BeEmpty(), fmt.Sprintf("unexpected diff (-want, +got):\n%s", diff))

		}, Entry("EmptyPod", &corev1.Pod{},
			[]corev1.ScopedResourceSelectorRequirement{
				{ScopeName: corev1.ResourceQuotaScopeNotTerminating},
				{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			}, nil), Entry("PriorityClass", &corev1.Pod{
			Spec: corev1.PodSpec{
				PriorityClassName: "class1",
			}}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeNotTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopePriorityClass, Operator: corev1.ScopeSelectorOpIn, Values: []string{"class1"}},
		}, nil), Entry("NotBestEffort", &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:                        resource.MustParse("1"),
							corev1.ResourceMemory:                     resource.MustParse("50M"),
							corev1.ResourceName("example.com/dongle"): resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:                        resource.MustParse("2"),
							corev1.ResourceMemory:                     resource.MustParse("100M"),
							corev1.ResourceName("example.com/dongle"): resource.MustParse("1"),
						},
					},
				}},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeNotTerminating},
			{ScopeName: corev1.ResourceQuotaScopeNotBestEffort},
		}, nil), Entry("Terminating", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
		}, nil), Entry("OnlyTerminating", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
		},
			[]corev1.ScopedResourceSelectorRequirement{
				{ScopeName: corev1.ResourceQuotaScopeTerminating},
			}), Entry("CrossNamespaceRequiredAffinity", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{LabelSelector: &metav1.LabelSelector{}, Namespaces: []string{"ns1"}, NamespaceSelector: &metav1.LabelSelector{}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil), Entry("CrossNamespaceRequiredAffinityWithSlice", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{LabelSelector: &metav1.LabelSelector{}, Namespaces: []string{"ns1"}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil), Entry("CrossNamespacePreferredAffinity", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{PodAffinityTerm: corev1.PodAffinityTerm{LabelSelector: &metav1.LabelSelector{}, Namespaces: []string{"ns2"}, NamespaceSelector: &metav1.LabelSelector{}}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil), Entry("CrossNamespacePreferredAffinityWithSelector", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{PodAffinityTerm: corev1.PodAffinityTerm{LabelSelector: &metav1.LabelSelector{}, NamespaceSelector: &metav1.LabelSelector{}}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil), Entry("CrossNamespacePreferredAntiAffinity", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{PodAffinityTerm: corev1.PodAffinityTerm{LabelSelector: &metav1.LabelSelector{}, NamespaceSelector: &metav1.LabelSelector{}}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil), Entry("CrossNamespaceRequiredAntiAffinity", &corev1.Pod{
			Spec: corev1.PodSpec{
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{LabelSelector: &metav1.LabelSelector{}, Namespaces: []string{"ns3"}},
						},
					},
				},
			},
		}, []corev1.ScopedResourceSelectorRequirement{
			{ScopeName: corev1.ResourceQuotaScopeTerminating},
			{ScopeName: corev1.ResourceQuotaScopeBestEffort},
			{ScopeName: corev1.ResourceQuotaScopeCrossNamespacePodAffinity},
		}, nil),
		)
	})

	Context("Test UsageResourceResize", func() {
		var eval *AaqEvaluator
		BeforeEach(func() {
			podInformer := fakeinformers.NewFakeSharedIndexInformer([]metav1.Object{})
			fakeClock := testingclock.NewFakeClock(time.Now())
			eval = NewAaqEvaluator(v1.NewPodLister(podInformer.GetIndexer()), newAaqEvaluatorsRegistry(1, "/fakeSocketSharedDirectory"), fakeClock)

		})
		DescribeTable("Test pod UsageResourceResize when ", func(pod *corev1.Pod, usageFgEnabled corev1.ResourceList, usageFgDisabled corev1.ResourceList) {
			for _, enabled := range []bool{true, false} {
				featuregatetesting.SetFeatureGateDuringTest(GinkgoTB(), feature.DefaultFeatureGate, "InPlacePodVerticalScaling", enabled)
				actual, err := eval.Usage(pod)
				Expect(err).ToNot(HaveOccurred())
				usage := usageFgEnabled
				if !enabled {
					usage = usageFgDisabled
				}
				Expect(quota.Equals(usage, actual)).To(BeTrue(), fmt.Sprintf("FG enabled: %v, expected: %v, actual: %v", enabled, usage, actual))
			}
		}, Entry("verify Max(Container.Spec.Requests, ContainerStatus.AllocatedResources) for memory resource", &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("400Mi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						AllocatedResources: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("150Mi"),
						},
					},
				},
			},
		},
			corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("200Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceMemory:         resource.MustParse("200Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
			corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("200Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceMemory:         resource.MustParse("200Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
		), Entry("verify Max(Container.Spec.Requests, ContainerStatus.AllocatedResources) for CPU resource", &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("200m"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						AllocatedResources: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("150m"),
						},
					},
				},
			},
		},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("150m"),
				corev1.ResourceLimitsCPU:   resource.MustParse("200m"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("150m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("100m"),
				corev1.ResourceLimitsCPU:   resource.MustParse("200m"),
				corev1.ResourcePods:        resource.MustParse("1"),
				corev1.ResourceCPU:         resource.MustParse("100m"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
		), Entry("verify Max(Container.Spec.Requests, ContainerStatus.AllocatedResources) for CPU and memory resource", &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("400Mi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						AllocatedResources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("150m"),
							corev1.ResourceMemory: resource.MustParse("250Mi"),
						},
					},
				},
			},
		},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("150m"),
				corev1.ResourceLimitsCPU:      resource.MustParse("200m"),
				corev1.ResourceRequestsMemory: resource.MustParse("250Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceCPU:            resource.MustParse("150m"),
				corev1.ResourceMemory:         resource.MustParse("250Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("100m"),
				corev1.ResourceLimitsCPU:      resource.MustParse("200m"),
				corev1.ResourceRequestsMemory: resource.MustParse("200Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceCPU:            resource.MustParse("100m"),
				corev1.ResourceMemory:         resource.MustParse("200Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
		), Entry("verify Max(Container.Spec.Requests, ContainerStatus.AllocatedResources==nil) for CPU and memory resource", &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("400Mi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{},
				},
			},
		},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("100m"),
				corev1.ResourceLimitsCPU:      resource.MustParse("200m"),
				corev1.ResourceRequestsMemory: resource.MustParse("200Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceCPU:            resource.MustParse("100m"),
				corev1.ResourceMemory:         resource.MustParse("200Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
			corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("100m"),
				corev1.ResourceLimitsCPU:      resource.MustParse("200m"),
				corev1.ResourceRequestsMemory: resource.MustParse("200Mi"),
				corev1.ResourceLimitsMemory:   resource.MustParse("400Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
				corev1.ResourceCPU:            resource.MustParse("100m"),
				corev1.ResourceMemory:         resource.MustParse("200Mi"),
				generic.ObjectCountQuotaResourceNameFor(schema.GroupResource{Resource: "pods"}): resource.MustParse("1"),
			},
		),
		)
	})

	Context("test calculators-registery ", func() {
		var registry Registry
		var fakeClock *testingclock.FakeClock
		testPod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "memory-demo-container",
						Image:   "busybox",
						Command: []string{"sh", "-c", "stress --vm 1 --vm-bytes 100M --vm-hang 0"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
						},
					},
				},
			},
		}
		testPodUsage, _ := core.PodUsageFunc(testPod, fakeClock)
		testPodWithGate := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "memory-demo-container",
						Image:   "busybox",
						Command: []string{"sh", "-c", "stress --vm 1 --vm-bytes 100M --vm-hang 0"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
						},
					},
				},
				SchedulingGates: []corev1.PodSchedulingGate{{"test"}},
			},
		}
		BeforeEach(func() {
			registry = newAaqEvaluatorsRegistry(1, "/fakeSocketSharedDirectory")
		})

		It("should add a built-in calculator", func() {
			fakeCalculator := new(FakeUsageCalculator)
			registry.Add(fakeCalculator)
		})

		DescribeTable("Test calculators-registery when ", func(pod *corev1.Pod, expectedUsage corev1.ResourceList,
			fakeCalc1 *FakeUsageCalculator, fakeCalc2 *FakeUsageCalculator) {
			pods := []metav1.Object{pod}
			fakeClock := testingclock.NewFakeClock(time.Now())
			podInformer := fakeinformers.NewFakeSharedIndexInformer(pods)
			registry.Add(fakeCalc1)
			registry.Add(fakeCalc2)
			eval := NewAaqEvaluator(v1.NewPodLister(podInformer.GetIndexer()), registry, fakeClock)
			actual, err := eval.Usage(pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(quota.Equals(expectedUsage, actual)).To(BeTrue())

		}, Entry("Should consider both calculators if both match", &corev1.Pod{}, corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		}, NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m")}, nil, true
		}), NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m"), "memory": resource.MustParse("128Mi")}, nil, true
		})), Entry("should not include calculator if doesn't match", &corev1.Pod{}, corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		}, NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m")}, nil, true
		}), NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m"), "memory": resource.MustParse("128Mi")}, nil, false
		})), Entry("should use built pod calculator if non calculator match", testPod, testPodUsage, NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m")}, nil, false
		}), NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m"), "memory": resource.MustParse("128Mi")}, nil, false
		})), Entry("should not include gated pod", testPodWithGate, corev1.ResourceList{}, NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m")}, nil, true
		}), NewFakeUsageCalculator(func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
			return corev1.ResourceList{"cpu": resource.MustParse("100m"), "memory": resource.MustParse("128Mi")}, nil, true
		})),
		)
	})
})

func NewFakeUsageCalculator(usageFunc func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool)) *FakeUsageCalculator {
	return &FakeUsageCalculator{usageFunc: usageFunc}
}

type FakeUsageCalculator struct {
	usageFunc func(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool)
}

func (m *FakeUsageCalculator) SetConfiguration(_ string) {}

func (m *FakeUsageCalculator) PodUsageFunc(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
	return m.usageFunc(pod, podsState)
}

func makePod(name, pcName string, resList corev1.ResourceList, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PodSpec{
			PriorityClassName: pcName,
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: resList,
						Limits:   resList,
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}
