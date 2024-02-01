package arq_controller

import (
	"context"
	"fmt"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	testingclock "k8s.io/utils/clock/testing"
	aaq_evaluator "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-evaluator"
	arq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-gate-controller"
	rq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned/fake"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/informers/externalversions"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

var _ = Describe("Test arq-controller", func() {
	DescribeTable("Test SyncResourceQuota when ", func(arq v1alpha1.ApplicationAwareResourceQuota, managedRQ corev1.ResourceQuota,
		status v1alpha1.ApplicationAwareResourceQuotaStatus, podsState []metav1.Object, expectedError string) {
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		arqmock := client.NewMockApplicationAwareResourceQuotaInterface(ctrl)
		podInformer := testsutils.NewFakeSharedIndexInformer(podsState)
		rqInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{&managedRQ})
		if expectedError != "" {
			podInformer.InternalGetIndexer = func(i cache.Indexer) cache.Indexer {
				return FakefailureIndexer{}
			}
		}
		expectedArq := arq.DeepCopy()
		expectedArq.Status = status
		arqmock.EXPECT().UpdateStatus(context.Background(), expectedArq, metav1.UpdateOptions{}).Times(1)
		cli.EXPECT().ApplicationAwareResourceQuotas(arq.Namespace).Return(arqmock).Times(1)
		qc := setupQuotaController(cli, podInformer, rqInformer, nil)
		if err := qc.syncResourceQuota(&arq); err != nil {
			Expect(expectedError).ToNot(BeEmpty(), "error was not expected")
			Expect(err.Error()).To(ContainSubstring(expectedError), fmt.Sprintf("unexpected error: %v", err))
		} else {
			Expect(expectedError).To(BeEmpty(), fmt.Sprintf("expected error %q, got none", expectedError))
		}
	}, Entry("non-matching-best-effort-scoped-quota", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("0"),
					corev1.ResourceMemory: getUsedQuantityForTest("0"),
					corev1.ResourcePods:   getUsedQuantityForTest("0"),
				},
			},
		},
		newTestPods(),
		"",
	), Entry("matching-best-effort-scoped-quota", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("0"),
					corev1.ResourceMemory: getUsedQuantityForTest("0"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newBestEffortTestPods(),
		"",
	), Entry("non-matching-priorityclass-scoped-quota-OpExists", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpExists},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("0"),
					corev1.ResourceMemory: getUsedQuantityForTest("0"),
					corev1.ResourcePods:   getUsedQuantityForTest("0"),
				},
			},
		},
		newTestPods(),
		"",
	), Entry("matching-priorityclass-scoped-quota-OpExists", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpExists},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("600m"),
					corev1.ResourceMemory: getUsedQuantityForTest("51Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newTestPodsWithPriorityClasses(),
		"",
	), Entry("matching-priorityclass-scoped-quota-OpIn", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpIn,
							Values:    []string{"high", "low"},
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("600m"),
					corev1.ResourceMemory: getUsedQuantityForTest("51Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newTestPodsWithPriorityClasses(),
		"",
	), Entry("matching-priorityclass-scoped-quota-OpIn-high", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpIn,
							Values:    []string{"high"},
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("500m"),
					corev1.ResourceMemory: getUsedQuantityForTest("50Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("1"),
				},
			},
		},
		newTestPodsWithPriorityClasses(),
		"",
	), Entry("matching-priorityclass-scoped-quota-OpNotIn-low", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpNotIn,
							Values:    []string{"high"},
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("100m"),
					corev1.ResourceMemory: getUsedQuantityForTest("1Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("1"),
				},
			},
		},
		newTestPodsWithPriorityClasses(),
		"",
	), Entry("non-matching-priorityclass-scoped-quota-OpIn", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpIn,
							Values:    []string{"random"},
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("0"),
					corev1.ResourceMemory: getUsedQuantityForTest("0"),
					corev1.ResourcePods:   getUsedQuantityForTest("0"),
				},
			},
		},
		newTestPodsWithPriorityClasses(),
		"",
	), Entry("non-matching-priorityclass-scoped-quota-OpNotIn", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpNotIn,
							Values:    []string{"random"},
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("200m"),
					corev1.ResourceMemory: getUsedQuantityForTest("2Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newTestPods(),
		"",
	), Entry("matching-priorityclass-scoped-quota-OpDoesNotExist", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				ScopeSelector: &corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
						{
							ScopeName: corev1.ResourceQuotaScopePriorityClass,
							Operator:  corev1.ScopeSelectorOpDoesNotExist,
						},
					},
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("200m"),
					corev1.ResourceMemory: getUsedQuantityForTest("2Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newTestPods(),
		"",
	), Entry("pods", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("100Gi"),
					corev1.ResourcePods:   resource.MustParse("5"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU:    getUsedQuantityForTest("200m"),
					corev1.ResourceMemory: getUsedQuantityForTest("2Gi"),
					corev1.ResourcePods:   getUsedQuantityForTest("2"),
				},
			},
		},
		newTestPods(),
		"",
	), Entry("quota-spec-hard-updated", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("3"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0"),
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: getUsedQuantityForTest("0"),
				},
			},
		},
		[]metav1.Object{},
		"",
	), Entry("quota-unchanged", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0"),
				},
			},
		},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: getUsedQuantityForTest("0"),
				},
			},
		},
		[]metav1.Object{},
		"",
	), Entry("quota-missing-status-with-calculation-error", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourcePods: resource.MustParse("1"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{},
	}, nil,
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourcePods: resource.MustParse("1"),
				},
				Used: corev1.ResourceList{},
			},
		},
		[]metav1.Object{},
		"error listing",
	), Entry("managed-quota-with-arq-not-synced", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{},
	}, corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota" + rq_controller.RQSuffix, Namespace: "testing"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
		},
	},
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
				Used: corev1.ResourceList{},
			},
		},
		[]metav1.Object{},
		"",
	), Entry("managed-quota-with-rq-not-synced", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
	}, corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota" + rq_controller.RQSuffix, Namespace: "testing"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
		},
	},
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
				Used: corev1.ResourceList{},
			},
		},
		[]metav1.Object{},
		"",
	), Entry("managed-quota-should-sync", v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
	}, corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota" + rq_controller.RQSuffix, Namespace: "testing"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("1"),
			},
		},
	},
		v1alpha1.ApplicationAwareResourceQuotaStatus{
			ResourceQuotaStatus: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse("1"),
				},
			},
		},
		[]metav1.Object{},
		"",
	),
	)

	Context("Test execute when", func() {
		var ctrl *gomock.Controller
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
		})
		It(" pods are ungated and aaqjqc queue should become unlocked", func() {
			aaqjqcInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: arq_controller.AaqjqcName, Namespace: "testNs"}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test", "pod-test2"}, ControllerLock: map[string]bool{arq_controller.ApplicationAwareResourceQuotaLockName: true}}}})
			cli := client.NewMockAAQClient(ctrl)
			fakek8sCli := k8sfake.NewSimpleClientset([]runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: "testNs"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-test2", Namespace: "testNs"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
					},
				},
			}...)
			cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
			mockAaaqjqcInterface := client.NewMockAAQJobQueueConfigInterface(ctrl)
			mockAaaqjqcInterface.EXPECT().UpdateStatus(context.Background(), &v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: arq_controller.AaqjqcName, Namespace: "testNs"}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test", "pod-test2"}, ControllerLock: map[string]bool{arq_controller.ApplicationAwareResourceQuotaLockName: false}}}, metav1.UpdateOptions{})
			cli.EXPECT().AAQJobQueueConfigs("testNs").Times(1).Return(mockAaaqjqcInterface)
			qc := setupQuotaController(cli, nil, nil, aaqjqcInformer)
			err, es := qc.execute("testNs")
			Expect(err).ToNot(HaveOccurred())
			Expect(es).To(Equal(Forget))
		})

		It(" not all pods are ungated and aaqjqc queue should not become unlocked", func() {
			aaqjqcInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: arq_controller.AaqjqcName, Namespace: "testNs"}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test", "pod-test2"}, ControllerLock: map[string]bool{arq_controller.ApplicationAwareResourceQuotaLockName: true}}}})
			cli := client.NewMockAAQClient(ctrl)
			fakek8sCli := k8sfake.NewSimpleClientset([]runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: "testNs"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-test2", Namespace: "testNs"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{
						SchedulingGates: []corev1.PodSchedulingGate{{"someGate"}},
						Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
					},
				},
			}...)
			cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
			qc := setupQuotaController(cli, nil, nil, aaqjqcInformer)
			err, es := qc.execute("testNs")
			Expect(err).ToNot(HaveOccurred())
			Expect(es).To(Equal(Immediate))
		})
	})

	DescribeTable("Test AddQuota when ", func(arq *v1alpha1.ApplicationAwareResourceQuota,
		expectedPriority bool) {
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		qc := setupQuotaController(cli, nil, nil, nil)
		qc.addQuota(klog.FromContext(context.Background()), arq)
		if expectedPriority {
			Expect(qc.missingUsageQueue.Len()).To(Equal(1))
			Expect(qc.arqQueue.Len()).To(Equal(0))
		} else {
			Expect(qc.missingUsageQueue.Len()).To(Equal(0))
			Expect(qc.arqQueue.Len()).To(Equal(1))
		}
	}, Entry("no status", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
	}, true), Entry("status, no usage", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
	}, true), Entry("status, no usage(to validate it works for extended resources)", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					"requests.example/foobars.example.com": resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					"requests.example/foobars.example.com": resource.MustParse("4"),
				},
			},
		},
	}, true), Entry("status, mismatch", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("6"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0"),
				},
			},
		},
	}, true), Entry("status, missing usage, but don't care (no informer)", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					"foobars.example.com": resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					"foobars.example.com": resource.MustParse("4"),
				},
			},
		},
	}, false), Entry("ready", &v1alpha1.ApplicationAwareResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "testing"},
		Spec: v1alpha1.ApplicationAwareResourceQuotaSpec{
			ResourceQuotaSpec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
			},
		},
		Status: v1alpha1.ApplicationAwareResourceQuotaStatus{
			corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
				Used: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0"),
				},
			},
		},
	}, false),
	)
})

