package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	schedulev1 "k8s.io/api/scheduling/v1"
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources/cluster"
	resourcesutils "kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/tests/utils"
	"reflect"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	conditions "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/framework"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk"
)

const (
	assertionPollInterval      = 2 * time.Second
	CompletionTimeout          = 270 * time.Second
	AAQControllerLabelSelector = resourcesutils.AAQLabel + "=" + resourcesutils.ControllerPodName
	AAQServerLabelSelector     = resourcesutils.AAQLabel + "=" + resourcesutils.AaqServerPodName

	aaqControllerPodPrefix = "aaq-controller-"
	aaqServerPodPrefix     = "aaq-server-"
)

var _ = Describe("ALL Operator tests", func() {
	Context("[Destructive]", func() {
		var _ = Describe("Operator tests", func() {
			f := framework.NewFramework("operator-test")

			// Condition flags can be found here with their meaning https://github.com/kubevirt/hyperconverged-cluster-operator/blob/main/docs/conditions.md
			It("Condition flags on CR should be healthy and operating", func() {
				Eventually(func() error {
					aaqObject, err := utils.GetAAQ(f)
					Expect(err).ToNot(HaveOccurred())
					conditionMap := sdk.GetConditionValues(aaqObject.Status.Conditions)
					if conditionMap[conditions.ConditionAvailable] != corev1.ConditionTrue {
						return fmt.Errorf("ConditionAvailable is false")
					}
					if conditionMap[conditions.ConditionProgressing] != corev1.ConditionFalse {
						return fmt.Errorf("ConditionProgressing is true")
					}
					if conditionMap[conditions.ConditionDegraded] != corev1.ConditionFalse {
						return fmt.Errorf("ConditionDegraded is true")
					}
					return nil
				}, 5*time.Minute, 2*time.Second).Should(BeNil())
			})
		})
	})

	var _ = Describe("Tests that require restore nodes", func() {
		var nodes *corev1.NodeList
		var aaqPods *corev1.PodList
		var err error
		f := framework.NewFramework("operator-delete-aaq-test")

		BeforeEach(func() {
			nodes, err = f.K8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(nodes.Items).ToNot(BeEmpty(), "There should be some compute node")
			Expect(err).ToNot(HaveOccurred())
			aaqPods = getAAQPods(f)
		})

		AfterEach(func() {
			var newAaqPods *corev1.PodList
			By("Restoring nodes")
			for _, node := range nodes.Items {
				Eventually(func() error {
					newNode, err := f.K8sClient.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					newNode.Spec = node.Spec
					_, err = f.K8sClient.CoreV1().Nodes().Update(context.TODO(), newNode, metav1.UpdateOptions{})
					return err
				}, 5*time.Minute, 2*time.Second).Should(BeNil())
			}

			By("Waiting for there to be amount of AAQ pods like before")
			Eventually(func() error {
				newAaqPods = getAAQPods(f)
				if len(aaqPods.Items) != len(newAaqPods.Items) {
					return fmt.Errorf("Original number of aaq pods: %d\n is diffrent from the new number of aaq pods: %d\n", len(aaqPods.Items), len(newAaqPods.Items))
				}
				return nil
			}, 5*time.Minute, 2*time.Second).Should(BeNil())

			for _, newAaqPod := range newAaqPods.Items {
				By(fmt.Sprintf("Waiting for AAQ pod %s to be ready", newAaqPod.Name))
				err := utils.WaitTimeoutForPodReady(f.K8sClient, newAaqPod.Name, newAaqPod.Namespace, 2*time.Minute)
				Expect(err).ToNot(HaveOccurred())
			}

			Eventually(func() error {
				services, err := f.K8sClient.CoreV1().Services(f.AAQInstallNs).List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "failed getting AAQ services")
				for _, service := range services.Items {
					endpoint, err := f.K8sClient.CoreV1().Endpoints(f.AAQInstallNs).Get(context.TODO(), service.Name, metav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred(), "failed getting service endpoint")
					for _, subset := range endpoint.Subsets {
						if len(subset.NotReadyAddresses) > 0 {
							return fmt.Errorf("not all endpoints of service %s are ready", service.Name)
						}
					}
				}
				return nil
			}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
		})

		It("should deploy components that tolerate CriticalAddonsOnly taint", func() {
			aaq, err := utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
			criticalAddonsToleration := corev1.Toleration{
				Key:      "CriticalAddonsOnly",
				Operator: corev1.TolerationOpExists,
			}

			if !tolerationExists(aaq.Spec.Infra.Tolerations, criticalAddonsToleration) {
				Skip("Unexpected AAQ CR (not from aaq-cr.yaml), doesn't tolerate CriticalAddonsOnly")
			}

			By("adding taints to all nodes")
			criticalPodTaint := corev1.Taint{
				Key:    "CriticalAddonsOnly",
				Value:  "",
				Effect: corev1.TaintEffectNoExecute,
			}

			for _, node := range nodes.Items {
				Eventually(func() error {
					nodeCopy, err := f.K8sClient.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					nodeCopy.Spec.Taints = append(nodeCopy.Spec.Taints, criticalPodTaint)
					_, err = f.K8sClient.CoreV1().Nodes().Update(context.TODO(), nodeCopy, metav1.UpdateOptions{})
					return err
				}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

				Eventually(func() error {
					nodeCopy, err := f.K8sClient.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					if !nodeHasTaint(*nodeCopy, criticalPodTaint) {
						return fmt.Errorf("node still doesn't have the criticalPodTaint")
					}
					return nil
				}, 5*time.Minute, 2*time.Second).Should(BeNil())
			}

			By("Checking that all pods are running")
			for _, aaqPods := range aaqPods.Items {
				By(fmt.Sprintf("AAQ pod: %s", aaqPods.Name))
				podUpdated, err := f.K8sClient.CoreV1().Pods(aaqPods.Namespace).Get(context.TODO(), aaqPods.Name, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "failed setting taint on node")
				Expect(podUpdated.Status.Phase).To(Equal(corev1.PodRunning))
			}
		})
	})

	var _ = Describe("Operator delete AAQ CR tests", func() {
		var cr *aaqv1.AAQ
		var err error
		f := framework.NewFramework("operator-delete-aaq-test")
		var aaqPods *corev1.PodList

		BeforeEach(func() {
			cr, err = utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
			aaqPods = getAAQPods(f)
		})

		AfterEach(func() {
			removeAAQ(f, cr)
			updateOrCreateAAQComponentsAndEnsureReady(f, cr, aaqPods)
		})

		It("should remove/install AAQ a number of times successfully", func() {
			for i := 0; i < 3; i++ {
				err := f.AaqClient.AaqV1alpha1().AAQs().Delete(context.TODO(), cr.Name, metav1.DeleteOptions{})
				Expect(err).ToNot(HaveOccurred())
				updateOrCreateAAQComponentsAndEnsureReady(f, cr, aaqPods)
			}
		})

	})

	var _ = Describe("aaq Operator deployment + aaq CR delete tests", func() {
		var aaqBackup *aaqv1.AAQ
		var aaqOperatorDeploymentBackup *appsv1.Deployment
		nodeSelectorTestValue := map[string]string{"kubernetes.io/arch": runtime.GOARCH}
		tolerationTestValue := []corev1.Toleration{{Key: "test", Value: "123"}, {Key: "CriticalAddonsOnly", Value: string(corev1.TolerationOpExists)}}
		f := framework.NewFramework("operator-delete-aaq-test")

		BeforeEach(func() {
			currentCR, err := utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())

			aaqBackup = &aaqv1.AAQ{
				ObjectMeta: metav1.ObjectMeta{
					Name: currentCR.Name,
				},
				Spec: currentCR.Spec,
			}

			currentAAQOperatorDeployment, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), "aaq-operator", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			aaqOperatorDeploymentBackup = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aaq-operator",
					Namespace: f.AAQInstallNs,
				},
				Spec: currentAAQOperatorDeployment.Spec,
			}

			removeAAQCrAndOperator(f, aaqBackup)
		})

		AfterEach(func() {
			removeAAQCrAndOperator(f, aaqBackup)
			deployAAQOperator(f, aaqBackup, aaqOperatorDeploymentBackup)
		})

		It("Should install AAQ infrastructure pods with node placement", func() {
			By("Creating modified AAQ CR, with infra nodePlacement")
			localSpec := aaqBackup.Spec.DeepCopy()
			localSpec.Infra = f.GetNodePlacementValuesWithRandomNodeAffinity(nodeSelectorTestValue, tolerationTestValue)

			tempAaqCr := &aaqv1.AAQ{
				ObjectMeta: metav1.ObjectMeta{
					Name: aaqBackup.Name,
				},
				Spec: *localSpec,
			}

			deployAAQOperator(f, tempAaqCr, aaqOperatorDeploymentBackup)

			By("Testing all infra deployments have the chosen node placement")
			for _, deploymentName := range []string{"aaq-server", "aaq-controller"} {
				deployment, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				err = f.PodSpecHasTestNodePlacementValues(deployment.Spec.Template.Spec, localSpec.Infra)
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})

	var _ = Describe("Strict Reconciliation tests", func() {
		f := framework.NewFramework("strict-reconciliation-test")

		It("aaq-deployment replicas back to original value on attempt to scale", func() {
			By("Overwrite number of replicas with 10")
			deploymentName := "aaq-controller"
			originalReplicaVal := scaleDeployment(f, deploymentName, 10)

			By("Ensuring original value of replicas restored & extra deployment pod was cleaned up")
			Eventually(func() error {
				depl, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				if *depl.Spec.Replicas != originalReplicaVal {
					return fmt.Errorf("original replicas value in deployment should be restore by aaq-operator")
				}
				return nil
			}, 5*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})

		It("Service spec.selector restored on overwrite attempt", func() {
			service, err := f.K8sClient.CoreV1().Services(f.AAQInstallNs).Get(context.TODO(), "aaq-server", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			originalSelectorVal := service.Spec.Selector[resourcesutils.AAQLabel]

			By("Overwrite spec.selector with empty string")
			service.Spec.Selector[resourcesutils.AAQLabel] = ""
			_, err = f.K8sClient.CoreV1().Services(f.AAQInstallNs).Update(context.TODO(), service, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				svc, err := f.K8sClient.CoreV1().Services(f.AAQInstallNs).Get(context.TODO(), "aaq-server", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				if svc.Spec.Selector[resourcesutils.AAQLabel] != originalSelectorVal {
					return fmt.Errorf("original spec.selector value: %s\n should Matches current: %s\n", originalSelectorVal, svc.Spec.Selector[resourcesutils.AAQLabel])
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})

		It("ServiceAccount values restored on update attempt", func() {
			serviceAccount, err := f.K8sClient.CoreV1().ServiceAccounts(f.AAQInstallNs).Get(context.TODO(), resourcesutils.ControllerServiceAccountName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Change one of ServiceAccount labels")
			serviceAccount.Labels[resourcesutils.AAQLabel] = "somebadvalue"

			_, err = f.K8sClient.CoreV1().ServiceAccounts(f.AAQInstallNs).Update(context.TODO(), serviceAccount, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting until label value restored")
			Eventually(func() error {
				sa, err := f.K8sClient.CoreV1().ServiceAccounts(f.AAQInstallNs).Get(context.TODO(), resourcesutils.ControllerServiceAccountName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				if sa.Labels[resourcesutils.AAQLabel] != "" {
					return fmt.Errorf("aaq label should be restores if modifed")
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})

		It("Certificate restored to ConfigMap on deletion attempt", func() {
			configMap, err := f.K8sClient.CoreV1().ConfigMaps(f.AAQInstallNs).Get(context.TODO(), "aaq-server-signer-bundle", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Empty ConfigMap's data")
			configMap.Data = map[string]string{}

			_, err = f.K8sClient.CoreV1().ConfigMaps(f.AAQInstallNs).Update(context.TODO(), configMap, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting until ConfigMap's data is not empty")
			Eventually(func() error {
				cm, err := f.K8sClient.CoreV1().ConfigMaps(f.AAQInstallNs).Get(context.TODO(), "aaq-server-signer-bundle", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				if len(cm.Data) == 0 {
					return fmt.Errorf("aaq-server-signer-bundle data should be filled by aaq-operator if modified")
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		})
	})

	var _ = Describe("Operator cert config tests", func() {
		var aaqBackup *aaqv1.AAQ
		var err error
		f := framework.NewFramework("operator-cert-config-test")

		BeforeEach(func() {
			aaqBackup, err = utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			if aaqBackup == nil {
				return
			}

			cr, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), aaqBackup.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			cr.Spec.CertConfig = aaqBackup.Spec.CertConfig

			_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), cr, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		validateCertConfig := func(obj metav1.Object, lifetime, refresh string) {
			cca, ok := obj.GetAnnotations()["operator.aaq.kubevirt.io/certConfig"]
			Expect(ok).To(BeTrue())
			certConfig := make(map[string]interface{})
			err := json.Unmarshal([]byte(cca), &certConfig)
			Expect(err).ToNot(HaveOccurred())
			l, ok := certConfig["lifetime"]
			Expect(ok).To(BeTrue())
			Expect(l.(string)).To(Equal(lifetime))
			r, ok := certConfig["refresh"]
			Expect(ok).To(BeTrue())
			Expect(r.(string)).To(Equal(refresh))
		}

		It("should allow update", func() {
			origNotAfterTime := map[string]time.Time{}
			caSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), "aaq-server", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			serverSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), resourcesutils.SecretResourceName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			for _, s := range []*corev1.Secret{caSecret, serverSecret} {
				notAfterAnn := s.Annotations["auth.openshift.io/certificate-not-after"]
				notAfterTIme, err := time.Parse(time.RFC3339, notAfterAnn)
				Expect(err).ToNot(HaveOccurred())
				origNotAfterTime[s.Name] = notAfterTIme
			}

			Eventually(func() error {
				cr, err := utils.GetAAQ(f)
				Expect(err).ToNot(HaveOccurred())
				cr.Spec.CertConfig = &aaqv1.AAQCertConfig{
					CA: &aaqv1.CertConfig{
						Duration:    &metav1.Duration{Duration: time.Minute * 20},
						RenewBefore: &metav1.Duration{Duration: time.Minute * 5},
					},
					Server: &aaqv1.CertConfig{
						Duration:    &metav1.Duration{Duration: time.Minute * 5},
						RenewBefore: &metav1.Duration{Duration: time.Minute * 2},
					},
				}
				newCR, err := f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), cr, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				Expect(newCR.Spec.CertConfig).To(Equal(cr.Spec.CertConfig))
				By("Cert config update complete")
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

			By("Verify secrets have been modified")
			Eventually(func() error {
				for _, secret := range []*corev1.Secret{caSecret, serverSecret} {
					currSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), secret.Name, metav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					notAfterAnn := currSecret.Annotations["auth.openshift.io/certificate-not-after"]
					notAfterTime, err := time.Parse(time.RFC3339, notAfterAnn)
					Expect(err).ToNot(HaveOccurred())
					if notAfterTime.Equal(origNotAfterTime[currSecret.Name]) {
						return fmt.Errorf("not-before-time annotation should change")
					}
				}
				return nil
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

			By("Verify secrets have the right values")
			Eventually(func() error {
				caSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), "aaq-server", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				serverSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), resourcesutils.SecretResourceName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				currCaSecretNotBeforeAnn := caSecret.Annotations["auth.openshift.io/certificate-not-before"]
				caNotBeforeTime, err := time.Parse(time.RFC3339, currCaSecretNotBeforeAnn)
				Expect(err).ToNot(HaveOccurred())
				currCaSecretNotAnn := caSecret.Annotations["auth.openshift.io/certificate-not-after"]
				caNotAfterTime, err := time.Parse(time.RFC3339, currCaSecretNotAnn)
				Expect(err).ToNot(HaveOccurred())
				if caNotAfterTime.Sub(caNotBeforeTime) < time.Minute*20 {
					return fmt.Errorf("Not-Before (%s) should be 20 minutes before Not-After (%s)\n", currCaSecretNotBeforeAnn, currCaSecretNotAnn)
				}
				if caNotAfterTime.Sub(caNotBeforeTime)-(time.Minute*20) > time.Second {
					return fmt.Errorf("Not-Before (%s) should be 20 minutes before Not-After (%s) with 1 second toleration\n", currCaSecretNotBeforeAnn, currCaSecretNotAnn)
				}
				// 20m - 5m = 15m
				validateCertConfig(caSecret, "20m0s", "15m0s")

				currServerSecretNotBeforeAnn := serverSecret.Annotations["auth.openshift.io/certificate-not-before"]
				serverNotBeforeTime, err := time.Parse(time.RFC3339, currServerSecretNotBeforeAnn)
				Expect(err).ToNot(HaveOccurred())
				currServerNotAfterAnn := serverSecret.Annotations["auth.openshift.io/certificate-not-after"]
				serverNotAfterTime, err := time.Parse(time.RFC3339, currServerNotAfterAnn)
				Expect(err).ToNot(HaveOccurred())
				if serverNotAfterTime.Sub(serverNotBeforeTime) < time.Minute*5 {
					return fmt.Errorf("Not-Before (%s) should be 5 minutes before Not-After (%s)\n", currServerSecretNotBeforeAnn, currServerNotAfterAnn)
				}
				if serverNotAfterTime.Sub(serverNotBeforeTime)-(time.Minute*5) > time.Second {
					return fmt.Errorf("Not-Before (%s) should be 5 minutes before Not-After (%s) with 1 second toleration\n", currServerSecretNotBeforeAnn, currServerNotAfterAnn)
				}
				// 5m - 2m = 3m
				validateCertConfig(serverSecret, "5m0s", "3m0s")
				return nil
			}, 2*time.Minute, 1*time.Second).Should(BeNil())
		})
	})

	var _ = Describe("Priority class tests", func() {
		var (
			aaq                   *aaqv1.AAQ
			systemClusterCritical = aaqv1.AAQPriorityClass("system-cluster-critical")
			osUserCrit            = &schedulev1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourcesutils.AAQPriorityClass,
				},
				Value: 10000,
			}
		)
		f := framework.NewFramework("operator-priority-class-test")
		verifyPodPriorityClass := func(prefix, priorityClassName, labelSelector string) {
			Eventually(func() string {
				controllerPod, err := utils.FindPodByPrefix(f.K8sClient, f.AAQInstallNs, prefix, labelSelector)
				if err != nil {
					return ""
				}
				return controllerPod.Spec.PriorityClassName
			}, 2*time.Minute, 1*time.Second).Should(BeEquivalentTo(priorityClassName))
		}

		BeforeEach(func() {
			list, err := f.K8sClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return
			}
			if len(list.Items) < 3 {
				Skip("This test require at least 3 nodes")
			}
			aaq, err := utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
			if aaq.Spec.PriorityClass != nil {
				By(fmt.Sprintf("Current priority class is: [%s]", *aaq.Spec.PriorityClass))
			}
		})

		AfterEach(func() {
			if aaq == nil {
				return
			}
			cr, err := utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())

			cr.Spec.PriorityClass = aaq.Spec.PriorityClass
			_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), cr, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			if !utils.IsOpenshift(f.K8sClient) {
				Eventually(func() error {
					if !errors.IsNotFound(f.K8sClient.SchedulingV1().PriorityClasses().Delete(context.TODO(), osUserCrit.Name, metav1.DeleteOptions{})) {
						return fmt.Errorf("Priority class " + osUserCrit.Name + " should not exsist ")
					}
					return nil
				}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
			}
			By("Ensuring the AAQ priority class is restored")
			prioClass := ""
			if cr.Spec.PriorityClass != nil {
				prioClass = string(*cr.Spec.PriorityClass)
			} else if utils.IsOpenshift(f.K8sClient) {
				prioClass = osUserCrit.Name
			}
			podToSelector := map[string]string{aaqControllerPodPrefix: AAQControllerLabelSelector, aaqServerPodPrefix: AAQServerLabelSelector}
			verifyPodPriorityClass(aaqControllerPodPrefix, prioClass, AAQControllerLabelSelector)
			verifyPodPriorityClass(aaqServerPodPrefix, prioClass, "")

			for prefix := range podToSelector {
				Eventually(func() error {
					pod, err := utils.FindPodByPrefix(f.K8sClient, f.AAQInstallNs, prefix, podToSelector[prefix])
					if err != nil {
						return err
					}
					if pod.Status.Phase != corev1.PodRunning {
						return fmt.Errorf(pod.Name + " is not running")
					}
					return nil
				}, 3*time.Minute, 1*time.Second).Should(BeNil())
			}
		})

		It("should use kubernetes priority class if set", func() {
			cr, err := utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
			By("Setting the priority class to system cluster critical, which is known to exist")
			cr.Spec.PriorityClass = &systemClusterCritical
			_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), cr, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
			By("Verifying the AAQ deployment is updated")
			verifyPodPriorityClass(aaqControllerPodPrefix, string(systemClusterCritical), AAQControllerLabelSelector)
			By("Verifying the AAQ api server is updated")
			verifyPodPriorityClass(aaqServerPodPrefix, string(systemClusterCritical), AAQServerLabelSelector)

		})

		It("should use openshift priority class if not set and available", func() {
			if utils.IsOpenshift(f.K8sClient) {
				Skip("This test is not needed in OpenShift")
			}
			_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(context.TODO(), osUserCrit, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			By("Verifying the AAQ control plane is updated")
			verifyPodPriorityClass(aaqControllerPodPrefix, osUserCrit.Name, AAQControllerLabelSelector)
			verifyPodPriorityClass(aaqServerPodPrefix, osUserCrit.Name, AAQServerLabelSelector)
		})
	})
	Context("Gating tests", func() {
		var cr *aaqv1.AAQ
		var err error
		f := framework.NewFramework("operator-delete-mtq-test")

		BeforeEach(func() {
			cr, err = utils.GetAAQ(f)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			Eventually(func() error {
				currCr, err := utils.GetAAQ(f)
				if err != nil {
					return err
				}
				currCr.Spec.NamespaceSelector = cr.Spec.NamespaceSelector
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), currCr, metav1.UpdateOptions{})
				return err
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())
		})

		It("Should be able to modify gated namespaces", func() {
			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"namespace": "fakenamespace"},
			}
			Eventually(func() error {
				currCr, err := utils.GetAAQ(f)
				if err != nil {
					return err
				}
				newCr := currCr.DeepCopy()
				newCr.Spec.NamespaceSelector = labelSelector
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), newCr, metav1.UpdateOptions{})
				return err
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

			Eventually(func() error {
				mwc, err := f.K8sClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), cluster.MutatingWebhookConfigurationName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(mwc.Webhooks[0].NamespaceSelector, labelSelector) {
					return fmt.Errorf("NamespaceSelector of MutatingWebhookConfigurations should be propagated")
				}
				return nil
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		})

		It("if namespace selector is not set, should use the default target label", func() {
			By("Updating AAQ to use a nil namespace selector")
			Eventually(func() error {
				currCr, err := utils.GetAAQ(f)
				if err != nil {
					return err
				}
				newCr := currCr.DeepCopy()
				newCr.Spec.NamespaceSelector = nil
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), newCr, metav1.UpdateOptions{})
				return err
			}).WithTimeout(90*time.Second).WithPolling(5*time.Second).ShouldNot(HaveOccurred(), "cannot update AAQ")

			expectedNamespaceSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: cluster.DefaultNamespaceSelectorLabel, Operator: metav1.LabelSelectorOpExists},
				},
			}

			Eventually(func() error {
				mwc, err := f.K8sClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), cluster.MutatingWebhookConfigurationName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(mwc.Webhooks[0].NamespaceSelector, expectedNamespaceSelector) {
					return fmt.Errorf("namespace selector does not target the default label. actual: %v, expected: %v", *mwc.Webhooks[0].NamespaceSelector, *expectedNamespaceSelector)
				}
				return nil
			}).WithTimeout(90*time.Second).WithPolling(5*time.Second).ShouldNot(HaveOccurred(), "default namespace selector is not as expected")
		})

		It("if namespace selector is empty, should target any namespace", func() {
			By("Updating AAQ to use a nil namespace selector")
			Eventually(func() error {
				currCr, err := utils.GetAAQ(f)
				if err != nil {
					return err
				}
				newCr := currCr.DeepCopy()
				newCr.Spec.NamespaceSelector = &metav1.LabelSelector{}
				_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), newCr, metav1.UpdateOptions{})
				return err
			}).WithTimeout(90*time.Second).WithPolling(5*time.Second).ShouldNot(HaveOccurred(), "cannot update AAQ")

			Eventually(func() error {
				mwc, err := f.K8sClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), cluster.MutatingWebhookConfigurationName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				selector := mwc.Webhooks[0].NamespaceSelector

				if selector == nil {
					return nil
				}

				if len(selector.MatchLabels) != 0 || len(selector.MatchExpressions) != 0 {
					return fmt.Errorf("namespace selector should be empty. actual: %v", *mwc.Webhooks[0].NamespaceSelector)
				}
				
				return nil
			}).WithTimeout(90*time.Second).WithPolling(5*time.Second).ShouldNot(HaveOccurred(), "default namespace selector is not as expected")
		})
	})
})

