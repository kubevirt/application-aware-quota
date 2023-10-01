package built_in_usage_calculators

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v12 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
	v15 "kubevirt.io/api/core/v1"
	aaq_evaluator "kubevirt.io/applications-aware-quota/pkg/aaq-controller/aaq-evaluator"
	"kubevirt.io/applications-aware-quota/pkg/client"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/log"
)

var MyConfigs = []v1alpha1.VmiCalculatorConfiguration{v1alpha1.VmiPodUsage, v1alpha1.VmiUsagePodResources, v1alpha1.VmiUsageVmiResources}

func NewVirtLauncherCalculator(stop <-chan struct{}) *VirtLauncherCalculator {
	aaqCli, err := client.GetAAQClient()
	if err != nil {
		panic("NewVirtLauncherCalculator: couldn't get aaqCli")
	}
	vmiInformer := util.GetVMIInformer(aaqCli)
	migrationInformer := util.GetMigrationInformer(aaqCli)
	go migrationInformer.Run(stop)
	go vmiInformer.Run(stop)

	if !cache.WaitForCacheSync(stop,
		migrationInformer.HasSynced,
		vmiInformer.HasSynced,
	) {
		klog.Warningf("failed to wait for caches to sync")
	}
	return &VirtLauncherCalculator{
		vmiInformer:       vmiInformer,
		migrationInformer: migrationInformer,
		aaqCli:            aaqCli,
	}
}

type VirtLauncherCalculator struct {
	vmiInformer       cache.SharedIndexInformer
	migrationInformer cache.SharedIndexInformer
	aaqCli            client.AAQClient
	calcConfig        v1alpha1.VmiCalculatorConfiguration
}

func (launchercalc *VirtLauncherCalculator) SetConfiguration(config string) {
	launchercalc.calcConfig = v1alpha1.VmiCalculatorConfiguration(config)
}

func (launchercalc *VirtLauncherCalculator) PodUsageFunc(obj runtime.Object, items []runtime.Object, clock clock.Clock) (corev1.ResourceList, error, bool) {
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
	launcherPods := UnfinishedVMIPods(launchercalc.aaqCli, items, vmi)

	if !podExists(launcherPods, pod) {
		return corev1.ResourceList{}, nil, true
	} else if !podExists(launcherPods, pod) { //sanity check
		return corev1.ResourceList{}, fmt.Errorf("can't detect pod as launcher pod"), true
	}

	migration, err := getLatestVmimIfExist(vmi, pod.Namespace, launchercalc.migrationInformer)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}

	if migration == nil || len(launcherPods) == 1 {
		usage, err := launchercalc.CalculateUsageByConfig(pod, vmi, true)
		if err != nil {
			return corev1.ResourceList{}, err, true
		}
		return usage, nil, true
	}

	if len(launcherPods) != 2 {
		return corev1.ResourceList{}, fmt.Errorf("something is wrong 2 launchers pods should exist while migration"), true
	}

	sourcePod := getSourcePod(launcherPods, migration)
	targetPod := getTargetPod(launcherPods, migration)
	if sourcePod == nil || targetPod == nil {
		var launcherPodsForErr []string
		for _, pod := range launcherPods {
			launcherPodsForErr = append(launcherPodsForErr, pod.Name)
		}
		return corev1.ResourceList{}, fmt.Errorf(fmt.Sprintf("something is wrong could not detect source or target pod launcherPods in ns: %v  source == nil: %v target == nil: %v ", launcherPodsForErr, sourcePod == nil, targetPod == nil)), true
	}

	if pod.Name == sourcePod.Name { // we are calculating source resources
		usage, err := launchercalc.CalculateUsageByConfig(pod, vmi, true)
		if err != nil {
			return corev1.ResourceList{}, err, true
		}
		return usage, nil, true

	}
	targetResources, err := launchercalc.CalculateUsageByConfig(pod, vmi, false)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}
	sourceResources, err := launchercalc.CalculateUsageByConfig(pod, vmi, true)
	if err != nil {
		return corev1.ResourceList{}, err, true
	}
	return v12.SubtractWithNonNegativeResult(targetResources, sourceResources), nil, true
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
	case v1alpha1.VmiUsagePodResources:
		return CalculateResourceLauncherVMIUsagePodResources(vmi, isSourceOrSingleLauncher), nil
	case v1alpha1.VmiUsageVmiResources:
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

