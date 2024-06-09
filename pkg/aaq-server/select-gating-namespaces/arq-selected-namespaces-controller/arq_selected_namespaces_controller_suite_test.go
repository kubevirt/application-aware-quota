package arq_selected_namespaces_controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestArqSelectedNamespacesController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ArqSelectedNamespacesController Suite")
}
