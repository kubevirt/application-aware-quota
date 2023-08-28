package aaq_server_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAaqServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AaqServer Suite")
}
