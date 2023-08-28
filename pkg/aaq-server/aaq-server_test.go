package aaq_server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/namespaced"
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
				namespaced.SecretResourceName,
				util.DefaultAaqNs,
				secretCache,
			),
		)
		AaqServer, err := AaqServer(
			util.DefaultHost,
			util.DefaultPort,
			secretCertManager,
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
