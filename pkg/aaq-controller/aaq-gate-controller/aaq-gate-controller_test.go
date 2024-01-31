package arq_controller

import (
	"context"
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
	testingclock "k8s.io/utils/clock/testing"
	aaq_evaluator "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned/fake"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/informers/externalversions"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests"
	"strings"
	"time"
)

var _ = Describe("Test aaq-gate-controller", func() {
	var testNs string = "test"

	Context("Test execute when ", func() {
		var ctrl *gomock.Controller
		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
		})
		It("aaqjc doesn't exist should create it", func() {
			cli := client.NewMockAAQClient(ctrl)
			aaqjqc := &v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName}}
			aaqjcInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{})

			aaqjcInterfaceMock := client.NewMockAAQJobQueueConfigInterface(ctrl)
			aaqjcInterfaceMock.EXPECT().Create(context.Background(), aaqjqc, metav1.CreateOptions{}).Return(aaqjqc, nil).Times(1)
			cli.EXPECT().AAQJobQueueConfigs(testNs).Return(aaqjcInterfaceMock).Times(1)

			qc := setupAAQGateController(cli, nil, nil, aaqjcInformer)
			err, es := qc.execute(testNs)
			Expect(err).ToNot(HaveOccurred())
			Expect(es).To(Equal(Forget))
		})

	})

	DescribeTable("Test execute when aaqjc is not empty", func(aaqjqc *v1alpha1.AAQJobQueueConfig, podsState []metav1.Object, expectedActionSet sets.String) {
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		podInformer := testsutils.NewFakeSharedIndexInformer(podsState)
		arqInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{})
		aaqjqcInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{aaqjqc})
		fakek8sCli := k8sfake.NewSimpleClientset(metav1ToRuntimePodsObjs(podsState)...)
		if expectedActionSet != nil {
			cli.EXPECT().CoreV1().Times(1).Return(fakek8sCli.CoreV1())
		}
		qc := setupAAQGateController(cli, podInformer, arqInformer, aaqjqcInformer)
		err, es := qc.execute(testNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(es).To(Equal(Immediate))
		actionSet := sets.NewString()
		for _, action := range fakek8sCli.Actions() {
			actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource().Resource}, "-"))
		}
		Expect(actionSet.Equal(expectedActionSet)).To(BeTrue(), fmt.Sprintf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet)))
	}, Entry(" should release gated pods and requeue",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, sets.NewString(
			strings.Join([]string{"update", "pods"}, "-"),
		),
	), Entry(" should not update pods without our gate and requeue",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, nil,
	),
	)

	DescribeTable("Test execute when aaqjc is empty and", func(expectedAaqjqc *v1alpha1.AAQJobQueueConfig, podsState []metav1.Object, arqsState []metav1.Object, expectedActionSet sets.String) {
		ctrl := gomock.NewController(GinkgoT())
		cli := client.NewMockAAQClient(ctrl)
		podInformer := testsutils.NewFakeSharedIndexInformer(podsState)
		arqInformer := testsutils.NewFakeSharedIndexInformer(arqsState)
		aaqjqcInformer := testsutils.NewFakeSharedIndexInformer([]metav1.Object{&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{}}})
		fakek8sCli := k8sfake.NewSimpleClientset(metav1ToRuntimePodsObjs(podsState)...)
		if expectedAaqjqc != nil {
			cli.EXPECT().CoreV1().MinTimes(1).Return(fakek8sCli.CoreV1())
			aaqjqcInterfaceMock := client.NewMockAAQJobQueueConfigInterface(ctrl)
			aaqjqcInterfaceMock.EXPECT().UpdateStatus(context.Background(), expectedAaqjqc, metav1.UpdateOptions{}).Times(1).Return(expectedAaqjqc, nil)
			cli.EXPECT().AAQJobQueueConfigs(testNs).Return(aaqjqcInterfaceMock).Times(1)
		}
		qc := setupAAQGateController(cli, podInformer, arqInformer, aaqjqcInformer)
		err, es := qc.execute(testNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(es).To(Equal(Forget))
		actionSet := sets.NewString()
		for _, action := range fakek8sCli.Actions() {
			actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource().Resource}, "-"))
		}
		Expect(actionSet.Equal(expectedActionSet)).To(BeTrue(), fmt.Sprintf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet)))
	}, Entry(" there aren't any pod in the test ns",
		nil,
		[]metav1.Object{}, []metav1.Object{},
		sets.NewString(),
	), Entry(" there is a pod without gate",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{},
		sets.NewString(),
	), Entry(" there is a pod with another gate",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{"fakeGate"}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{},
		sets.NewString(),
	), Entry(" there is a pod with several gates",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{"fakeGate"}, {util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{},
		sets.NewString(),
	), Entry(" there is a pod with gate that should be ungated without arqs",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{}, sets.NewString(
			strings.Join([]string{"update", "pods"}, "-"),
		),
	), Entry(" there is a pod with gate that should be ungated with non-blocking-arqs",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(
			strings.Join([]string{"update", "pods"}, "-"),
		),
	), Entry(" there is a pod with gate that should be ungated with blocking-arqs",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Mi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(),
	), Entry(" there is a pod with gate that should not be ungated with two arqs one of them is blocking",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq1").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Mi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(),
	), Entry(" there is a pod with gate that should be ungated with two non-blocking arqs",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{Name: util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq1").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("1Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(
			strings.Join([]string{"update", "pods"}, "-"),
		),
	), Entry(" there are two pods with gate with args with enough place just for one of them",
		&v1alpha1.AAQJobQueueConfig{ObjectMeta: metav1.ObjectMeta{Name: AaqjqcName, Namespace: testNs}, Status: v1alpha1.AAQJobQueueConfigStatus{PodsInJobQueue: []string{"pod-test2"}, ControllerLock: map[string]bool{ApplicationAwareResourceQuotaLockName: true}}},
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "2Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test2", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "1Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("2Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq1").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("1Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(
			strings.Join([]string{"update", "pods"}, "-"),
		),
	), Entry(" there are two pods with gate with blocking arqs each arq block another pod",
		nil,
		[]metav1.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "50Mi"), testsutils.GetResourceList("", ""))}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test2", Namespace: testNs},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					SchedulingGates: []corev1.PodSchedulingGate{{util.AAQGate}},
					Containers:      []corev1.Container{{Name: "ctr", Image: "image", Resources: testsutils.GetResourceRequirements(testsutils.GetResourceList("500m", "2Gi"), testsutils.GetResourceList("", ""))}},
				},
			},
		}, []metav1.Object{
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq").WithResource(corev1.ResourceRequestsCPU, resource.MustParse("400m")).WithSyncStatusHardEmptyStatusUsed().Build(),
			tests.NewArqBuilder().WithNamespace(testNs).WithName("testarq1").WithResource(corev1.ResourceRequestsMemory, resource.MustParse("1Gi")).WithSyncStatusHardEmptyStatusUsed().Build(),
		},
		sets.NewString(),
	),
	)

})

