/*
 * This file is part of the AAQ project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"
)

// ApplicationsResourceQuota defines resources that should be reserved for a VMI migration
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=arq;arqs,categories=all
// +kubebuilder:subresource:status
// +k8s:openapi-gen=true
// +genclient
type ApplicationsResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationsResourceQuotaSpec   `json:"spec" valid:"required"`
	Status ApplicationsResourceQuotaStatus `json:"status,omitempty"`
}

// ApplicationsResourceQuotaSpec is an extension of corev1.ResourceQuotaSpec
type ApplicationsResourceQuotaSpec struct {
	corev1.ResourceQuotaSpec `json:",inline"`
}

// ApplicationsResourceQuotaStatus is an extension of corev1.ResourceQuotaStatus
type ApplicationsResourceQuotaStatus struct {
	corev1.ResourceQuotaStatus `json:",inline"`
}

// ApplicationsResourceQuota List is a list of ApplicationsResourceQuotas
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApplicationsResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=atomic
	Items []ApplicationsResourceQuota `json:"items"`
}

// this has to be here otherwise informer-gen doesn't recognize it
// see https://github.com/kubernetes/code-generator/issues/59
// +genclient:nonNamespaced

// AAQ is the AAQ Operator CRD
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=aaq;aaqs,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
type AAQ struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AAQSpec `json:"spec"`
	// +optional
	Status AAQStatus `json:"status"`
}

// CertConfig contains the tunables for TLS certificates
type CertConfig struct {
	// The requested 'duration' (i.e. lifetime) of the Certificate.
	Duration *metav1.Duration `json:"duration,omitempty"`

	// The amount of time before the currently issued certificate's `notAfter`
	// time that we will begin to attempt to renew the certificate.
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`
}

// AAQCertConfig has the CertConfigs for AAQ
type AAQCertConfig struct {
	// CA configuration
	// CA certs are kept in the CA bundle as long as they are valid
	CA *CertConfig `json:"ca,omitempty"`

	// Server configuration
	// Certs are rotated and discarded
	Server *CertConfig `json:"server,omitempty"`
}

// AAQJobQueueConfig
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=aaqjqc;aaqjqcs,categories=all
// +kubebuilder:subresource:status
type AAQJobQueueConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// CA configuration
	// CA certs are kept in the CA bundle as long as they are valid
	Spec AAQJobQueueConfigSpec `json:"spec"`
	// +optional
	Status AAQJobQueueConfigStatus `json:"status"`
}

// AAQJobQueueConfigSpec defines our specification for JobQueueConfigs
type AAQJobQueueConfigSpec struct {
}

// AAQJobQueueConfigStatus defines the status with metadata for current jobs
type AAQJobQueueConfigStatus struct {
	// BuiltInCalculationConfigToApply
	PodsInJobQueue []string `json:"podsInJobQueue,omitempty"`
}

// AAQSpec defines our specification for the AAQ installation
type AAQSpec struct {
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// PullPolicy describes a policy for if/when to pull a container image
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty" valid:"required"`
	// Rules on which nodes AAQ infrastructure pods will be scheduled
	Infra sdkapi.NodePlacement `json:"infra,omitempty"`
	// Restrict on which nodes AAQ workload pods will be scheduled
	Workloads sdkapi.NodePlacement `json:"workload,omitempty"`
	// certificate configuration
	CertConfig *AAQCertConfig `json:"certConfig,omitempty"`
	// PriorityClass of the AAQ control plane
	PriorityClass *AAQPriorityClass `json:"priorityClass,omitempty"`
	// namespaces where pods should be gated before scheduling
	// Default to the empty LabelSelector, which matches everything.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// holds aaq configurations.
	Configuration AAQConfiguration `json:"configuration,omitempty"`
}

// AAQConfiguration holds all AAQ configurations
type AAQConfiguration struct {
	// VmiCalculatorConfiguration Default is VmiPodUsage please look for VmiCalculatorConfiguration type for more information.
	VmiCalculatorConfiguration VmiCalculatorConfiguration `json:"vmiCalculatorConfiguration,omitempty"`
}

type VmiCalcConfigName string

type VmiCalculatorConfiguration struct {
	ConfigName VmiCalcConfigName `json:"configName,omitempty"`
}

const (
	// VmiPodUsage Calculate usage of launcher like any other pod but hide migration additional resources
	VmiPodUsage VmiCalcConfigName = "VmiPodUsage"
	// VirtualResources Calculate memory.request/limits as the vmi's ram size and cpu.request/limits as number of threads of vmi
	VirtualResources VmiCalcConfigName = "VirtualResources"
	// DedicatedVirtualResources Calculate vmi.requests.memory as the vmi's ram size and vmi.requests.cpu as number of threads of vmi
	// in this configuration no memory.request/limits and cpu.request/limits won't be included
	DedicatedVirtualResources VmiCalcConfigName = "DedicatedVirtualResources"
	// in this configuration no memory.request/limits and cpu.request/limits won't be included
	IgnoreVmiCalculator VmiCalcConfigName = "IgnoreVmiCalculator"
	//VirtualResources:
	// ResourcePodsOfVmi Launcher Pods, number.
	ResourcePodsOfVmi corev1.ResourceName = "requests.instances/vmi"
	// Vmi CPUs, Total Threads number(Cores*Sockets*Threads).
	ResourceRequestsVmiCPU corev1.ResourceName = "requests.cpu/vmi"
	// Vmi Memory Ram Size, in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024)
	ResourceRequestsVmiMemory corev1.ResourceName = "requests.memory/vmi"
)

// AAQPriorityClass defines the priority class of the AAQ control plane.
type AAQPriorityClass string

// AAQPhase is the current phase of the AAQ deployment
type AAQPhase string

// AAQStatus defines the status of the installation
type AAQStatus struct {
	sdkapi.Status `json:",inline"`
}

// AAQList provides the needed parameters to do request a list of AAQ from the system
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AAQList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items provides a list of AAQ
	Items []AAQ `json:"items"`
}

// AAQJobQueueConfigList provides the needed parameters to do request a list of AAQJobQueueConfigs from the system
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AAQJobQueueConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items provides a list of AAQ
	Items []AAQJobQueueConfig `json:"items"`
}
