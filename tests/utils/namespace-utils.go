package utils

import (
	"context"
	"fmt"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AddLabelToNamespace adds a label to the specified namespace
func AddLabelToNamespace(clientset *kubernetes.Clientset, namespace, key, value string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v12.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Add the label to the namespace
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[key] = value

	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Update(context.TODO(), ns, v12.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating namespace %s: %v", namespace, err)
	}

	return nil
}

// RemoveLabelFromNamespace removes a label from the specified namespace
func RemoveLabelFromNamespace(clientset *kubernetes.Clientset, namespace, key string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v12.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Remove the label from the namespace
	if _, ok := ns.Labels[key]; ok {
		delete(ns.Labels, key)
	}

	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Update(context.TODO(), ns, v12.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating namespace %s: %v", namespace, err)
	}

	return nil
}
