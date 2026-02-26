package overhead_calculator

import (
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/cache"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/built-in-usage-calculators/cpu"
	"kubevirt.io/application-aware-quota/pkg/log"
	"runtime"
	"strconv"
)

const (
	VirtLauncherMonitorOverhead = "25Mi"  // The `ps` RSS for virt-launcher-monitor
	VirtLauncherOverhead        = "100Mi" // The `ps` RSS for the virt-launcher process
	VirtlogdOverhead            = "25Mi"  // The `ps` RSS for virtlogd
	VirtqemudOverhead           = "40Mi"  // The `ps` RSS for virtqemud
	QemuOverhead                = "30Mi"  // The `ps` RSS for qemu, minus the RAM of its (stressed) guest, minus the virtual page table
)

func CalculateMemoryOverhead(kvConfig *v1.KubeVirtConfiguration, netBindingPluginMemoryCalculator netBindingPluginMemoryCalculator, vmi *v1.VirtualMachineInstance) resource.Quantity {
	// Set default with vmi Architecture. compatible with multi-architecture hybrid environments
	vmiCPUArch := vmi.Spec.Architecture
	if vmiCPUArch == "" {
		vmiCPUArch = runtime.GOARCH
	}
	memoryOverhead := GetMemoryOverhead(vmi, runtime.GOARCH, kvConfig.AdditionalGuestMemoryOverheadRatio)

	var networkBinding map[string]v1.InterfaceBindingPlugin
	if kvConfig.NetworkConfiguration != nil {
		networkBinding = kvConfig.NetworkConfiguration.Binding
	}

	if netBindingPluginMemoryCalculator != nil {
		memoryOverhead.Add(
			netBindingPluginMemoryCalculator.Calculate(vmi, networkBinding),
		)
	}

	return memoryOverhead
}

// GetMemoryOverhead computes the estimation of total
// memory needed for the domain to operate properly.
// This includes the memory needed for the guest and memory
// for Qemu and OS overhead.
// The return value is overhead memory quantity
//
// Note: This is the best estimation we were able to come up with
//
//	and is still not 100% accurate
func GetMemoryOverhead(vmi *v1.VirtualMachineInstance, cpuArch string, additionalOverheadRatio *string) resource.Quantity {
	domain := vmi.Spec.Domain
	vmiMemoryReq := domain.Resources.Requests.Memory()

	overhead := *resource.NewScaledQuantity(0, resource.Kilo)

	// Add the memory needed for pagetables (one bit for every 512b of RAM size)
	pagetableMemory := resource.NewScaledQuantity(vmiMemoryReq.ScaledValue(resource.Kilo), resource.Kilo)
	pagetableMemory.Set(pagetableMemory.Value() / 512)
	overhead.Add(*pagetableMemory)

	// Add fixed overhead for KubeVirt components, as seen in a random run, rounded up to the nearest MiB
	// Note: shared libraries are included in the size, so every library is counted (wrongly) as many times as there are
	//   processes using it. However, the extra memory is only in the order of 10MiB and makes for a nice safety margin.
	overhead.Add(resource.MustParse(VirtLauncherMonitorOverhead))
	overhead.Add(resource.MustParse(VirtLauncherOverhead))
	overhead.Add(resource.MustParse(VirtlogdOverhead))
	overhead.Add(resource.MustParse(VirtqemudOverhead))
	overhead.Add(resource.MustParse(QemuOverhead))

	// Add CPU table overhead (8 MiB per vCPU and 8 MiB per IO thread)
	// overhead per vcpu in MiB
	coresMemory := resource.MustParse("8Mi")
	var vcpus int64
	if domain.CPU != nil {
		cpuTopology := &v1.CPUTopology{
			Cores:   domain.CPU.Cores,
			Sockets: domain.CPU.Sockets,
			Threads: domain.CPU.Threads,
		}
		vcpus = cpu.GetNumberOfVCPUs(cpuTopology)
	} else {
		// Currently, a default guest CPU topology is set by the API webhook mutator, if not set by a user.
		// However, this wasn't always the case.
		// In case when the guest topology isn't set, take value from resources request or limits.
		resources := vmi.Spec.Domain.Resources
		if cpuLimit, ok := resources.Limits[k8sv1.ResourceCPU]; ok {
			vcpus = cpuLimit.Value()
		} else if cpuRequests, ok := resources.Requests[k8sv1.ResourceCPU]; ok {
			vcpus = cpuRequests.Value()
		}
	}

	// if neither CPU topology nor request or limits provided, set vcpus to 1
	if vcpus < 1 {
		vcpus = 1
	}
	value := coresMemory.Value() * vcpus
	coresMemory = *resource.NewQuantity(value, coresMemory.Format)
	overhead.Add(coresMemory)

	// static overhead for IOThread
	overhead.Add(resource.MustParse("8Mi"))

	// Add video RAM overhead
	if domain.Devices.AutoattachGraphicsDevice == nil || *domain.Devices.AutoattachGraphicsDevice == true {
		overhead.Add(resource.MustParse("32Mi"))
	}

	// When use uefi boot on aarch64 with edk2 package, qemu will create 2 pflash(64Mi each, 128Mi in total)
	// it should be considered for memory overhead
	// Additional information can be found here: https://github.com/qemu/qemu/blob/master/hw/arm/virt.c#L120
	if cpuArch == "arm64" {
		overhead.Add(resource.MustParse("128Mi"))
	}

	// Additional overhead of 1G for VFIO devices. VFIO requires all guest RAM to be locked
	// in addition to MMIO memory space to allow DMA. 1G is often the size of reserved MMIO space on x86 systems.
	// Additial information can be found here: https://www.redhat.com/archives/libvir-list/2015-November/msg00329.html
	if IsVFIOVMI(vmi) {
		overhead.Add(resource.MustParse("1Gi"))
	}

	// DownardMetrics volumes are using emptyDirs backed by memory.
	// the max. disk size is only 256Ki.
	if HasDownwardMetricDisk(vmi) {
		overhead.Add(resource.MustParse("1Mi"))
	}

	addProbeOverheads(vmi, &overhead)

	// Consider memory overhead for SEV guests.
	// Additional information can be found here: https://libvirt.org/kbase/launch_security_sev.html#memory
	if IsSEVVMI(vmi) || IsSEVSNPVMI(vmi) || IsSEVESVMI(vmi) {
		overhead.Add(resource.MustParse("256Mi"))
	}

	// Having a TPM device will spawn a swtpm process
	// In `ps`, swtpm has VSZ of 53808 and RSS of 3496, so 53Mi should do
	if HasDevice(&vmi.Spec) {
		overhead.Add(resource.MustParse("53Mi"))
	}

	if vmi.IsCPUDedicated() || vmi.WantsToHaveQOSGuaranteed() {
		overhead.Add(resource.MustParse("100Mi"))
	}

	// Multiplying the ratio is expected to be the last calculation before returning overhead
	if additionalOverheadRatio != nil && *additionalOverheadRatio != "" {
		ratio, err := strconv.ParseFloat(*additionalOverheadRatio, 64)
		if err != nil {
			// This error should never happen as it's already validated by webhooks
			log.Log.Warningf("cannot add additional overhead to virt infra overhead calculation: %v", err)
			return overhead
		}

		overhead = multiplyMemory(overhead, ratio)
	}

	return overhead
}

