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
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

const launcherLabel = "virt-launcher"

var MyConfigs = []v1alpha1.VmiCalcConfigName{v1alpha1.VmiPodUsage, v1alpha1.VirtualResources, v1alpha1.DedicatedVirtualResources}

func NewVirtLauncherCalculator(vmiInformer cache.SharedIndexInformer, migrationInformer cache.SharedIndexInformer, calcConfig v1alpha1.VmiCalcConfigName) *VirtLauncherCalculator {
	return &VirtLauncherCalculator{
		vmiInformer:       vmiInformer,
		migrationInformer: migrationInformer,
		calcConfig:        calcConfig,
	}
}

type VirtLauncherCalculator struct {
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
	if !validConfig(config, MyConfigs) {
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
			v1alpha1.ResourcePodsOfVmi: *(resource.NewQuantity(1, resource.DecimalSI)),
		}
		usageToConvertVmiResources := CalculateResourceLauncherVMIUsagePodResources(vmi, isSourceOrSingleLauncher)
		if memRq, ok := usageToConvertVmiResources[corev1.ResourceRequestsMemory]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiMemory] = memRq
		}
		if memLim, ok := usageToConvertVmiResources[corev1.ResourceLimitsMemory]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiMemory] = memLim
		}
		if cpuRq, ok := usageToConvertVmiResources[corev1.ResourceRequestsCPU]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiCPU] = cpuRq
		}
		if cpuLim, ok := usageToConvertVmiResources[corev1.ResourceLimitsCPU]; ok {
			vmiRl[v1alpha1.ResourceRequestsVmiCPU] = cpuLim
		}
		return vmiRl, nil
	}

	podEvaluator := core.NewPodEvaluator(nil, clock.RealClock{})
	return podEvaluator.Usage(pod)
}

func validConfig(target v1alpha1.VmiCalcConfigName, configs []v1alpha1.VmiCalcConfigName) bool {
	for _, item := range configs {
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
		vcpus := GetNumberOfVCPUs(cpuTopology)
		cpuGuest = *resource.NewQuantity(vcpus, resource.BinarySI)
	}

	if isSourceOrSingleLauncher && vmi.Status.Memory != nil {
		memoryGuest = vmi.Status.Memory.GuestCurrent.DeepCopy()
	}

	if isSourceOrSingleLauncher && vmi.Status.CurrentCPUTopology != nil {
		cpuTopology = vmi.Status.CurrentCPUTopology
		vcpus := GetNumberOfVCPUs(cpuTopology)
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
		corev1.ResourceRequestsCPU:    cpuGuest,
	}
	limits := corev1.ResourceList{
		corev1.ResourceLimitsMemory: memoryGuest,
		corev1.ResourceLimitsCPU:    cpuGuest,
	}

	result = v12.Add(result, requests)
	result = v12.Add(result, limits)
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
