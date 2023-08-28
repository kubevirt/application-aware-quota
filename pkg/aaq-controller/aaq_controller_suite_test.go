package aaq_controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestAaqControllerApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqController Suite")
}