func GetConfigFromKubeVirtCR(kvInformer cache.SharedIndexInformer) *v1.KubeVirtConfiguration {
	objects := kvInformer.GetStore().List()
	for _, obj := range objects {
		if kv, ok := obj.(*v1.KubeVirt); ok && kv.DeletionTimestamp == nil {
			return &kv.Spec.Configuration
		}
	}
	return nil
}

type netBindingPluginMemoryCalculator interface {
	Calculate(vmi *v1.VirtualMachineInstance, registeredPlugins map[string]v1.InterfaceBindingPlugin) resource.Quantity
}
type MemoryCalculator struct{}

func (mc MemoryCalculator) Calculate(
	vmi *v1.VirtualMachineInstance,
	registeredPlugins map[string]v1.InterfaceBindingPlugin,
) resource.Quantity {
	return sumPluginsMemoryRequests(
		filterUniquePlugins(vmi.Spec.Domain.Devices.Interfaces, registeredPlugins),
	)
}

func filterUniquePlugins(interfaces []v1.Interface, registeredPlugins map[string]v1.InterfaceBindingPlugin) []v1.InterfaceBindingPlugin {
	var uniquePlugins []v1.InterfaceBindingPlugin

	uniquePluginsSet := map[string]struct{}{}

	for _, iface := range interfaces {
		if iface.Binding == nil {
			continue
		}

		pluginName := iface.Binding.Name
		if _, seen := uniquePluginsSet[pluginName]; seen {
			continue
		}

		plugin, exists := registeredPlugins[pluginName]
		if !exists {
			continue
		}

		uniquePluginsSet[pluginName] = struct{}{}
		uniquePlugins = append(uniquePlugins, plugin)
	}

	return uniquePlugins
}

func sumPluginsMemoryRequests(uniquePlugins []v1.InterfaceBindingPlugin) resource.Quantity {
	result := resource.Quantity{}

	for _, plugin := range uniquePlugins {
		if plugin.ComputeResourceOverhead == nil {
			continue
		}

		requests := plugin.ComputeResourceOverhead.Requests
		if requests == nil {
			continue
		}

		result.Add(requests[k8sv1.ResourceMemory])
	}

	return result
}

