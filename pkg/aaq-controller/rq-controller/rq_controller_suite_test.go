package rq_controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRqController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RqController Suite")
}
