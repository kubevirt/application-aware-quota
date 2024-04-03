package acrq_controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAcrqController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AcrqController Suite")
}
