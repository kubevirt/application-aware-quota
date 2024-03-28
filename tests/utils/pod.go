package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultPollPeriodFast = 30 * time.Second
	defaultPollPeriod     = 270 * time.Second
	// PodWaitForTime is the time to wait for Pod operations to complete
	PodWaitForTime = defaultPollPeriod
	// PodWaitForTimeFast is the fast time to wait for Pod operations to complete (30 sec)
	PodWaitForTimeFast = defaultPollPeriodFast
	// PodWaitIntervalFast is the fast polling interval (250ms)
	PodWaitIntervalFast = 250 * time.Millisecond

	podCreateTime = defaultPollPeriod
	podDeleteTime = defaultPollPeriod

	//VerifierPodName is the name of the verifier pod.
	VerifierPodName = "verifier"
)

// DeleteVerifierPod deletes the verifier pod
func DeleteVerifierPod(clientSet *kubernetes.Clientset, namespace string) error {
	return DeletePodByName(clientSet, VerifierPodName, namespace, nil)
}

// CreatePod calls the Kubernetes API to create a Pod
func CreatePod(clientSet *kubernetes.Clientset, namespace string, podDef *k8sv1.Pod) (*k8sv1.Pod, error) {
	err := wait.PollImmediate(2*time.Second, podCreateTime, func() (bool, error) {
		var err error
		_, err = clientSet.CoreV1().Pods(namespace).Create(context.TODO(), podDef, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
	return podDef, err
}

// DeletePodByName deletes the pod based on the passed in name from the passed in Namespace
func DeletePodByName(clientSet *kubernetes.Clientset, podName, namespace string, gracePeriod *int64) error {
	_ = clientSet.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{
		GracePeriodSeconds: gracePeriod,
	})
	return wait.PollImmediate(2*time.Second, podDeleteTime, func() (bool, error) {
		_, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

// DeletePodNoGrace deletes the passed in Pod from the passed in Namespace
func DeletePodNoGrace(clientSet *kubernetes.Clientset, pod *k8sv1.Pod, namespace string) error {
	zero := int64(0)
	return DeletePodByName(clientSet, pod.Name, namespace, &zero)
}

// DeletePod deletes the passed in Pod from the passed in Namespace
func DeletePod(clientSet *kubernetes.Clientset, pod *k8sv1.Pod, namespace string) error {
	return DeletePodByName(clientSet, pod.Name, namespace, nil)
}

// FindPodBySuffix finds the first pod which has the passed in suffix. Returns error if multiple pods with the same suffix are found.
func FindPodBySuffix(clientSet *kubernetes.Clientset, namespace, suffix, labelSelector string) (*k8sv1.Pod, error) {
	return findPodByCompFunc(clientSet, namespace, suffix, labelSelector, strings.HasSuffix)
}

// FindPodByPrefix finds the first pod which has the passed in prefix. Returns error if multiple pods with the same prefix are found.
func FindPodByPrefix(clientSet *kubernetes.Clientset, namespace, prefix, labelSelector string) (*k8sv1.Pod, error) {
	return findPodByCompFunc(clientSet, namespace, prefix, labelSelector, strings.HasPrefix)
}

// FindPodBySuffixOnce finds once (no polling) the first pod which has the passed in suffix. Returns error if multiple pods with the same suffix are found.
func FindPodBySuffixOnce(clientSet *kubernetes.Clientset, namespace, suffix, labelSelector string) (*k8sv1.Pod, error) {
	return findPodByCompFuncOnce(clientSet, namespace, suffix, labelSelector, strings.HasSuffix)
}

// FindPodByPrefixOnce finds once (no polling) the first pod which has the passed in prefix. Returns error if multiple pods with the same prefix are found.
func FindPodByPrefixOnce(clientSet *kubernetes.Clientset, namespace, prefix, labelSelector string) (*k8sv1.Pod, error) {
	return findPodByCompFuncOnce(clientSet, namespace, prefix, labelSelector, strings.HasPrefix)
}

func findPodByCompFunc(clientSet *kubernetes.Clientset, namespace, prefix, labelSelector string, compFunc func(string, string) bool) (*k8sv1.Pod, error) {
	var result *k8sv1.Pod
	var err error
	_ = wait.PollImmediate(2*time.Second, podCreateTime, func() (bool, error) {
		result, err = findPodByCompFuncOnce(clientSet, namespace, prefix, labelSelector, compFunc)
		if result != nil {
			return true, err
		}
		// If no result yet, continue polling even if there is an error
		return false, nil
	})
	return result, err
}

func findPodByCompFuncOnce(clientSet *kubernetes.Clientset, namespace, prefix, labelSelector string, compFunc func(string, string) bool) (*k8sv1.Pod, error) {
	var result *k8sv1.Pod
	podList, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	for _, pod := range podList.Items {
		if compFunc(pod.Name, prefix) {
			if result == nil {
				result = pod.DeepCopy()
			}
		}
	}
	if result == nil {
		return nil, errors.NewNotFound(v1.Resource("pod"), prefix)
	}
	return result, nil
}

// WaitTimeoutForPodReady waits for the given pod to be created and ready
func WaitTimeoutForPodReady(clientSet *kubernetes.Clientset, podName, namespace string, timeout time.Duration) error {
	return WaitTimeoutForPodCondition(clientSet, podName, namespace, k8sv1.PodReady, 2*time.Second, timeout)
}

// WaitTimeoutForPodReadyPollPeriod waits for the given pod to be created and ready using the passed in poll period
func WaitTimeoutForPodReadyPollPeriod(clientSet *kubernetes.Clientset, podName, namespace string, pollperiod, timeout time.Duration) error {
	return WaitTimeoutForPodCondition(clientSet, podName, namespace, k8sv1.PodReady, pollperiod, timeout)
}

// WaitTimeoutForPodSucceeded waits for pod to succeed
func WaitTimeoutForPodSucceeded(clientSet *kubernetes.Clientset, podName, namespace string, timeout time.Duration) error {
	return WaitTimeoutForPodStatus(clientSet, podName, namespace, k8sv1.PodSucceeded, timeout)
}

// WaitTimeoutForPodFailed waits for pod to fail
func WaitTimeoutForPodFailed(clientSet *kubernetes.Clientset, podName, namespace string, timeout time.Duration) error {
	return WaitTimeoutForPodStatus(clientSet, podName, namespace, k8sv1.PodFailed, timeout)
}

// WaitTimeoutForPodStatus waits for the given pod to be created and have a expected status
func WaitTimeoutForPodStatus(clientSet *kubernetes.Clientset, podName, namespace string, status k8sv1.PodPhase, timeout time.Duration) error {
	return wait.PollImmediate(2*time.Second, timeout, podStatus(clientSet, podName, namespace, status))
}

// WaitTimeoutForPodCondition waits for the given pod to be created and have an expected condition
func WaitTimeoutForPodCondition(clientSet *kubernetes.Clientset, podName, namespace string, conditionType k8sv1.PodConditionType, pollperiod, timeout time.Duration) error {
	return wait.PollImmediate(pollperiod, timeout, podCondition(clientSet, podName, namespace, conditionType))
}

func podStatus(clientSet *kubernetes.Clientset, podName, namespace string, status k8sv1.PodPhase) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		fmt.Fprintf(ginkgo.GinkgoWriter, "INFO: Checking POD %s phase: %s\n", podName, string(pod.Status.Phase))
		switch pod.Status.Phase {
		case status:
			return true, nil
		}
		return false, nil
	}
}

func podCondition(clientSet *kubernetes.Clientset, podName, namespace string, conditionType k8sv1.PodConditionType) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == conditionType {
				fmt.Fprintf(ginkgo.GinkgoWriter, "INFO: Checking POD %s condition: %s=%s\n", podName, string(cond.Type), string(cond.Status))
				if cond.Status == k8sv1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	}
}

// PodGetNode returns the node on which a given pod is executing
func PodGetNode(clientSet *kubernetes.Clientset, podName, namespace string) (string, error) {
	pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Spec.NodeName, nil
}

// WaitPodDeleted waits fo a pod to no longer exist
// returns whether the pod is deleted along with any error
func WaitPodDeleted(clientSet *kubernetes.Clientset, podName, namespace string, timeout time.Duration) (bool, error) {
	var result bool
	err := wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		_, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				result = true
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	return result, err
}

// IsExpectedNode waits to check if the specified pod is schedule on the specified node
func IsExpectedNode(clientSet *kubernetes.Clientset, nodeName, podName, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(2*time.Second, timeout, isExpectedNode(clientSet, nodeName, podName, namespace))
}

// returns true is the specified pod running on the specified nodeName. Otherwise returns false
func isExpectedNode(clientSet *kubernetes.Clientset, nodeName, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		fmt.Fprintf(ginkgo.GinkgoWriter, "INFO: Checking Node name: %s\n", string(pod.Spec.NodeName))
		if pod.Spec.NodeName == nodeName {
			return true, nil
		}
		return false, nil
	}
}

// GetSchedulableNode return a schedulable node from a nodes list
func GetSchedulableNode(nodes *v1.NodeList) *string {
	for _, node := range nodes.Items {
		if node.Spec.Taints == nil {
			return &node.Name
		}
		schedulableNode := true
		for _, taint := range node.Spec.Taints {
			if taint.Effect == "NoSchedule" {
				schedulableNode = false
				break
			}
		}
		if schedulableNode {
			return &node.Name
		}
	}
	return nil
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []v1.PodCondition, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

// GetPodCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func GetPodCondition(status *v1.PodStatus, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	return GetPodConditionFromList(status.Conditions, conditionType)
}

// PodSchedulingGated returns a condition function that returns true if the given pod
// gets unschedulable status of reason 'SchedulingGated'.
func PodSchedulingGated(c kubernetes.Interface, podNamespace, podName string) bool {
	pod, err := c.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf(fmt.Sprintf("the error is %v", err.Error()))
		// This could be a connection error so we want to retry.
		return false
	}
	_, cond := GetPodCondition(&pod.Status, v1.PodScheduled)

	return cond != nil && cond.Status == v1.ConditionFalse &&
		cond.Reason == v1.PodReasonSchedulingGated && pod.Spec.NodeName == ""
}

// newTestPodForQuota returns a pod that has the specified requests and limits
func NewTestPodForQuota(name string, requests v1.ResourceList, limits v1.ResourceList) *v1.Pod {
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

func VerifyPodIsGated(c kubernetes.Interface, podNamespace, podName string) {
	// Retry every 1 seconds
	EventuallyWithOffset(1, func() bool {
		// Consistently retry every 1 second for ~10 seconds
		success := true
		for i := 0; i < 10; i++ {
			if !PodSchedulingGated(c, podNamespace, podName) {
				success = false
				break
			}

			time.Sleep(1 * time.Second)
		}
		return success
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
}

func VerifyPodIsNotGated(c kubernetes.Interface, podNamespace, podName string) {
	// Retry every 1 seconds
	EventuallyWithOffset(1, func() bool {
		// Consistently retry every 1 second for ~10 seconds
		success := true
		for i := 0; i < 10; i++ {
			if PodSchedulingGated(c, podNamespace, podName) {
				success = false
				break
			}
			time.Sleep(1 * time.Second)
		}
		return success
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
}
