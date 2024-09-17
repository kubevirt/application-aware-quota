# Application Aware Quota
## Motivation
A mechanism to Manage VM billing and ResourceQuotas in a multi-tenant cluster running OpenShift Virtualization.

Native ResourceQuotas in Openshift are designed to handle predefined pod compute resources such as
`requests.cpu`, `limits.cpu`, `requests.memory`, `limits.memory`, and more. 

OpenShift Virtualization allows the deployment and management of virtual machines alongside
containerized applications.

Administrators of multi-tenant clusters running Virtual Machines with OpenShift Virtualization 
might expect tenants to be billed only for the resources explicitly requested in the VM object. 
However, this presents specific challenges with resource quotas:

1. VM Live Migration: VM instances run inside pods. Occasionally, the pod associated with a VM needs to
   be evicted from one node and rescheduled onto another. VMs have a large state that requires careful
   handling during migration. To manage this, a new pod is created to transfer the VM state before
   terminating the old one, which differs from how standard pods are moved. Running both pods simultaneously
   temporarily doubles the amount of resources the tenant is accounted for and can impact resource quotas,
   potentially leading to migration issues and upgrade failures.
2. Each Virtual Machine operates within its own container. This means that the virtualization overhead, 
   runs within each VM pod instead of centrally on the host, as in traditional virtualization platforms.
   This overhead is factored into quota calculations. In traditional virtualization platforms,
   service providers typically base VM quotas solely on RAM and CPU, since users don't directly request
   resources for this overhead or expect its effects. Moreover, the resource requirements for this overhead can
   change between versions without users being notified.

As a result, deploying Virtual Machines in a multi-tenant cluster while restricting hardware consumption
from different entities is highly challenging. It requires significant resource allocation to
handle live migrations and manage VM infrastructure overhead. This also couples users to the
VM implementation, making them dependent on infrastructure overhead that can change unexpectedly.

### Solution: AAQ Operator
By default, AAQ Operator allocates resources for VM-associated pods, appending a `/vmi` suffix to
`requests/limits.cpu` and `requests/limits.memory` compute resources, derived from the VM's RAM size and CPU threads.

For both limits and requests with the `/vmi` suffix, the same values will apply because, unlike pods, virtual
machines are already limited to the virtual memory and virtual CPU that were allocated to them.

### Application Aware Resource Quota
The ApplicationAwareResourceQuota API is compatible with the native ResourceQuota, meaning it shares the same specification and status definitions.

Example manifest:

```yaml
apiVersion: aaq.kubevirt.io/v1alpha1
kind: ApplicationAwareResourceQuota
metadata:
  name: example-resource-quota
spec:
  hard:
    requests.memory: 1Gi
    limits.memory: 1Gi
    requests.cpu/vmi: "1"
    requests.memory/vmi: 1Gi
```

### Application Aware Cluster Resource Quota
This quota is API compatible with ClusterResourceQuota, meaning it shares the same specification and status definitions.

The main differences between the AAQ cluster quota and AAQ quota are:

- unlike AAQ quotas AAQ cluster quotas are not namespaced.
- When creating AAQ cluster quota, you can select multiple namespaces based
  on annotation selection, label selection, or both though`spec.selector.labels` or
  `spec.selector.annotations`. These fields follows the standard selector format.


  **Note**: If both `spec.selector.labels` and`spec.selector.annotations` are set only namespaces
  that match both will be selected

  **Note**: AAQ must be configured to allow AAQ cluster quota management

  Refer to the ["Configure AAQ Operator"](#configure-aaq-operator) section for more details. 

Example manifest:
```yaml
apiVersion: aaq.kubevirt.io/v1alpha1
kind: ApplicationAwareClusterResourceQuota
metadata:
  name: example-resource-quota
spec:
  quota:
    hard:
    requests.memory: 1Gi
    limits.memory: 1Gi
    requests.cpu/vmi: "1"
    requests.memory/vmi: 1Gi
  selector:
    annotations: null
    labels:
      matchLabels:
        kubernetes.io/metadata.name: default
```

### How it works

These new quotas are controller-based rather than admission-based. Here’s how they work:

1. [Scheduling Gates](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/): a scheduling gates is added to    the created pod via a mutating webhook.
   See the "Configure AAQ Operator" section below for more information
2. Controller Evaluation: a controller evaluates whether
   there is sufficient room for the pod according to the quota.
3. Quotas Update: If there is enough room, the scheduling gate is 
   removed from the pod, and the quotas used status is updated accordingly.
   If not, an event is created explaining why the quota doesn't allow the pod's creation.

For non-compute resources, the quota acts like the standard ResourceQuota.

***Warning***: Pods that set `.spec.nodeName` are not allowed to use in any namespace where AAQ Operates.

non-schedulable resources are supported by both AAQ quota and AAQ cluster 
quota by managing a k8s resource quota behind the scenes.

The primary purpose of these managed quotas is to set unified quotas for bot native and extended 
accounting resources, eliminating the need to manage both ResourceQuota and AAQ quota, or
ClusterResourceQuota and AAQ cluster quota separately.

### Configure AAQ Operator
To enable the AAQ feature, set the `hco.spec.featureGates.enableApplicationAwareQuota` field to `true`. 

You can do this from the command line with the following command:
```bash
$ oc patch hco kubevirt-hyperconverged -n openshift-cnv \
 --type json -p '[{"op": "add", "path": "/spec/featureGates/enableApplicationAwareQuota", "value": true}]'
```

#### Configuring AAQ Settings
The applicationAwareConfig object within the HyperConverged resource's spec allows you to customize AAQ behavior. Key fields include:
* `vmiCalcConfigName` -  Defines how resource counting is managed for VM-associated pods. The available options are:
   * `VmiPodUsage` - Counts compute resources for VM-associated pods in the same way as native resource quotas, while excluding migration-related resources.
   * `VirtualResources` - Counts compute resources based on the VM's specifications, using the VM's RAM size for memory and virtual CPUs for processing.
   * `DedicatedVirtualResources` (default) - Similar to VirtualResources, but separates resource tracking for VM-associated pods by adding a `/vmi` suffix to CPU and memory resource names (e.g., `requests.cpu/vmi`, `limits.memory/vmi`), distinguishing them from other pods.
* `namespaceSelector` - determines in which namespaces AAQ's scheduling gate will be added to pods at creation time.
        This field follows the standard selector format.
* `allowApplicationAwareClusterResourceQuota` (default = false) -  Set to true to enable the creation and management of AAQ's cluster quotas.

Example CLI command to configure AAQ:
```bash
$ oc patch hco kubevirt-hyperconverged -n openshift-cnv --type merge -p '{
  "spec": {
    "applicationAwareConfig": {
      "vmiCalcConfigName": "DedicatedVirtualResources",
      "namespaceSelector": {
        "matchLabels": {
          "app": "my-app"
        }
      },
      "allowApplicationAwareClusterResourceQuota": true
    }
  }
}'
```
