package tests

import (
	"context"
	"encoding/json"
	"fmt"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources"
	aaqclientset "kubevirt.io/applications-aware-quota/pkg/generated/aaq/clientset/versioned"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/applications-aware-quota/tests/framework"
	"kubevirt.io/applications-aware-quota/tests/utils"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	watch "k8s.io/apimachinery/pkg/watch"
	quota "k8s.io/apiserver/pkg/quota/v1"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/client-go/util/retry"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/pointer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// how long to wait for a Applications Resource Quota update to occur
	resourceQuotaTimeout = 2 * time.Minute
	podName              = "pfpod"
)

var classGold = "gold"
var extendedResourceName = "example.com/dongle"

var _ = Describe("ApplicationsResourceQuota", func() {
	f := framework.NewFramework("resourcequota")

	It("should create a ApplicationsResourceQuota and ensure its status is promptly calculated.", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a service.", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Service")
		service := newTestServiceForQuota("test-service", v1.ServiceTypeClusterIP, false)
		service, err = f.K8sClient.CoreV1().Services(f.Namespace.Name).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating a NodePort Service")
		nodeport := newTestServiceForQuota("test-service-np", v1.ServiceTypeNodePort, false)
		nodeport, err = f.K8sClient.CoreV1().Services(f.Namespace.Name).Create(ctx, nodeport, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Not allowing a LoadBalancer Service with NodePort to be created that exceeds remaining quota")
		loadbalancer := newTestServiceForQuota("test-service-lb", v1.ServiceTypeLoadBalancer, true)
		_, err = f.K8sClient.CoreV1().Services(f.Namespace.Name).Create(ctx, loadbalancer, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures service creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceServices] = resource.MustParse("2")
		usedResources[v1.ResourceServicesNodePorts] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting Services")
		err = f.K8sClient.CoreV1().Services(f.Namespace.Name).Delete(ctx, service.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		err = f.K8sClient.CoreV1().Services(f.Namespace.Name).Delete(ctx, nodeport.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourceServices] = resource.MustParse("0")
		usedResources[v1.ResourceServicesNodePorts] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a secret.", func(ctx context.Context) {
		By("Discovering how many secrets are in namespace by default")
		found, unchanged := 0, 0
		// On contended servers the service account controller can slow down, leading to the count changing during a run.
		// Wait up to 5s for the count to stabilize, assuming that updates come at a consistent rate, and are not held indefinitely.
		err := wait.PollWithContext(ctx, 1*time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
			secrets, err := f.K8sClient.CoreV1().Secrets(f.Namespace.Name).List(ctx, metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			if len(secrets.Items) == found {
				// loop until the number of secrets has stabilized for 5 seconds
				unchanged++
				return unchanged > 4, nil
			}
			unchanged = 0
			found = len(secrets.Items)
			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())
		defaultSecrets := fmt.Sprintf("%d", found)
		hardSecrets := fmt.Sprintf("%d", found+1)

		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		ApplicationsResourceQuota.Spec.Hard[v1.ResourceSecrets] = resource.MustParse(hardSecrets)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceSecrets] = resource.MustParse(defaultSecrets)
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Secret")
		secret := newTestSecretForQuota("test-secret")
		secret, err = f.K8sClient.CoreV1().Secrets(f.Namespace.Name).Create(ctx, secret, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures secret creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceSecrets] = resource.MustParse(hardSecrets)
		// we expect there to be two secrets because each namespace will receive
		// a service account token secret by default
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a secret")
		err = f.K8sClient.CoreV1().Secrets(f.Namespace.Name).Delete(ctx, secret.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourceSecrets] = resource.MustParse(defaultSecrets)
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a pod.", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Pod that fits quota")
		podName := "test-pod"
		requests := v1.ResourceList{}
		limits := v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("252Mi")
		requests[v1.ResourceEphemeralStorage] = resource.MustParse("30Gi")
		requests[v1.ResourceName(extendedResourceName)] = resource.MustParse("2")
		limits[v1.ResourceName(extendedResourceName)] = resource.MustParse("2")
		pod := newTestPodForQuota(podName, requests, limits)
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)
		podToUpdate := pod

		By("Ensuring ApplicationsResourceQuota status captures the pod usage")
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceCPU] = requests[v1.ResourceCPU]
		usedResources[v1.ResourceMemory] = requests[v1.ResourceMemory]
		usedResources[v1.ResourceEphemeralStorage] = requests[v1.ResourceEphemeralStorage]
		usedResources[v1.ResourceName(v1.DefaultResourceRequestsPrefix+extendedResourceName)] = requests[v1.ResourceName(extendedResourceName)]
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Not allowing a pod to be created that exceeds remaining quota")
		requests = v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("600m")
		requests[v1.ResourceMemory] = resource.MustParse("100Mi")
		pod = newTestPodForQuota("fail-pod", requests, v1.ResourceList{})
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Not allowing a pod to be created that exceeds remaining quota(validation on extended resources)")
		requests = v1.ResourceList{}
		limits = v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("100Mi")
		requests[v1.ResourceEphemeralStorage] = resource.MustParse("30Gi")
		requests[v1.ResourceName(extendedResourceName)] = resource.MustParse("2")
		limits[v1.ResourceName(extendedResourceName)] = resource.MustParse("2")
		pod = newTestPodForQuota("fail-pod-for-extended-resource", requests, limits)
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring a pod cannot update its resource requirements")
		// a pod cannot dynamically update its resource requirements.
		requests = v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("100m")
		requests[v1.ResourceMemory] = resource.MustParse("100Mi")
		requests[v1.ResourceEphemeralStorage] = resource.MustParse("10Gi")
		podToUpdate.Spec.Containers[0].Resources.Requests = requests
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Update(ctx, podToUpdate, metav1.UpdateOptions{})
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring attempts to update pod resource requirements did not change quota usage")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podName, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceCPU] = resource.MustParse("0")
		usedResources[v1.ResourceMemory] = resource.MustParse("0")
		usedResources[v1.ResourceEphemeralStorage] = resource.MustParse("0")
		usedResources[v1.ResourceName(v1.DefaultResourceRequestsPrefix+extendedResourceName)] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a configMap.", func(ctx context.Context) {
		found, unchanged := 0, 0
		// On contended servers the service account controller can slow down, leading to the count changing during a run.
		// Wait up to 15s for the count to stabilize, assuming that updates come at a consistent rate, and are not held indefinitely.
		err := wait.PollWithContext(ctx, 1*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			configmaps, err := f.K8sClient.CoreV1().ConfigMaps(f.Namespace.Name).List(ctx, metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			if len(configmaps.Items) == found {
				// loop until the number of configmaps has stabilized for 15 seconds
				unchanged++
				return unchanged > 15, nil
			}
			unchanged = 0
			found = len(configmaps.Items)
			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())
		defaultConfigMaps := fmt.Sprintf("%d", found)
		hardConfigMaps := fmt.Sprintf("%d", found+1)

		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		ApplicationsResourceQuota.Spec.Hard[v1.ResourceConfigMaps] = resource.MustParse(hardConfigMaps)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceConfigMaps] = resource.MustParse(defaultConfigMaps)
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ConfigMap")
		configMap := newTestConfigMapForQuota("test-configmap")
		configMap, err = f.K8sClient.CoreV1().ConfigMaps(f.Namespace.Name).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures configMap creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceConfigMaps] = resource.MustParse(hardConfigMaps)
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a ConfigMap")
		err = f.K8sClient.CoreV1().ConfigMaps(f.Namespace.Name).Delete(ctx, configMap.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourceConfigMaps] = resource.MustParse(defaultConfigMaps)
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a replication controller.", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceReplicationControllers] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ReplicationController")
		replicationController := newTestReplicationControllerForQuota("test-rc", "nginx", 0)
		replicationController, err = f.K8sClient.CoreV1().ReplicationControllers(f.Namespace.Name).Create(ctx, replicationController, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures replication controller creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceReplicationControllers] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a ReplicationController")
		// Without the delete options, the object isn't actually
		// removed until the GC verifies that all children have been
		// detached. ReplicationControllers default to "orphan", which
		// is different from most resources. (Why? To preserve a common
		// workflow from prior to the GC's introduction.)
		err = f.K8sClient.CoreV1().ReplicationControllers(f.Namespace.Name).Delete(ctx, replicationController.Name, metav1.DeleteOptions{
			PropagationPolicy: func() *metav1.DeletionPropagation {
				p := metav1.DeletePropagationBackground
				return &p
			}(),
		})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourceReplicationControllers] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a replica set.", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceName("count/replicasets.apps")] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ReplicaSet")
		replicaSet := newTestReplicaSetForQuota("test-rs", "nginx", 0)
		replicaSet, err = f.K8sClient.AppsV1().ReplicaSets(f.Namespace.Name).Create(ctx, replicaSet, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures replicaset creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceName("count/replicasets.apps")] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a ReplicaSet")
		err = f.K8sClient.AppsV1().ReplicaSets(f.Namespace.Name).Delete(ctx, replicaSet.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourceName("count/replicasets.apps")] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a persistent volume claim", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a PersistentVolumeClaim")
		pvc := newTestPersistentVolumeClaimForQuota("test-claim")
		pvc, err = f.K8sClient.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures persistent volume claim creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("1Gi")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a PersistentVolumeClaim")
		err = f.K8sClient.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a persistent volume claim with a storage class", func(ctx context.Context) {
		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("0")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourcePersistentVolumeClaims)] = resource.MustParse("0")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourceRequestsStorage)] = resource.MustParse("0")

		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a PersistentVolumeClaim with storage class")
		pvc := newTestPersistentVolumeClaimForQuota("test-claim")
		pvc.Spec.StorageClassName = &classGold
		pvc, err = f.K8sClient.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures persistent volume claim creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("1Gi")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourcePersistentVolumeClaims)] = resource.MustParse("1")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourceRequestsStorage)] = resource.MustParse("1Gi")

		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting a PersistentVolumeClaim")
		err = f.K8sClient.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released usage")
		usedResources[v1.ResourcePersistentVolumeClaims] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsStorage] = resource.MustParse("0")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourcePersistentVolumeClaims)] = resource.MustParse("0")
		usedResources[core.V1ResourceByStorageClass(classGold, v1.ResourceRequestsStorage)] = resource.MustParse("0")

		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should create a ApplicationsResourceQuota and capture the life of a custom resource(the applicationsResourceQuota crd itself).", func(ctx context.Context) {
		testcrd := extv1.CustomResourceDefinition{}
		_ = k8syaml.NewYAMLToJSONDecoder(strings.NewReader(resources.AAQCRDs["applicationsresourcequota"])).Decode(&testcrd)
		countResourceName := "count/" + testcrd.Spec.Names.Plural + "." + testcrd.Spec.Group
		quotaName := "quota-for-" + testcrd.Spec.Names.Plural
		_, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, &v1alpha1.ApplicationsResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: quotaName},
			Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{
					v1.ResourceName(countResourceName): resource.MustParse("1"),
				},
			},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = updateApplicationsResourceQuotaUntilUsageAppears(ctx, f.AaqClient, f.Namespace.Name, quotaName, v1.ResourceName(countResourceName))
		Expect(err).ToNot(HaveOccurred())
		err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(f.Namespace.Name).Delete(ctx, quotaName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Counting existing ApplicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota")
		quotaName = "test-quota"
		ApplicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		ApplicationsResourceQuota.Spec.Hard[v1.ResourceName(countResourceName)] = resource.MustParse("1")
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated since the managed quota should be created")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		usedResources[v1.ResourceName(countResourceName)] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status captures custom resource creation")
		usedResources = v1.ResourceList{}
		usedResources[v1.ResourceName(countResourceName)] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a second instance of the resource")
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, &v1alpha1.ApplicationsResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: quotaName + "2"},
			Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{
					v1.ResourceName(countResourceName): resource.MustParse("10"),
				},
			},
			},
		})
		// since we only give one quota, this creation should fail.
		Expect(err).To(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota with terminating scopes.", func(ctx context.Context) {
		By("Creating a ApplicationsResourceQuota with terminating scope")
		quotaTerminatingName := "quota-terminating"
		resourceQuotaTerminating, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestApplicationsResourceQuotaWithScope(quotaTerminatingName, v1.ResourceQuotaScopeTerminating))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota with not terminating scope")
		quotaNotTerminatingName := "quota-not-terminating"
		resourceQuotaNotTerminating, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestApplicationsResourceQuotaWithScope(quotaNotTerminatingName, v1.ResourceQuotaScopeNotTerminating))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a long running pod")
		podName := "test-pod"
		requests := v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("200Mi")
		limits := v1.ResourceList{}
		limits[v1.ResourceCPU] = resource.MustParse("1")
		limits[v1.ResourceMemory] = resource.MustParse("400Mi")
		pod := newTestPodForQuota(podName, requests, limits)
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with not terminating scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsCPU] = requests[v1.ResourceCPU]
		usedResources[v1.ResourceRequestsMemory] = requests[v1.ResourceMemory]
		usedResources[v1.ResourceLimitsCPU] = limits[v1.ResourceCPU]
		usedResources[v1.ResourceLimitsMemory] = limits[v1.ResourceMemory]
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with terminating scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podName, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a terminating pod")
		podName = "terminating-pod"
		pod = newTestPodForQuota(podName, requests, limits)
		activeDeadlineSeconds := int64(3600)
		pod.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with terminating scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsCPU] = requests[v1.ResourceCPU]
		usedResources[v1.ResourceRequestsMemory] = requests[v1.ResourceMemory]
		usedResources[v1.ResourceLimitsCPU] = limits[v1.ResourceCPU]
		usedResources[v1.ResourceLimitsMemory] = limits[v1.ResourceMemory]
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with not terminating scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podName, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota with best effort scope.", func(ctx context.Context) {
		By("Creating a ApplicationsResourceQuota with best effort scope")
		resourceQuotaBestEffort, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestApplicationsResourceQuotaWithScope("quota-besteffort", v1.ResourceQuotaScopeBestEffort))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota with not best effort scope")
		resourceQuotaNotBestEffort, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestApplicationsResourceQuotaWithScope("quota-not-besteffort", v1.ResourceQuotaScopeNotBestEffort))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a best-effort pod")
		pod := newTestPodForQuota(podName, v1.ResourceList{}, v1.ResourceList{})
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with best effort scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with not best effort ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a not best-effort pod")
		requests := v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("200Mi")
		limits := v1.ResourceList{}
		limits[v1.ResourceCPU] = resource.MustParse("1")
		limits[v1.ResourceMemory] = resource.MustParse("400Mi")
		pod = newTestPodForQuota("burstable-pod", requests, limits)
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with not best effort scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with best effort scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should be able to update and delete ApplicationsResourceQuota.", func(ctx context.Context) {
		ns := f.Namespace.Name

		By("Creating a ApplicationsResourceQuota")
		quotaName := "test-quota"
		arq := &v1alpha1.ApplicationsResourceQuota{
			Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{},
			},
			},
		}
		arq.ObjectMeta.Name = quotaName
		arq.Spec.Hard[v1.ResourceCPU] = resource.MustParse("1")
		arq.Spec.Hard[v1.ResourceMemory] = resource.MustParse("500Mi")
		arq, err := createApplicationsResourceQuota(ctx, f.AaqClient, ns, arq)
		Expect(err).ToNot(HaveOccurred())

		By("Getting a ApplicationsResourceQuota")
		resourceQuotaResult, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, quotaName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceCPU, resource.MustParse("1")))
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceMemory, resource.MustParse("500Mi")))

		By("Updating a ApplicationsResourceQuota")
		arq.Spec.Hard[v1.ResourceCPU] = resource.MustParse("2")
		arq.Spec.Hard[v1.ResourceMemory] = resource.MustParse("1Gi")
		resourceQuotaResult, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Update(ctx, arq, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceCPU, resource.MustParse("2")))
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceMemory, resource.MustParse("1Gi")))

		By("Verifying a ApplicationsResourceQuota was modified")
		resourceQuotaResult, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, quotaName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceCPU, resource.MustParse("2")))
		Expect(resourceQuotaResult.Spec.Hard).To(HaveKeyWithValue(v1.ResourceMemory, resource.MustParse("1Gi")))

		By("Deleting a ApplicationsResourceQuota")
		err = deleteApplicationsResourceQuota(ctx, f.AaqClient, ns, quotaName)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the deleted ApplicationsResourceQuota")
		_, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, quotaName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("Expected `not found` error, got: %v", err))
	})

	It("should manage the lifecycle of a ApplicationsResourceQuota", func(ctx context.Context) {
		ns := f.Namespace.Name

		rqName := "e2e-quota-" + utilrand.String(5)
		label := map[string]string{"e2e-rq-label": rqName}
		labelSelector := labels.SelectorFromSet(label).String()

		By("Creating a ApplicationsResourceQuota")
		arq := &v1alpha1.ApplicationsResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:   rqName,
				Labels: label,
			},
			Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{},
			},
			},
		}
		arq.Spec.Hard[v1.ResourceCPU] = resource.MustParse("1")
		arq.Spec.Hard[v1.ResourceMemory] = resource.MustParse("500Mi")
		_, err := createApplicationsResourceQuota(ctx, f.AaqClient, ns, arq)
		Expect(err).ToNot(HaveOccurred())

		By("Getting a ApplicationsResourceQuota")
		resourceQuotaResult, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, rqName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resourceQuotaResult.Spec.Hard[v1.ResourceCPU]).To(Equal(resource.MustParse("1")))
		Expect(resourceQuotaResult.Spec.Hard[v1.ResourceMemory]).To(Equal(resource.MustParse("500Mi")))

		By("Listing all ApplicationsResourceQuota with LabelSelector")
		rq, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas("").List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		Expect(err).ToNot(HaveOccurred())
		Expect(rq.Items).To(HaveLen(1), "Failed to find ResourceQuotes %v", rqName)

		By("Patching the ApplicationsResourceQuota")
		payload := "{\"metadata\":{\"labels\":{\"" + rqName + "\":\"patched\"}},\"spec\":{\"hard\":{ \"memory\":\"750Mi\"}}}"
		patchedArq, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Patch(ctx, rqName, types.MergePatchType, []byte(payload), metav1.PatchOptions{})
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to patch ApplicationsResourceQuota %s in namespace %s", rqName, ns))

		Expect(patchedArq.Labels[rqName]).To(Equal("patched"), "Failed to find the label for this ApplicationsResourceQuota. Current labels: %v", patchedArq.Labels)
		Expect(*patchedArq.Spec.Hard.Memory()).To(Equal(resource.MustParse("750Mi")), "Hard memory value for ApplicationsResourceQuota %q is %s not 750Mi.", patchedArq.ObjectMeta.Name, patchedArq.Spec.Hard.Memory().String())

		By("Deleting a Collection of ApplicationsResourceQuota")
		err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labelSelector})
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the deleted ApplicationsResourceQuota")
		_, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, rqName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("Expected `not found` error, got: %v", err))
	})

	It("should apply changes to a ApplicationsResourceQuota status", func(ctx context.Context) {
		ns := f.Namespace.Name
		arqClient := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(ns)
		rqName := "e2e-rq-status-" + utilrand.String(5)
		label := map[string]string{"e2e-rq-label": rqName}
		labelSelector := labels.SelectorFromSet(label).String()

		w := &cache.ListWatch{
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelector
				return arqClient.Watch(ctx, options)
			},
		}

		rqList, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas("").List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Creating ApplicationsResourceQuota %q", rqName))
		ApplicationsResourceQuota := &v1alpha1.ApplicationsResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:   rqName,
				Labels: label,
			},
			Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("500m"),
					v1.ResourceMemory: resource.MustParse("500Mi"),
				},
			},
			},
		}
		_, err = createApplicationsResourceQuota(ctx, f.AaqClient, ns, ApplicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		initialResourceQuota, err := arqClient.Get(ctx, rqName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(*initialResourceQuota.Spec.Hard.Cpu()).To(Equal(resource.MustParse("500m")), "Hard cpu value for ApplicationsResourceQuota %q is %s not 500m.", initialResourceQuota.Name, initialResourceQuota.Spec.Hard.Cpu().String())
		fmt.Printf("Applications Resource Quota %q reports spec: hard cpu limit of %s", rqName, initialResourceQuota.Spec.Hard.Cpu())
		Expect(*initialResourceQuota.Spec.Hard.Memory()).To(Equal(resource.MustParse("500Mi")), "Hard memory value for ApplicationsResourceQuota %q is %s not 500Mi.", initialResourceQuota.Name, initialResourceQuota.Spec.Hard.Memory().String())
		fmt.Printf("Applications Resource Quota %q reports spec: hard memory limit of %s", rqName, initialResourceQuota.Spec.Hard.Memory())

		By(fmt.Sprintf("Updating ApplicationsResourceQuota %q /status", rqName))
		var updatedResourceQuota *v1alpha1.ApplicationsResourceQuota
		hardLimits := quota.Add(v1.ResourceList{}, initialResourceQuota.Spec.Hard)

		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			updateStatus, err := arqClient.Get(ctx, rqName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			updateStatus.Status = v1alpha1.ApplicationsResourceQuotaStatus{ResourceQuotaStatus: v1.ResourceQuotaStatus{
				Hard: hardLimits,
			},
			}
			updatedResourceQuota, err = arqClient.UpdateStatus(ctx, updateStatus, metav1.UpdateOptions{})
			return err
		})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Confirm /status for %q ApplicationsResourceQuota via watch", rqName))
		ctxUntil, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		_, err = watchtools.Until(ctxUntil, rqList.ResourceVersion, w, func(event watch.Event) (bool, error) {
			if rq, ok := event.Object.(*v1alpha1.ApplicationsResourceQuota); ok {
				found := rq.Name == updatedResourceQuota.Name &&
					rq.Namespace == ns &&
					apiequality.Semantic.DeepEqual(rq.Status.Hard, updatedResourceQuota.Spec.Hard)
				if !found {
					fmt.Printf("observed ApplicationsResourceQuota %q in namespace %q with hard status: %#v", rq.Name, rq.Namespace, rq.Status.Hard)
					return false, nil
				}
				fmt.Printf("Found ApplicationsResourceQuota %q in namespace %q with hard status: %#v", rq.Name, rq.Namespace, rq.Status.Hard)
				return found, nil
			}
			fmt.Printf("Observed event: %+v", event.Object)
			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("ApplicationsResourceQuota %q /status was updated", updatedResourceQuota.Name)

		// Sync ApplicationsResourceQuota list before patching /status
		rqList, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas("").List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		Expect(err).ToNot(HaveOccurred())

		By("Patching hard spec values for cpu & memory")
		xResourceQuota, err := arqClient.Patch(ctx, updatedResourceQuota.Name, types.MergePatchType,
			[]byte(`{"spec":{"hard":{"cpu":"1","memory":"1Gi"}}}`),
			metav1.PatchOptions{})
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("Applications Resource Quota %q reports spec: hard cpu limit of %s", rqName, xResourceQuota.Spec.Hard.Cpu())
		fmt.Printf("Applications Resource Quota %q reports spec: hard memory limit of %s", rqName, xResourceQuota.Spec.Hard.Memory())

		By(fmt.Sprintf("Patching %q /status", rqName))
		hardLimits = quota.Add(v1.ResourceList{}, xResourceQuota.Spec.Hard)

		rqStatusJSON, err := json.Marshal(hardLimits)
		Expect(err).ToNot(HaveOccurred())
		patchedResourceQuota, err := arqClient.Patch(ctx, rqName, types.MergePatchType,
			[]byte(`{"status": {"hard": `+string(rqStatusJSON)+`}}`),
			metav1.PatchOptions{}, "status")
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Confirm /status for %q ApplicationsResourceQuota via watch", rqName))
		ctxUntil, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		_, err = watchtools.Until(ctxUntil, rqList.ResourceVersion, w, func(event watch.Event) (bool, error) {
			if rq, ok := event.Object.(*v1alpha1.ApplicationsResourceQuota); ok {
				found := rq.Name == patchedResourceQuota.Name &&
					rq.Namespace == ns &&
					apiequality.Semantic.DeepEqual(rq.Status.Hard, patchedResourceQuota.Spec.Hard)
				if !found {
					fmt.Printf("observed ApplicationsResourceQuota %q in namespace %q with hard status: %#v", rq.Name, rq.Namespace, rq.Status.Hard)
					return false, nil
				}
				return found, nil
			}
			fmt.Printf("Observed event: %+v", event.Object)
			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("ApplicationsResourceQuota %q /status was patched", patchedResourceQuota.Name)

		By(fmt.Sprintf("Get %q /status", rqName))

		rqResource := schema.GroupVersionResource{Group: "aaq.kubevirt.io", Version: "v1alpha1", Resource: "applicationsresourcequotas"}
		unstruct, err := f.DynamicClient.Resource(rqResource).Namespace(ns).Get(ctx, ApplicationsResourceQuota.Name, metav1.GetOptions{}, "status")
		Expect(err).ToNot(HaveOccurred())

		rq, err := unstructuredToApplicationsResourceQuota(unstruct)
		Expect(err).ToNot(HaveOccurred())

		Expect(*rq.Status.Hard.Cpu()).To(Equal(resource.MustParse("1")), "Hard cpu value for ApplicationsResourceQuota %q is %s not 1.", rq.Name, rq.Status.Hard.Cpu().String())
		fmt.Printf("Resourcequota %q reports status: hard cpu of %s", rqName, rq.Status.Hard.Cpu())
		Expect(*rq.Status.Hard.Memory()).To(Equal(resource.MustParse("1Gi")), "Hard memory value for ApplicationsResourceQuota %q is %s not 1Gi.", rq.Name, rq.Status.Hard.Memory().String())
		fmt.Printf("Resourcequota %q reports status: hard memory of %s", rqName, rq.Status.Hard.Memory())

		// Sync ApplicationsResourceQuota list before repatching /status
		rqList, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas("").List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Repatching %q /status before checking Spec is unchanged", rqName))
		newHardLimits := v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("2Gi"),
		}
		rqStatusJSON, err = json.Marshal(newHardLimits)
		Expect(err).ToNot(HaveOccurred())

		repatchedResourceQuota, err := arqClient.Patch(ctx, rqName, types.MergePatchType,
			[]byte(`{"status": {"hard": `+string(rqStatusJSON)+`}}`),
			metav1.PatchOptions{}, "status")
		Expect(err).ToNot(HaveOccurred())

		Expect(*repatchedResourceQuota.Status.Hard.Cpu()).To(Equal(resource.MustParse("2")), "Hard cpu value for ApplicationsResourceQuota %q is %s not 2.", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Cpu().String())
		fmt.Printf("Resourcequota %q reports status: hard cpu of %s", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Cpu())
		Expect(*repatchedResourceQuota.Status.Hard.Memory()).To(Equal(resource.MustParse("2Gi")), "Hard memory value for ApplicationsResourceQuota %q is %s not 2Gi.", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Memory().String())
		fmt.Printf("Resourcequota %q reports status: hard memory of %s", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Memory())

		_, err = watchtools.Until(ctxUntil, rqList.ResourceVersion, w, func(event watch.Event) (bool, error) {
			if rq, ok := event.Object.(*v1alpha1.ApplicationsResourceQuota); ok {
				found := rq.Name == patchedResourceQuota.Name &&
					rq.Namespace == ns && rq.Status.Hard != nil &&
					*rq.Status.Hard.Cpu() == resource.MustParse("2") &&
					*rq.Status.Hard.Memory() == resource.MustParse("2Gi")
				return found, nil
			}
			fmt.Printf("Observed event: %+v", event.Object)
			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())

		err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			resourceQuotaResult, err := arqClient.Get(ctx, rqName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if apiequality.Semantic.DeepEqual(resourceQuotaResult.Spec.Hard.Cpu(), resourceQuotaResult.Status.Hard.Cpu()) {
				Expect(*resourceQuotaResult.Status.Hard.Cpu()).To(Equal(resource.MustParse("1")), "Hard cpu value for ApplicationsResourceQuota %q is %s not 1.", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Cpu().String())
				Expect(*resourceQuotaResult.Status.Hard.Memory()).To(Equal(resource.MustParse("1Gi")), "Hard memory value for ApplicationsResourceQuota %q is %s not 1Gi.", repatchedResourceQuota.Name, repatchedResourceQuota.Status.Hard.Memory().String())
				fmt.Printf("ApplicationsResourceQuota %q Spec was unchanged and /status reset", resourceQuotaResult.Name)

				return true, nil
			}

			return false, nil
		})
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ApplicationsResourceQuota", func() {
	f := framework.NewFramework("scope-selectors")
	It("should verify ApplicationsResourceQuota with best effort scope using scope-selectors.", func(ctx context.Context) {
		By("Creating a ApplicationsResourceQuota with best effort scope")
		resourceQuotaBestEffort, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeSelector("quota-besteffort", v1.ResourceQuotaScopeBestEffort))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota with not best effort scope")
		resourceQuotaNotBestEffort, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeSelector("quota-not-besteffort", v1.ResourceQuotaScopeNotBestEffort))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a best-effort pod")
		pod := newTestPodForQuota(podName, v1.ResourceList{}, v1.ResourceList{})
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with best effort scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with not best effort ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a not best-effort pod")
		requests := v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("200Mi")
		limits := v1.ResourceList{}
		limits[v1.ResourceCPU] = resource.MustParse("1")
		limits[v1.ResourceMemory] = resource.MustParse("400Mi")
		pod = newTestPodForQuota("burstable-pod", requests, limits)
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with not best effort scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with best effort scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotBestEffort.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})
	It("should verify ApplicationsResourceQuota with terminating scopes through scope selectors.", func(ctx context.Context) {
		By("Creating a ApplicationsResourceQuota with terminating scope")
		quotaTerminatingName := "quota-terminating"
		resourceQuotaTerminating, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeSelector(quotaTerminatingName, v1.ResourceQuotaScopeTerminating))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a ApplicationsResourceQuota with not terminating scope")
		quotaNotTerminatingName := "quota-not-terminating"
		resourceQuotaNotTerminating, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeSelector(quotaNotTerminatingName, v1.ResourceQuotaScopeNotTerminating))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a long running pod")
		podName := "test-pod"
		requests := v1.ResourceList{}
		requests[v1.ResourceCPU] = resource.MustParse("500m")
		requests[v1.ResourceMemory] = resource.MustParse("200Mi")
		limits := v1.ResourceList{}
		limits[v1.ResourceCPU] = resource.MustParse("1")
		limits[v1.ResourceMemory] = resource.MustParse("400Mi")
		pod := newTestPodForQuota(podName, requests, limits)
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with not terminating scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsCPU] = requests[v1.ResourceCPU]
		usedResources[v1.ResourceRequestsMemory] = requests[v1.ResourceMemory]
		usedResources[v1.ResourceLimitsCPU] = limits[v1.ResourceCPU]
		usedResources[v1.ResourceLimitsMemory] = limits[v1.ResourceMemory]
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with terminating scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podName, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a terminating pod")
		podName = "terminating-pod"
		pod = newTestPodForQuota(podName, requests, limits)
		activeDeadlineSeconds := int64(3600)
		pod.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with terminating scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsCPU] = requests[v1.ResourceCPU]
		usedResources[v1.ResourceRequestsMemory] = requests[v1.ResourceMemory]
		usedResources[v1.ResourceLimitsCPU] = limits[v1.ResourceCPU]
		usedResources[v1.ResourceLimitsMemory] = limits[v1.ResourceMemory]
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota with not terminating scope ignored the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaNotTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podName, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaTerminating.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ApplicationsResourceQuota", func() {
	f := framework.NewFramework("make-room-for-pods")
	It("should be able to create a ApplicationsResourceQuota and gated a pod, increase quota and release pod", func(ctx context.Context) {
		By("Counting existing applicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a applicationsResourceQuota")
		quotaName := "test-quota"
		applicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		applicationsResourceQuota, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, applicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Pod that doesn't fits quota")
		podName := "test-pod"
		requests := v1.ResourceList{}
		limits := v1.ResourceList{}
		requests[v1.ResourceMemory] = resource.MustParse("600Mi") //quota has only 500Mi
		pod := newTestPodForQuota(podName, requests, limits)
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Update arq to fit pod")
		Eventually(func() error {
			currApplicationsResourceQuota, err := f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(f.Namespace.Name).Get(ctx, applicationsResourceQuota.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			currApplicationsResourceQuota.Spec.Hard[v1.ResourceMemory] = resource.MustParse("600Mi")
			_, err = f.AaqClient.AaqV1alpha1().ApplicationsResourceQuotas(f.Namespace.Name).Update(ctx, currApplicationsResourceQuota, metav1.UpdateOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).Should(BeNil())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)
	})

	It("should be able to create a ApplicationsResourceQuota and gated pod, delete another pod release first pod", func(ctx context.Context) {
		By("Counting existing applicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a applicationsResourceQuota")
		quotaName := "test-quota"
		applicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		applicationsResourceQuota, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, applicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Pod that fits quota")
		podName := "test-pod1"
		requests := v1.ResourceList{}
		limits := v1.ResourceList{}
		requests[v1.ResourceMemory] = resource.MustParse("300Mi") //quota has only 500Mi
		pod := newTestPodForQuota(podName, requests, limits)
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Creating a Pod that doesn't fits quota")
		podName2 := "test-pod2"
		pod2 := newTestPodForQuota(podName2, requests, limits)
		pod2, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod2.Name)

		By("Make room by deleting first pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod2.Name)
	})

	It("should be able to create a ApplicationsResourceQuota and a pod with different gate and release the pod by removing the other gate", func(ctx context.Context) {
		By("Counting existing applicationsResourceQuota")
		c, err := countApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a applicationsResourceQuota")
		quotaName := "test-quota"
		applicationsResourceQuota := newTestApplicationsResourceQuota(quotaName)
		applicationsResourceQuota, err = createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, applicationsResourceQuota)
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourceQuotas] = resource.MustParse(strconv.Itoa(c + 1))
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quotaName, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a Pod that fits quota")
		podName := "test-pod1"
		requests := v1.ResourceList{}
		limits := v1.ResourceList{}
		requests[v1.ResourceMemory] = resource.MustParse("300Mi") //quota has only 500Mi
		pod := newTestPodForQuota(podName, requests, limits)
		pod.Spec.SchedulingGates = []v1.PodSchedulingGate{{"testGate"}}
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Remove test scheduling gate to release pod")
		Eventually(func() error {
			pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			pod.Spec.SchedulingGates = []v1.PodSchedulingGate{{util.AAQGate}}
			_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Update(ctx, pod, metav1.UpdateOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).Should(BeNil())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)
	})
})

