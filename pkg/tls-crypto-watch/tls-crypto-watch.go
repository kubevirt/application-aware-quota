/*
 * This file is part of the AAQ project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */

package tlscryptowatch

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	ocpcrypto "github.com/openshift/library-go/pkg/crypto"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"

	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/informers"
	aaqv1alpha1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

const noSrvCertMessage = "No server certificate, server is not yet ready to receive traffic"

// CryptoConfig contains TLS crypto configurables
type CryptoConfig struct {
	CipherSuites []uint16
	MinVersion   uint16
}

// AaqConfigTLSWatcher is the interface of aaqConfigTLSWatcher
type AaqConfigTLSWatcher interface {
	GetTLSConfig() *CryptoConfig
	GetInformer() cache.SharedIndexInformer
}

type aaqConfigTLSWatcher struct {
	// keep this around for tests
	informer cache.SharedIndexInformer

	config *CryptoConfig
	mutex  sync.RWMutex
}

// NewAaqConfigTLSWatcher creates a new aaqConfigTLSWatcher
func NewAaqConfigTLSWatcher(ctx context.Context, aaqCli client.AAQClient) (AaqConfigTLSWatcher, error) {
	aaqInformer := informers.GetAAQInformer(aaqCli)

	ctw := &aaqConfigTLSWatcher{
		informer: aaqInformer,
		config:   DefaultCryptoConfig(),
	}

	_, err := aaqInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			klog.V(3).Infof("aaqInformer add callback: %+v", obj)
			ctw.updateConfig(obj.(*aaqv1alpha1.AAQ))
		},
		UpdateFunc: func(_, obj interface{}) {
			klog.V(3).Infof("aaqInformer update callback: %+v", obj)
			ctw.updateConfig(obj.(*aaqv1alpha1.AAQ))
		},
		DeleteFunc: func(obj interface{}) {
			aaq := obj.(*aaqv1alpha1.AAQ)
			klog.Errorf("AAQ %s deleted", aaq.Name)
		},
	})

	if err != nil {
		return nil, err
	}

	go aaqInformer.Run(ctx.Done())

	klog.V(3).Infoln("Waiting for cache sync")
	cache.WaitForCacheSync(ctx.Done(), aaqInformer.HasSynced)
	klog.V(3).Infoln("Cache sync complete")

	return ctw, nil
}

func (ctw *aaqConfigTLSWatcher) GetTLSConfig() *CryptoConfig {
	ctw.mutex.RLock()
	defer ctw.mutex.RUnlock()
	return ctw.config
}

func (ctw *aaqConfigTLSWatcher) GetInformer() cache.SharedIndexInformer {
	return ctw.informer
}

func (ctw *aaqConfigTLSWatcher) updateConfig(aaq *aaqv1alpha1.AAQ) {
	newConfig := &CryptoConfig{}

	cipherNames, minTypedTLSVersion := SelectCipherSuitesAndMinTLSVersion(aaq.Spec.TLSSecurityProfile)
	minTLSVersion, _ := ocpcrypto.TLSVersion(string(minTypedTLSVersion))
	ciphers := CipherSuitesIDs(cipherNames)
	newConfig.CipherSuites = ciphers
	newConfig.MinVersion = minTLSVersion

	ctw.mutex.Lock()
	defer ctw.mutex.Unlock()
	ctw.config = newConfig
}

// SelectCipherSuitesAndMinTLSVersion returns cipher names and minimal TLS version according to the input profile
func SelectCipherSuitesAndMinTLSVersion(profile *aaqv1alpha1.TLSSecurityProfile) ([]string, aaqv1alpha1.TLSProtocolVersion) {
	if profile != nil && profile.Custom != nil {
		return profile.Custom.TLSProfileSpec.Ciphers, profile.Custom.TLSProfileSpec.MinTLSVersion
	}

	profileType := aaqv1alpha1.TLSProfileIntermediateType
	if profile != nil && profile.Type != "" {
		profileType = profile.Type
	}

	return aaqv1alpha1.TLSProfiles[profileType].Ciphers, aaqv1alpha1.TLSProfiles[profileType].MinTLSVersion
}

