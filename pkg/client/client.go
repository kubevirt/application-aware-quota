/*
 * This file is part of the KubeVirt project
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
 * Copyright 2018 Red Hat, Inc.
 *
 */

package util

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE

/*
 ATTENTION: Rerun code generators when interface signatures are modified.
*/

import (
	"context"
	"io"
	"net"
	"time"


	secv1 "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	autov1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	v1 "kubevirt.io/api/core/v1"
	kubevirtclient "kubevirt.io/applications-aware-quota/pkg/generated/kubevirt/clientset/versioned"
	generatedclient "kubevirt.io/applications-aware-quota/pkg/generated/aaq/clientset/versioned"
	"kubevirt.io/client-go/version"
)

type AAQClient interface {
	RestClient() *rest.RESTClient
	kubernetes.Interface

	GeneratedKubeVirtClient() generatedclient.Interface
	CdiClient() cdiclient.Interface
	Config() *rest.Config
}

type aaq struct {
	master                  string
	kubeconfig              string
	restClient              *rest.RESTClient
	config                  *rest.Config
	generatedKubeVirtClient *generatedclient.Clientset
	kubevirtClient               *kuebvirtclient.Clientset
	dynamicClient           dynamic.Interface
	*kubernetes.Clientset
}

func (k aaq) Config() *rest.Config {
	return k.config
}

func (k aaq) KubeVirtClient() kubevirtclient.Interface {
	return k.kubevirtClient
}


func (k aaq) RestClient() *rest.RESTClient {
	return k.restClient
}

func (k aaq) GeneratedAAQClient() generatedclient.Interface {
	return k.generatedAAQClient
}

func (k aaq) VirtualMachinePool(namespace string) poolv1.VirtualMachinePoolInterface {
	return k.generatedKubeVirtClient.PoolV1alpha1().VirtualMachinePools(namespace)
}


func (k aaq) DynamicClient() dynamic.Interface {
	return k.dynamicClient
}

type StreamOptions struct {
	In  io.Reader
	Out io.Writer
}

type StreamInterface interface {
	Stream(options StreamOptions) error
	AsConn() net.Conn
}

type VirtualMachineInstanceInterface interface {
	Get(ctx context.Context, name string, options *metav1.GetOptions) (*v1.VirtualMachineInstance, error)
	List(ctx context.Context, opts *metav1.ListOptions) (*v1.VirtualMachineInstanceList, error)
	Create(ctx context.Context, instance *v1.VirtualMachineInstance) (*v1.VirtualMachineInstance, error)
	Update(ctx context.Context, instance *v1.VirtualMachineInstance) (*v1.VirtualMachineInstance, error)
	Delete(ctx context.Context, name string, options *metav1.DeleteOptions) error
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, patchOptions *metav1.PatchOptions, subresources ...string) (result *v1.VirtualMachineInstance, err error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	SerialConsole(name string, options *SerialConsoleOptions) (StreamInterface, error)
	USBRedir(vmiName string) (StreamInterface, error)
	VNC(name string) (StreamInterface, error)
	Screenshot(ctx context.Context, name string, options *v1.ScreenshotOptions) ([]byte, error)
	PortForward(name string, port int, protocol string) (StreamInterface, error)
	Pause(ctx context.Context, name string, pauseOptions *v1.PauseOptions) error
	Unpause(ctx context.Context, name string, unpauseOptions *v1.UnpauseOptions) error
	Freeze(ctx context.Context, name string, unfreezeTimeout time.Duration) error
	Unfreeze(ctx context.Context, name string) error
	SoftReboot(ctx context.Context, name string) error
	GuestOsInfo(ctx context.Context, name string) (v1.VirtualMachineInstanceGuestAgentInfo, error)
	UserList(ctx context.Context, name string) (v1.VirtualMachineInstanceGuestOSUserList, error)
	FilesystemList(ctx context.Context, name string) (v1.VirtualMachineInstanceFileSystemList, error)
	AddVolume(ctx context.Context, name string, addVolumeOptions *v1.AddVolumeOptions) error
	RemoveVolume(ctx context.Context, name string, removeVolumeOptions *v1.RemoveVolumeOptions) error
	VSOCK(name string, options *v1.VSOCKOptions) (StreamInterface, error)
	SEVFetchCertChain(name string) (v1.SEVPlatformInfo, error)
	SEVQueryLaunchMeasurement(name string) (v1.SEVMeasurementInfo, error)
	SEVSetupSession(name string, sevSessionOptions *v1.SEVSessionOptions) error
	SEVInjectLaunchSecret(name string, sevSecretOptions *v1.SEVSecretOptions) error
}

type ReplicaSetInterface interface {
	Get(name string, options metav1.GetOptions) (*v1.VirtualMachineInstanceReplicaSet, error)
	List(opts metav1.ListOptions) (*v1.VirtualMachineInstanceReplicaSetList, error)
	Create(*v1.VirtualMachineInstanceReplicaSet) (*v1.VirtualMachineInstanceReplicaSet, error)
	Update(*v1.VirtualMachineInstanceReplicaSet) (*v1.VirtualMachineInstanceReplicaSet, error)
	Delete(name string, options *metav1.DeleteOptions) error
	GetScale(replicaSetName string, options metav1.GetOptions) (*autov1.Scale, error)
	UpdateScale(replicaSetName string, scale *autov1.Scale) (*autov1.Scale, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.VirtualMachineInstanceReplicaSet, err error)
	UpdateStatus(*v1.VirtualMachineInstanceReplicaSet) (*v1.VirtualMachineInstanceReplicaSet, error)
	PatchStatus(name string, pt types.PatchType, data []byte) (result *v1.VirtualMachineInstanceReplicaSet, err error)
}

