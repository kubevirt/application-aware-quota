package tests

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	utils2 "kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/tests/framework"
	"kubevirt.io/application-aware-quota/tests/framework/matcher"
	"kubevirt.io/application-aware-quota/tests/utils"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Leader election test", func() {
	f := framework.NewFramework("operator-test")
	Context("when the controller pod is not running and an election happens", func() {
		It("should succeed afterwards", func() {
			//will remove this once json patch will be available
			labelSelector, err := labels.Parse(fmt.Sprint(utils2.AAQLabel + "=aaq-controller"))
			if err != nil {
				panic(err)
			}
			fieldSelector := fields.ParseSelectorOrDie("status.phase=" + string(k8sv1.PodRunning))
			controllerPods, err := f.K8sClient.CoreV1().Pods(f.AAQInstallNs).List(context.Background(),
				metav1.ListOptions{LabelSelector: labelSelector.String(), FieldSelector: fieldSelector.String()})
			Expect(err).ToNot(HaveOccurred())
			if len(controllerPods.Items) < 2 {
				Skip("Skip multi-replica test on single-replica deployments")
			}

			var newLeaderPodName string
			currLeaderPodName := utils.GetLeader(f.K8sClient, f.AAQInstallNs)
			Expect(currLeaderPodName).ToNot(Equal(""))

			By("Destroying the leading controller pod")
			Expect(f.K8sClient.CoreV1().Pods(f.AAQInstallNs).Delete(context.Background(), currLeaderPodName, metav1.DeleteOptions{})).To(Succeed())
			By("Making sure another pod take the lead")
			Eventually(func() string {
				newLeaderPodName = utils.GetLeader(f.K8sClient, f.AAQInstallNs)
				return newLeaderPodName
			}, 90*time.Second, 5*time.Second).ShouldNot(Equal(currLeaderPodName))

			By("Making sure leader is running")
			var leaderPod *k8sv1.Pod
			Eventually(func() error {
				leaderPod, err = f.K8sClient.CoreV1().Pods(f.AAQInstallNs).Get(context.Background(), newLeaderPodName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())
			Expect(matcher.ThisPod(leaderPod, f.K8sClient)()).To(matcher.HaveConditionTrue(k8sv1.PodReady))
		})
	})

})
