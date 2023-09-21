package tests

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	utils2 "kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/applications-aware-quota/tests/framework"
	"kubevirt.io/applications-aware-quota/tests/utils"
	"kubevirt.io/kubevirt/tests/framework/matcher"
	"kubevirt.io/kubevirt/tests/util"
	"time"

	"kubevirt.io/kubevirt/tests/framework/kubevirt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/tests/flags"
)

var _ = Describe("Leader election test", func() {
	f := framework.NewFramework("operator-test")

	var (
		virtClient kubecli.KubevirtClient
	)

	BeforeEach(func() {
		virtClient = kubevirt.Client()
	})

	Context("when the controller pod is not running and an election happens", func() {
		It("should succeed afterwards", func() {
			//will remove this once json patch will be available
			labelSelector, err := labels.Parse(fmt.Sprint(utils2.AAQLabel + "=aaq-controller"))
			util.PanicOnError(err)
			fieldSelector := fields.ParseSelectorOrDie("status.phase=" + string(k8sv1.PodRunning))
			controllerPods, err := virtClient.CoreV1().Pods(flags.KubeVirtInstallNamespace).List(context.Background(),
				metav1.ListOptions{LabelSelector: labelSelector.String(), FieldSelector: fieldSelector.String()})
			Expect(err).ToNot(HaveOccurred())
			if len(controllerPods.Items) < 2 {
				Skip("Skip multi-replica test on single-replica deployments")
			}
			//

			var newLeaderPodName string
			currLeaderPodName := utils.GetLeader(virtClient, f.AAQInstallNs)
			Expect(currLeaderPodName).ToNot(Equal(""))

			By("Destroying the leading controller pod")
			Expect(virtClient.CoreV1().Pods(f.AAQInstallNs).Delete(context.Background(), currLeaderPodName, metav1.DeleteOptions{})).To(Succeed())
			By("Making sure another pod take the lead")
			Eventually(func() string {
				newLeaderPodName = utils.GetLeader(virtClient, f.AAQInstallNs)
				return newLeaderPodName
			}, 90*time.Second, 5*time.Second).ShouldNot(Equal(currLeaderPodName))

			By("Making sure leader is running")
			var leaderPod *k8sv1.Pod
			Eventually(func() error {
				leaderPod, err = virtClient.CoreV1().Pods(f.AAQInstallNs).Get(context.Background(), newLeaderPodName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())
			Expect(matcher.ThisPod(leaderPod)()).To(matcher.HaveConditionTrue(k8sv1.PodReady))

		})

	})

})
