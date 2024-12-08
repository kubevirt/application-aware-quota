package built_in_usage_calculators

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	quota "k8s.io/apiserver/pkg/quota/v1"
	v12 "kubevirt.io/api/core/v1"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

var _ = Describe("Test virt-launcher calculator", func() {
	fakeNs := "fakeNs"
	fakeVmiName := "fakeVmiName"
	fakeVmiUID := "123"
	fakeVmimUID := "1234"
	fakeVmimName := "fakeVmimName"
	podForTests := NewPodBuilder().
		WithName("pod").
		WithNamespace(fakeNs).
		WithOwnerReference(v12.VirtualMachineInstanceGroupVersionKind.Kind, fakeVmiName, fakeVmiUID).
		WithLabel(v12.AppLabel, launcherLabel).
		Build()
	vmiForTests := NewVmiBuilder().
		WithName(fakeVmiName).
		WithNamespace(fakeNs).
		WithUID(fakeVmiUID).
		WithNode("node1").
		Build()
	sourcePodForTests := NewPodBuilder().
		WithName("sourcePod").
		WithNamespace(fakeNs).
		WithOwnerReference(v12.VirtualMachineInstanceGroupVersionKind.Kind, fakeVmiName, fakeVmiUID).
		WithLabel(v12.AppLabel, launcherLabel).
		WithCreationTimestamp(parseTimeOrDie("2015-04-22T11:49:36Z")).
		WithNode("node1").
		Build()
	targetPodForTests := NewPodBuilder().
		WithName("targetPod").
		WithNamespace(fakeNs).
		WithOwnerReference(v12.VirtualMachineInstanceGroupVersionKind.Kind, fakeVmiName, fakeVmiUID).
		WithLabel(v12.AppLabel, launcherLabel).
		WithLabel(v12.MigrationJobLabel, fakeVmimUID).
		WithAnnotations(v12.MigrationJobNameAnnotation, fakeVmimName).
		WithCreationTimestamp(parseTimeOrDie("2015-04-22T12:49:36Z")).
		WithNode("node2").
		Build()
	orphanVmiPodForTests := NewPodBuilder().
		WithName("orphanPod").
		WithNamespace(fakeNs).
		WithOwnerReference(v12.VirtualMachineInstanceGroupVersionKind.Kind, fakeVmiName, fakeVmiUID).
		WithLabel(v12.AppLabel, launcherLabel).
		WithCreationTimestamp(parseTimeOrDie("2015-04-22T10:49:36Z")).
		WithNode("node2").
		Build()
	vmimForTests := NewVmimBuilder().
		WithName(fakeVmimName).
		WithNamespace(fakeNs).
		WithVmiName(fakeVmiName).
		WithCreationTimestamp(parseTimeOrDie("2015-04-22T12:30:36Z")).
		WithUID(fakeVmimUID).
		Build()
	DescribeTable("Test PodUsageFunc when", func(vmis []metav1.Object, vmims []metav1.Object, pod *v1.Pod, existingPods []*v1.Pod, calcConfig v1alpha1.VmiCalcConfigName, expectedRl v1.ResourceList, expectMatch bool, errExpected bool) {
		launcherClac := VirtLauncherCalculator{
			vmiInformer:       testsutils.NewFakeSharedIndexInformer(vmis),
			migrationInformer: testsutils.NewFakeSharedIndexInformer(vmims),
			calcConfig:        calcConfig,
		}
		rl, err, match := launcherClac.PodUsageFunc(pod, existingPods)
		if errExpected {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(match).To(Equal(expectMatch), "match value doesn't expected value")
		Expect(quota.Equals(rl, expectedRl)).To(BeTrue(), "Word count wrong. Got %v, want %v", rl, expectedRl)
	}, Entry("the pod is not associated with a virtual machine, the pod should not match the calculator",
		[]metav1.Object{},
		[]metav1.Object{},
		&v1.Pod{},
		[]*v1.Pod{},
		v1alpha1.VmiPodUsage,
		v1.ResourceList{},
		false,
		false),
		Entry("the pod is associated with a virtual machine but the virtual machine doesn't exist, the pod should not match the calculator",
			[]metav1.Object{},
			[]metav1.Object{},
			podForTests,
			[]*v1.Pod{},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{},
			false,
			false),
		Entry("the pod is associated with a virtual machine but the pod is not recognized as one of the launcher pods, error should be returned and the pod should match",
			[]metav1.Object{vmiForTests},
			[]metav1.Object{},
			podForTests,
			[]*v1.Pod{},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{},
			true,
			true),
		Entry("the pod is associated with a virtual machine without migration with the VmiPodUsage configuration",
			[]metav1.Object{vmiForTests},
			[]metav1.Object{},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)), "count/pods": *(resource.NewQuantity(1, resource.DecimalSI))},
			true,
			false),
		Entry("the pod is a source pod of a virtual machine instance migration with the VmiPodUsage configuration",
			[]metav1.Object{vmiForTests},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)), "count/pods": *(resource.NewQuantity(1, resource.DecimalSI))},
			true,
			false),
		Entry("the pod is a target pod of a virtual machine instance migration with the VmiPodUsage configuration, so the target-source difference should be calculated which is zero",
			[]metav1.Object{vmiForTests},
			[]metav1.Object{vmimForTests},
			targetPodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(0, resource.DecimalSI)), "count/pods": *(resource.NewQuantity(0, resource.DecimalSI))},
			true,
			false),
		Entry("the pod is a orphan pod of a virtual machine instance , the counting should be zero",
			[]metav1.Object{vmiForTests},
			[]metav1.Object{vmimForTests},
			orphanVmiPodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VmiPodUsage,
			v1.ResourceList{},
			true,
			false),
		Entry("evaluate source pod of a vmi with resource limits",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithResourcesLimitsCPU(resource.MustParse("2")).
				WithResourcesLimitsMemory(resource.MustParse("2Gi")).
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				v1.ResourceRequestsCPU:    resource.MustParse("2"),
				v1.ResourceLimitsCPU:      resource.MustParse("2"),
			},
			true,
			false),
		Entry("evaluate source pod of a vmi with resource request",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithResourcesRequestCPU(resource.MustParse("2")).
				WithResourcesRequestMemory(resource.MustParse("2Gi")).
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				v1.ResourceRequestsCPU:    resource.MustParse("2"),
				v1.ResourceLimitsCPU:      resource.MustParse("2"),
			},
			true,
			false),
		Entry("evaluate source pod of a vmi with both resource request and limits, request determine",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithResourcesRequestCPU(resource.MustParse("2")).
				WithResourcesRequestMemory(resource.MustParse("2Gi")).
				WithResourcesLimitsCPU(resource.MustParse("4")).
				WithResourcesLimitsMemory(resource.MustParse("4Gi")).
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				v1.ResourceRequestsCPU:    resource.MustParse("2"),
				v1.ResourceLimitsCPU:      resource.MustParse("2"),
			},
			true,
			false),
		Entry("evaluate source pod of a vmi with both resource request, limits , Ram size and CPU definition, Ram size determine for memory and CPU definition determine for CPU",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithResourcesRequestCPU(resource.MustParse("2")).
				WithResourcesRequestMemory(resource.MustParse("2Gi")).
				WithResourcesLimitsCPU(resource.MustParse("4")).
				WithResourcesLimitsMemory(resource.MustParse("4Gi")).
				WithGuestMemory(resource.MustParse("5Gi")).
				WithGuestCPUCoresSocketsThreads(2, 2, 2).
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("5Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("5Gi"),
				v1.ResourceRequestsCPU:    *(resource.NewQuantity(8, resource.BinarySI)),
				v1.ResourceLimitsCPU:      *(resource.NewQuantity(8, resource.BinarySI)),
			},
			true,
			false),
		Entry("evaluate source pod of a vmi in a hot-plug process, counting for source pods is by actual usage ",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithGuestMemory(resource.MustParse("2Gi")).               //desired
				WithGuestCPUCoresSocketsThreads(1, 1, 1).                 //desired
				WithActualCpuDuringHotPlug(2, 2, 2).                      //actual
				WithActualMemoryDuringHotPlug(resource.MustParse("5Gi")). //actual
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(1, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("5Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("5Gi"),
				v1.ResourceRequestsCPU:    *(resource.NewQuantity(8, resource.BinarySI)),
				v1.ResourceLimitsCPU:      *(resource.NewQuantity(8, resource.BinarySI)),
			},
			true,
			false),
		Entry("evaluate target pod of a vmi in a hot-plug process, counting for target pods is by difference of the desired and actual usage",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithGuestMemory(resource.MustParse("5Gi")).               //desired
				WithGuestCPUCoresSocketsThreads(2, 2, 2).                 //desired
				WithActualCpuDuringHotPlug(1, 1, 1).                      //actual
				WithActualMemoryDuringHotPlug(resource.MustParse("2Gi")). //actual
				Build()},
			[]metav1.Object{vmimForTests},
			targetPodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: *(resource.NewQuantity(0, resource.DecimalSI)),
				"count/pods":              *(resource.NewQuantity(0, resource.DecimalSI)),
				v1.ResourceRequestsMemory: resource.MustParse("3Gi"),
				v1.ResourceLimitsMemory:   resource.MustParse("3Gi"),
				v1.ResourceRequestsCPU:    *(resource.NewQuantity(7, resource.BinarySI)),
				v1.ResourceLimitsCPU:      *(resource.NewQuantity(7, resource.BinarySI)),
			},
			true,
			false),
		Entry("evaluate target pod of a vmi in a hot-plug process, when target request less resources counting should be zero",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithGuestMemory(resource.MustParse("2Gi")).               //desired
				WithGuestCPUCoresSocketsThreads(1, 1, 1).                 //desired
				WithActualCpuDuringHotPlug(2, 2, 2).                      //actual
				WithActualMemoryDuringHotPlug(resource.MustParse("5Gi")). //actual
				Build()},
			[]metav1.Object{vmimForTests},
			targetPodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.VirtualResources,
			v1.ResourceList{v1.ResourcePods: resource.MustParse("0"),
				"count/pods":              resource.MustParse("0"),
				v1.ResourceRequestsMemory: resource.MustParse("0"),
				v1.ResourceLimitsMemory:   resource.MustParse("0"),
				v1.ResourceRequestsCPU:    resource.MustParse("0"),
				v1.ResourceLimitsCPU:      resource.MustParse("0"),
			},
			true,
			false),
		Entry("evaluate source pod of a vmi with DedicatedVirtualResources Configuration",
			[]metav1.Object{NewVmiBuilder().
				WithName(fakeVmiName).
				WithNamespace(fakeNs).
				WithUID(fakeVmiUID).
				WithNode("node1").
				WithGuestMemory(resource.MustParse("5Gi")).
				WithGuestCPUCoresSocketsThreads(2, 2, 2).
				Build()},
			[]metav1.Object{vmimForTests},
			sourcePodForTests,
			[]*v1.Pod{sourcePodForTests, orphanVmiPodForTests, targetPodForTests},
			v1alpha1.DedicatedVirtualResources,
			v1.ResourceList{v1alpha1.ResourcePodsOfVmi: *(resource.NewQuantity(1, resource.DecimalSI)),
				v1alpha1.ResourceRequestsVmiCPU:    *(resource.NewQuantity(8, resource.BinarySI)),
				v1alpha1.ResourceRequestsVmiMemory: resource.MustParse("5Gi"),
			},
			true,
			false),
	)
})

