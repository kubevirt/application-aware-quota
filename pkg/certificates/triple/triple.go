package triple

import (
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	certutil "kubevirt.io/application-aware-quota/pkg/certificates/triple/cert"
)

type KeyPair struct {
	Key  *ecdsa.PrivateKey
	Cert *x509.Certificate
}

func NewCA(name string, duration time.Duration) (*KeyPair, error) {
	key, err := certutil.NewECDSAPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("unable to create a private key for a new CA: %v", err)
	}

	signerName := fmt.Sprintf("%s@%d", name, time.Now().Unix())
	config := certutil.Config{
		CommonName: signerName,
	}

	cert, err := certutil.NewSelfSignedCACert(config, key, duration)
	if err != nil {
		return nil, fmt.Errorf("unable to create a self-signed certificate for a new CA: %v", err)
	}
	return &KeyPair{
		Key:  key,
		Cert: cert,
	}, nil
}

func NewServerKeyPair(ca *KeyPair, commonName, svcName, svcNamespace, dnsDomain string, ips, hostnames []string, duration time.Duration) (*KeyPair, error) {
	key, err := certutil.NewECDSAPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("unable to create a server private key: %v", err)
	}

	namespacedName := fmt.Sprintf("%s.%s", svcName, svcNamespace)
	internalAPIServerFQDN := []string{
		svcName,
		namespacedName,
		fmt.Sprintf("%s.svc", namespacedName),
		fmt.Sprintf("%s.svc.%s", namespacedName, dnsDomain),
	}

	altNames := certutil.AltNames{}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil {
			altNames.IPs = append(altNames.IPs, ip)
		}
	}
	altNames.DNSNames = append(altNames.DNSNames, hostnames...)
	altNames.DNSNames = append(altNames.DNSNames, internalAPIServerFQDN...)

	config := certutil.Config{
		CommonName: commonName,
		AltNames:   altNames,
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	cert, err := certutil.NewSignedCert(config, key, ca.Cert, ca.Key, duration)
	if err != nil {
		return nil, fmt.Errorf("unable to sign the server certificate: %v", err)
	}

	return &KeyPair{
		Key:  key,
		Cert: cert,
	}, nil
}

func NewClientKeyPair(ca *KeyPair, commonName string, organizations []string, duration time.Duration) (*KeyPair, error) {
	key, err := certutil.NewECDSAPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("unable to create a client private key: %v", err)
	}

	config := certutil.Config{
		CommonName:   commonName,
		Organization: organizations,
		Usages:       []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	cert, err := certutil.NewSignedCert(config, key, ca.Cert, ca.Key, duration)
	if err != nil {
		return nil, fmt.Errorf("unable to sign the client certificate: %v", err)
	}

	return &KeyPair{
		Key:  key,
		Cert: cert,
	}, nil
}
