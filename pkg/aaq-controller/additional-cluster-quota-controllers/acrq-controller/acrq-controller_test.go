package acrq_controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
)

var _ = Describe("Test aaq-gate-controller", func() {
	var testNs = "test"

	Context("Test execute when ", func() {
		It("should forget the key if its namespace doesn't exist", func() {
			qc := AcrqController{namespaceLister: testsutils.FakeNamespaceLister{Namespaces: map[string]*corev1.Namespace{}}}
			err, es := qc.execute(testNs)
			Expect(err).ToNot(HaveOccurred())
			Expect(es).To(Equal(Forget))
		})
	})
})
