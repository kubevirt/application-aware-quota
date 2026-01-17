package built_in_usage_calculators

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	v15 "kubevirt.io/api/core/v1"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/built-in-usage-calculators/cpu"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/built-in-usage-calculators/overhead_calculator"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

const launcherLabel = "virt-launcher"

var MyConfigs = []v1alpha1.VmiCalcConfigName{v1alpha1.VmiPodUsage, v1alpha1.VirtualResources, v1alpha1.DedicatedVirtualResources, v1alpha1.GuestEffectiveResources}

func NewVirtLauncherCalculator(kvInformer cache.SharedIndexInformer, vmiInformer cache.SharedIndexInformer, migrationInformer cache.SharedIndexInformer, calcConfig v1alpha1.VmiCalcConfigName) *VirtLauncherCalculator {
	return &VirtLauncherCalculator{
		kvInformer:        kvInformer,
		vmiInformer:       vmiInformer,
		migrationInformer: migrationInformer,
		calcConfig:        calcConfig,
	}
}

type VirtLauncherCalculator struct {
	kvInformer        cache.SharedIndexInformer
	vmiInformer       cache.SharedIndexInformer
	migrationInformer cache.SharedIndexInformer
	calcConfig        v1alpha1.VmiCalcConfigName
}

func (launchercalc *VirtLauncherCalculator) PodUsageFunc(pod *corev1.Pod, existingPods []*corev1.Pod) (corev1.ResourceList, error, bool) {
	if pod.OwnerReferences == nil || len(pod.OwnerReferences) == 0 || pod.OwnerReferences[0].Kind != v15.VirtualMachineInstanceGroupVersionKind.Kind {
		return corev1.ResourceList{}, nil, false
	}

	vmiObj, vmiExists, err := launchercalc.vmiInformer.GetIndexer().GetByKey(fmt.Sprintf("%s/%s", pod.Namespace, pod.OwnerReferences[0].Name))
	if err != nil || !vmiExists {
		return corev1.ResourceList{}, nil, false
	}
	vmi := vmiObj.(*v15.VirtualMachineInstance)
	launcherPods := UnfinishedVMIPods(existingPods, vmi)

	if !podExists(launcherPods, pod) { //sanity check
		return corev1.ResourceList{}, fmt.Errorf("can't detect pod as launcher pod"), true
	}

	migration, err := getLatestVmimIfExist(vmi, pod.Namespace, launchercalc.migrationInformer)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}

	sourcePod := getSourcePod(launcherPods, vmi)
	targetPod := getTargetPod(launcherPods, migration)

	sourceResources, err := launchercalc.calculateSourceUsageByConfig(pod, vmi)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}

	if pod.Name == sourcePod.Name {
		return sourceResources, nil, true
	} else if targetPod != nil && pod.Name == targetPod.Name {
		targetResources, err := launchercalc.calculateTargetUsageByConfig(pod, vmi)
		if err != nil {
			return corev1.ResourceList{}, err, true
		}
		return v12.SubtractWithNonNegativeResult(targetResources, sourceResources), nil, true
	}
	//shouldn't get here unless we evaluate orphan launcher pod, in that case the pod will be deleted soon
	return corev1.ResourceList{}, nil, true
}

func (launchercalc *VirtLauncherCalculator) calculateSourceUsageByConfig(pod *corev1.Pod, vmi *v15.VirtualMachineInstance) (corev1.ResourceList, error) {
	return launchercalc.CalculateUsageByConfig(pod, vmi, true)
}

func (launchercalc *VirtLauncherCalculator) calculateTargetUsageByConfig(pod *corev1.Pod, vmi *v15.VirtualMachineInstance) (corev1.ResourceList, error) {
	return launchercalc.CalculateUsageByConfig(pod, vmi, false)
}

