package built_in_usage_calculators

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	v15 "kubevirt.io/api/core/v1"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/client-go/log"
)

var podObjectCountName = generic.ObjectCountQuotaResourceNameFor(corev1.SchemeGroupVersion.WithResource("pods").GroupResource())

func NewVirtLauncherCalculator(stop <-chan struct{}) *VirtLauncherCalculator {
	virtCli, err := util.GetVirtCli()
	if err != nil {
		panic("NewVirtLauncherCalculator: couldn't get virtCli")
	}
	vmiInformer := util.GetVMIInformer(virtCli)
	podInformer := util.GetLauncherPodInformer(virtCli)
	migrationInformer := util.GetMigrationInformer(virtCli)
	go migrationInformer.Run(stop)
	go vmiInformer.Run(stop)
	go podInformer.Run(stop)

	if !cache.WaitForCacheSync(stop,
		migrationInformer.HasSynced,
		vmiInformer.HasSynced,
		podInformer.HasSynced,
	) {
		klog.Warningf("failed to wait for caches to sync")
	}
	return &VirtLauncherCalculator{
		vmiInformer:       vmiInformer,
		migrationInformer: migrationInformer,
		podInformer:       podInformer,
	}
}

type VirtLauncherCalculator struct {
	vmiInformer       cache.SharedIndexInformer
	migrationInformer cache.SharedIndexInformer
	podInformer       cache.SharedIndexInformer
}

func (launchercalc *VirtLauncherCalculator) PodUsageFunc(obj runtime.Object, clock clock.Clock) (corev1.ResourceList, error, bool) {
	pod, err := aaq_evaluator.ToExternalPodOrError(obj)
	if err != nil {
		return corev1.ResourceList{}, err, false
	}

	if pod.OwnerReferences == nil || len(pod.OwnerReferences) == 0 || pod.OwnerReferences[0].Kind != v15.VirtualMachineInstanceGroupVersionKind.Kind || !core.QuotaV1Pod(pod, clock) {
		return corev1.ResourceList{}, nil, false
	}

	vmiObj, vmiExists, err := launchercalc.vmiInformer.GetStore().GetByKey(fmt.Sprintf("%s/%s", pod.Namespace, pod.OwnerReferences[0].Name))
	if err != nil || !vmiExists {
		return corev1.ResourceList{}, nil, false
	}

	vmi := vmiObj.(*v15.VirtualMachineInstance)
	launcherPods := UnfinishedVMIPods(launchercalc.podInformer, vmi)

	if !podExists(launcherPods, pod) { //sanity check
		return corev1.ResourceList{}, fmt.Errorf("can't detect pod as launcher pod"), true
	}

	migration, err := getVmimIfExist(vmi, pod.Namespace, launchercalc.migrationInformer)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}

	if migration == nil {
		if len(launcherPods) > 1 {
			return corev1.ResourceList{}, fmt.Errorf("something is wrong multiple launchers while not migrating"), true
		}
		return CalculateResourceListForLauncherPod(vmi, true), nil, true
	}

	if len(launcherPods) != 2 {
		return corev1.ResourceList{}, fmt.Errorf("something is wrong 2 launchers pods should exist while migration"), true
	}

	sourcePod := getSourcePod(launcherPods, migration)
	targetPod := getTargetPod(launcherPods, migration)
	if sourcePod == nil || targetPod == nil {
		return corev1.ResourceList{}, fmt.Errorf("something is wrong could not detect source or target pod"), true
	}

	if pod.Name == sourcePod.Name { // we are calculating source resources
		return CalculateResourceListForLauncherPod(vmi, true), nil, true
	}

	return v12.SubtractWithNonNegativeResult(CalculateResourceListForLauncherPod(vmi, false), CalculateResourceListForLauncherPod(vmi, true)), nil, true
}
func CalculateResourceListForLauncherPod(vmi *v15.VirtualMachineInstance, isSourceOrSingleLauncher bool) corev1.ResourceList {
	result := corev1.ResourceList{
		podObjectCountName: *(resource.NewQuantity(1, resource.DecimalSI)),
	}
	//todo: once memory hot-plug is implemented we need to update this
	guestMemReq := v12.Max(corev1.ResourceList{corev1.ResourceRequestsMemory: *vmi.Spec.Domain.Memory.Guest}, vmi.Spec.Domain.Resources.Requests)
	guestMemLim := v12.Max(corev1.ResourceList{corev1.ResourceLimitsMemory: *vmi.Spec.Domain.Memory.Guest}, vmi.Spec.Domain.Resources.Limits)
	requests := corev1.ResourceList{
		corev1.ResourceRequestsMemory: guestMemReq[corev1.ResourceRequestsMemory],
	}
	limits := corev1.ResourceList{
		corev1.ResourceLimitsMemory: guestMemLim[corev1.ResourceLimitsMemory],
	}

	var cpuTopology *v15.CPUTopology
	if isSourceOrSingleLauncher {
		cpuTopology = vmi.Status.CurrentCPUTopology
	} else {
		cpuTopology = &v15.CPUTopology{
			Cores:   vmi.Spec.Domain.CPU.Cores,
			Sockets: vmi.Spec.Domain.CPU.Sockets,
			Threads: vmi.Spec.Domain.CPU.Threads,
		}
	}
	vcpus := GetNumberOfVCPUs(cpuTopology)
	vcpuQuantity := *resource.NewQuantity(vcpus, resource.BinarySI)
	guestCpuReq := v12.Max(corev1.ResourceList{corev1.ResourceRequestsCPU: vcpuQuantity}, vmi.Spec.Domain.Resources.Requests)
	guestCpuLim := v12.Max(corev1.ResourceList{corev1.ResourceLimitsCPU: vcpuQuantity}, vmi.Spec.Domain.Resources.Limits)

	requests[corev1.ResourceRequestsCPU] = guestCpuReq[corev1.ResourceRequestsCPU]
	limits[corev1.ResourceLimitsCPU] = guestCpuLim[corev1.ResourceLimitsCPU]
	result = v12.Add(result, podComputeUsageHelper(requests, limits))

	return result
}

