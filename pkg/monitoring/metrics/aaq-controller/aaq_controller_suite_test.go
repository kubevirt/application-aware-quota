package aaq_controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAaqController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqController Monitoring Suite")
}