func getAAQPods(f *framework.Framework) *corev1.PodList {
	By("Getting AAQ pods")
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/component": "multi-tenant"}}
	aaqPods, err := f.K8sClient.CoreV1().Pods(f.AAQInstallNs).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed listing aaq pods")
	ExpectWithOffset(1, aaqPods.Items).ToNot(BeEmpty(), "no aaq pods found")
	return aaqPods
}

func removeAAQCrAndOperator(f *framework.Framework, aaq *aaqv1.AAQ) {
	removeAAQ(f, aaq)
	By("Deleting AAQ operator")
	err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Delete(context.TODO(), "aaq-operator", metav1.DeleteOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	By("Waiting for AAQ operator deployment to be deleted")
	EventuallyWithOffset(1, func() error {
		if aaqOperatorDeploymentGone(f) == false {
			return fmt.Errorf("operator deployment should be deleted")
		}
		return nil
	}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
}
func removeAAQ(f *framework.Framework, cr *aaqv1.AAQ) {
	By("Deleting AAQ CR if exists")
	_ = f.AaqClient.AaqV1alpha1().AAQs().Delete(context.TODO(), cr.Name, metav1.DeleteOptions{})

	By("Waiting for AAQ CR and infra deployments to be gone now that we are sure there's no AAQ CR")

	EventuallyWithOffset(1, func() error {
		for _, deploymentName := range []string{"aaq-server", "aaq-controller"} {
			_, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if !errors.IsNotFound(err) {
				return fmt.Errorf(deploymentName + " should be deleted")
			}
		}
		_, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), cr.Name, metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			return fmt.Errorf("aaq should be deleted")
		}
		return nil
	}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
}
func deployAAQOperator(f *framework.Framework, aaq *aaqv1.AAQ, aaqOperatorDeployment *appsv1.Deployment) {
	By("Re-creating AAQ (CR and deployment)")
	_, err := f.AaqClient.AaqV1alpha1().AAQs().Create(context.TODO(), aaq, metav1.CreateOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	By("Recreating AAQ operator")
	_, err = f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Create(context.TODO(), aaqOperatorDeployment, metav1.CreateOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	By("Verifying AAQ server, controller exist, before continuing")
	EventuallyWithOffset(1, func() error {
		if !infraDeploymentAvailable(f, aaq) {
			return fmt.Errorf("aaq operator should be available")
		}
		return nil
	}, CompletionTimeout, assertionPollInterval).ShouldNot(HaveOccurred())
}
func updateOrCreateAAQComponentsAndEnsureReady(f *framework.Framework, updatedAaq *aaqv1.AAQ, aaqPods *corev1.PodList) {
	By("Check if AAQ CR exists")
	aaq, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), updatedAaq.Name, metav1.GetOptions{})
	if err == nil {
		if aaq.DeletionTimestamp == nil {
			By("AAQ CR exists")
			aaq.Spec = updatedAaq.Spec
			_, err = f.AaqClient.AaqV1alpha1().AAQs().Update(context.TODO(), aaq, metav1.UpdateOptions{})
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
			return
		}

		By("Waiting for AAQ CR deletion")
		EventuallyWithOffset(1, func() error {
			_, err = f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), updatedAaq.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
			return fmt.Errorf("AAQ with deletion timestemp is not being deleted")
		}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())
	} else {
		ExpectWithOffset(1, errors.IsNotFound(err)).To(BeTrue())
	}

	aaq = &aaqv1.AAQ{
		ObjectMeta: metav1.ObjectMeta{
			Name: updatedAaq.Name,
		},
		Spec: updatedAaq.Spec,
	}

	By("Create AAQ CR")
	_, err = f.AaqClient.AaqV1alpha1().AAQs().Create(context.TODO(), aaq, metav1.CreateOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	waitForAAQToBeReady(f, updatedAaq, aaqPods)
}

func waitForAAQToBeReady(f *framework.Framework, cr *aaqv1.AAQ, aaqPods *corev1.PodList) {
	var newAaqPods *corev1.PodList

	By("Waiting for AAQ CR to be Available")
	EventuallyWithOffset(2, func() error {
		aaq, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), cr.Name, metav1.GetOptions{})
		ExpectWithOffset(2, err).ToNot(HaveOccurred())
		ExpectWithOffset(2, aaq.Status.Phase).ShouldNot(Equal(sdkapi.PhaseError))
		if !conditions.IsStatusConditionTrue(aaq.Status.Conditions, conditions.ConditionAvailable) {
			return fmt.Errorf("AAQ CR should be Available")
		}
		return nil
	}, 10*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

	By("Verifying AAQ server and controller exist, before continuing")
	EventuallyWithOffset(2, func() error {
		if !infraDeploymentAvailable(f, cr) {
			return fmt.Errorf("AAQ deploymets should be Available")
		}
		return nil
	}, CompletionTimeout, assertionPollInterval).ShouldNot(HaveOccurred(), "Timeout reading AAQ deployments")

	By("Waiting for there to be as many AAQ pods as before")
	EventuallyWithOffset(2, func() error {
		newAaqPods = getAAQPods(f)
		if len(aaqPods.Items) != len(newAaqPods.Items) {
			return fmt.Errorf("number of aaq pods: %d\n should be equal to new number of aaq pods: %d\n", len(aaqPods.Items), len(newAaqPods.Items))
		}
		return nil
	}, 5*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

	for _, newAaqPod := range newAaqPods.Items {
		By(fmt.Sprintf("Waiting for AAQ pod %s to be ready", newAaqPod.Name))
		err := utils.WaitTimeoutForPodReady(f.K8sClient, newAaqPod.Name, newAaqPod.Namespace, 20*time.Minute)
		ExpectWithOffset(2, err).ToNot(HaveOccurred())
	}
}

func tolerationExists(tolerations []corev1.Toleration, testValue corev1.Toleration) bool {
	for _, toleration := range tolerations {
		if reflect.DeepEqual(toleration, testValue) {
			return true
		}
	}
	return false
}

func nodeHasTaint(node corev1.Node, testedTaint corev1.Taint) bool {
	for _, taint := range node.Spec.Taints {
		if reflect.DeepEqual(taint, testedTaint) {
			return true
		}
	}
	return false
}

func infraDeploymentAvailable(f *framework.Framework, cr *aaqv1.AAQ) bool {
	aaq, _ := f.AaqClient.AaqV1alpha1().AAQs().Get(context.TODO(), cr.Name, metav1.GetOptions{})
	if !conditions.IsStatusConditionTrue(aaq.Status.Conditions, conditions.ConditionAvailable) {
		return false
	}

	for _, deploymentName := range []string{"aaq-server", "aaq-controller"} {
		_, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return false
		}
	}

	return true
}

func aaqOperatorDeploymentGone(f *framework.Framework) bool {
	_, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), "aaq-operator", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return true
	}
	ExpectWithOffset(2, err).ToNot(HaveOccurred())
	return false
}

func scaleDeployment(f *framework.Framework, deploymentName string, replicas int32) int32 {
	operatorDeployment, err := f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	originalReplicas := *operatorDeployment.Spec.Replicas
	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec/replicas", "value": %d}]`, replicas)
	_, err = f.K8sClient.AppsV1().Deployments(f.AAQInstallNs).Patch(context.TODO(), deploymentName, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return originalReplicas
}
