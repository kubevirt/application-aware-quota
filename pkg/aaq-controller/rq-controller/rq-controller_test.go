package rq_controller

import (
	"fmt"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned/fake"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/informers/externalversions"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/builders"
	"strings"
)

var _ = Describe("Test rq-controller", func() {
	var testNs = "test"
	Context("If arq doesn't exist ", func() {
		var ctrl *gomock.Controller
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
		})
		It("should delete managed rq", func() {
			cli := client.NewMockAAQClient(ctrl)
			fakek8sCli := k8sfake.NewSimpleClientset()
			cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
			qc := setupRQController(cli, nil, nil)

			err, es := qc.execute("ns/fake")
			Expect(err).ToNot(HaveOccurred())
			Expect(es).To(Equal(Forget))
			actionSet := sets.NewString()
			for _, action := range fakek8sCli.Actions() {
				actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource().Resource}, "-"))
			}
			Expect(actionSet.Equal(sets.NewString(strings.Join([]string{"delete", "resourcequotas"}, "-")))).To(BeTrue(), fmt.Sprintf("Expected deletion action but got:\n%v\"", actionSet))

		})

	})
	DescribeTable("Test execute when ", func(arq *v1alpha1.ApplicationAwareResourceQuota, rqsState []metav1.Object, expectedActionSet sets.String, expectedEnqueueState enqueueState) {
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		arqInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{arq})
		rqInformer := testsutils.NewFakeSharedIndexInformer(rqsState)
		fakek8sCli := k8sfake.NewSimpleClientset(metav1ToRuntimeRQsObjs(rqsState)...)
		if expectedActionSet != nil {
			cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
		}
		qc := setupRQController(cli, arqInformer, rqInformer)
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(arq)
		Expect(err).ToNot(HaveOccurred())
		err, es := qc.execute(key)
		Expect(err).ToNot(HaveOccurred())
		Expect(es).To(Equal(expectedEnqueueState))
		actionSet := sets.NewString()
		for _, action := range fakek8sCli.Actions() {
			actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource().Resource}, "-"))
		}
		Expect(actionSet.Equal(expectedActionSet)).To(BeTrue(), fmt.Sprintf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet)))
	}, Entry(" there aren't any scheduable resources, managed rq should not be created",
		builders.NewArqBuilder().WithNamespace(testNs).WithName("arq-test").WithResource(corev1.ResourceLimitsMemory, resource.MustParse("1m")).Build(),
		[]metav1.Object{},
		sets.NewString(
			strings.Join([]string{"delete", "resourcequotas"}, "-"),
		),
		Forget,
	), Entry(" there are scheduable resources, managed rq should be created",
		builders.NewArqBuilder().WithNamespace(testNs).WithName("arq-test").WithResource(corev1.ResourceServices, resource.MustParse("5")).Build(),
		[]metav1.Object{},
		sets.NewString(
			strings.Join([]string{"create", "resourcequotas"}, "-"),
		),
		Forget,
	), Entry(" there are scheduable resources, but managed rq should not be created because it already exist",
		builders.NewArqBuilder().WithNamespace(testNs).WithName("arq-test").WithResource(corev1.ResourceServices, resource.MustParse("5")).Build(),
		[]metav1.Object{builders.NewQuotaBuilder().WithName("arq-test"+RQSuffix).WithNamespace(testNs).WithLabel(util.AAQLabel, "true").WithResource(corev1.ResourceServices, resource.MustParse("5")).Build()},
		nil,
		Forget,
	), Entry(" there are scheduable resources, managed rq should be updated because it is different from what it should be",
		builders.NewArqBuilder().WithNamespace(testNs).WithName("arq-test").WithResource(corev1.ResourceServices, resource.MustParse("5")).Build(),
		[]metav1.Object{builders.NewQuotaBuilder().WithName("arq-test"+RQSuffix).WithNamespace(testNs).WithLabel(util.AAQLabel, "true").WithResource(corev1.ResourceServices, resource.MustParse("2")).Build()},
		sets.NewString(
			strings.Join([]string{"update", "resourcequotas"}, "-"),
		),
		Forget,
	), Entry(" there are no scheduable resources, managed rq should be deleted if exist",
		builders.NewArqBuilder().WithNamespace(testNs).WithName("arq-test").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("5Mi")).Build(),
		[]metav1.Object{builders.NewQuotaBuilder().WithName("arq-test"+RQSuffix).WithNamespace(testNs).WithLabel(util.AAQLabel, "true").WithResource(corev1.ResourceServices, resource.MustParse("2")).Build()},
		sets.NewString(
			strings.Join([]string{"delete", "resourcequotas"}, "-"),
		),
		Forget,
	),
	)

})

func setupRQController(clientSet client.AAQClient, arqInformer cache.SharedIndexInformer, rqInformer cache.SharedIndexInformer) *RQController {
	informerFactory := externalversions.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	kubeInformerFactory := informers.NewSharedInformerFactory(k8sfake.NewSimpleClientset(), 0)

	if arqInformer == nil {
		arqInformer = informerFactory.Aaq().V1alpha1().ApplicationAwareResourceQuotas().Informer()
	}
	if rqInformer == nil {
		rqInformer = kubeInformerFactory.Core().V1().ResourceQuotas().Informer()
	}

	stop := make(chan struct{})
	qc := NewRQController(clientSet,
		rqInformer,
		arqInformer,
		stop,
	)
	informerFactory.Start(stop)
	kubeInformerFactory.Start(stop)
	return qc
}

func metav1ToRuntimeRQsObjs(objs []metav1.Object) []runtime.Object {
	var rtobjs []runtime.Object
	for _, obj := range objs {
		rtobjs = append(rtobjs, runtime.Object(obj.(*corev1.ResourceQuota)))
	}
	return rtobjs
}