var _ = Describe("ApplicationsResourceQuota", func() {
	f := framework.NewFramework("resourcequota-priorityclass")

	It("should verify ApplicationsResourceQuota's priority class scope (quota set to pod count: 1) against a pod with same priority class.", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass1"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpIn, []string{"pclass1"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class")
		podName := "testpod-pclass1"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass1")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's priority class scope (quota set to pod count: 1) against 2 pods with same priority class.", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass2"}, Value: int32(1000)}, metav1.CreateOptions{})

		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpIn, []string{"pclass2"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating first pod with priority class should pass")
		podName := "testpod-pclass2-1"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass2")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating 2nd pod with priority class should fail")
		podName2 := "testpod-pclass2-2"
		pod2 := newTestPodForQuotaWithPriority(f, podName2, v1.ResourceList{}, v1.ResourceList{}, "pclass2")
		_, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsGated(f.K8sClient, f.Namespace.Name, pod2.Name)
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod2.Name, *metav1.NewDeleteOptions(0))

		By("Deleting first pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's priority class scope (quota set to pod count: 1) against 2 pods with different priority class.", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass3"}, Value: int32(1000)}, metav1.CreateOptions{})

		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpIn, []string{"pclass4"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class with pclass3")
		podName := "testpod-pclass3-1"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass3")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope remains same")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a 2nd pod with priority class pclass3")
		podName2 := "testpod-pclass2-2"
		pod2 := newTestPodForQuotaWithPriority(f, podName2, v1.ResourceList{}, v1.ResourceList{}, "pclass3")
		pod2, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope remains same")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting both pods")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod2.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's multiple priority class scope (quota set to pod count: 2) against 2 pods with same priority classes.", func(ctx context.Context) {
		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass5"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		_, err = f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass6"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("2")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpIn, []string{"pclass5", "pclass6"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class pclass5")
		podName := "testpod-pclass5"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass5")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class is updated with the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating 2nd pod with priority class pclass6")
		podName2 := "testpod-pclass6"
		pod2 := newTestPodForQuotaWithPriority(f, podName2, v1.ResourceList{}, v1.ResourceList{}, "pclass6")
		pod2, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope is updated with the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("2")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting both pods")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod2.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's priority class scope (quota set to pod count: 1) against a pod with different priority class (ScopeSelectorOpNotIn).", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass7"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpNotIn, []string{"pclass7"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class pclass7")
		podName := "testpod-pclass7"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass7")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class is not used")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's priority class scope (quota set to pod count: 1) against a pod with different priority class (ScopeSelectorOpExists).", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass8"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpExists, []string{}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class pclass8")
		podName := "testpod-pclass8"
		pod := newTestPodForQuotaWithPriority(f, podName, v1.ResourceList{}, v1.ResourceList{}, "pclass8")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class is updated with the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should verify ApplicationsResourceQuota's priority class scope (cpu, memory quota set) against a pod with same priority class.", func(ctx context.Context) {

		_, err := f.K8sClient.SchedulingV1().PriorityClasses().Create(ctx, &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "pclass9"}, Value: int32(1000)}, metav1.CreateOptions{})
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), fmt.Sprintf("unexpected error while creating priority class: %v", err))
		}

		hard := v1.ResourceList{}
		hard[v1.ResourcePods] = resource.MustParse("1")
		hard[v1.ResourceRequestsCPU] = resource.MustParse("1")
		hard[v1.ResourceRequestsMemory] = resource.MustParse("1Gi")
		hard[v1.ResourceLimitsCPU] = resource.MustParse("3")
		hard[v1.ResourceLimitsMemory] = resource.MustParse("3Gi")

		By("Creating a ApplicationsResourceQuota with priority class scope")
		resourceQuotaPriorityClass, err := createApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeForPriorityClass("quota-priorityclass", hard, v1.ScopeSelectorOpIn, []string{"pclass9"}))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		usedResources := v1.ResourceList{}
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0Gi")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0Gi")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod with priority class")
		podName := "testpod-pclass9"
		request := v1.ResourceList{}
		request[v1.ResourceCPU] = resource.MustParse("1")
		request[v1.ResourceMemory] = resource.MustParse("1Gi")
		limit := v1.ResourceList{}
		limit[v1.ResourceCPU] = resource.MustParse("2")
		limit[v1.ResourceMemory] = resource.MustParse("2Gi")

		pod := newTestPodForQuotaWithPriority(f, podName, request, limit, "pclass9")
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota with priority class scope captures the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("1")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("1Gi")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("2")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("2Gi")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pod")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		usedResources[v1.ResourcePods] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceRequestsMemory] = resource.MustParse("0Gi")
		usedResources[v1.ResourceLimitsCPU] = resource.MustParse("0")
		usedResources[v1.ResourceLimitsMemory] = resource.MustParse("0Gi")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, resourceQuotaPriorityClass.Name, usedResources)
		Expect(err).ToNot(HaveOccurred())
	})

})