func GetNumberOfVCPUs(cpuSpec *v15.CPUTopology) int64 {
	vCPUs := cpuSpec.Cores
	if cpuSpec.Sockets != 0 {
		if vCPUs == 0 {
			vCPUs = cpuSpec.Sockets
		} else {
			vCPUs *= cpuSpec.Sockets
		}
	}
	if cpuSpec.Threads != 0 {
		if vCPUs == 0 {
			vCPUs = cpuSpec.Threads
		} else {
			vCPUs *= cpuSpec.Threads
		}
	}
	return int64(vCPUs)
}

func UnfinishedVMIPods(podInformer cache.SharedIndexInformer, vmi *v15.VirtualMachineInstance) (pods []*corev1.Pod) {
	objs, err := podInformer.GetIndexer().ByIndex(cache.NamespaceIndex, vmi.Namespace)
	if err != nil {
		log.Log.Infof("AaqGateController: Error failed to list pod from podInformer")
	}
	for _, obj := range objs {
		pod := obj.(*corev1.Pod)
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		if app, ok := pod.Labels[v15.AppLabel]; !ok || app != util.LauncherLabel {
			continue
		}
		if pod.OwnerReferences != nil {
			continue
		}

		ownerRefs := pod.GetOwnerReferences()
		found := false
		for _, ownerRef := range ownerRefs {
			if ownerRef.UID != vmi.GetUID() {
				found = true
			}
		}

		if !found {
			continue
		}

		pods = append(pods, pod)
	}
	return pods
}

