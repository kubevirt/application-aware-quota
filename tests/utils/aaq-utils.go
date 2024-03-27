package utils

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
