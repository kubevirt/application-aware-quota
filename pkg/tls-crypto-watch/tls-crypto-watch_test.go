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

var _ = Describe("SelectGroups", func() {
	It("should return nil for nil profile", func() {
		Expect(SelectGroups(nil)).To(BeNil())
	})

	It("should return nil for non-custom profile type", func() {
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileIntermediateType,
		}
		Expect(SelectGroups(profile)).To(BeNil())
	})

	It("should return groups from a custom profile", func() {
		groups := []string{aaqv1alpha1.TLSGroupX25519, aaqv1alpha1.TLSGroupSecp384r1}
		profile := &aaqv1alpha1.TLSSecurityProfile{
			Type: aaqv1alpha1.TLSProfileCustomType,
			Custom: &aaqv1alpha1.CustomTLSProfile{
				TLSProfileSpec: aaqv1alpha1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256"},
					MinTLSVersion: aaqv1alpha1.VersionTLS12,
					Groups:        groups,
				},
			},
		}
		Expect(SelectGroups(profile)).To(Equal(groups))
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
		Expect(SelectGroups(profile)).To(BeNil())
	})
})
