package tests

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	rq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/builders"
	"kubevirt.io/application-aware-quota/tests/framework"
	"reflect"
	"time"
)

var _ = Describe("ResourceQuotas mirrors ApplicationAwareResourceQuota non-scheduable resources", func() {
	f := framework.NewFramework("resourcequota")

	It("should create a ApplicationAwareResourceQuota and ensure its status is promptly calculated.", func(ctx context.Context) {
		noneScheduableResources := v1.ResourceList{
			v1.ResourceServices:               resource.MustParse("10"),
			v1.ResourceConfigMaps:             resource.MustParse("10"),
			v1.ResourceSecrets:                resource.MustParse("10"),
			v1.ResourcePersistentVolumeClaims: resource.MustParse("10"),
			v1.ResourceRequestsStorage:        resource.MustParse("10Gi"),
			core.V1ResourceByStorageClass(classGold, v1.ResourcePersistentVolumeClaims): resource.MustParse("10"),
			core.V1ResourceByStorageClass(classGold, v1.ResourceRequestsStorage):        resource.MustParse("10Gi"),
			v1.ResourceName("count/replicasets.apps"):                                   resource.MustParse("5"),
		}
		arq := builders.NewArqBuilder().WithName("test-quota").WithNamespace(f.Namespace.GetName()).WithResource(v1.ResourcePods, resource.MustParse("5"))

		for resource, quantity := range noneScheduableResources {
			arq = arq.WithResource(resource, quantity)
		}

		By("Creating a ApplicationAwareResourceQuota")
		_, err := createApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, arq.Build())
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror RQ")
		Eventually(func() error {
			rq, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			if err != nil {
				return err
			}
			arq, err := f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			if err != nil {
				return err
			}

			// Check if all nonScheduableResources are in the status.hard of the ResourceQuota
			for resource := range noneScheduableResources {
				quantity, exists := rq.Status.Hard[resource]
				if !exists {
					return fmt.Errorf("resource %s does not exist in managed ResourceQuota status.hard", resource)
				}
				expectedQuantity := arq.Status.Hard[resource]
				if quantity.Cmp(expectedQuantity) != 0 {
					return fmt.Errorf("resource %s has an unexpected quantity in managed ResourceQuota status.hard: expected %s, got %s", resource, expectedQuantity.String(), quantity.String())
				}
			}
			_, exists := rq.Status.Hard[v1.ResourcePods]
			if exists {
				return fmt.Errorf("managed quota should not include scheduabled resources such as %v", v1.ResourcePods)
			}

			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("should delete managed quota once arq doesn't contain nonscheduable resources", func(ctx context.Context) {
		arq := builders.NewArqBuilder().WithName("test-quota").
			WithNamespace(f.Namespace.GetName()).
			WithResource(v1.ResourcePods, resource.MustParse("5")).
			WithResource(v1.ResourceServices, resource.MustParse("10")).
			Build()

		By("Creating a ApplicationAwareResourceQuota")
		_, err := createApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, arq)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror RQ")
		Eventually(func() error {
			rq, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			if err != nil {
				return err
			}
			_, exists := rq.Status.Hard[v1.ResourceServices]
			if !exists {
				return fmt.Errorf("resource %s does not exist in ResourceQuota status.hard", v1.ResourceServices)
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		arq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		delete(arq.Spec.Hard, v1.ResourceServices)

		arq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Update(ctx, arq, v12.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Waiting RQ to be deleted")
		Eventually(func() bool {
			_, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			return errors.IsNotFound(err)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue(), "managed ResourceQuota should be deleted")
	})

	It("should delete managed quota once arq doesn't contain nonscheduable resources", func(ctx context.Context) {
		arq := builders.NewArqBuilder().WithName("test-quota").
			WithNamespace(f.Namespace.GetName()).
			WithResource(v1.ResourcePods, resource.MustParse("5")).
			WithResource(v1.ResourceServices, resource.MustParse("10")).
			Build()

		By("Creating a ApplicationAwareResourceQuota")
		_, err := createApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, arq)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror RQ")
		Eventually(func() error {
			rq, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			if err != nil {
				return err
			}
			_, exists := rq.Status.Hard[v1.ResourceServices]
			if !exists {
				return fmt.Errorf("resource %s does not exist in ResourceQuota status.hard", v1.ResourceServices)
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		err = f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Delete(ctx, "test-quota", v12.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Waiting RQ to be deleted")
		Eventually(func() bool {
			_, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			return errors.IsNotFound(err)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue(), "managed ResourceQuota should be deleted")
	})

	It("should update managed quota once arq nonscheduable resources boundaries change", func(ctx context.Context) {
		arq := builders.NewArqBuilder().WithName("test-quota").
			WithNamespace(f.Namespace.GetName()).
			WithResource(v1.ResourcePods, resource.MustParse("5")).
			WithResource(v1.ResourceServices, resource.MustParse("10")).
			Build()

		By("Creating a ApplicationAwareResourceQuota")
		_, err := createApplicationAwareResourceQuota(ctx, f.AaqClient, f.Namespace.Name, arq)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror RQ")
		Eventually(func() error {
			rq, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			if err != nil {
				return err
			}
			quantity, exists := rq.Status.Hard[v1.ResourceServices]
			if !exists {
				return fmt.Errorf("resource %s does not exist in ResourceQuota status.hard", v1.ResourceServices)
			}
			expectedQuantity := resource.MustParse("10")
			if quantity.Cmp(expectedQuantity) != 0 {
				return fmt.Errorf("resource %s has an unexpected quantity in managed ResourceQuota status.hard: expected %s, got %s", v1.ResourceServices, expectedQuantity.String(), quantity.String())
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		arq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		arq.Spec.Hard[v1.ResourceServices] = resource.MustParse("15")

		arq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareResourceQuotas(f.Namespace.GetName()).Update(ctx, arq, v12.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror RQ to get updated")
		Eventually(func() error {
			rq, err := f.K8sClient.CoreV1().ResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota"+rq_controller.RQSuffix, v12.GetOptions{})
			if err != nil {
				return err
			}
			quantity, exists := rq.Status.Hard[v1.ResourceServices]
			if !exists {
				return fmt.Errorf("resource %s does not exist in ResourceQuota status.hard", v1.ResourceServices)
			}
			expectedQuantity := resource.MustParse("15")
			if quantity.Cmp(expectedQuantity) != 0 {
				return fmt.Errorf("resource %s has an unexpected quantity in managed ResourceQuota status.hard: expected %s, got %s", v1.ResourceServices, expectedQuantity.String(), quantity.String())
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("ApplicationAwareAppliedClusterResourceQuota mirrors ApplicationAwareClusterResourceQuota", func() {
	f := framework.NewFramework("resourcequota")
	var labelSelector *v12.LabelSelector

	BeforeEach(func() {
		aaq, err := f.AaqClient.AaqV1alpha1().AAQs().Get(context.Background(), "aaq", v12.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		if !aaq.Spec.Configuration.AllowApplicationAwareClusterResourceQuota {
			aaq.Spec.Configuration.AllowApplicationAwareClusterResourceQuota = true
			_, err := f.AaqClient.AaqV1alpha1().AAQs().Update(context.Background(), aaq, v12.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				return aaqControllerReady(f.K8sClient, f.AAQInstallNs)
			}, 1*time.Minute, 1*time.Second).ShouldNot(BeTrue(), "config change should trigger redeployment of the controller")
			Eventually(func() bool {
				return aaqControllerReady(f.K8sClient, f.AAQInstallNs)
			}, 10*time.Minute, 1*time.Second).Should(BeTrue(), "aaq-controller should be ready with the new config Eventually")

		}
		labelSelector = &v12.LabelSelector{
			MatchLabels: map[string]string{"foo": "foo"},
		}
		// Function to add label to a namespace
		err = addLabelToNamespace(f.K8sClient, "default", "foo", "foo")
		Expect(err).ToNot(HaveOccurred())
		err = addLabelToNamespace(f.K8sClient, f.Namespace.GetName(), "foo", "foo")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Function to add label to a namespace
		err := removeLabelFromNamespace(f.K8sClient, "default", "foo")
		Expect(err).ToNot(HaveOccurred())
		err = removeLabelFromNamespace(f.K8sClient, f.Namespace.GetName(), "foo")
		Expect(err).ToNot(HaveOccurred())

		acrqs, err := f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().List(context.Background(), v12.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		for _, acrq := range acrqs.Items {
			f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Delete(context.Background(), acrq.Name, v12.DeleteOptions{})
		}
	})

	It("Create a ApplicationAwareClusterResourceQuota and ensure its mirrored by ApplicationAwareAppliedClusterResourceQuota.", func(ctx context.Context) {
		resources := v1.ResourceList{
			v1.ResourcePods:           resource.MustParse("10"),
			v1.ResourceRequestsMemory: resource.MustParse("1Gi"),
		}
		scopeSelector := &v1.ScopeSelector{
			MatchExpressions: []v1.ScopedResourceSelectorRequirement{
				{
					ScopeName: v1.ResourceQuotaScopePriorityClass,
					Operator:  v1.ScopeSelectorOpNotIn,
					Values:    []string{"pclass1"},
				},
			},
		}
		acrqBuilder := builders.NewAcrqBuilder().
			WithName("test-quota").
			WithLabelSelector(labelSelector).
			WithScopes([]v1.ResourceQuotaScope{v1.ResourceQuotaScopeNotTerminating}).
			WithScopesSelector(scopeSelector)

		for resource, quantity := range resources {
			acrqBuilder = acrqBuilder.WithResource(resource, quantity)
		}
		acrq := acrqBuilder.Build()
		By("Creating a ApplicationAwareClusterResourceQuota")
		_, err := createApplicationAwareClusterResourceQuota(ctx, f.AaqClient, f.Namespace.Name, acrq)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror aacrq")
		Eventually(func() bool {
			acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}

			aacrq, err := f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())

			aacrqDefaultNs, err := f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(v1.NamespaceDefault).Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())

			return AacrqsMatchAcrq([]*v1alpha1.ApplicationAwareAppliedClusterResourceQuota{aacrq, aacrqDefaultNs}, acrq)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue(), "acrq should be mirrored eventually")

		By("Update acrq ")
		Eventually(func() error {
			acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, "test-quota", v12.GetOptions{})
			if err != nil {
				return err
			}
			acrq.Spec.Quota.Hard[v1.ResourcePods] = resource.MustParse("50")
			acrq.Spec.Quota.Hard[v1.ResourceRequestsMemory] = resource.MustParse("5Gi")
			acrq.Spec.Quota.Scopes = []v1.ResourceQuotaScope{v1.ResourceQuotaScopeTerminating}
			acrq.Spec.Quota.ScopeSelector.MatchExpressions[0].Values = []string{"pclass2"}
			acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Update(ctx, acrq, v12.UpdateOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		By("Waiting for acrq update to be mirrored")
		Eventually(func() bool {
			acrq, err = f.AaqClient.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}

			aacrq, err := f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())

			aacrqDefaultNs, err := f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(v1.NamespaceDefault).Get(ctx, "test-quota", v12.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())

			return AacrqsMatchAcrq([]*v1alpha1.ApplicationAwareAppliedClusterResourceQuota{aacrq, aacrqDefaultNs}, acrq)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue(), "acrq should be mirrored eventually")
	})

	It("Create a ApplicationAwareClusterResourceQuota and ensure that ApplicationAwareAppliedClusterResourceQuotas match the namespaces included in the acrq spec", func(ctx context.Context) {
		acrq := builders.NewAcrqBuilder().
			WithName("test-quota").
			WithLabelSelector(labelSelector).
			WithResource(v1.ResourcePods, resource.MustParse("10")).
			Build()

		By("Creating a ApplicationAwareClusterResourceQuota")
		_, err := createApplicationAwareClusterResourceQuota(ctx, f.AaqClient, f.Namespace.Name, acrq)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for the mirror aacrq")
		Eventually(func() error {
			_, err = f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			if err != nil {
				return err
			}
			_, err = f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(v1.NamespaceDefault).Get(ctx, "test-quota", v12.GetOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred(), "acrq should be mirrored eventually")

		By("Update testNs to not included")
		Eventually(func() error {
			testNs, err := f.K8sClient.CoreV1().Namespaces().Get(context.Background(), f.Namespace.GetName(), v12.GetOptions{})
			if err != nil {
				return err
			}
			testNs.Labels["foo"] = "goo"
			_, err = f.K8sClient.CoreV1().Namespaces().Update(context.Background(), testNs, v12.UpdateOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		By("Waiting for removal of testNs as it doesnt match anymore")
		Eventually(func() bool {
			_, err = f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(v1.NamespaceDefault).Get(ctx, "test-quota", v12.GetOptions{})
			if err != nil {
				return false
			}
			_, err = f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			return errors.IsNotFound(err)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue(), "aacrq in the test ns should be removed eventually")

		By("Add testNS again ")
		Eventually(func() error {
			testNs, err := f.K8sClient.CoreV1().Namespaces().Get(context.Background(), f.Namespace.GetName(), v12.GetOptions{})
			if err != nil {
				return err
			}
			testNs.Labels["foo"] = "foo"
			_, err = f.K8sClient.CoreV1().Namespaces().Update(context.Background(), testNs, v12.UpdateOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		By("Make sure that aacrq is created again")
		Eventually(func() error {
			_, err := f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(f.Namespace.GetName()).Get(ctx, "test-quota", v12.GetOptions{})
			if err != nil {
				return err
			}
			_, err = f.AaqClient.AaqV1alpha1().ApplicationAwareAppliedClusterResourceQuotas(v1.NamespaceDefault).Get(ctx, "test-quota", v12.GetOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred(), "acrq should be mirrored eventually")

	})

})

// addLabelToNamespace adds a label to the specified namespace
func addLabelToNamespace(clientset *kubernetes.Clientset, namespace, key, value string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v12.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Add the label to the namespace
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[key] = value

	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Update(context.TODO(), ns, v12.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating namespace %s: %v", namespace, err)
	}

	return nil
}

// removeLabelFromNamespace removes a label from the specified namespace
func removeLabelFromNamespace(clientset *kubernetes.Clientset, namespace, key string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v12.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Remove the label from the namespace
	if _, ok := ns.Labels[key]; ok {
		delete(ns.Labels, key)
	}

	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Update(context.TODO(), ns, v12.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating namespace %s: %v", namespace, err)
	}

	return nil
}

func aaqControllerReady(clientset *kubernetes.Clientset, aaqInstallNs string) bool {
	deployment, err := clientset.AppsV1().Deployments(aaqInstallNs).Get(context.TODO(), util.ControllerPodName, v12.GetOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	if *deployment.Spec.Replicas != deployment.Status.ReadyReplicas {
		return false
	}
	return true
}

func AacrqsMatchAcrq(aacrqs []*v1alpha1.ApplicationAwareAppliedClusterResourceQuota, acrq *v1alpha1.ApplicationAwareClusterResourceQuota) bool {
	for _, aacrq := range aacrqs {
		if !reflect.DeepEqual(aacrq.Spec.Selector, acrq.Spec.Selector) {
			return false
		}

		if !reflect.DeepEqual(aacrq.Spec.Quota.Scopes, acrq.Spec.Quota.Scopes) {
			return false
		}

		if !reflect.DeepEqual(aacrq.Spec.Quota.ScopeSelector, acrq.Spec.Quota.ScopeSelector) {
			return false
		}

		if !quota.Equals(aacrq.Spec.Quota.Hard, acrq.Spec.Quota.Hard) {
			return false
		}

		for _, acrqns := range acrq.Status.Namespaces {
			found := false
			for _, aacrqns := range aacrq.Status.Namespaces {
				if acrqns.Namespace == aacrqns.Namespace {
					found = true
					if !quota.Equals(aacrqns.Status.Hard, acrqns.Status.Hard) || !quota.Equals(aacrqns.Status.Used, acrqns.Status.Used) {
						return false
					}
				}
			}
			if !found {
				return false
			}
		}

		for _, aacrqns := range aacrq.Status.Namespaces {
			found := false
			for _, acrqns := range acrq.Status.Namespaces {
				if acrqns.Namespace == aacrqns.Namespace {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