// Check if a VMI spec requests a VFIO device
func IsVFIOVMI(vmi *v1.VirtualMachineInstance) bool {

	if IsHostDevVMI(vmi) || IsGPUVMI(vmi) || isSRIOVVmi(vmi) {
		return true
	}
	return false
}

// Check if a VMI spec requests a HostDevice
func IsHostDevVMI(vmi *v1.VirtualMachineInstance) bool {
	if vmi.Spec.Domain.Devices.HostDevices != nil && len(vmi.Spec.Domain.Devices.HostDevices) != 0 {
		return true
	}
	return false
}

// Check if a VMI spec requests GPU
func IsGPUVMI(vmi *v1.VirtualMachineInstance) bool {
	if vmi.Spec.Domain.Devices.GPUs != nil && len(vmi.Spec.Domain.Devices.GPUs) != 0 {
		return true
	}
	return false
}

func isSRIOVVmi(vmi *v1.VirtualMachineInstance) bool {
	for _, iface := range vmi.Spec.Domain.Devices.Interfaces {
		if iface.SRIOV != nil {
			return true
		}
	}
	return false
}

func HasDownwardMetricDisk(vmi *v1.VirtualMachineInstance) bool {
	for _, volume := range vmi.Spec.Volumes {
		if volume.DownwardMetrics != nil {
			return true
		}
	}
	return false
}

func addProbeOverheads(vmi *v1.VirtualMachineInstance, quantity *resource.Quantity) {
	// We need to add this overhead due to potential issues when using exec probes.
	// In certain situations depending on things like node size and kernel versions
	// the exec probe can cause a significant memory overhead that results in the pod getting OOM killed.
	// To prevent this, we add this overhead until we have a better way of doing exec probes.
	// The virtProbeTotalAdditionalOverhead is added for the virt-probe binary we use for probing and
	// only added once, while the virtProbeOverhead is the general memory consumption of virt-probe
	// that we add per added probe.
	virtProbeTotalAdditionalOverhead := resource.MustParse("100Mi")
	virtProbeOverhead := resource.MustParse("10Mi")
	hasLiveness := vmi.Spec.LivenessProbe != nil && vmi.Spec.LivenessProbe.Exec != nil
	hasReadiness := vmi.Spec.ReadinessProbe != nil && vmi.Spec.ReadinessProbe.Exec != nil
	if hasLiveness {
		quantity.Add(virtProbeOverhead)
	}
	if hasReadiness {
		quantity.Add(virtProbeOverhead)
	}
	if hasLiveness || hasReadiness {
		quantity.Add(virtProbeTotalAdditionalOverhead)
	}
}

func IsSEVVMI(vmi *v1.VirtualMachineInstance) bool {
	if vmi.Spec.Domain.LaunchSecurity == nil {
		return false
	}
	return vmi.Spec.Domain.LaunchSecurity.SEV != nil || vmi.Spec.Domain.LaunchSecurity.SNP != nil
}

// Check if a VMI spec requests AMD SEV-SNP
func IsSEVSNPVMI(vmi *v1.VirtualMachineInstance) bool {
	return vmi.Spec.Domain.LaunchSecurity != nil && vmi.Spec.Domain.LaunchSecurity.SNP != nil
}

// Check if VMI spec requests AMD SEV-ES
func IsSEVESVMI(vmi *v1.VirtualMachineInstance) bool {
	if vmi.Spec.Domain.LaunchSecurity == nil ||
		vmi.Spec.Domain.LaunchSecurity.SEV == nil ||
		vmi.Spec.Domain.LaunchSecurity.SEV.Policy == nil ||
		vmi.Spec.Domain.LaunchSecurity.SEV.Policy.EncryptedState == nil {
		return false
	}
	return *vmi.Spec.Domain.LaunchSecurity.SEV.Policy.EncryptedState
}

func HasDevice(vmiSpec *v1.VirtualMachineInstanceSpec) bool {
	return vmiSpec.Domain.Devices.TPM != nil &&
		(vmiSpec.Domain.Devices.TPM.Enabled == nil || *vmiSpec.Domain.Devices.TPM.Enabled)
}

func multiplyMemory(mem resource.Quantity, multiplication float64) resource.Quantity {
	overheadAddition := float64(mem.ScaledValue(resource.Kilo)) * (multiplication - 1.0)
	additionalOverhead := resource.NewScaledQuantity(int64(overheadAddition), resource.Kilo)

	mem.Add(*additionalOverhead)
	return mem
}