var _ = Describe("ApplicationsResourceQuota", func() {
	f := framework.NewFramework("cross-namespace-pod-affinity")

	It("should verify ApplicationsResourceQuota with cross namespace pod affinity scope using scope-selectors.", func(ctx context.Context) {
		By("Creating a ApplicationsResourceQuota with cross namespace pod affinity scope")
		quota, err := createApplicationsResourceQuota(
			ctx, f.AaqClient, f.Namespace.Name, newTestResourceQuotaWithScopeSelector("quota-cross-namespace-pod-affinity", v1.ResourceQuotaScopeCrossNamespacePodAffinity))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring ApplicationsResourceQuota status is calculated")
		wantUsedResources := v1.ResourceList{v1.ResourcePods: resource.MustParse("0")}
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quota.Name, wantUsedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod that does not use cross namespace affinity")
		pod := newTestPodWithAffinityForQuota(f, "no-cross-namespace-affinity", &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
					TopologyKey: "region",
				}}}})
		pod, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Creating a pod that uses namespaces field")
		podWithNamespaces := newTestPodWithAffinityForQuota(f, "with-namespaces", &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
					TopologyKey: "region",
					Namespaces:  []string{"ns1"},
				}}}})
		podWithNamespaces, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, podWithNamespaces, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota captures podWithNamespaces usage")
		wantUsedResources[v1.ResourcePods] = resource.MustParse("1")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quota.Name, wantUsedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a pod that uses namespaceSelector field")
		podWithNamespaceSelector := newTestPodWithAffinityForQuota(f, "with-namespace-selector", &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
					TopologyKey: "region",
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"ads"},
							},
						},
					}}}}})
		podWithNamespaceSelector, err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Create(ctx, podWithNamespaceSelector, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		verifyPodIsNotGated(f.K8sClient, f.Namespace.Name, pod.Name)

		By("Ensuring Applications Resource Quota captures podWithNamespaceSelector usage")
		wantUsedResources[v1.ResourcePods] = resource.MustParse("2")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quota.Name, wantUsedResources)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the pods")
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podWithNamespaces.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())
		err = f.K8sClient.CoreV1().Pods(f.Namespace.Name).Delete(ctx, podWithNamespaceSelector.Name, *metav1.NewDeleteOptions(0))
		Expect(err).ToNot(HaveOccurred())

		By("Ensuring Applications Resource Quota status released the pod usage")
		wantUsedResources[v1.ResourcePods] = resource.MustParse("0")
		err = waitForApplicationsResourceQuota(ctx, f.AaqClient, f.Namespace.Name, quota.Name, wantUsedResources)
		Expect(err).ToNot(HaveOccurred())
	})
})