func NewPodBuilder() *PodBuilder {
	return &PodBuilder{&v1.Pod{}}
}

type PodBuilder struct {
	pod *v1.Pod
}

func (b *PodBuilder) WithNamespace(ns string) *PodBuilder {
	b.pod.ObjectMeta.Namespace = ns
	return b
}

func (b *PodBuilder) WithCreationTimestamp(time metav1.Time) *PodBuilder {
	b.pod.ObjectMeta.CreationTimestamp = time
	return b
}

func (b *PodBuilder) WithName(name string) *PodBuilder {
	b.pod.ObjectMeta.Name = name
	return b
}

func (b *PodBuilder) WithNode(name string) *PodBuilder {
	b.pod.Spec.NodeName = name
	return b
}

func (b *PodBuilder) WithOwnerReference(kind, name, uid string) *PodBuilder {
	b.pod.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: name, UID: types.UID(uid)}}
	return b
}

func (b *PodBuilder) WithLabel(key, value string) *PodBuilder {
	if b.pod.Labels == nil {
		b.pod.Labels = map[string]string{}
	}
	b.pod.Labels[key] = value
	return b
}

func (b *PodBuilder) WithAnnotations(key, value string) *PodBuilder {
	if b.pod.Annotations == nil {
		b.pod.Annotations = map[string]string{}
	}
	b.pod.Annotations[key] = value
	return b
}

