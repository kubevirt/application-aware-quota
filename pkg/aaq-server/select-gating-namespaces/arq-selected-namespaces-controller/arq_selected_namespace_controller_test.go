package arq_selected_namespaces_controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"kubevirt.io/application-aware-quota/tests/builders"
)

var _ = Describe("Test arq-selected-namespaces-controller", func() {
	testns := "test-ns"
	DescribeTable("check namespace selection based on arq existence",
		func(arqObjects []metav1.Object, expectedContains bool, description string) {
			arqInformer := testsutils.NewFakeSharedIndexInformer(arqObjects)
			selectedNamespacesController := setupArqSelectedNamespacesController(arqInformer)
			result, err := selectedNamespacesController.execute(testns)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(Forget))
			Expect(selectedNamespacesController.nsSet.Contains(testns)).To(Equal(expectedContains), description)
		},
		Entry("namespace is added if arq exists",
			[]metav1.Object{builders.NewArqBuilder().WithName("arq").WithNamespace(testns).Build()},
			true, "namespace should be selected when arq exists in it"),
		Entry("namespace is not added if arq does not exist", []metav1.Object{},
			false, "namespace should not be selected when arq does not exist in it"),
	)
})

func setupArqSelectedNamespacesController(arqInformer cache.SharedIndexInformer) *ArqSelectedNamespacesController {
	stop := make(chan struct{})
	qc := NewArqSelectedNamespacesController(
		arqInformer,
		stop,
	)
	return qc
}
