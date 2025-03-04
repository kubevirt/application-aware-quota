# application-aware-quota
Empower users to customize and manage resource quotas per component in Kubernetes, utilizing the scheduling gates feature.

## Motivation
In a multi-tenant cluster where multiple users or teams share a Kubernetes cluster, 
there is a risk that one team might consume more than its fair share of resources. 
Resource quotas are a tool for administrators to manage and mitigate this concern.

However, the native ResourceQuotas architecture was not designed with easy extension in mind.

Native ResourceQuotas in Kubernetes are designed to handle predefined pod resources such as
`requests.cpu`, `limits.cpu`, `requests.memory`, `limits.memory`, and more. Let's define
these values as native accounting resources. AAQ let's you define other resources as well,
let's define them as extended.

Kubernetes Operators often introduce new objects in the form of Custom Resource Definitions,
which, in turn, result in the creation of pods that are considered an "implementation detail" of the
Custom Resource's operations.

Because ResourceQuotas are not extendable they are not suited for more complex scenarios, such as limiting 
resources based on the broader application's context rather than solely on the pod's resource requirements. 
With AAQ, it's possible to not take into account the pod's resource requirements into consideration at all, 
but instead base it on other, possible multiple factors such as different pod(s) fields, CRD fields, and more.

Another issue is that the native ResourceQuotas in Kubernetes are admission-based, which can
cause the resource quota admission controller to become a bottleneck for the API Server during
evaluation and ResourceQuota status update calls.

Furthermore, native ResourceQuotas do not support operating across multiple namespaces.
Instead, multiple separate ResourceQuota objects would need to be created and managed
individually for each namespace.