func validConfig(target v1alpha1.VmiCalculatorConfiguration) bool {
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
	}
	//todo: once memory hot-plug is implemented we need to update this
	memoryGuest := corev1.ResourceList{}
	memoryGuestHugePages := corev1.ResourceList{}
	domainResourcesReq := corev1.ResourceList{}
	domainResourcesLim := corev1.ResourceList{}

	if vmi.Spec.Domain.Memory != nil && vmi.Spec.Domain.Memory.Guest != nil {
		memoryGuest = corev1.ResourceList{corev1.ResourceRequestsMemory: *vmi.Spec.Domain.Memory.Guest}
	}
	if vmi.Spec.Domain.Memory != nil && vmi.Spec.Domain.Memory.Hugepages != nil {
		quantity, err := resource.ParseQuantity(vmi.Spec.Domain.Memory.Hugepages.PageSize)
		if err == nil {
			memoryGuestHugePages = corev1.ResourceList{corev1.ResourceRequestsMemory: quantity}
		}
	}
	if vmi.Spec.Domain.Resources.Requests != nil {
		resourceMemory, resourceMemoryExist := vmi.Spec.Domain.Resources.Requests[corev1.ResourceMemory]
		resourceRequestsMemory, resourceRequestsMemoryExist := vmi.Spec.Domain.Resources.Requests[corev1.ResourceRequestsMemory]
		if resourceMemoryExist && !resourceRequestsMemoryExist {
			domainResourcesReq = corev1.ResourceList{corev1.ResourceRequestsMemory: resourceMemory}
		} else if resourceRequestsMemoryExist {
			domainResourcesReq = corev1.ResourceList{corev1.ResourceRequestsMemory: resourceRequestsMemory}
		}
	}
	if vmi.Spec.Domain.Resources.Limits != nil {
		resourceMemory, resourceMemoryExist := vmi.Spec.Domain.Resources.Limits[corev1.ResourceMemory]
		resourceLimitsMemory, resourceLimitsMemoryExist := vmi.Spec.Domain.Resources.Limits[corev1.ResourceLimitsMemory]
		if resourceMemoryExist && !resourceLimitsMemoryExist {
			domainResourcesLim = corev1.ResourceList{corev1.ResourceLimitsMemory: resourceMemory}
		} else if resourceLimitsMemoryExist {
			domainResourcesLim = corev1.ResourceList{corev1.ResourceLimitsMemory: resourceLimitsMemory}
		}
	}

	tmpMemReq := v12.Max(memoryGuest, domainResourcesReq)
	tmpMemLim := v12.Max(memoryGuest, domainResourcesLim)

	finalMemReq := v12.Max(tmpMemReq, memoryGuestHugePages)
	finalMemLim := v12.Max(tmpMemLim, corev1.ResourceList{corev1.ResourceLimitsMemory: finalMemReq[corev1.ResourceRequestsMemory]})
	requests := corev1.ResourceList{
		corev1.ResourceRequestsMemory: finalMemReq[corev1.ResourceRequestsMemory],
	}
	limits := corev1.ResourceList{
		corev1.ResourceLimitsMemory: finalMemLim[corev1.ResourceLimitsMemory],
	}

	var cpuTopology *v15.CPUTopology
	if isSourceOrSingleLauncher && vmi.Status.CurrentCPUTopology != nil {
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

	guestCpuReq := v12.Max(corev1.ResourceList{corev1.ResourceRequestsCPU: vcpuQuantity}, domainResourcesReq)
	guestCpuLim := v12.Max(corev1.ResourceList{corev1.ResourceLimitsCPU: vcpuQuantity}, domainResourcesLim)

	requests[corev1.ResourceRequestsCPU] = guestCpuReq[corev1.ResourceRequestsCPU]
	limits[corev1.ResourceLimitsCPU] = guestCpuLim[corev1.ResourceLimitsCPU]

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

func UnfinishedVMIPods(aaqCli client.AAQClient, items []runtime.Object, vmi *v15.VirtualMachineInstance) (podsToReturn []*corev1.Pod) {
	pods := []corev1.Pod{}
	if items != nil {
		for _, item := range items {
			pod, err := aaq_evaluator.ToExternalPodOrError(item)
			if err != nil {
				continue
			}
			pods = append(pods, *pod)
		}
	} else {
		podsList, err := aaqCli.CoreV1().Pods(vmi.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			log.Log.Infof("AaqGateController: Error: %v", err)
		}
		pods = podsList.Items
	}

	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		if app, ok := pod.Labels[v15.AppLabel]; !ok || app != util.LauncherLabel {
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
		if migrationLabelExist && migrationAnnExist && migrationUID == string(migration.UID) && migrationName == migration.Name {
			return pod
		}
	}
	return nil
}

func getSourcePod(allPods []*corev1.Pod, migration *v15.VirtualMachineInstanceMigration) *corev1.Pod {
	targetPod := getTargetPod(allPods, migration)
	if targetPod == nil {
		return nil
	}
	for _, pod := range allPods {
		if pod.Name != targetPod.Name {
			return pod
		}
	}
	return nil
}