func podExists(pods []*corev1.Pod, targetPod *corev1.Pod) bool {
	for _, pod := range pods {
		if pod.ObjectMeta.Namespace == targetPod.ObjectMeta.Namespace &&
			pod.ObjectMeta.Name == targetPod.ObjectMeta.Name {
			return true
		}
	}
	return false
}

var requestedResourcePrefixes = []string{
	corev1.ResourceHugePagesPrefix,
}

// maskResourceWithPrefix mask resource with certain prefix
// e.g. hugepages-XXX -> requests.hugepages-XXX
func maskResourceWithPrefix(resource corev1.ResourceName, prefix string) corev1.ResourceName {
	return corev1.ResourceName(fmt.Sprintf("%s%s", prefix, string(resource)))
}

// podComputeUsageHelper can summarize the pod compute quota usage based on requests and limits
func podComputeUsageHelper(requests corev1.ResourceList, limits corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	result[corev1.ResourcePods] = resource.MustParse("1")
	if request, found := requests[corev1.ResourceCPU]; found {
		result[corev1.ResourceCPU] = request
		result[corev1.ResourceRequestsCPU] = request
	}
	if limit, found := limits[corev1.ResourceCPU]; found {
		result[corev1.ResourceLimitsCPU] = limit
	}
	if request, found := requests[corev1.ResourceMemory]; found {
		result[corev1.ResourceMemory] = request
		result[corev1.ResourceRequestsMemory] = request
	}
	if limit, found := limits[corev1.ResourceMemory]; found {
		result[corev1.ResourceLimitsMemory] = limit
	}
	if request, found := requests[corev1.ResourceEphemeralStorage]; found {
		result[corev1.ResourceEphemeralStorage] = request
		result[corev1.ResourceRequestsEphemeralStorage] = request
	}
	if limit, found := limits[corev1.ResourceEphemeralStorage]; found {
		result[corev1.ResourceLimitsEphemeralStorage] = limit
	}
	for resource, request := range requests {
		// for resources with certain prefix, e.g. hugepages
		if v12.ContainsPrefix(requestedResourcePrefixes, resource) {
			result[resource] = request
			result[maskResourceWithPrefix(resource, corev1.DefaultResourceRequestsPrefix)] = request
		}
		// for extended resources
		if helper.IsExtendedResourceName(resource) {
			// only quota objects in format of "requests.resourceName" is allowed for extended resource.
			result[maskResourceWithPrefix(resource, corev1.DefaultResourceRequestsPrefix)] = request
		}
	}

	return result
}

func getVmimIfExist(vmi *v15.VirtualMachineInstance, ns string, migrationInformer cache.SharedIndexInformer) (*v15.VirtualMachineInstanceMigration, error) {
	migrationObjs, err := migrationInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return nil, fmt.Errorf("can't fetch migrations")
	}
	for _, migrationObj := range migrationObjs {
		vmim := migrationObj.(*v15.VirtualMachineInstanceMigration)
		if vmim.Status.Phase != v15.MigrationFailed && vmim.Status.Phase != v15.MigrationSucceeded && vmim.Spec.VMIName == vmi.Name {
			return vmim, nil
		}
	}
	return nil, nil
}

func getTargetPod(allPods []*corev1.Pod, migration *v15.VirtualMachineInstanceMigration) *corev1.Pod {
	for _, pod := range allPods {
		migrationUID, migrationLabelExist := pod.Labels[v15.MigrationJobLabel]
		migrationName, migrationAnnExist := pod.Annotations[v15.MigrationJobNameAnnotation]
		if migrationLabelExist && migrationAnnExist && migrationUID == string(migration.UID) && migrationName == migration.Name {
			return pod
		}
	}
	return nil
}

func getSourcePod(allPods []*corev1.Pod, migration *v15.VirtualMachineInstanceMigration) *corev1.Pod {
	targetPod := getTargetPod(allPods, migration)
	for _, pod := range allPods {
		if pod.Name != targetPod.Name {
			return pod
		}
	}
	return nil
}