func setupAAQGateController(clientSet client.AAQClient, podInformer cache.SharedIndexInformer, arqInformer cache.SharedIndexInformer, aaqjcInformer cache.SharedIndexInformer) *AaqGateController {
	informerFactory := externalversions.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	kubeInformerFactory := informers.NewSharedInformerFactory(k8sfake.NewSimpleClientset(), 0)
	if podInformer == nil {
		podInformer = kubeInformerFactory.Core().V1().Pods().Informer()
	}
	if arqInformer == nil {
		arqInformer = informerFactory.Aaq().V1alpha1().ApplicationAwareResourceQuotas().Informer()
	}
	if aaqjcInformer == nil {
		aaqjcInformer = informerFactory.Aaq().V1alpha1().AAQJobQueueConfigs().Informer()
	}
	fakeClock := testingclock.NewFakeClock(time.Now())
	stop := make(chan struct{})
	qc := NewAaqGateController(clientSet,
		podInformer,
		arqInformer,
		aaqjcInformer,
		nil,
		aaq_evaluator.NewAaqCalculatorsRegistry(3, fakeClock),
		nil,
		nil,
		nil,
		false,
		stop,
	)
	informerFactory.Start(stop)
	kubeInformerFactory.Start(stop)
	return qc
}

func metav1ToRuntimePodsObjs(objs []metav1.Object) []runtime.Object {
	var rtobjs []runtime.Object
	for _, obj := range objs {
		rtobjs = append(rtobjs, runtime.Object(obj.(*corev1.Pod)))
	}
	return rtobjs
}
