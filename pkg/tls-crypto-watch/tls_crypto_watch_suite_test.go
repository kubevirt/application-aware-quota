package tlscryptowatch

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTLSCryptoWatch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TLS Crypto Watch Suite")
}
