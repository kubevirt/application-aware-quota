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
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	rq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/application-aware-quota/tests/builders"
	"kubevirt.io/application-aware-quota/tests/framework"
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
