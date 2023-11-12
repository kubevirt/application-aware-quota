package arq_controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAaqGateController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqGateController Suite")
}
