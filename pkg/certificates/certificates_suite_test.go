package certificates_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCertificates(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Certificates Suite")
}
