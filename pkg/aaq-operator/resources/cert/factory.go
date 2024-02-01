package cert

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources/cluster"
	"kubevirt.io/application-aware-quota/pkg/util"
	"time"
)

// FactoryArgs contains the required parameters to generate certs
type FactoryArgs struct {
	Namespace string

	SignerDuration *time.Duration
	// Duration to subtract from cert NotAfter value
	SignerRenewBefore *time.Duration

	TargetDuration *time.Duration
	// Duration to subtract from cert NotAfter value
	TargetRenewBefore *time.Duration
}

// CertificateConfig contains cert configuration data
type CertificateConfig struct {
	Lifetime time.Duration
	Refresh  time.Duration
}

// CertificateDefinition contains the data required to create/manage certtificate chains
type CertificateDefinition struct {
	// configurable by user
	Configurable bool

	// current CA key/cert
	SignerSecret *corev1.Secret
	SignerConfig CertificateConfig

	// all valid CA certs
	CertBundleConfigmap *corev1.ConfigMap

	// current key/cert for target
	TargetSecret *corev1.Secret
	TargetConfig CertificateConfig

	// only one of the following should be set
	// contains target key/cert for server
	TargetService *string
	// contains target user name
	TargetUser *string
}

// CreateCertificateDefinitions creates certificate definitions
func CreateCertificateDefinitions(args *FactoryArgs) []CertificateDefinition {
	defs := createCertificateDefinitions()
	for i := range defs {
		def := &defs[i]

		if def.SignerSecret != nil {
			addNamespace(args.Namespace, def.SignerSecret)
		}

		if def.CertBundleConfigmap != nil {
			addNamespace(args.Namespace, def.CertBundleConfigmap)
		}

		if def.TargetSecret != nil {
			addNamespace(args.Namespace, def.TargetSecret)
		}

		if def.Configurable {
			if args.SignerDuration != nil {
				def.SignerConfig.Lifetime = *args.SignerDuration
			}

			if args.SignerRenewBefore != nil {
				// convert to time from cert NotBefore
				def.SignerConfig.Refresh = def.SignerConfig.Lifetime - *args.SignerRenewBefore
			}

			if args.TargetDuration != nil {
				def.TargetConfig.Lifetime = *args.TargetDuration
			}

			if args.TargetRenewBefore != nil {
				// convert to time from cert NotBefore
				def.TargetConfig.Refresh = def.TargetConfig.Lifetime - *args.TargetRenewBefore
			}
		}
	}

	return defs
}

func addNamespace(namespace string, obj metav1.Object) {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}
}

func createCertificateDefinitions() []CertificateDefinition {
	return []CertificateDefinition{
		{
			Configurable: true,
			SignerSecret: createSecret("aaq-server"),
			SignerConfig: CertificateConfig{
				Lifetime: 48 * time.Hour,
				Refresh:  24 * time.Hour,
			},
			CertBundleConfigmap: createConfigMap("aaq-server-signer-bundle"),
			TargetSecret:        createSecret(util.SecretResourceName),
			TargetConfig: CertificateConfig{
				Lifetime: 24 * time.Hour,
				Refresh:  12 * time.Hour,
			},
			TargetService: &[]string{cluster.AaqServerServiceName}[0],
		},
	}
}

func createSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: util.ResourceBuilder.WithCommonLabels(nil),
		},
	}
}

func createConfigMap(name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: util.ResourceBuilder.WithCommonLabels(nil),
		},
	}
}
