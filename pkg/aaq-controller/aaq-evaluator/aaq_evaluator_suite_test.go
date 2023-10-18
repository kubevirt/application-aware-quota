package aaq_evaluator_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestAaqEvaluator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqController Suite")
}
