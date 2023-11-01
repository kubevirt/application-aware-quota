package matcher

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ThisPod fetches the latest state of the pod. If the object does not exist, nil is returned.
func ThisPod(pod *v1.Pod, cli *kubernetes.Clientset) func() (*v1.Pod, error) {
	return ThisPodWith(pod.Namespace, pod.Name, cli)
}

// ThisPodWith fetches the latest state of the pod based on namespace and name. If the object does not exist, nil is returned.
func ThisPodWith(namespace string, name string, cli *kubernetes.Clientset) func() (*v1.Pod, error) {
	return func() (p *v1.Pod, err error) {
		p, err = cli.CoreV1().Pods(namespace).Get(context.Background(), name, k8smetav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil, nil
		}
		//Since https://github.com/kubernetes/client-go/issues/861 we manually add the Kind
		p.Kind = "Pod"
		return
	}
}
