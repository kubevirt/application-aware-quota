package built_in_usage_calculators

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

var _ = Describe("Testmetadata-filter calculator", func() {
	matchLabel := "match"
	matchLabelValue := "matchValue"
	matchLabelMap := map[string]string{matchLabel: matchLabelValue}
	matchAnno := "matchAnno"
	matchAnnoValue := "matchAnnoValue"
	matchAnnoMap := map[string]string{matchAnno: matchAnnoValue}

	DescribeTable("Test PodUsageFunc when", func(calculator *MetadataFilterCalculator, pod *v1.Pod, expectedRl v1.ResourceList, expectMatch bool, errExpected bool) {
		rl, err, match := calculator.PodUsageFunc(pod, nil)
		if errExpected {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(match).To(Equal(expectMatch), "match value doesn't expected value")
		Expect(quota.Equals(rl, expectedRl)).To(BeTrue(), "Word count wrong. Got %v, want %v", rl, expectedRl)
	}, Entry("the pod has no annotations or labels, the pod should not match the calculator",
		NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VirtualResources),
		NewPodBuilder().
			WithName("pod").
			WithNamespace("ns").
			Build(),
		v1.ResourceList{},
		false,
		false),
		Entry("the pod has non matching label, the pod should not match the calculator",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("nonMatch", "nonMatchValue").
				Build(),
			v1.ResourceList{},
			false,
			false),
		Entry("the pod has non matching annotation, the pod should not match the calculator",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithAnnotations("nonMatch", "nonMatchValue").
				Build(),
			v1.ResourceList{},
			false,
			false),
		Entry("the pod has matching label but non matching config, the pod should match the calculator and return actual usage",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VmiPodUsage),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel(matchLabel, matchLabelValue).
				WithResourcesRequestCPU(resource.MustParse("1")).
				WithResourcesLimitsCPU(resource.MustParse("2")).
				WithResourcesRequestMemory(resource.MustParse("1Gi")).
				WithResourcesLimitsMemory(resource.MustParse("2Gi")).
				Build(),
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceCPU:            resource.MustParse("1"),
				v1.ResourceRequestsCPU:    resource.MustParse("1"),
				v1.ResourceLimitsCPU:      resource.MustParse("2"),
				v1.ResourceMemory:         resource.MustParse("1Gi"),
				v1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
			},
			true,
			false),
		Entry("the pod has matching annotation but non matching config, the pod should match the calculator and return actual usage",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VmiPodUsage),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithAnnotations(matchAnno, matchAnnoValue).
				WithResourcesRequestCPU(resource.MustParse("1")).
				WithResourcesRequestMemory(resource.MustParse("1Gi")).
				Build(),
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceCPU:            resource.MustParse("1"),
				v1.ResourceRequestsCPU:    resource.MustParse("1"),
				v1.ResourceMemory:         resource.MustParse("1Gi"),
				v1.ResourceRequestsMemory: resource.MustParse("1Gi"),
			},
			true,
			false),
		Entry("the pod has matching label and config, the pod should match the calculator and filter",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel(matchLabel, matchLabelValue).
				Build(),
			v1.ResourceList{},
			true,
			false),
		Entry("the pod has matching annotation and config, the pod should match the calculator and filter",
			NewMetadataFilterCalculator(matchLabelMap, matchAnnoMap, []v1alpha1.VmiCalcConfigName{v1alpha1.VirtualResources}, v1alpha1.VirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithAnnotations(matchAnno, matchAnnoValue).
				Build(),
			v1.ResourceList{},
			true,
			false),
		Entry("CDI Filter: the pod has matching label but non matching config, the pod should match the calculator and return actual usage",
			NewCDIFilterCalculator(v1alpha1.VmiPodUsage),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("app", "containerized-data-importer").
				WithResourcesRequestCPU(resource.MustParse("1")).
				WithResourcesRequestMemory(resource.MustParse("1Gi")).
				Build(),
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceCPU:            resource.MustParse("1"),
				v1.ResourceRequestsCPU:    resource.MustParse("1"),
				v1.ResourceMemory:         resource.MustParse("1Gi"),
				v1.ResourceRequestsMemory: resource.MustParse("1Gi"),
			},
			true,
			false),
		Entry("CDI Filter: the pod has matching label and config (VirtualResources), the pod should match the calculator and filter",
			NewCDIFilterCalculator(v1alpha1.VirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("app", "containerized-data-importer").
				Build(),
			v1.ResourceList{},
			true,
			false),
		Entry("CDI Filter: the pod has matching label and config (DedicatedVirtualResources), the pod should match the calculator and filter",
			NewCDIFilterCalculator(v1alpha1.DedicatedVirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("app", "containerized-data-importer").
				Build(),
			v1.ResourceList{},
			true,
			false),
		Entry("CDI Filter: the pod has non matching label (key), the pod should not match the calculator",
			NewCDIFilterCalculator(v1alpha1.DedicatedVirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("appx", "containerized-data-importer").
				Build(),
			v1.ResourceList{},
			false,
			false),
		Entry("CDI Filter: the pod has non matching label (value), the pod should not match the calculator",
			NewCDIFilterCalculator(v1alpha1.DedicatedVirtualResources),
			NewPodBuilder().
				WithName("pod").
				WithNamespace("ns").
				WithLabel("app", "xcontainerized-data-importer").
				Build(),
			v1.ResourceList{},
			false,
			false),
	)
})
