package cluster

import (
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources"
	"strings"
)

// createAaqJobQueueConfigsCRD creates the ARQ schema
func createAaqJobQueueConfigsCRD() *extv1.CustomResourceDefinition {
	crd := extv1.CustomResourceDefinition{}
	_ = k8syaml.NewYAMLToJSONDecoder(strings.NewReader(resources.AAQCRDs["aaqjobqueueconfig"])).Decode(&crd)
	return &crd
}
