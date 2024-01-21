package handler

import (
	"encoding/json"
	"fmt"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/applications-aware-quota/tests"
	"net/http"
)

var _ = Describe("Test handler of aaq server", func() {
	It("Pod should be gated", func() {
		pod := &v1.Pod{}
		podBytes, err := json.Marshal(pod)
		Expect(err).ToNot(HaveOccurred())

		v := Handler{
			request: &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Kind: "Pod",
				},
				Object: runtime.RawExtension{
					Raw:    podBytes,
					Object: pod,
				},
				Operation: admissionv1.Create,
			},
		}
		admissionReview, err := v.Handle()
		Expect(err).ToNot(HaveOccurred())
		Expect(admissionReview.Response.Allowed).To(BeTrue())
		Expect(admissionReview.Response.Result.Code).To(Equal(int32(http.StatusAccepted)))
		Expect(admissionReview.Response.Result.Message).To(Equal(allowPodRequest))
	})

	DescribeTable("Pod Ungating should", func(ourController bool) {
		pod := &v1.Pod{
			Spec: v1.PodSpec{},
		}
		podBytes, err := json.Marshal(pod)
		Expect(err).ToNot(HaveOccurred())

		oldPod := &v1.Pod{
			Spec: v1.PodSpec{
				SchedulingGates: []v1.PodSchedulingGate{
					{
						util.AAQGate,
					},
				},
			},
		}
		oldPodBytes, err := json.Marshal(oldPod)
		Expect(err).ToNot(HaveOccurred())
		userName := util.ControllerServiceAccountName
		massage := aaqControllerPodUpdate
		allowed := true
		code := int32(http.StatusAccepted)
		if !ourController {
			userName = "some-controller"
			massage = invalidPodUpdate
			allowed = false
			code = int32(http.StatusForbidden)

		}
		v := Handler{
			request: &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Kind: "Pod",
				},
				Object: runtime.RawExtension{
					Raw:    podBytes,
					Object: pod,
				},
				OldObject: runtime.RawExtension{
					Raw:    oldPodBytes,
					Object: oldPod,
				},
				Operation: admissionv1.Update,
				UserInfo: authenticationv1.UserInfo{
					Username: fmt.Sprintf("system:serviceaccount:%v:%v", util.DefaultAaqNs, userName),
				},
			},
			aaqNS: util.DefaultAaqNs,
		}
		admissionReview, err := v.Handle()
		Expect(err).ToNot(HaveOccurred())
		Expect(admissionReview.Response.Allowed).To(Equal(allowed))
		Expect(admissionReview.Response.Result.Code).To(Equal(code))
		Expect(admissionReview.Response.Result.Message).To(Equal(massage))
	},
		Entry(" be allowed for aaq-controller", true),
		Entry(" not be allowed for external users", false),
	)

	It("Pod Update without gate modifying our gate should be allowed", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				SchedulingGates: []v1.PodSchedulingGate{
					{
						util.AAQGate,
					},
				},
			},
		}
		podBytes, err := json.Marshal(pod)
		Expect(err).ToNot(HaveOccurred())

		oldPod := &v1.Pod{
			Spec: v1.PodSpec{
				SchedulingGates: []v1.PodSchedulingGate{
					{
						util.AAQGate,
					},
					{
						"FakeGate",
					},
				},
			},
		}
		oldPodBytes, err := json.Marshal(oldPod)
		Expect(err).ToNot(HaveOccurred())

		v := Handler{
			request: &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Kind: "Pod",
				},
				Object: runtime.RawExtension{
					Raw:    podBytes,
					Object: pod,
				},
				OldObject: runtime.RawExtension{
					Raw:    oldPodBytes,
					Object: oldPod,
				},
				Operation: admissionv1.Update,
				UserInfo: authenticationv1.UserInfo{
					Username: fmt.Sprintf("system:serviceaccount:%v:%v", "SomeNs", "someUser"),
				},
			},
			aaqNS: util.DefaultAaqNs,
		}
		admissionReview, err := v.Handle()
		Expect(err).ToNot(HaveOccurred())
		Expect(admissionReview.Response.Allowed).To(BeTrue())
		Expect(admissionReview.Response.Result.Code).To(Equal(int32(http.StatusAccepted)))
		Expect(admissionReview.Response.Result.Message).To(Equal(validPodUpdate))
	})

	DescribeTable("ARQ", func(operation admissionv1.Operation) {
		arq := tests.NewArqBuilder().WithNamespace("testNS").WithResource(v1.ResourceRequestsMemory, resource.MustParse("4Gi")).Build()
		arqBytes, err := json.Marshal(arq)
		Expect(err).ToNot(HaveOccurred())
		fakek8sCli := fake.NewSimpleClientset()
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
		v := Handler{
			request: &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Kind: "ApplicationsResourceQuota",
				},
				Object: runtime.RawExtension{
					Raw:    arqBytes,
					Object: arq,
				},
				Operation: operation,
				UserInfo: authenticationv1.UserInfo{
					Username: fmt.Sprintf("system:serviceaccount:%v:%v", "SomeNs", "someUser"),
				},
			},
			aaqNS:  util.DefaultAaqNs,
			aaqCli: cli,
		}
		admissionReview, err := v.Handle()
		Expect(err).ToNot(HaveOccurred())
		Expect(admissionReview.Response.Allowed).To(BeTrue())
		Expect(admissionReview.Response.Result.Code).To(Equal(int32(http.StatusAccepted)))
		Expect(admissionReview.Response.Result.Message).To(Equal(allowArqRequest))
	},
		Entry(" valid Update should be allowed", admissionv1.Update),
		Entry(" valid Creation should be allowed", admissionv1.Create),
	)
})
