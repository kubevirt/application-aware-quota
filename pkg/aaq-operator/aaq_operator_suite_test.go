package aaq_operator_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAaqOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqOperator Suite")
}