// newTestResourceQuotaWithScopeSelector returns a quota that enforces default constraints for testing with scopeSelectors
func newTestResourceQuotaWithScopeSelector(name string, scope v1.ResourceQuotaScope) *v1alpha1.ApplicationsResourceQuota {
	hard := v1.ResourceList{}
	hard[v1.ResourcePods] = resource.MustParse("5")
	switch scope {
	case v1.ResourceQuotaScopeTerminating, v1.ResourceQuotaScopeNotTerminating:
		hard[v1.ResourceRequestsCPU] = resource.MustParse("1")
		hard[v1.ResourceRequestsMemory] = resource.MustParse("500Mi")
		hard[v1.ResourceLimitsCPU] = resource.MustParse("2")
		hard[v1.ResourceLimitsMemory] = resource.MustParse("1Gi")
	}
	return &v1alpha1.ApplicationsResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{Hard: hard,
			ScopeSelector: &v1.ScopeSelector{
				MatchExpressions: []v1.ScopedResourceSelectorRequirement{
					{
						ScopeName: scope,
						Operator:  v1.ScopeSelectorOpExists},
				},
			},
		},
		},
	}
}

// newTestApplicationsResourceQuotaWithScope returns a quota that enforces default constraints for testing with scopes
func newTestApplicationsResourceQuotaWithScope(name string, scope v1.ResourceQuotaScope) *v1alpha1.ApplicationsResourceQuota {
	hard := v1.ResourceList{}
	hard[v1.ResourcePods] = resource.MustParse("5")
	switch scope {
	case v1.ResourceQuotaScopeTerminating, v1.ResourceQuotaScopeNotTerminating:
		hard[v1.ResourceRequestsCPU] = resource.MustParse("1")
		hard[v1.ResourceRequestsMemory] = resource.MustParse("500Mi")
		hard[v1.ResourceLimitsCPU] = resource.MustParse("2")
		hard[v1.ResourceLimitsMemory] = resource.MustParse("1Gi")
	}
	return &v1alpha1.ApplicationsResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{Hard: hard, Scopes: []v1.ResourceQuotaScope{scope}}},
	}
}