func (launchercalc *VirtLauncherCalculator) CalculateUsageByConfig(pod *corev1.Pod, vmi *v15.VirtualMachineInstance, isSourceOrSingleLauncher bool) (corev1.ResourceList, error) {
	config := launchercalc.calcConfig
	if !validConfig(config) {
		config = util.DefaultLauncherConfig
	}
	switch config {
	case v1alpha1.VmiPodUsage:
		podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
		return podEvaluator.Usage(pod)
	case v1alpha1.VirtualResources:
		return CalculateResourceLauncherVMIUsagePodResources(vmi, isSourceOrSingleLauncher), nil
	case v1alpha1.DedicatedVirtualResources:
		vmiRl := corev1.ResourceList{
			v1alpha1.ResourcePodsOfVmi:      *(resource.NewQuantity(1, resource.DecimalSI)),
			v1alpha1.ResourcePodsOfVmiShort: *(resource.NewQuantity(1, resource.DecimalSI)),
		}
		usageToConvertVmiResources := CalculateResourceLauncherVMIUsagePodResources(vmi, isSourceOrSingleLauncher)
		if memRq, ok := usageToConvertVmiResources[corev1.ResourceRequestsMemory]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiMemory] = memRq
			vmiRl[v1alpha1.ResourceRequestsVmiMemoryShort] = memRq
		}
		if memLim, ok := usageToConvertVmiResources[corev1.ResourceLimitsMemory]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiMemory] = memLim
			vmiRl[v1alpha1.ResourceRequestsVmiMemoryShort] = memLim
		}
		if cpuRq, ok := usageToConvertVmiResources[corev1.ResourceRequestsCPU]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiCPU] = cpuRq
			vmiRl[v1alpha1.ResourceRequestsVmiCPUShort] = cpuRq
		}
		if cpuLim, ok := usageToConvertVmiResources[corev1.ResourceLimitsCPU]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiCPU] = cpuLim
			vmiRl[v1alpha1.ResourceRequestsVmiCPUShort] = cpuLim
		}
		return vmiRl, nil
	case v1alpha1.GuestEffectiveResources:
		computeOnlyPod := createPodWithComputeContainerOnly(pod)
		podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
		usageWithOverhead, err := podEvaluator.Usage(computeOnlyPod)
		if err != nil {
			return nil, err
		}

		overheadResources := corev1.ResourceList{}
		if launchercalc.kvInformer != nil {
			kvConfig := overhead_calculator.GetConfigFromKubeVirtCR(launchercalc.kvInformer)
			if kvConfig != nil {
				memoryOverhead := overhead_calculator.CalculateMemoryOverhead(kvConfig, overhead_calculator.MemoryCalculator{}, vmi)
				if !memoryOverhead.IsZero() {
					if _, hasMemoryRequests := usageWithOverhead[corev1.ResourceRequestsMemory]; hasMemoryRequests {
						overheadResources[corev1.ResourceRequestsMemory] = memoryOverhead
					}
					if _, hasMemoryLimits := usageWithOverhead[corev1.ResourceLimitsMemory]; hasMemoryLimits {
						overheadResources[corev1.ResourceLimitsMemory] = memoryOverhead
					}
				}
			}
		}
		return v12.SubtractWithNonNegativeResult(usageWithOverhead, overheadResources), nil
	}

	podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
	return podEvaluator.Usage(pod)
}

func validConfig(target v1alpha1.VmiCalcConfigName) bool {
	for _, item := range MyConfigs {
		if item == target {
			return true
		}
	}
	return false
}

func CalculateResourceLauncherVMIUsagePodResources(vmi *v15.VirtualMachineInstance, isSourceOrSingleLauncher bool) corev1.ResourceList {
	result := corev1.ResourceList{
		corev1.ResourcePods: *(resource.NewQuantity(1, resource.DecimalSI)),
		"count/pods":        *(resource.NewQuantity(1, resource.DecimalSI)),
	}

	var memoryGuest resource.Quantity
	var cpuGuest resource.Quantity
	var cpuTopology *v15.CPUTopology

	if vmi.Spec.Domain.Resources.Limits != nil {
		memoryResource, memoryResourceExist := vmi.Spec.Domain.Resources.Limits[corev1.ResourceMemory]
		if memoryResourceExist {
			memoryGuest = memoryResource.DeepCopy()
		}
		cpuResource, cpuResourceExist := vmi.Spec.Domain.Resources.Limits[corev1.ResourceCPU]
		if cpuResourceExist {
			cpuGuest = cpuResource.DeepCopy()
		}
	}

	if vmi.Spec.Domain.Resources.Requests != nil {
		memoryResource, memoryResourceExist := vmi.Spec.Domain.Resources.Requests[corev1.ResourceMemory]
		if memoryResourceExist {
			memoryGuest = memoryResource.DeepCopy()
		}
		cpuResource, cpuResourceExist := vmi.Spec.Domain.Resources.Requests[corev1.ResourceCPU]
		if cpuResourceExist {
			cpuGuest = cpuResource.DeepCopy()
		}
	}

	if vmi.Spec.Domain.Memory != nil && vmi.Spec.Domain.Memory.Guest != nil {
		memoryGuest = vmi.Spec.Domain.Memory.Guest.DeepCopy()
	}

	if vmi.Spec.Domain.CPU != nil {
		cpuTopology = &v15.CPUTopology{
			Cores:   vmi.Spec.Domain.CPU.Cores,
			Sockets: vmi.Spec.Domain.CPU.Sockets,
			Threads: vmi.Spec.Domain.CPU.Threads,
		}
		vcpus := cpu.GetNumberOfVCPUs(cpuTopology)
		cpuGuest = *resource.NewQuantity(vcpus, resource.BinarySI)
	}

	if isSourceOrSingleLauncher && vmi.Status.Memory != nil {
		// In KubeVirt, GuestRequested is only updated after migration succeeds.
		// Here's how the counting works in different migration states:
		// 1. User requests a hotplug, but migration hasn’t started yet:
		//    - For the source, we count using GuestRequested, which holds the old value.
		//    - For the target, we count as 0 since it doesn’t exist yet.
		// 2. Migration has started, and both source and target exist:
		//    - For the source, we still count using GuestRequested with the old value.
		//    - For the target, we count using Spec.Domain.Memory(new value) - GuestRequested(old value), to block hot-plug
		//      if there is no room for the delta.
		// 3. Migration finished, only the target (now the new source) exists, but the hotplug hasn’t been done yet:
		//    - For the target, we count using GuestRequested, which now has the new value.
		//    (virt-handler will update GuestRequested to the new value before finalizing the migration.)
		// 4. After the hotplug is completed, we the counting is the same as in step 3.
		memoryGuest = vmi.Status.Memory.GuestRequested.DeepCopy()
	}

	if isSourceOrSingleLauncher && vmi.Status.CurrentCPUTopology != nil {
		cpuTopology = vmi.Status.CurrentCPUTopology
		vcpus := cpu.GetNumberOfVCPUs(cpuTopology)
		cpuGuest = *resource.NewQuantity(vcpus, resource.BinarySI)
	}

	if cpuGuest.IsZero() {
		log.Log.Errorf("Couldn't find guest cpu")
	}
	if memoryGuest.IsZero() {
		log.Log.Errorf("Couldn't find guest memory")
	}

	requests := corev1.ResourceList{
		corev1.ResourceRequestsMemory: memoryGuest,
		corev1.ResourceMemory:         memoryGuest,
		corev1.ResourceRequestsCPU:    cpuGuest,
		corev1.ResourceCPU:            cpuGuest,
	}
	limits := corev1.ResourceList{
		corev1.ResourceLimitsMemory: memoryGuest,
		corev1.ResourceLimitsCPU:    cpuGuest,
	}

	result = v12.Add(result, requests)
	result = v12.Add(result, limits)
	return result
}

