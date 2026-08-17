package util

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

func TestUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Util Suite")
}

var _ = Describe("CreateContainer SecurityContext", func() {
	newContainer := func() corev1.Container {
		return CreateContainer("test", "img:latest", "1", "Always")
	}

	DescribeTable("should enforce restricted SecurityContext fields",
		func(check func(corev1.Container)) {
			check(newContainer())
		},
		Entry("ReadOnlyRootFilesystem=true", func(c corev1.Container) {
			Expect(c.SecurityContext).NotTo(BeNil())
			Expect(c.SecurityContext.ReadOnlyRootFilesystem).NotTo(BeNil())
			Expect(*c.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
		}),
		Entry("AllowPrivilegeEscalation=false", func(c corev1.Container) {
			Expect(c.SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
			Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		}),
		Entry("RunAsNonRoot=true", func(c corev1.Container) {
			Expect(c.SecurityContext.RunAsNonRoot).NotTo(BeNil())
			Expect(*c.SecurityContext.RunAsNonRoot).To(BeTrue())
		}),
		Entry("drops ALL capabilities", func(c corev1.Container) {
			Expect(c.SecurityContext.Capabilities).NotTo(BeNil())
			Expect(c.SecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		}),
		Entry("SeccompProfile=RuntimeDefault", func(c corev1.Container) {
			Expect(c.SecurityContext.SeccompProfile).NotTo(BeNil())
			Expect(c.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		}),
	)
})
