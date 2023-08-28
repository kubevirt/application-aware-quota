package framework

import (
	"context"
	"fmt"
	"reflect"

	sdkapi "kubevirt.io/controller-lifecycle-operator-sdk/api"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *Framework) GetNodePlacementValuesWithRandomNodeAffinity(nodeSelectorTestValue map[string]string, tolerationTestValue []v1.Toleration) sdkapi.NodePlacement {
	nodes, _ := f.K8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})

	nodeName := nodes.Items[0].Name
	for _, node := range nodes.Items {
		if _, hasLabel := node.Labels["node-role.kubernetes.io/worker"]; hasLabel {
			nodeName = node.Name
			break
		}
	}

	affinityTestValue := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{Key: "kubernetes.io/hostname", Operator: v1.NodeSelectorOpIn, Values: []string{nodeName}},
						},
					},
				},
			},
		},
	}

	return sdkapi.NodePlacement{
		NodeSelector: nodeSelectorTestValue,
		Affinity:     affinityTestValue,
		Tolerations:  tolerationTestValue,
	}
}

// PodSpecHasTestNodePlacementValues compares if the pod spec has the set of node placement values defined for testing purposes
func (f *Framework) PodSpecHasTestNodePlacementValues(podSpec v1.PodSpec, nodePlacement sdkapi.NodePlacement) error {

	errfmt := "mismatched nodeSelectors, podSpec:\n%v\nExpected:\n%v\n"
	if !reflect.DeepEqual(podSpec.NodeSelector, nodePlacement.NodeSelector) || !reflect.DeepEqual(podSpec.Affinity, nodePlacement.Affinity) {
		return fmt.Errorf(errfmt, podSpec.NodeSelector, nodePlacement.NodeSelector)
	}

	for _, nodePlacementToleration := range nodePlacement.Tolerations {
		foundMatchingToleration := false
		for _, toleration := range podSpec.Tolerations {
			if toleration == nodePlacementToleration {
				foundMatchingToleration = true
			}
		}
		if !foundMatchingToleration {
			return fmt.Errorf(errfmt, podSpec.NodeSelector, nodePlacement.NodeSelector)
		}
	}
	return nil
}