type VirtualMachineInstancePresetInterface interface {
	Get(name string, options metav1.GetOptions) (*v1.VirtualMachineInstancePreset, error)
	List(opts metav1.ListOptions) (*v1.VirtualMachineInstancePresetList, error)
	Create(*v1.VirtualMachineInstancePreset) (*v1.VirtualMachineInstancePreset, error)
	Update(*v1.VirtualMachineInstancePreset) (*v1.VirtualMachineInstancePreset, error)
	Delete(name string, options *metav1.DeleteOptions) error
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.VirtualMachineInstancePreset, err error)
}

// VirtualMachineInterface provides convenience methods to work with
// virtual machines inside the cluster
type VirtualMachineInterface interface {
	Get(ctx context.Context, name string, options *metav1.GetOptions) (*v1.VirtualMachine, error)
	GetWithExpandedSpec(ctx context.Context, name string) (*v1.VirtualMachine, error)
	List(ctx context.Context, opts *metav1.ListOptions) (*v1.VirtualMachineList, error)
	Create(ctx context.Context, vm *v1.VirtualMachine) (*v1.VirtualMachine, error)
	Update(ctx context.Context, vm *v1.VirtualMachine) (*v1.VirtualMachine, error)
	Delete(ctx context.Context, name string, options *metav1.DeleteOptions) error
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, patchOptions *metav1.PatchOptions, subresources ...string) (result *v1.VirtualMachine, err error)
	UpdateStatus(ctx context.Context, vm *v1.VirtualMachine) (*v1.VirtualMachine, error)
	PatchStatus(ctx context.Context, name string, pt types.PatchType, data []byte, patchOptions *metav1.PatchOptions) (result *v1.VirtualMachine, err error)
	Restart(ctx context.Context, name string, restartOptions *v1.RestartOptions) error
	ForceRestart(ctx context.Context, name string, restartOptions *v1.RestartOptions) error
	Start(ctx context.Context, name string, startOptions *v1.StartOptions) error
	Stop(ctx context.Context, name string, stopOptions *v1.StopOptions) error
	ForceStop(ctx context.Context, name string, stopOptions *v1.StopOptions) error
	Migrate(ctx context.Context, name string, migrateOptions *v1.MigrateOptions) error
	AddVolume(ctx context.Context, name string, addVolumeOptions *v1.AddVolumeOptions) error
	RemoveVolume(ctx context.Context, name string, removeVolumeOptions *v1.RemoveVolumeOptions) error
	PortForward(name string, port int, protocol string) (StreamInterface, error)
	MemoryDump(ctx context.Context, name string, memoryDumpRequest *v1.VirtualMachineMemoryDumpRequest) error
	RemoveMemoryDump(ctx context.Context, name string) error
}

type VirtualMachineInstanceMigrationInterface interface {
	Get(name string, options *metav1.GetOptions) (*v1.VirtualMachineInstanceMigration, error)
	List(opts *metav1.ListOptions) (*v1.VirtualMachineInstanceMigrationList, error)
	Create(migration *v1.VirtualMachineInstanceMigration, options *metav1.CreateOptions) (*v1.VirtualMachineInstanceMigration, error)
	Update(*v1.VirtualMachineInstanceMigration) (*v1.VirtualMachineInstanceMigration, error)
	Delete(name string, options *metav1.DeleteOptions) error
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.VirtualMachineInstanceMigration, err error)
	UpdateStatus(*v1.VirtualMachineInstanceMigration) (*v1.VirtualMachineInstanceMigration, error)
	PatchStatus(name string, pt types.PatchType, data []byte) (result *v1.VirtualMachineInstanceMigration, err error)
}

type KubeVirtInterface interface {
	Get(name string, options *metav1.GetOptions) (*v1.KubeVirt, error)
	List(opts *metav1.ListOptions) (*v1.KubeVirtList, error)
	Create(instance *v1.KubeVirt) (*v1.KubeVirt, error)
	Update(*v1.KubeVirt) (*v1.KubeVirt, error)
	Delete(name string, options *metav1.DeleteOptions) error
	Patch(name string, pt types.PatchType, data []byte, patchOptions *metav1.PatchOptions, subresources ...string) (result *v1.KubeVirt, err error)
	UpdateStatus(*v1.KubeVirt) (*v1.KubeVirt, error)
	PatchStatus(name string, pt types.PatchType, data []byte, patchOptions *metav1.PatchOptions) (result *v1.KubeVirt, err error)
}

type ServerVersionInterface interface {
	Get() (*version.Info, error)
}

type ExpandSpecInterface interface {
	ForVirtualMachine(vm *v1.VirtualMachine) (*v1.VirtualMachine, error)
}
