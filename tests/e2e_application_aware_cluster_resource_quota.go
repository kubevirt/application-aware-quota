package tests

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/application-aware-quota/tests/builders"
	"kubevirt.io/application-aware-quota/tests/framework"
	"kubevirt.io/application-aware-quota/tests/libaaq"
	"kubevirt.io/application-aware-quota/tests/utils"
	"time"
)

var _ = Describe("ApplicationAwareAppliedClusterResourceQuota", func() {
	f := framework.NewFramework("application-aware-applied-cluster-resource-quota")
	var labelSelector *v12.LabelSelector

	BeforeEach(func() {
		aaq, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.Background(), "aaq", v12.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		if !aaq.Spec.Configuration.AllowApplicationAwareClusterResourceQuota {
			aaq.Spec.Configuration.AllowApplicationAwareClusterResourceQuota = true
			_, err := f.AaqClient.AaqV1alpha1().AAQs().Update(context.Background(), aaq, v12.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				return libaaq.IsAaqWorkloadsReadyForAtLeast5Seconds(f.K8sClient, f.AAQInstallNs)
			}, 10*time.Minute, 1*time.Second).Should(BeTrue(), "aaq-controller should be ready with the new config Eventually")
		}
		labelSelector = &v12.LabelSelector{
			MatchLabels: map[string]string{"foo": "foo"},
		}
		// Function to add label to a namespace
		err = utils.AddLabelToNamespace(f.K8sClient, "default", "foo", "foo")
		Expect(err).ToNot(HaveOccurred())
		err = utils.AddLabelToNamespace(f.K8sClient, f.Namespace.GetName(), "foo", "foo")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := utils.RemoveLabelFromNamespace(f.K8sClient, "default", "foo")
		Expect(err).ToNot(HaveOccurred())
		err = utils.RemoveLabelFromNamespace(f.K8sClient, f.Namespace.GetName(), "foo")
		Expect(err).ToNot(HaveOccurred())

		acrqs, err := f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().List(context.Background(), v12.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		for _, acrq := range acrqs.Items {
			err := f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Delete(context.Background(), acrq.Name, v12.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	Context("Making sure AAQ Gate Controller receive events", func() {
		It("Removing label from a namespace should change the quota-namespace mapping and trigger the gate controller", func(ctx context.Context) {
			acrq := builders.NewAcrqBuilder().
				WithName("test-quota").
				WithLabelSelector(labelSelector).
				WithScopes([]v1.ResourceQuotaScope{v1.ResourceQuotaScopeNotTerminating}).
				WithResource(v1.ResourceRequestsMemory, resource.MustParse("1Mi")).Build()

			By("Creating a ApplicationAwareClusterResourceQuota")
			_, err := utils.CreateApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the ApplicationAwareClusterResourceQuota to be propagated")
			expectedRresources := v1.ResourceList{}
			expectedRresources[v1.ResourceRequestsMemory] = resource.MustParse("0")
			err = utils.WaitForApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq.Name, expectedRresources)
			Expect(err).ToNot(HaveOccurred())

			By("Creating a pod that should be Gated")
			podName := "test-pod"
			requests := v1.ResourceList{}
			requests[v1.ResourceMemory] = resource.MustParse("200Mi")
			pod := utils.NewTestPodForQuota(podName, requests, v1.ResourceList{})
			_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, v12.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			utils.VerifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

			By("Making sure acrq include both default, test namespaces")
			Eventually(func() []string {
				acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, acrq.Name, v12.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				var namespaces []string
				for _, ns := range acrq.Status.Namespaces {
					namespaces = append(namespaces, ns.Namespace)
				}
				return namespaces
			}, 10*time.Second, 1*time.Second).Should(ContainElements("default", f.Namespace.Name), "acrq should include both default and test namespaces")

			err = utils.RemoveLabelFromNamespace(f.K8sClient, f.Namespace.GetName(), "foo")
			Expect(err).ToNot(HaveOccurred())

			By("Making sure acrq include only default namespace after removing label")
			Eventually(func() []string {
				acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, acrq.Name, v12.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				var namespaces []string
				for _, ns := range acrq.Status.Namespaces {
					namespaces = append(namespaces, ns.Namespace)
				}
				return namespaces
			}, 10*time.Second, 1*time.Second).ShouldNot(ContainElements(f.Namespace.Namespace), "acrq should include both default and test namespaces")

			By("Verify pod is not longer effected by acrq")
			utils.VerifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)
		})

		It("Removing a cluster quota should change the quota-namespace mapping and trigger the gate controller", func(ctx context.Context) {
			acrq := builders.NewAcrqBuilder().
				WithName("test-quota").
				WithLabelSelector(labelSelector).
				WithScopes([]v1.ResourceQuotaScope{v1.ResourceQuotaScopeNotTerminating}).
				WithResource(v1.ResourceRequestsMemory, resource.MustParse("100Mi")).Build()
			acrq2 := builders.NewAcrqBuilder().
				WithName("test-quota2").
				WithLabelSelector(labelSelector).
				WithScopes([]v1.ResourceQuotaScope{v1.ResourceQuotaScopeNotTerminating}).
				WithResource(v1.ResourceRequestsMemory, resource.MustParse("300Mi")).Build()

			By("Creating a ApplicationAwareClusterResourceQuotas")
			_, err := utils.CreateApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq)
			Expect(err).ToNot(HaveOccurred())
			_, err = utils.CreateApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq2)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the ApplicationAwareClusterResourceQuotas to be propagated")
			expectedRresources := v1.ResourceList{}
			expectedRresources[v1.ResourceRequestsMemory] = resource.MustParse("0")
			err = utils.WaitForApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq.Name, expectedRresources)
			Expect(err).ToNot(HaveOccurred())
			err = utils.WaitForApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq2.Name, expectedRresources)
			Expect(err).ToNot(HaveOccurred())

			By("Creating a pod that should be Gated")
			podName := "test-pod"
			requests := v1.ResourceList{}
			requests[v1.ResourceMemory] = resource.MustParse("200Mi")
			pod := utils.NewTestPodForQuota(podName, requests, v1.ResourceList{})
			_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, v12.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			utils.VerifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

			By("Deleting the blocking ApplicationAwareClusterResourceQuota")
			err = utils.DeleteApplicationAwareClusterResourceQuota(ctx, f.AaqClient, acrq.Name)
			Expect(err).ToNot(HaveOccurred())

			By("Verify pod is not longer effected by blocking acrq")
			utils.VerifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)
		})
	})
})