func UnfinishedVMIPods(pods []*corev1.Pod, vmi *v15.VirtualMachineInstance) (podsToReturn []*corev1.Pod) {
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		if app, ok := pod.Labels[v15.AppLabel]; !ok || app != launcherLabel {
			continue
		}
		if pod.OwnerReferences == nil {
			continue
		}

		ownerRefs := pod.GetOwnerReferences()
		found := false
		for _, ownerRef := range ownerRefs {
			if ownerRef.UID == vmi.GetUID() {
				found = true
			}
		}

		if !found {
			continue
		}
		podsToReturn = append(podsToReturn, pod.DeepCopy())
	}
	return podsToReturn
}

func podExists(pods []*corev1.Pod, targetPod *corev1.Pod) bool {
	for _, pod := range pods {
		if pod.Namespace == targetPod.Namespace &&
			pod.Name == targetPod.Name {
			return true
		}
	}
	return false
}

func getLatestVmimIfExist(vmi *v15.VirtualMachineInstance, ns string, migrationInformer cache.SharedIndexInformer) (*v15.VirtualMachineInstanceMigration, error) {
	migrationObjs, err := migrationInformer.GetIndexer().ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return nil, fmt.Errorf("can't fetch migrations")
	}

	var latestVmim *v15.VirtualMachineInstanceMigration
	latestTimestamp := metav1.Time{}

	for _, migrationObj := range migrationObjs {
		vmim := migrationObj.(*v15.VirtualMachineInstanceMigration)
		if vmim.Status.Phase != v15.MigrationFailed && vmim.Spec.VMIName == vmi.Name {
			if vmim.CreationTimestamp.After(latestTimestamp.Time) {
				latestTimestamp = vmim.CreationTimestamp
				latestVmim = vmim
			}
		}
	}

	return latestVmim, nil
}

func getTargetPod(allPods []*corev1.Pod, migration *v15.VirtualMachineInstanceMigration) *corev1.Pod {
	for _, pod := range allPods {
		migrationUID, migrationLabelExist := pod.Labels[v15.MigrationJobLabel]
		migrationName, migrationAnnExist := pod.Annotations[v15.MigrationJobNameAnnotation]
		if migration != nil && migrationLabelExist && migrationAnnExist && migrationUID == string(migration.UID) && migrationName == migration.Name {
			return pod
		}
	}
	return nil
}

func getSourcePod(pods []*corev1.Pod, vmi *v15.VirtualMachineInstance) *corev1.Pod {
	var curPod *corev1.Pod = nil
	for _, pod := range pods {
		if vmi.Status.NodeName != "" &&
			vmi.Status.NodeName != pod.Spec.NodeName {
			// This pod isn't scheduled to the current node.
			// This can occur during the initial migration phases when
			// a new target node is being prepared for the VMI.
			continue
		}

		if curPod == nil || curPod.CreationTimestamp.Before(&pod.CreationTimestamp) {
			curPod = pod
		}
	}

	return curPod
}

const computeContainerName = "compute"

func createPodWithComputeContainerOnly(pod *corev1.Pod) *corev1.Pod {
	computeOnlyPod := &corev1.Pod{
		ObjectMeta: pod.ObjectMeta,
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{},
		},
		Status: pod.Status,
	}

	for _, container := range pod.Spec.Containers {
		if container.Name == computeContainerName {
			computeOnlyPod.Spec.Containers = append(computeOnlyPod.Spec.Containers, container)
			break
		}
	}

	return computeOnlyPod
}
