package aaq_server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/kubevirt/pkg/certificates/bootstrap"
	"net/http"
	"net/http/httptest"
)

var _ = Describe("Test aaq serve functions", func() {
	It("Healthz", func() {
		secretCache := cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc)
		secretCertManager := bootstrap.NewFallbackCertificateManager(
			bootstrap.NewSecretCertificateManager(
				util.SecretResourceName,
				util.DefaultAaqNs,
				secretCache,
			),
		)
		fakeCli := fake.NewSimpleClientset()
		AaqServer, err := AaqServer("aaq",
			util.DefaultHost,
			util.DefaultPort,
			secretCertManager,
			fakeCli,
		)
		req, err := http.NewRequest("GET", healthzPath, nil)
		Expect(err).ToNot(HaveOccurred())
		rr := httptest.NewRecorder()
		AaqServer.ServeHTTP(rr, req)
		status := rr.Code
		Expect(status).To(Equal(http.StatusOK))

	})

	It("ServePath", func() {
		//todo: implement
	})
})