type errorLister struct {
}

func (errorLister) List(selector labels.Selector) (ret []runtime.Object, err error) {
	return nil, fmt.Errorf("error listing")
}
func (errorLister) Get(name string) (runtime.Object, error) {
	return nil, fmt.Errorf("error getting")
}
func (errorLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return errorLister{}
}

func setupQuotaController(clientSet client.AAQClient, podInformer cache.SharedIndexInformer, rqInformer cache.SharedIndexInformer, aaqjqcInformer cache.SharedIndexInformer) *ArqController {
	informerFactory := externalversions.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	kubeInformerFactory := informers.NewSharedInformerFactory(k8sfake.NewSimpleClientset(), 0)
	if podInformer == nil {
		podInformer = kubeInformerFactory.Core().V1().Pods().Informer()
	}
	if rqInformer == nil {
		rqInformer = kubeInformerFactory.Core().V1().ResourceQuotas().Informer()
	}
	if aaqjqcInformer == nil {
		aaqjqcInformer = informerFactory.Aaq().V1alpha1().AAQJobQueueConfigs().Informer()
	}
	fakeClock := testingclock.NewFakeClock(time.Now())
	stop := make(chan struct{})
	qc := NewArqController(clientSet,
		podInformer,
		informerFactory.Aaq().V1alpha1().ApplicationAwareResourceQuotas().Informer(),
		rqInformer,
		aaqjqcInformer,
		aaq_evaluator.NewAaqCalculatorsRegistry(3, fakeClock),
		stop,
	)
	informerFactory.Start(stop)
	kubeInformerFactory.Start(stop)
	return qc
}

