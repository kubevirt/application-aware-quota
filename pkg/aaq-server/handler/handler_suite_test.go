package handler_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMutation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mutation Suite")
}
