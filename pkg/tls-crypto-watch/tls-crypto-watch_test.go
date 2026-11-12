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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	aaqv1alpha1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

var _ = Describe("CurvePreferenceIDs", func() {
	It("should return nil for nil input", func() {
		Expect(CurvePreferenceIDs(nil)).To(BeNil())
	})

	It("should return nil for empty input", func() {
		Expect(CurvePreferenceIDs([]string{})).To(BeNil())
	})

	It("should map recognised classical group names", func() {
		ids := CurvePreferenceIDs([]string{
			aaqv1alpha1.TLSGroupX25519,
			aaqv1alpha1.TLSGroupSecp256r1,
			aaqv1alpha1.TLSGroupSecp384r1,
			aaqv1alpha1.TLSGroupSecp521r1,
		})
		Expect(ids).To(ConsistOf(
			tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521,
		))
	})

	It("should map post-quantum group names", func() {
		ids := CurvePreferenceIDs([]string{
			aaqv1alpha1.TLSGroupX25519MLKEM768,
			aaqv1alpha1.TLSGroupSecP256r1MLKEM768,
			aaqv1alpha1.TLSGroupSecP384r1MLKEM1024,
		})
		Expect(ids).To(ConsistOf(
			tls.X25519MLKEM768, tls.SecP256r1MLKEM768, tls.SecP384r1MLKEM1024,
		))
	})

	It("should accept the IANA spelling x25519", func() {
		ids := CurvePreferenceIDs([]string{"x25519", aaqv1alpha1.TLSGroupX25519})
		Expect(ids).To(Equal([]tls.CurveID{tls.X25519}))
	})

	It("should skip unrecognised group names", func() {
		ids := CurvePreferenceIDs([]string{
			aaqv1alpha1.TLSGroupX25519, "unknownGroup", aaqv1alpha1.TLSGroupSecp256r1,
		})
		Expect(ids).To(ConsistOf(tls.X25519, tls.CurveP256))
	})

	It("should return nil when all names are unrecognised", func() {
		Expect(CurvePreferenceIDs([]string{"foo", "bar"})).To(BeNil())
	})

	It("should deduplicate resolved IDs", func() {
		ids := CurvePreferenceIDs([]string{
			aaqv1alpha1.TLSGroupX25519,
			aaqv1alpha1.TLSGroupSecp256r1,
			aaqv1alpha1.TLSGroupX25519,
			aaqv1alpha1.TLSGroupSecp384r1,
			aaqv1alpha1.TLSGroupSecp256r1,
		})
		Expect(ids).To(ConsistOf(tls.X25519, tls.CurveP256, tls.CurveP384))
	})
})

var _ = Describe("CipherSuitesIDs", func() {
	It("should return empty for nil input", func() {
		Expect(CipherSuitesIDs(nil)).To(BeEmpty())
	})

	It("should return empty for empty input", func() {
		Expect(CipherSuitesIDs([]string{})).To(BeEmpty())
	})

	It("should map recognised cipher names, preserving order", func() {
		ids := CipherSuitesIDs([]string{
			"TLS_AES_128_GCM_SHA256",
			"ECDHE-ECDSA-AES128-GCM-SHA256",
			"AES128-SHA",
		})
		Expect(ids).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		}))
	})

	It("should skip unrecognised cipher names", func() {
		ids := CipherSuitesIDs([]string{
			"TLS_AES_128_GCM_SHA256", "unknownCipher", "AES128-SHA",
		})
		Expect(ids).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		}))
	})

	It("should return empty when all names are unrecognised", func() {
		Expect(CipherSuitesIDs([]string{"foo", "bar"})).To(BeEmpty())
	})
})

var _ = Describe("TLS MinVersion and Ciphers", func() {
	It("should return the intermediate's MinVersion and ciphers for nil profile", func() {
		cfg := getCryptoConfig(nil)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		}))
	})

	It("should return the modern's MinVersion and ciphers, for the Modern profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileModernType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(cfg.CipherSuites).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		}))
	})

	It("should return the intermediate's MinVersion and ciphers, for the Intermediate profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileIntermediateType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		}))
	})

	It("should return the old's MinVersion and ciphers, for the Old profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileOldType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS10)))
		Expect(cfg.CipherSuites).To(Equal(CipherSuitesIDs(aaqv1alpha1.TLSProfiles[aaqv1alpha1.TLSProfileOldType].Ciphers)))
	})

	It("should return MinVersion and ciphers from a custom profile", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileCustomType,
			Custom: &aaqv1alpha1.CustomTLSProfile{
				TLSProfileSpec: aaqv1alpha1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
					MinTLSVersion: aaqv1alpha1.VersionTLS11,
					Groups:        []string{aaqv1alpha1.TLSGroupX25519},
				},
			},
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS11)))
		Expect(cfg.CipherSuites).To(Equal([]uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		}))
	})

	It("should return empty ciphers when custom profile has no ciphers", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileCustomType,
			Custom: &aaqv1alpha1.CustomTLSProfile{
				TLSProfileSpec: aaqv1alpha1.TLSProfileSpec{
					MinTLSVersion: aaqv1alpha1.VersionTLS12,
					Groups:        []string{aaqv1alpha1.TLSGroupX25519},
				},
			},
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(BeEmpty())
	})

	It("should fall back to intermediate's MinVersion and ciphers for an invalid profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: "NotARealProfileType",
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal([]uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		}))
	})
})

var _ = Describe("TLS Groups", func() {
	It("should return the intermediate group list for nil profile", func() {
		cfg := getCryptoConfig(nil)
		Expect(cfg.CurvePreferences).To(ConsistOf(
			tls.X25519,
			tls.X25519MLKEM768,
			tls.CurveP256,
			tls.CurveP384,
		))
	})

	It("should return the modern's list of groups, for the Modern profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileModernType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.CurvePreferences).To(ConsistOf(
			tls.X25519,
			tls.X25519MLKEM768,
			tls.CurveP256,
			tls.CurveP384,
		))
	})

	It("should return the intermediate's list of groups, for the Intermediate profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileIntermediateType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.CurvePreferences).To(ConsistOf(
			tls.X25519,
			tls.X25519MLKEM768,
			tls.CurveP256,
			tls.CurveP384,
		))
	})

	It("should return the old's list of groups, for the Old profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileOldType,
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.CurvePreferences).To(ConsistOf(
			tls.X25519,
			tls.X25519MLKEM768,
			tls.CurveP256,
			tls.CurveP384,
		))
	})

	It("should return groups from a custom profile", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileCustomType,
			Custom: &aaqv1alpha1.CustomTLSProfile{
				TLSProfileSpec: aaqv1alpha1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256"},
					MinTLSVersion: aaqv1alpha1.VersionTLS12,
					Groups:        []string{aaqv1alpha1.TLSGroupX25519, aaqv1alpha1.TLSGroupSecp384r1},
				},
			},
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.CurvePreferences).To(ConsistOf(
			tls.X25519,
			tls.CurveP384,
		))
	})

	It("should return nil when custom profile has no groups", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileCustomType,
			Custom: &aaqv1alpha1.CustomTLSProfile{
				TLSProfileSpec: aaqv1alpha1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256"},
					MinTLSVersion: aaqv1alpha1.VersionTLS12,
				},
			},
		}

		cfg := getCryptoConfig(profile)
		Expect(cfg.CurvePreferences).To(BeNil())
	})
})
