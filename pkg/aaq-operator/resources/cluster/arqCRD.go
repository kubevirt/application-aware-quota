package cluster

import (
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources"
	"strings"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// createApplicationAwareResourceQuotaCRD creates the ARQ schema
func createApplicationAwareResourceQuotaCRD() *extv1.CustomResourceDefinition {
	crd := extv1.CustomResourceDefinition{}
	_ = k8syaml.NewYAMLToJSONDecoder(strings.NewReader(resources.AAQCRDs["applicationawareresourcequota"])).Decode(&crd)
	return &crd
}