func newTestPods() []metav1.Object {
	return []metav1.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running-2", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-failed", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
			},
		},
	}
}

func newBestEffortTestPods() []metav1.Object {
	return []metav1.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("", ""), testsutils.GetResourceList("", ""))}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running-2", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("", ""), testsutils.GetResourceList("", ""))}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-failed", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
			},
		},
	}
}

func newTestPodsWithPriorityClasses() []metav1.Object {
	return []metav1.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:           []corev1.Volume{{Name: "vol"}},
				Containers:        []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "50Gi"), testsutils.GetResourceList("", ""))}},
				PriorityClassName: "high",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-running-2", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Volumes:           []corev1.Volume{{Name: "vol"}},
				Containers:        []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
				PriorityClassName: "low",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-failed", Namespace: "testing"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
			Spec: corev1.PodSpec{
				Volumes:    []corev1.Volume{{Name: "vol"}},
				Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("100m", "1Gi"), testsutils.GetResourceList("", ""))}},
			},
		},
	}
}

func getUsedQuantityForTest(val string) resource.Quantity {
	q := resource.Quantity{Format: resource.DecimalSI}
	q.Add(resource.MustParse(val))
	return q
}

type FakefailureIndexer struct {
	indexer cache.Indexer
}

func (f FakefailureIndexer) Add(obj interface{}) error {
	return f.indexer.Add(obj)
}

func (f FakefailureIndexer) Update(obj interface{}) error {
	return f.indexer.Update(obj)
}

func (f FakefailureIndexer) Delete(obj interface{}) error {
	return f.indexer.Delete(obj)
}

func (f FakefailureIndexer) List() []interface{} {
	return f.indexer.List()
}

func (f FakefailureIndexer) ListKeys() []string {
	return f.indexer.ListKeys()
}

func (f FakefailureIndexer) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return f.indexer.Get(obj)
}

func (f FakefailureIndexer) GetByKey(key string) (item interface{}, exists bool, err error) {
	return f.indexer.GetByKey(key)
}

func (f FakefailureIndexer) Replace(i []interface{}, s string) error {
	return f.indexer.Replace(i, s)
}

func (f FakefailureIndexer) Resync() error {
	return f.indexer.Resync()
}

func (f FakefailureIndexer) Index(indexName string, obj interface{}) ([]interface{}, error) {
	return f.indexer.Index(indexName, obj)
}

func (f FakefailureIndexer) IndexKeys(indexName, indexedValue string) ([]string, error) {
	return f.indexer.IndexKeys(indexName, indexedValue)
}

func (f FakefailureIndexer) ListIndexFuncValues(indexName string) []string {
	return f.indexer.ListIndexFuncValues(indexName)
}

func (f FakefailureIndexer) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	return []interface{}{}, fmt.Errorf("error listing")
}

func (f FakefailureIndexer) GetIndexers() cache.Indexers {
	return f.indexer.GetIndexers()
}

func (f FakefailureIndexer) AddIndexers(newIndexers cache.Indexers) error {
	return f.indexer.AddIndexers(newIndexers)
}