// DefaultCryptoConfig returns a crypto config with legitimate defaults to start with
func DefaultCryptoConfig() *CryptoConfig {
	cipherNames, minTypedTLSVersion := SelectCipherSuitesAndMinTLSVersion(nil)
	minTLSVersion, _ := ocpcrypto.TLSVersion(string(minTypedTLSVersion))

	return &CryptoConfig{
		CipherSuites: CipherSuitesIDs(cipherNames),
		MinVersion:   minTLSVersion,
	}
}

// CipherSuitesIDs translates cipher names to IDs which can be straight to the tls.Config
func CipherSuitesIDs(names []string) []uint16 {
	// ref: https://www.iana.org/assignments/tls-parameters/tls-parameters.xml
	var idByName = map[string]uint16{
		// TLS 1.2
		"ECDHE-ECDSA-AES128-GCM-SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"ECDHE-RSA-AES128-GCM-SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"ECDHE-ECDSA-AES256-GCM-SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"ECDHE-RSA-AES256-GCM-SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"ECDHE-ECDSA-CHACHA20-POLY1305": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		"ECDHE-RSA-CHACHA20-POLY1305":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"ECDHE-ECDSA-AES128-SHA256":     tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
		"ECDHE-RSA-AES128-SHA256":       tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		"AES128-GCM-SHA256":             tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		"AES256-GCM-SHA384":             tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		"AES128-SHA256":                 tls.TLS_RSA_WITH_AES_128_CBC_SHA256,

		// TLS 1
		"ECDHE-ECDSA-AES128-SHA": tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		"ECDHE-RSA-AES128-SHA":   tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		"ECDHE-ECDSA-AES256-SHA": tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		"ECDHE-RSA-AES256-SHA":   tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,

		// SSL 3
		"AES128-SHA":   tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		"AES256-SHA":   tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		"DES-CBC3-SHA": tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
	}
	for _, cipherSuite := range tls.CipherSuites() {
		idByName[cipherSuite.Name] = cipherSuite.ID
	}

	ids := []uint16{}
	for _, name := range names {
		if id, ok := idByName[name]; ok {
			ids = append(ids, id)
		}
	}

	return ids
}

// SetupTLS configures TLS for a server with dynamic cipher suites and TLS version from the AAQ CR
func SetupTLS(certManager certificate.Manager, tlsWatcher AaqConfigTLSWatcher) *tls.Config {
	tlsConfig := &tls.Config{
		GetCertificate: func(info *tls.ClientHelloInfo) (certificate *tls.Certificate, err error) {
			cert := certManager.Current()
			if cert == nil {
				return nil, fmt.Errorf(noSrvCertMessage)
			}
			return cert, nil
		},
		GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
			crt := certManager.Current()
			if crt == nil {
				klog.Error(noSrvCertMessage)
				return nil, fmt.Errorf(noSrvCertMessage)
			}

			// Get TLS config from watcher, use defaults if nil
			var ciphers []uint16
			var minTLSVersion uint16 = tls.VersionTLS12
			if tlsWatcher != nil {
				cryptoConfig := tlsWatcher.GetTLSConfig()
				if cryptoConfig != nil {
					ciphers = cryptoConfig.CipherSuites
					minTLSVersion = cryptoConfig.MinVersion
				}
			}

			config := &tls.Config{
				CipherSuites: ciphers,
				MinVersion:   minTLSVersion,
				Certificates: []tls.Certificate{*crt},
				ClientAuth:   tls.VerifyClientCertIfGiven,
			}

			config.BuildNameToCertificate() //nolint:staticcheck
			return config, nil
		},
	}
	tlsConfig.BuildNameToCertificate() //nolint:staticcheck
	return tlsConfig
}
