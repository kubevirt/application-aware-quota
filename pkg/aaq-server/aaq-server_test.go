package aaq_server

import (
	"bytes"
	"encoding/json"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/applications-aware-quota/pkg/certificates/bootstrap"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/pkg/util"
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
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		AaqServer, err := AaqServer("aaq",
			util.DefaultHost,
			util.DefaultPort,
			secretCertManager,
			cli,
			false,
		)
		req, err := http.NewRequest("GET", healthzPath, nil)
		Expect(err).ToNot(HaveOccurred())
		rr := httptest.NewRecorder()
		AaqServer.ServeHTTP(rr, req)
		status := rr.Code
		Expect(status).To(Equal(http.StatusOK))

	})

	It("servePath", func() {
		secretCache := cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc)
		secretCertManager := bootstrap.NewFallbackCertificateManager(
			bootstrap.NewSecretCertificateManager(
				util.SecretResourceName,
				util.DefaultAaqNs,
				secretCache,
			),
		)
		fakek8sCli := k8sfake.NewSimpleClientset()
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())

		mtqLockServer, err := AaqServer(util.DefaultAaqNs,
			util.DefaultHost,
			util.DefaultPort,
			secretCertManager,
			cli,
			false,
		)

		// Create a new ApplicationsResourceQuota create request
		admissionReview := v1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AdmissionReview",
				APIVersion: "admission.k8s.io/v1",
			},
			Request: &v1.AdmissionRequest{
				UID: types.UID("<unique-identifier>"),
				Kind: metav1.GroupVersionKind{
					Kind: "ApplicationsResourceQuota",
				},
				Resource: metav1.GroupVersionResource{
					Group:    "aaq.kubevirt.io",
					Version:  "v1alpha1",
					Resource: "applicationsresourcequotas",
				},
				Name:      "example-resource-quota",
				Operation: v1.Create,
				Namespace: "testNS",
				Object: runtime.RawExtension{
					Raw: []byte(`{
                    "apiVersion": "aaq.kubevirt.io/v1alpha1",
                    "kind": "ApplicationsResourceQuota",
                    "metadata": {
                        "name": "example-resource-quota"
                    },
                    "spec": {
                        "hard": {
                            "services": "5",
                            "pods": "10",
                            "limits.cpu": "1"
                        }
                    }
                }`),
				},
			},
		}
		// Convert the admission review to JSON
		admissionReviewBody, err := json.Marshal(admissionReview)
		if err != nil {
			// Handle error
		}

		// Create a new HTTP POST request with the admission review body
		req, err := http.NewRequest("POST", ServePath, bytes.NewBuffer(admissionReviewBody))
		if err != nil {
			// Handle error
		}
		// Set the Content-Type header to "application/json"
		req.Header.Set("Content-Type", "application/json")

		Expect(err).ToNot(HaveOccurred())
		rr := httptest.NewRecorder()
		mtqLockServer.ServeHTTP(rr, req)
		status := rr.Code
		Expect(status).To(Equal(http.StatusOK))
	})
})
