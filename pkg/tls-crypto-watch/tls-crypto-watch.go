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
	"crypto/tls"
	"fmt"

	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"
	v1 "kubevirt.io/api/core/v1"
)

const noSrvCertMessage = "No server certificate, server is not yet ready to receive traffic"

var (
	cipherSuites         = tls.CipherSuites()
	insecureCipherSuites = tls.InsecureCipherSuites()
)

// TLSVersion converts from human-readable TLS version (for example "1.1")
// to the values accepted by tls.Config (for example 0x301).
func TLSVersion(version v1.TLSProtocolVersion) uint16 {
	switch version {
	case v1.VersionTLS10:
		return tls.VersionTLS10
	case v1.VersionTLS11:
		return tls.VersionTLS11
	case v1.VersionTLS12:
		return tls.VersionTLS12
	case v1.VersionTLS13:
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

func CipherSuiteNameMap() map[string]uint16 {
	var idByName = map[string]uint16{}
	for _, cipherSuite := range cipherSuites {
		idByName[cipherSuite.Name] = cipherSuite.ID
	}
	for _, cipherSuite := range insecureCipherSuites {
		idByName[cipherSuite.Name] = cipherSuite.ID
	}
	return idByName
}

func CipherSuiteIds(names []string) []uint16 {
	var idByName = CipherSuiteNameMap()
	var ids []uint16
	for _, name := range names {
		if id, ok := idByName[name]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// SetupTLS configures TLS for a server
func SetupTLS(certManager certificate.Manager) *tls.Config {
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
			tlsConfig := &v1.TLSConfiguration{ //maybe we will want to add config in AAQ CR in the future
				MinTLSVersion: v1.VersionTLS12,
				Ciphers:       nil,
			}
			ciphers := CipherSuiteIds(tlsConfig.Ciphers)
			minTLSVersion := TLSVersion(tlsConfig.MinTLSVersion)
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