// newTestResourceQuotaWithScopeForPriorityClass returns a quota
// that enforces default constraints for testing with ResourceQuotaScopePriorityClass scope
func newTestResourceQuotaWithScopeForPriorityClass(name string, hard v1.ResourceList, op v1.ScopeSelectorOperator, values []string) *v1alpha1.ApplicationsResourceQuota {
	return &v1alpha1.ApplicationsResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ApplicationsResourceQuotaSpec{v1.ResourceQuotaSpec{Hard: hard,
			ScopeSelector: &v1.ScopeSelector{
				MatchExpressions: []v1.ScopedResourceSelectorRequirement{
					{
						ScopeName: v1.ResourceQuotaScopePriorityClass,
						Operator:  op,
						Values:    values,
					},
				},
			},
		},
		},
	}
}

// newTestApplicationsResourceQuota returns a quota that enforces default constraints for testing
func newTestApplicationsResourceQuota(name string) *v1alpha1.ApplicationsResourceQuota {
	hard := v1.ResourceList{}
	hard[v1.ResourcePods] = resource.MustParse("5")
	hard[v1.ResourceServices] = resource.MustParse("10")
	hard[v1.ResourceServicesNodePorts] = resource.MustParse("1")
	hard[v1.ResourceServicesLoadBalancers] = resource.MustParse("1")
	hard[v1.ResourceReplicationControllers] = resource.MustParse("10")
	hard[v1.ResourceQuotas] = resource.MustParse("2")
	hard[v1.ResourceCPU] = resource.MustParse("1")
	hard[v1.ResourceMemory] = resource.MustParse("500Mi")
	hard[v1.ResourceConfigMaps] = resource.MustParse("10")
	hard[v1.ResourceSecrets] = resource.MustParse("10")
	hard[v1.ResourcePersistentVolumeClaims] = resource.MustParse("10")
	hard[v1.ResourceRequestsStorage] = resource.MustParse("10Gi")
	hard[v1.ResourceEphemeralStorage] = resource.MustParse("50Gi")
	hard[core.V1ResourceByStorageClass(classGold, v1.ResourcePersistentVolumeClaims)] = resource.MustParse("10")
	hard[core.V1ResourceByStorageClass(classGold, v1.ResourceRequestsStorage)] = resource.MustParse("10Gi")
	// test quota on discovered resource type
	hard[v1.ResourceName("count/replicasets.apps")] = resource.MustParse("5")
	// test quota on extended resource
	hard[v1.ResourceName(v1.DefaultResourceRequestsPrefix+extendedResourceName)] = resource.MustParse("3")
	return &v1alpha1.ApplicationsResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.ApplicationsResourceQuotaSpec{ResourceQuotaSpec: v1.ResourceQuotaSpec{Hard: hard}},
	}
}