Additionally, if a quota is set with `requests.cpu`, `limits.cpu`, `requests.memory`, or
`limits.memory` native accounting resources in a namespace, then pods that don't set these 
resources are not allowed to be created. This essentially means that in such namespaces, best 
effort pods and some burstable pods are not allowed. The design decision which caused this 
limitation in Kubernetes is admitted to be a mistake, but is still kept for backward compatibility 
reasons as mentioned in the following
[link](https://github.com/kubernetes/kubernetes/blob/c876b30c2b30c0355045d7548c22b6cd42ab58da/pkg/quota/v1/evaluator/core/pods.go#L89).

## User Stories

**User Story**: Managing VM Billing and Resource Quotas in a Multi-Tenant Cluster with KubeVirt

KubeVirt is an operator that enables the deployment and management of virtual machines alongside
containerized applications.

As an administrator of a multi-tenant cluster running Virtual Machines with 
[KubeVirt](https://github.com/kubevirt/kubevirt?tab=readme-ov-file#introduction, 
I expect tenants to be billed only for the resources they explicitly request in the VM object. 
However, this presents specific challenges with resource quotas:

1. VM Live Migration: VM instances run inside pods. Occasionally, the pod associated with a VM needs to 
be evicted from one node and rescheduled onto another. VMs have a large state that requires careful 
handling during migration. To manage this, KubeVirt creates a new pod to transfer the VM state before 
terminating the old one, which differs from how standard pods are moved. Running both pods simultaneously
temporarily doubles the amount of resources the tenant is accounted for and can impact resource quotas, 
potentially leading to migration issues and upgrade failures.
2. In KubeVirt, each Virtual Machine operates within its own container. This means that the virtualization 
overhead, such as the hypervisor, runs within each VM pod instead of centrally on the host, as in traditional 
virtualization. This overhead is factored into quota calculations. In traditional virtualization platforms, 
service providers typically base VM quotas solely on RAM and CPU threads, since users don't directly request 
resources for this overhead or expect its effects. Moreover, the resource requirements for this overhead can 
change between versions without users being notified.

As a result, deploying KubeVirt in a multi-tenant cluster while restricting hardware consumption 
from different entities is highly challenging. It requires significant resource allocation to 
handle live migrations and manage VM infrastructure overhead. This also couples users to the 
VM implementation, making them dependent on infrastructure overhead that can change unexpectedly.

**User Story**: Creating Pods with lacking resource requests/limits in a multi-tenant cluster

As a user in a multi-tenant Kubernetes cluster, I need to deploy best-effort pods for low-priority workloads,
which should utilize spare cluster resources and have a higher priority to be terminated during shortages.
However, there’s a catch: my namespace has a ResourceQuota enforced by the cluster administrator. 
Currently, this quota might restrict the creation of pods that do not specify `requests.cpu`, `limits.cpu`, 
`requests.memory`, or `limits.memory`. This limitation prevents the deployment of best-effort pods and 
certain burstable pods in my namespace.

**User Story**: Managing Resource Quota Across Multiple Namespaces

As an administrator responsible for a multi-tenant Kubernetes cluster, I need to establish resource 
boundaries for a tenant that uses multiple namespaces. However, the native ResourceQuotas in Kubernetes 
are namespaced, meaning they apply only within individual namespaces. This limitation prevents me from
setting global resource boundaries that span multiple namespaces for a single tenant.

## Solution: Application Aware Resource Quota and Application Aware Cluster Resource Quota

The AAQ Operator uses a different approach compared to the native ResourceQuota. the AAQ Operator
introduces alternative quota implementations in the form of a Custom Resource Definitions
called ApplicationAwareResourceQuota and ApplicationAwareClusterResourceQuota.

### Application Aware Resource Quota
The ApplicationAwareResourceQuota API is compatible with
the native ResourceQuota, meaning it shares the same
[specification and status definitions](https://github.com/kubevirt/application-aware-quota-api/blob/ac6757c09496bfab205d499a41daa6ed8815eab6/pkg/apis/core/v1alpha1/types.go#L46).

Please see [arq-example.yaml](https://github.com/kubevirt/application-aware-quota/blob/main/manifests/examples/arq-example.yaml) for an example manifest.

### Application Aware Cluster Resource Quota
The AAQ Operator includes a Multi-Namespace Quota called ApplicationAwareClusterResourceQuota,
AAQ cluster quota is inspired by the 
[ClusterResourceQuota available in OpenShift distributions](https://docs.openshift.com/container-platform/4.15/applications/quotas/quotas-setting-across-multiple-projects.html).
This brings entire new capabilities to plain ResourceQuotas, which only support a per-namespace restrictions.

This quota is API compatible with ClusterResourceQuota, meaning it shares the same [specification and status definitions](https://github.com/kubevirt/application-aware-quota-api/blob/ac6757c09496bfab205d499a41daa6ed8815eab6/pkg/apis/core/v1alpha1/types.go#L257).

The main differences between the AAQ cluster quota and AAQ quota are:

- unlike AAQ quotas AAQ cluster quotas are not namespaced.
- When creating AAQ cluster quota, you can select multiple namespaces based
  on annotation selection, label selection, or both though`spec.selector.labels` or
  `spec.selector.annotations`. These fields follows the standard Kubernetes selector format.

  **Note**: If both `spec.selector.labels` and`spec.selector.annotations` are set only namespaces
  that match both will be selected

  **Note**: AAQ must be configured to allow AAQ cluster quota management

  Refer to the "Configure AAQ Operator" section for more details. 

Please see [acrq-example.yaml](https://github.com/kubevirt/application-aware-quota/blob/main/manifests/examples/acrq-example.yaml) for an example manifest.

### How it works

These new quotas are controller-based rather than admission-based. Here’s how they work:

1. [Scheduling Gates](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/): a scheduling gates is added to    the created pod via a mutating webhook.
   See the "Configure AAQ Operator" section below for more information
2. Controller Evaluation: a controller evaluates whether
   there is sufficient room for the pod according to the quota.
3. Quotas Update: If there is enough room, the scheduling gate is removed from 
   the pod, and the quotas used status is updated accordingly.

***Warning***: Pods that set `.spec.nodeName` are not allowed to use in any namespace where AAQ Operates.
For more information please see [this discussion](https://github.com/kubernetes/enhancements/issues/3521#issuecomment-2016957473).

non-schedulable resources are supported by both AAQ quota and AAQ cluster 
quota by managing a k8s resource quota behind the scenes.

**Note**: Managed ClusterResourceQuota is only created on OpenShift distributions, as
ClusterResourceQuota is supported only on OpenShift. In non-OpenShift environments, only
pod-level accounting resources are allowed with AAQ cluster quota.

The primary purpose of these managed quotas is to set unified quotas for bot native and extended 
accounting resources, eliminating the need to manage both ResourceQuota and AAQ quota, or
ClusterResourceQuota and AAQ cluster quota separately.

## Pluggable policies for custom counting

To define how you want resources to be counted for your application, you should set a sidecar container 
in the AAQ object, where you configure the AAQ Operator. More details can be found in the "Configure AAQ Operator" section.

To simplify the implementation of the sidecar container, you can use the [libsidecar](https://github.com/kubevirt/application-aware-quota-api/blob/main/libsidecar/sidecar.go) library. 
To implement the sidecar container, you need to implement the following interface:

```go
type SidecarCalculator interface {
    PodUsageFunc(podToEvaluate *corev1.Pod, existingPods []*corev1.Pod) (corev1.ResourceList, bool, error)
}
```

The PodUsageFunc function takes as input the pod that needs to be evaluated and all the other pods in the cluster. 
It returns the resources that should be counted for this pod, a boolean indicating if the pod belongs to an
application policy and an error.

If a pod doesn’t match any policy, the native quota evaluation applies, just like with the standard ResourceQuota.

If a pod matches multiple policies, all matching policies are evaluated, and the results are summed up.
Please see [label-sidecar.go](https://github.com/kubevirt/application-aware-quota/blob/main/example_sidecars/label-sidecar/cmd/label-sidecar.go) for an example sidecar policy.

### Configure AAQ Operator
To configure AAQ Operator, set the following fields of the aaq object:

* `aaq.spec.namespaceSelector` - determines in which namespaces AAQ's scheduling gate will be added to pods at creation time. This field follows the standard Kubernetes selector format.

    ***Important Note***: if a namespace selector is not being defined, AAQ would target namespaces with the `application-aware-quota/enable-gating` label as default.

    ***Warning***: Pods that set `.spec.nodeName` are not allowed to use in any namespace that AAQ is targeting. Such pods would fail at creation time. Therefore, users shouldn't be targeting system namespaces where `.spec.nodeName` might be set.
    
    For more information please see [this discussion](https://github.com/kubernetes/enhancements/issues/3521#issuecomment-2016957473)


* `aaq.spec.configuration.allowApplicationAwareClusterResourceQuota` - (default = false) - allows creation and management of ClusterAppsResourceQuota.

  ***Note***: this might lead to longer scheduling time.


* `aaq.spec.configuration.SidecarEvaluators` - Slice of standard Kubernetes container objects. Adding containers to the SidecarEvaluators includes a 
sidecar calculator for custom counting. These containers should mount an emptyDir volume under `/var/run/aaq-sockets` called `sockets-dir` and use gRPC to send 
the custom counting evaluation results to the controller. The setup, including gRPC client/server, is simplified with the [libsidecar](https://github.com/kubevirt/application-aware-quota-api/blob/main/libsidecar/sidecar.go) library, 
which handles the heavy lifting.

  We've implemented a [simplistic side-car container](https://github.com/kubevirt/application-aware-quota/tree/main/example_sidecars/label-sidecar) for education purposes. Let's use it as an example.
  In order to use it, AAQ needs to be defined as follows:

  ```yaml
  spec:
    configuration:
      sidecarEvaluators:
      - args:
        - --config
        - double
        image: registry:5000/label-sidecar:latest
        name: sidecar-evaluator
        volumeMounts:
        - mountPath: /var/run/aaq-sockets
          name: sockets-dir
  ```

  For an example of sidecar implementation using the libsidecar library,
  refer to [https://github.com/kubevirt/application-aware-quota/tree/main/example_sidecars/label-sidecar).

### Deploy it on your cluster

Deploying the AAQ controller is straightforward.

  ```bash
  $ export VERSION=$(curl -s https://api.github.com/repos/kubevirt/application-aware-quota/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  $ kubectl create -f https://github.com/kubevirt/application-aware-quota/releases/download/$VERSION/aaq-operator.yaml
  $ kubectl create -f https://github.com/kubevirt/application-aware-quota/releases/download/$VERSION/aaq-cr.yaml
  ```


### Deploy it with our CI system

AAQ includes a self-contained development and test environment.  We use Docker to build, and we provide a simple way to get a test cluster up and running on your laptop. The development tools include a version of kubectl that you can use to communicate with the cluster. A wrapper script to communicate with the cluster can be invoked using ./kubevirtci/cluster-up/kubectl.sh.

```bash
$ mkdir $GOPATH/src/kubevirt.io && cd $GOPATH/src/kubevirt.io
$ git clone https://github.com/kubevirt/application-aware-quota && cd application-aware-quota
$ make cluster-up
$ make cluster-sync
$ ./kubevirtci/cluster-up/kubectl.sh .....
```
For development on external cluster (not provisioned by our AAQ),
check out the [external provider](cluster-sync/external/README.md).
