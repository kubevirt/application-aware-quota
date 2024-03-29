package certificates_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"kubevirt.io/application-aware-quota/pkg/certificates"
)

var _ = Describe("Certificates", func() {

	var certDir string

	BeforeEach(func() {
		var err error
		certDir, err = os.MkdirTemp("", "certsdir")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should be generated in temporary directory", func() {
		store, err := certificates.GenerateSelfSignedCert(certDir, "testname", "testnamespace")
		Expect(err).ToNot(HaveOccurred())
		_, err = store.Current()
		Expect(err).ToNot(HaveOccurred())
		Expect(store.CurrentPath()).To(ContainSubstring(certDir))
	})

	AfterEach(func() {
		os.RemoveAll(certDir)
	})
})
