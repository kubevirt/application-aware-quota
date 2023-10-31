package aaq_evaluator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/tools/cache"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/util/node"
	testingclock "k8s.io/utils/clock/testing"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"time"
)

var _ = Describe("AaqEvaluator", func() {
	Context("Test usage func", func() {
		var eval *AaqEvaluator
		var deletionTimestampPastGracePeriod metav1.Time
		var deletionTimestampNotPastGracePeriod metav1.Time
		var terminationGracePeriodSeconds int64

		BeforeEach(func() {
			podInformer := util.NewPodFakeInformer([]runtime.Object{})
			fakeClock := testingclock.NewFakeClock(time.Now())
			now := fakeClock.Now()
			terminationGracePeriodSeconds = int64(30)
			deletionTimestampPastGracePeriod = metav1.NewTime(now.Add(time.Duration(terminationGracePeriodSeconds) * time.Second * time.Duration(-2)))
			deletionTimestampNotPastGracePeriod = metav1.NewTime(fakeClock.Now())
			eval = NewAaqEvaluator(podInformer, NewAaqCalculatorsRegistry(1, fakeClock), fakeClock)
		})
		DescribeTable("Test pod Usage", func(pod *api.Pod, expectedUsage corev1.ResourceList) {
			actual, err := eval.Usage(pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(quota.Equals(expectedUsage, actual)).To(BeTrue())
		},
			Entry(" with init container CPU", &api.Pod{
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
			Entry(" with init container MEM", &api.Pod{
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
			Entry(" with init container local ephemeral storage", &api.Pod{
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
			Entry(" with init container hugepages", &api.Pod{
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
			Entry(" with init container extended resources", &api.Pod{
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
			Entry(" with container CPU", &api.Pod{
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
			Entry(" with container MEM", &api.Pod{
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
			Entry(" with container local ephemeral storage", &api.Pod{
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
			Entry(" with container hugepages", &api.Pod{
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
			Entry(" with container extended resources", &api.Pod{
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
			Entry(" with terminated generic count still appears", &api.Pod{
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
			Entry(" with init container maximums override sum of containers", &api.Pod{
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
			Entry(" with pod deletion timestamp exceeded", &api.Pod{
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
			Entry(" with pod deletion timestamp not exceeded", &api.Pod{
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
			Entry(" with count pod overhead as usage", &api.Pod{
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

	Context("Test usage func", func() {
		DescribeTable("Test pod Usage", func(objs []runtime.Object, expectedUsage corev1.ResourceList) {
			fakeClock := testingclock.NewFakeClock(time.Now())
			podInformer := util.NewPodFakeInformer(objs)
			evaluator := NewAaqEvaluator(podInformer, NewAaqCalculatorsRegistry(1, fakeClock), fakeClock)

		}, Entry("entry"),
		)
	}

})

func newGenericLister(groupResource schema.GroupResource, items []runtime.Object) cache.GenericLister {
	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	for _, item := range items {
		store.Add(item)
	}
	return cache.NewGenericLister(store, groupResource)
}
