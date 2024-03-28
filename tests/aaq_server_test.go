package tests

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	matav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/application-aware-quota/pkg/util"
	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/builders"
	"kubevirt.io/application-aware-quota/tests/framework"
	"kubevirt.io/application-aware-quota/tests/utils"
	"time"
)

var _ = Describe("AAQ Server", func() {
	f := framework.NewFramework("aaq-server-test")

	Context("Test Valid/Invalid ARQ creations", func() {
		It("Simple empty ARQ should be allowed to create", func() {
			arq := builders.NewArqBuilder().WithName("arq").Build()
			_, err := f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Create(context.Background(), arq, matav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("ARQ with invalid name should be rejected", func() {
			var illegalName v1.ResourceName = "illegalName"
			arq := builders.NewArqBuilder().WithName("arq").WithResource(illegalName, resource.MustParse("1m")).Build()
			_, err := f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Create(context.Background(), arq, matav1.CreateOptions{})
			Expect(err).To(HaveOccurred(), "resourceName must be a standard resource for quota")
		})

		It("Simple empty ARQ with valid resource should be allowed to create", func() {
			arq := builders.NewArqBuilder().WithName("arq").WithResource(v1.ResourceLimitsMemory, resource.MustParse("1m")).Build()
			_, err := f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Create(context.Background(), arq, matav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("Simple empty ARQ with 2 valid resources should be allowed to create", func() {
			arq := builders.NewArqBuilder().WithName("arq").WithResource(v1.ResourceLimitsMemory, resource.MustParse("1m")).WithResource(v1.ResourceLimitsCPU, resource.MustParse("1")).Build()
			_, err := f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Create(context.Background(), arq, matav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("Shouldn't remove another gate from pod when adding our gate", func() {
		sg := v1.PodSchedulingGate{Name: "testSg"}
		podName := "simple-pod"
		pod := &v1.Pod{
			ObjectMeta: matav1.ObjectMeta{
				Name:      podName,
				Namespace: f.Namespace.GetName(),
			},
			Spec: v1.PodSpec{
				SchedulingGates: []v1.PodSchedulingGate{
					sg,
				},
				Containers: []v1.Container{
					{
						Name:  "my-container",
						Image: "nginx:latest",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("200Mi"),
							},
						},
					},
				},
			},
		}
		_, err := f.K8sClient.CoreV1().Pods(f.Namespace.GetName()).Create(context.Background(), pod, matav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() []v1.PodSchedulingGate {
			curPod, err := f.K8sClient.CoreV1().Pods(f.Namespace.GetName()).Get(context.Background(), podName, matav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			return curPod.Spec.SchedulingGates
		}, 2*time.Minute, 1*time.Second).Should(ContainElements(sg, v1.PodSchedulingGate{Name: util.AAQGate}))

		Eventually(func() error { //make sure the pod won't interrupt other tests
			err := f.K8sClient.CoreV1().Pods(f.Namespace.GetName()).Delete(context.Background(), podName, matav1.DeleteOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("should be invalid to create two AAQ instances", func() {
		currentAaq, err := utils.GetAAQ(f)
		Expect(err).ToNot(HaveOccurred())

		secondAaq := &aaqv1.AAQ{
			ObjectMeta: matav1.ObjectMeta{
				Name: "second-aaq-instance",
			},
			Spec: currentAaq.Spec,
		}

		secondAaq, err = f.AaqClient.AaqV1alpha1().AAQs().Create(context.TODO(), secondAaq, matav1.CreateOptions{})
		Expect(err).Should(HaveOccurred(), "shouldn't be possible to create a second AAQ instance")
	})
})
