package tests

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	aaqclientset "kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned"
	"kubevirt.io/application-aware-quota/tests/builders"
	"kubevirt.io/application-aware-quota/tests/framework"
	"kubevirt.io/application-aware-quota/tests/libaaq"
	"kubevirt.io/application-aware-quota/tests/utils"
	"time"
)

var _ = Describe("ApplicationAwareQuota plugable policies", func() {
	f := framework.NewFramework("plugable-policies")

	Context("label-sidecar evaluator", func() {
		ctx := context.Background()
		BeforeEach(func() {
			currAAQ, err := utils.GetAAQ(f.AaqClient)
			Expect(err).ToNot(HaveOccurred())
			if libaaq.CheckIfPlugablePolicyExistInAAQ(currAAQ, libaaq.LabelSidecar, libaaq.Double) {
				return
			}
			Eventually(func() error {
				currAAQ, err = utils.GetAAQ(f.AaqClient)
				Expect(err).ToNot(HaveOccurred())
				currAAQ = libaaq.AddPlugablePolicy(currAAQ, libaaq.LabelSidecar, libaaq.Double)
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.Background(), currAAQ, v12.UpdateOptions{})
				return err
			}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred(), "waiting for aaq policies update")

			Eventually(func() bool {
				if isAaqControllerReadyForAtLeast5Seconds(f.K8sClient, f.AAQInstallNs) {
					return true
				}
				aaqControllerPods := utils.GetAAQControllerPods(f.K8sClient, f.AAQInstallNs)
				for _, p := range aaqControllerPods.Items {
					for _, cs := range p.Status.ContainerStatuses {
						if cs.Name == string(libaaq.LabelSidecar) && cs.State.Waiting != nil &&
							(cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull") {
							Skip("label-sidecar doesn't exist in the current environment")
						}
					}
				}
				return false
			}, 120*time.Second, 1*time.Second).Should(BeTrue(), "waiting for aaq controller to be ready for at least 5 seconds")
		})

		It("should be able to change the configuration from double counting to triple counting", func() {
			arq := builders.NewArqBuilder().WithName("test-arq").WithNamespace(f.Namespace.Name).
				WithResource(v1.ResourceRequestsMemory, resource.MustParse("4Gi")).Build()
			_, err := createApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, arq)
			Expect(err).ToNot(HaveOccurred())
			err = waitForApplicationAwareResourceQuotaToStartForcingBoundaries(ctx, f.AaqClient, f.Namespace.Name, "test-arq")
			Expect(err).ToNot(HaveOccurred())
			podName := "test-pod"
			requests := v1.ResourceList{}
			requests[v1.ResourceMemory] = resource.MustParse("256Mi")
			pod := newAppsPodForQuota(podName, requests, true, false)
			pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, v12.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			utils.VerifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

			expectedCounting := requests[v1.ResourceMemory]
			expectedCounting.Add(requests[v1.ResourceMemory])

			By("Ensuring Application Aware Resource Quota status include double counting")
			err = waitForApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, "test-arq", v1.ResourceList{v1.ResourceRequestsMemory: expectedCounting})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				aaq, err := utils.GetAAQ(f.AaqClient)
				Expect(err).ToNot(HaveOccurred())
				aaq.Spec.Configuration.SidecarEvaluators = []v1.Container{}
				aaq = libaaq.AddPlugablePolicy(aaq, libaaq.LabelSidecar, libaaq.Triple)
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.Background(), aaq, v12.UpdateOptions{})
				return err
			}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred(), "waiting for aaq policies update")

			Eventually(func() bool {
				return isAaqControllerReadyForAtLeast5Seconds(f.K8sClient, f.AAQInstallNs)
			}, 120*time.Second, 1*time.Second).Should(BeTrue(), "waiting for aaq controller to be ready for at least 5 seconds")

			By("Ensuring Application Aware Resource Quota status include triple counting")
			expectedCounting.Add(requests[v1.ResourceMemory])
			err = waitForApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, "test-arq", v1.ResourceList{v1.ResourceRequestsMemory: expectedCounting})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func newAppsPodForQuota(name string, requests v1.ResourceList, withAppLabel bool, withAppAnnotation bool) *v1.Pod {
	p := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			// prevent disruption to other test workloads in parallel test runs by ensuring the quota
			// test pods don't get scheduled onto a node
			NodeSelector: map[string]string{
				"x-test.k8s.io/unsatisfiable": "not-schedulable",
			},
			Containers: []v1.Container{
				{
					Name:  "pause",
					Image: "busybox",
					Resources: v1.ResourceRequirements{
						Requests: requests,
					},
				},
			},
		},
	}
	if withAppLabel {
		p.Labels = map[string]string{libaaq.LabelAppLabel: "true"}
	}
	if withAppAnnotation {
		p.Annotations = map[string]string{libaaq.AnnotationAppAnnotation: "true"}
	}
	return p
}

func isAaqControllerReadyForAtLeast5Seconds(k8sClient *kubernetes.Clientset, aaqInstallNs string) bool {
	startTime := time.Now()
	for {
		if !libaaq.AaqControllerReady(k8sClient, aaqInstallNs) {
			return false
		}
		if time.Since(startTime) >= 5*time.Second {
			return true
		}
		time.Sleep(1 * time.Second)
	}
}

// wait for Application Aware Resource Quota status to be calculated
func waitForApplicationAwareResourceQuotaToStartForcingBoundaries(ctx context.Context, c *aaqclientset.Clientset, ns, quotaName string) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, resourceQuotaTimeout, false, func(ctx context.Context) (bool, error) {
		ApplicationAwareResourceQuota, err := c.AaqV1alpha1().ApplicationAwareResourceQuotas(ns).Get(ctx, quotaName, v12.GetOptions{})
		if err != nil {
			return false, err
		}
		// used may not yet be calculated
		if ApplicationAwareResourceQuota.Status.Used == nil {
			return false, nil
		}
		return len(ApplicationAwareResourceQuota.Status.Used) != 0, nil
	})
}