// newTestPodForQuota returns a pod that has the specified requests and limits
func newTestPodForQuota(name string, requests v1.ResourceList, limits v1.ResourceList) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			// prevent disruption to other test workloads in parallel test runs by ensuring the quota
			// test pods don't get scheduled onto a node
			NodeSelector: map[string]string{
				"x-test.k8s.io/unsatisfiable": "not-schedulable",
			},
			Containers: []v1.Container{
				{
					Name:  "pause",
					Image: "busybox",
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
		},
	}
}

// newTestPodForQuotaWithPriority returns a pod that has the specified requests, limits and priority class
func newTestPodForQuotaWithPriority(f *framework.Framework, name string, requests v1.ResourceList, limits v1.ResourceList, pclass string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			// prevent disruption to other test workloads in parallel test runs by ensuring the quota
			// test pods don't get scheduled onto a node
			NodeSelector: map[string]string{
				"x-test.k8s.io/unsatisfiable": "not-schedulable",
			},
			Containers: []v1.Container{
				{
					Name:  "pause",
					Image: "busybox",
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			PriorityClassName: pclass,
		},
	}
}

// newTestPodForQuota returns a pod that has the specified requests and limits
func newTestPodWithAffinityForQuota(f *framework.Framework, name string, affinity *v1.Affinity) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PodSpec{
			// prevent disruption to other test workloads in parallel test runs by ensuring the quota
			// test pods don't get scheduled onto a node
			NodeSelector: map[string]string{
				"x-test.k8s.io/unsatisfiable": "not-schedulable",
			},
			Affinity: affinity,
			Containers: []v1.Container{
				{
					Name:      "pause",
					Image:     "busybox",
					Resources: v1.ResourceRequirements{},
				},
			},
		},
	}
}