func (b *PodBuilder) WithResourcesRequestMemory(q resource.Quantity) *PodBuilder {
	if b.pod.Spec.Containers == nil {
		b.pod.Spec.Containers = []v1.Container{{}}
	}
	if b.pod.Spec.Containers[0].Resources.Requests == nil {
		b.pod.Spec.Containers[0].Resources.Requests = v1.ResourceList{}
	}
	b.pod.Spec.Containers[0].Resources.Requests[v1.ResourceMemory] = q
	return b
}

func (b *PodBuilder) WithResourcesRequestCPU(q resource.Quantity) *PodBuilder {
	if b.pod.Spec.Containers == nil {
		b.pod.Spec.Containers = []v1.Container{{}}
	}
	if b.pod.Spec.Containers[0].Resources.Requests == nil {
		b.pod.Spec.Containers[0].Resources.Requests = v1.ResourceList{}
	}
	b.pod.Spec.Containers[0].Resources.Requests[v1.ResourceCPU] = q
	return b
}

func (b *PodBuilder) WithResourcesLimitsMemory(q resource.Quantity) *PodBuilder {
	if b.pod.Spec.Containers == nil {
		b.pod.Spec.Containers = []v1.Container{{}}
	}
	if b.pod.Spec.Containers[0].Resources.Limits == nil {
		b.pod.Spec.Containers[0].Resources.Limits = v1.ResourceList{}
	}
	b.pod.Spec.Containers[0].Resources.Limits[v1.ResourceMemory] = q
	return b
}

