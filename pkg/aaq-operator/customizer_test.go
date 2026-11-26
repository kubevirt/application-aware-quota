/*
Copyright 2026 The AAQ Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aaq_operator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/application-aware-quota/pkg/util"
	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

var _ = Describe("Customizer", func() {
	newDeployment := func(name string) *appsv1.Deployment {
		return &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: aaqNamespace,
				Name:      name,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: name,
							Args: []string{"-v=1"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("50Mi"),
								},
							},
						}},
					},
				},
			},
		}
	}

	newService := func(name string) *corev1.Service {
		return &corev1.Service{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
			ObjectMeta: metav1.ObjectMeta{Namespace: aaqNamespace, Name: name},
		}
	}

	It("should apply a strategic merge patch setting container resources", func() {
		controller := newDeployment(util.ControllerResourceName)
		server := newDeployment(util.AaqServerResourceName)

		customizer, err := NewCustomizer(aaqv1.CustomizeComponents{
			Patches: []aaqv1.CustomizeComponentsPatch{{
				ResourceName: util.ControllerResourceName,
				ResourceType: "Deployment",
				Patch:        `{"spec":{"template":{"spec":{"containers":[{"name":"aaq-controller","resources":{"requests":{"cpu":"100m","memory":"256Mi"},"limits":{"memory":"512Mi"}}}]}}}}`,
				Type:         aaqv1.StrategicMergePatchType,
			}},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(customizer.Apply([]client.Object{controller, server})).To(Succeed())

		c := controller.Spec.Template.Spec.Containers[0]
		Expect(c.Resources.Requests.Cpu().String()).To(Equal("100m"))
		Expect(c.Resources.Requests.Memory().String()).To(Equal("256Mi"))
		Expect(c.Resources.Limits.Memory().String()).To(Equal("512Mi"))
		// the rest of the container is kept
		Expect(c.Args).To(Equal([]string{"-v=1"}))
		Expect(controller.Annotations).To(HaveKeyWithValue(util.AnnCustomizeComponentHash, customizer.Hash()))
		Expect(controller.Kind).To(Equal("Deployment"))

		// the other deployment is untouched
		Expect(server.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().String()).To(Equal("10m"))
		Expect(server.Annotations).ToNot(HaveKey(util.AnnCustomizeComponentHash))
	})

	It("should apply json and merge patches, in order, with wildcards", func() {
		controller := newDeployment(util.ControllerResourceName)
		server := newDeployment(util.AaqServerResourceName)
		service := newService(util.AaqServerResourceName)

		customizer, err := NewCustomizer(aaqv1.CustomizeComponents{
			Patches: []aaqv1.CustomizeComponentsPatch{
				{
					ResourceName: "*",
					ResourceType: "deployment",
					Patch:        `[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--extra"}]`,
					Type:         aaqv1.JSONPatchType,
				},
				{
					ResourceName: util.AaqServerResourceName,
					ResourceType: "Deployment",
					Patch:        `{"metadata":{"labels":{"new-key":"added-this-label"}}}`,
					Type:         aaqv1.MergePatchType,
				},
				{
					ResourceName: util.AaqServerResourceName,
					ResourceType: "Deployment",
					Patch:        `[{"op":"remove","path":"/spec/template/spec/containers/0/resources/requests/cpu"}]`,
					Type:         aaqv1.JSONPatchType,
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(customizer.Apply([]client.Object{controller, server, service})).To(Succeed())

		Expect(controller.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"-v=1", "--extra"}))
		Expect(server.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"-v=1", "--extra"}))
		Expect(server.Labels).To(HaveKeyWithValue("new-key", "added-this-label"))
		Expect(controller.Labels).ToNot(HaveKey("new-key"))
		Expect(server.Spec.Template.Spec.Containers[0].Resources.Requests).ToNot(HaveKey(corev1.ResourceCPU))
		Expect(server.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKey(corev1.ResourceMemory))
		// services are not matched by a Deployment patch
		Expect(service.Annotations).ToNot(HaveKey(util.AnnCustomizeComponentHash))
	})

	It("should not touch objects of non customizable kinds", func() {
		cm := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{Name: "cm"},
		}
		customizer, err := NewCustomizer(aaqv1.CustomizeComponents{
			Patches: []aaqv1.CustomizeComponentsPatch{{
				ResourceName: "*",
				ResourceType: "*",
				Patch:        `{"metadata":{"labels":{"k":"v"}}}`,
				Type:         aaqv1.MergePatchType,
			}},
		})
		Expect(err).ToNot(HaveOccurred())
		var nilDeployment *appsv1.Deployment
		Expect(customizer.Apply([]client.Object{cm, nilDeployment})).To(Succeed())
		Expect(cm.Labels).To(BeEmpty())
	})

	It("should do nothing without patches", func() {
		controller := newDeployment(util.ControllerResourceName)
		expected := controller.DeepCopy()
		customizer, err := NewCustomizer(aaqv1.CustomizeComponents{})
		Expect(err).ToNot(HaveOccurred())
		Expect(customizer.Apply([]client.Object{controller})).To(Succeed())
		Expect(controller).To(Equal(expected))
	})

	DescribeTable("should fail on an invalid patch and leave the object untouched", func(p aaqv1.CustomizeComponentsPatch) {
		controller := newDeployment(util.ControllerResourceName)
		expected := controller.DeepCopy()
		p.ResourceName = util.ControllerResourceName
		p.ResourceType = "Deployment"
		customizer, err := NewCustomizer(aaqv1.CustomizeComponents{Patches: []aaqv1.CustomizeComponentsPatch{p}})
		Expect(err).ToNot(HaveOccurred())
		Expect(customizer.Apply([]client.Object{controller})).ToNot(Succeed())
		Expect(controller).To(Equal(expected))
	},
		Entry("unsupported type", aaqv1.CustomizeComponentsPatch{Patch: `{}`, Type: "foo"}),
		Entry("invalid json patch", aaqv1.CustomizeComponentsPatch{Patch: `{"op":"add"}`, Type: aaqv1.JSONPatchType}),
		Entry("invalid merge patch", aaqv1.CustomizeComponentsPatch{Patch: `not-json`, Type: aaqv1.MergePatchType}),
		Entry("invalid strategic merge patch", aaqv1.CustomizeComponentsPatch{Patch: `not-json`, Type: aaqv1.StrategicMergePatchType}),
	)

	It("should compute a stable hash that changes with the customizations", func() {
		c1 := aaqv1.CustomizeComponents{Patches: []aaqv1.CustomizeComponentsPatch{{
			ResourceName: util.ControllerResourceName, ResourceType: "Deployment", Patch: `{"a":"b"}`, Type: aaqv1.MergePatchType,
		}}}
		c2 := *c1.DeepCopy()
		h1, err := getHash(c1)
		Expect(err).ToNot(HaveOccurred())
		h2, err := getHash(c2)
		Expect(err).ToNot(HaveOccurred())
		Expect(h1).To(Equal(h2))

		c2.Patches[0].Patch = `{"a":"c"}`
		h2, err = getHash(c2)
		Expect(err).ToNot(HaveOccurred())
		Expect(h1).ToNot(Equal(h2))
	})

	DescribeTable("valueMatchesKey", func(value, key string, expected bool) {
		Expect(valueMatchesKey(value, key)).To(Equal(expected))
	},
		Entry("should match wildcard", "*", "Deployment", true),
		Entry("should match with different cases", "deployment", "Deployment", true),
		Entry("should not match", "Service", "Deployment", false),
	)
})