// newTestPersistentVolumeClaimForQuota returns a simple persistent volume claim
func newTestPersistentVolumeClaimForQuota(name string) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
				v1.ReadOnlyMany,
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse("1Gi"),
				},
			},
		},
	}
}

// newTestReplicationControllerForQuota returns a simple replication controller
func newTestReplicationControllerForQuota(name, image string, replicas int32) *v1.ReplicationController {
	return &v1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ReplicationControllerSpec{
			Replicas: pointer.Int32(replicas),
			Selector: map[string]string{
				"name": name,
			},
			Template: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"name": name},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  name,
							Image: image,
						},
					},
				},
			},
		},
	}
}

// newTestReplicaSetForQuota returns a simple replica set
func newTestReplicaSetForQuota(name, image string, replicas int32) *appsv1.ReplicaSet {
	zero := int64(0)
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": name}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"name": name},
				},
				Spec: v1.PodSpec{
					TerminationGracePeriodSeconds: &zero,
					Containers: []v1.Container{
						{
							Name:  name,
							Image: image,
						},
					},
				},
			},
		},
	}
}

// newTestServiceForQuota returns a simple service
func newTestServiceForQuota(name string, serviceType v1.ServiceType, allocateLoadBalancerNodePorts bool) *v1.Service {
	var allocateNPs *bool
	// Only set allocateLoadBalancerNodePorts when service type is LB
	if serviceType == v1.ServiceTypeLoadBalancer {
		allocateNPs = &allocateLoadBalancerNodePorts
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type: serviceType,
			Ports: []v1.ServicePort{{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 80,
				},
			}},
			AllocateLoadBalancerNodePorts: allocateNPs,
		},
	}
}