func (b *PodBuilder) WithResourcesLimitsCPU(q resource.Quantity) *PodBuilder {
	if b.pod.Spec.Containers == nil {
		b.pod.Spec.Containers = []v1.Container{{}}
	}
	if b.pod.Spec.Containers[0].Resources.Limits == nil {
		b.pod.Spec.Containers[0].Resources.Limits = v1.ResourceList{}
	}
	b.pod.Spec.Containers[0].Resources.Limits[v1.ResourceCPU] = q
	return b
}

func (b *PodBuilder) Build() *v1.Pod {
	return b.pod
}

func NewVmiBuilder() *VmiBuilder {
	return &VmiBuilder{&v12.VirtualMachineInstance{}}
}

type VmiBuilder struct {
	vmi *v12.VirtualMachineInstance
}

func (b *VmiBuilder) WithNamespace(ns string) *VmiBuilder {
	b.vmi.ObjectMeta.Namespace = ns
	return b
}

func (b *VmiBuilder) WithName(name string) *VmiBuilder {
	b.vmi.ObjectMeta.Name = name
	return b
}

func (b *VmiBuilder) WithNode(nodeName string) *VmiBuilder {
	b.vmi.Status.NodeName = nodeName
	return b
}

func (b *VmiBuilder) WithUID(uid string) *VmiBuilder {
	b.vmi.ObjectMeta.UID = types.UID(uid)
	return b
}

func (b *VmiBuilder) WithResourcesRequestMemory(q resource.Quantity) *VmiBuilder {
	if b.vmi.Spec.Domain.Resources.Requests == nil {
		b.vmi.Spec.Domain.Resources.Requests = v1.ResourceList{}
	}
	b.vmi.Spec.Domain.Resources.Requests[v1.ResourceMemory] = q
	return b
}

