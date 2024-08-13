package utils

import (
	"context"
	"fmt"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/application-aware-quota/pkg/util"
	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/framework"
)

func GetAAQ(f *framework.Framework) (*aaqv1.AAQ, error) {
	aaqs, err := f.AaqClient.AaqV1alpha1().AAQs().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(aaqs.Items) != 1 {
		return nil, fmt.Errorf("should have only single aaq")
	}
	return &aaqs.Items[0], nil
}

func AaqControllerReady(clientset *kubernetes.Clientset, aaqInstallNs string) (bool, error) {
	deployment, err := clientset.AppsV1().Deployments(aaqInstallNs).Get(context.TODO(), util.ControllerPodName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if *deployment.Spec.Replicas != deployment.Status.ReadyReplicas {
		return false, nil
	}
	return true, nil
}

func GetAAQControllerPods(clientset *kubernetes.Clientset, aaqInstallNs string) *corev1.PodList {
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"aaq.kubevirt.io": "aaq-controller"}}
	aaqPods, err := clientset.CoreV1().Pods(aaqInstallNs).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed listing aaq pods")
	ExpectWithOffset(1, aaqPods.Items).ToNot(BeEmpty(), "no aaq pods found")
	return aaqPods
}
