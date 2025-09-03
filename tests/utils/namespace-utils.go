package utils

import (
	"context"
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/application-aware-quota/pkg/util/patch"
)

// AddLabelToNamespace adds a label to the specified namespace
func AddLabelToNamespace(clientset *kubernetes.Clientset, namespace, key, value string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Add the label to the namespace
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[key] = value

	labelPatch := patch.New()
	labelPatch.AddOption(
		patch.WithAdd("/metadata/labels", ns.Labels),
	)

	patchBytes, err := labelPatch.GeneratePayload()
	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Patch(context.Background(), ns.Name, types.JSONPatchType, patchBytes, v1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error patching namespace %s: %v", namespace, err)
	}

	return nil
}

// RemoveLabelFromNamespace removes a label from the specified namespace
func RemoveLabelFromNamespace(clientset *kubernetes.Clientset, namespace, key string) error {
	// Get the namespace object
	ns, err := clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting namespace %s: %v", namespace, err)
	}

	// Remove the label from the namespace
	delete(ns.Labels, key)

	labelPatch := patch.New()
	labelPatch.AddOption(
		patch.WithAdd("/metadata/labels", ns.Labels),
	)

	patchBytes, err := labelPatch.GeneratePayload()
	// Update the namespace
	_, err = clientset.CoreV1().Namespaces().Patch(context.Background(), ns.Name, types.JSONPatchType, patchBytes, v1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error patching namespace %s: %v", namespace, err)
	}

	return nil
}