func (b *VmiBuilder) WithResourcesRequestCPU(q resource.Quantity) *VmiBuilder {
	if b.vmi.Spec.Domain.Resources.Requests == nil {
		b.vmi.Spec.Domain.Resources.Requests = v1.ResourceList{}
	}
	b.vmi.Spec.Domain.Resources.Requests[v1.ResourceCPU] = q
	return b
}

func (b *VmiBuilder) WithResourcesLimitsMemory(q resource.Quantity) *VmiBuilder {
	if b.vmi.Spec.Domain.Resources.Limits == nil {
		b.vmi.Spec.Domain.Resources.Limits = v1.ResourceList{}
	}
	b.vmi.Spec.Domain.Resources.Limits[v1.ResourceMemory] = q
	return b
}

func (b *VmiBuilder) WithResourcesLimitsCPU(q resource.Quantity) *VmiBuilder {
	if b.vmi.Spec.Domain.Resources.Limits == nil {
		b.vmi.Spec.Domain.Resources.Limits = v1.ResourceList{}
	}
	b.vmi.Spec.Domain.Resources.Limits[v1.ResourceCPU] = q
	return b
}

func (b *VmiBuilder) WithGuestMemory(q resource.Quantity) *VmiBuilder {
	if b.vmi.Spec.Domain.Memory == nil {
		b.vmi.Spec.Domain.Memory = &v12.Memory{}
	}
	b.vmi.Spec.Domain.Memory.Guest = &q
	return b
}

func (b *VmiBuilder) WithGuestCPUCoresSocketsThreads(core, socket, thread uint32) *VmiBuilder {
	if b.vmi.Spec.Domain.CPU == nil {
		b.vmi.Spec.Domain.CPU = &v12.CPU{}
	}
	b.vmi.Spec.Domain.CPU.Cores = core
	b.vmi.Spec.Domain.CPU.Sockets = socket
	b.vmi.Spec.Domain.CPU.Threads = thread
	return b
}

func (b *VmiBuilder) WithActualCpuDuringHotPlug(core, socket, thread uint32) *VmiBuilder {
	if b.vmi.Status.CurrentCPUTopology == nil {
		b.vmi.Status.CurrentCPUTopology = &v12.CPUTopology{}
	}
	b.vmi.Status.CurrentCPUTopology.Cores = core
	b.vmi.Status.CurrentCPUTopology.Sockets = socket
	b.vmi.Status.CurrentCPUTopology.Threads = thread
	return b
}

func (b *VmiBuilder) WithActualMemoryDuringHotPlug(q resource.Quantity) *VmiBuilder {
	if b.vmi.Status.Memory == nil {
		b.vmi.Status.Memory = &v12.MemoryStatus{}
	}
	b.vmi.Status.Memory.GuestCurrent = &q

	return b
}

func (b *VmiBuilder) Build() *v12.VirtualMachineInstance {
	return b.vmi
}

func NewVmimBuilder() *VmimBuilder {
	return &VmimBuilder{&v12.VirtualMachineInstanceMigration{}}
}

type VmimBuilder struct {
	vmim *v12.VirtualMachineInstanceMigration
}

func (b *VmimBuilder) WithNamespace(ns string) *VmimBuilder {
	b.vmim.ObjectMeta.Namespace = ns
	return b
}

func (b *VmimBuilder) WithName(name string) *VmimBuilder {
	b.vmim.ObjectMeta.Name = name
	return b
}

func (b *VmimBuilder) WithVmiName(name string) *VmimBuilder {
	b.vmim.Spec.VMIName = name
	return b
}

func (b *VmimBuilder) WithUID(uid string) *VmimBuilder {
	b.vmim.ObjectMeta.UID = types.UID(uid)
	return b
}

func (b *VmimBuilder) WithCreationTimestamp(time metav1.Time) *VmimBuilder {
	b.vmim.ObjectMeta.CreationTimestamp = time
	return b
}

func (b *VmimBuilder) Build() *v12.VirtualMachineInstanceMigration {
	return b.vmim
}

func parseTimeOrDie(ts string) metav1.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return metav1.Time{Time: t}
}