func newTestConfigMapForQuota(name string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string]string{
			"a": "b",
		},
	}
}

func newTestSecretForQuota(name string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: map[string][]byte{
			"data-1": []byte("value-1\n"),
			"data-2": []byte("value-2\n"),
			"data-3": []byte("value-3\n"),
		},
	}
}

// createApplicationsResourceQuota in the specified namespace
func createApplicationsResourceQuota(ctx context.Context, c *aaqclientset.Clientset, namespace string, applicationsResourceQuota *v1alpha1.ApplicationsResourceQuota) (*v1alpha1.ApplicationsResourceQuota, error) {
	return c.AaqV1alpha1().ApplicationsResourceQuotas(namespace).Create(ctx, applicationsResourceQuota, metav1.CreateOptions{})
}

// deleteApplicationsResourceQuota with the specified name
func deleteApplicationsResourceQuota(ctx context.Context, c *aaqclientset.Clientset, namespace, name string) error {
	return c.AaqV1alpha1().ApplicationsResourceQuotas(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// countApplicationsResourceQuota counts the number of ApplicationsResourceQuota in the specified namespace
// On contended servers the service account controller can slow down, leading to the count changing during a run.
// Wait up to 5s for the count to stabilize, assuming that updates come at a consistent rate, and are not held indefinitely.
func countApplicationsResourceQuota(ctx context.Context, c *aaqclientset.Clientset, namespace string) (int, error) {
	found, unchanged := 0, 0
	return found, wait.PollWithContext(ctx, 1*time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
		arqs, err := c.AaqV1alpha1().ApplicationsResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		if len(arqs.Items) == found {
			// loop until the number of Applications Resource Quotas has stabilized for 5 seconds
			unchanged++
			return unchanged > 4, nil
		}
		unchanged = 0
		found = len(arqs.Items)
		return false, nil
	})
}

// wait for Applications Resource Quota status to show the expected used resources value
func waitForApplicationsResourceQuota(ctx context.Context, c *aaqclientset.Clientset, ns, quotaName string, used v1.ResourceList) error {
	return wait.PollWithContext(ctx, 2*time.Second, resourceQuotaTimeout, func(ctx context.Context) (bool, error) {
		ApplicationsResourceQuota, err := c.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, quotaName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		// used may not yet be calculated
		if ApplicationsResourceQuota.Status.Used == nil {
			return false, nil
		}
		// verify that the quota shows the expected used resource values
		for k, v := range used {
			if actualValue, found := ApplicationsResourceQuota.Status.Used[k]; !found || (actualValue.Cmp(v) != 0) {
				fmt.Printf(fmt.Sprintf("resource %s, expected %s, actual %s\n", k, v.String(), actualValue.String()))
				return false, nil
			}
		}
		return true, nil
	})
}

// updateApplicationsResourceQuotaUntilUsageAppears updates the Applications Resource Quota object until the usage is populated
// for the specific resource name.
func updateApplicationsResourceQuotaUntilUsageAppears(ctx context.Context, c *aaqclientset.Clientset, ns, quotaName string, resourceName v1.ResourceName) error {
	return wait.PollWithContext(ctx, 2*time.Second, resourceQuotaTimeout, func(ctx context.Context) (bool, error) {
		arq, err := c.AaqV1alpha1().ApplicationsResourceQuotas(ns).Get(ctx, quotaName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		// verify that the quota shows the expected used resource values
		_, ok := arq.Status.Used[resourceName]
		if ok {
			return true, nil
		}

		current := arq.Spec.Hard[resourceName]
		current.Add(resource.MustParse("1"))
		arq.Spec.Hard[resourceName] = current
		_, err = c.AaqV1alpha1().ApplicationsResourceQuotas(ns).Update(ctx, arq, metav1.UpdateOptions{})
		// ignoring conflicts since someone else may already updated it.
		if apierrors.IsConflict(err) {
			return false, nil
		}
		return false, err
	})
}

func unstructuredToApplicationsResourceQuota(obj *unstructured.Unstructured) (*v1alpha1.ApplicationsResourceQuota, error) {
	json, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}
	rq := &v1alpha1.ApplicationsResourceQuota{}
	err = runtime.DecodeInto(clientscheme.Codecs.LegacyCodec(v1.SchemeGroupVersion), json, rq)

	return rq, err
}

func verifyPodIsGated(c kubernetes.Interface, podNamespace, podName string) {
	// Retry every 1 seconds
	EventuallyWithOffset(1, func() bool {
		// Consistently retry every 1 second for ~10 seconds
		success := true
		for i := 0; i < 10; i++ {
			if !utils.PodSchedulingGated(c, podNamespace, podName) {
				success = false
				break
			}

			time.Sleep(1 * time.Second)
		}
		return success
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
}

func verifyPodIsNotGated(c kubernetes.Interface, podNamespace, podName string) {
	// Retry every 1 seconds
	EventuallyWithOffset(1, func() bool {
		// Consistently retry every 1 second for ~10 seconds
		success := true
		for i := 0; i < 10; i++ {
			if utils.PodSchedulingGated(c, podNamespace, podName) {
				success = false
				break
			}
			time.Sleep(1 * time.Second)
		}
		return success
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
}
