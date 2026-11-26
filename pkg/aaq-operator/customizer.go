/*
Copyright 2026 The AAQ Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aaq_operator

import (
	"crypto/sha1" //nolint:gosec // not used for security, only to fingerprint the customizations
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/application-aware-quota/pkg/util"
	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// customizableKinds are the kinds of operator-managed resources that spec.customizeComponents can patch.
var customizableKinds = map[string]bool{
	"Deployment":                     true,
	"Service":                        true,
	"ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration":   true,
}

// Customizer applies the patches of spec.customizeComponents to the resources deployed by the operator.
type Customizer struct {
	patches []aaqv1.CustomizeComponentsPatch
	hash    string
}

// NewCustomizer returns a Customizer for the given customizations.
func NewCustomizer(customizations aaqv1.CustomizeComponents) (*Customizer, error) {
	hash, err := getHash(customizations)
	if err != nil {
		return nil, err
	}
	return &Customizer{
		patches: customizations.Patches,
		hash:    hash,
	}, nil
}

// Hash returns a fingerprint of the customizations.
func (c *Customizer) Hash() string {
	return c.hash
}

// GetPatchesForResource returns the patches matching the given resource kind and name, in order.
func (c *Customizer) GetPatchesForResource(resourceType, name string) []aaqv1.CustomizeComponentsPatch {
	var patches []aaqv1.CustomizeComponentsPatch
	for _, p := range c.patches {
		if valueMatchesKey(p.ResourceType, resourceType) && valueMatchesKey(p.ResourceName, name) {
			patches = append(patches, p)
		}
	}
	return patches
}

// Apply applies the matching patches, in order, to each customizable object of the slice.
// Patched objects are annotated with the customizations hash.
func (c *Customizer) Apply(objects []client.Object) error {
	if len(c.patches) == 0 {
		return nil
	}
	for _, obj := range objects {
		if obj == nil || reflect.ValueOf(obj).IsNil() {
			continue
		}
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if !customizableKinds[kind] {
			continue
		}
		name := obj.GetName()
		patches := c.GetPatchesForResource(kind, name)
		if len(patches) == 0 {
			continue
		}
		patches = append(patches, aaqv1.CustomizeComponentsPatch{
			Patch: fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, util.AnnCustomizeComponentHash, c.hash),
			Type:  aaqv1.MergePatchType,
		})
		for _, p := range patches {
			if err := applyPatch(obj, p); err != nil {
				return fmt.Errorf("failed to apply %s patch to %s %s: %w", p.Type, kind, name, err)
			}
		}
	}
	return nil
}

func valueMatchesKey(value, key string) bool {
	return value == "*" || strings.EqualFold(key, value)
}

func applyPatch(obj client.Object, patch aaqv1.CustomizeComponentsPatch) error {
	if obj == nil {
		return nil
	}

	original, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	var modified []byte
	switch patch.Type {
	case aaqv1.JSONPatchType:
		p, err := jsonpatch.DecodePatch([]byte(patch.Patch))
		if err != nil {
			return err
		}
		opts := jsonpatch.NewApplyOptions()
		opts.AllowMissingPathOnRemove = true
		opts.EnsurePathExistsOnAdd = true
		if modified, err = p.ApplyWithOptions(original, opts); err != nil {
			return err
		}
	case aaqv1.MergePatchType:
		if modified, err = jsonpatch.MergePatch(original, []byte(patch.Patch)); err != nil {
			return err
		}
	case aaqv1.StrategicMergePatchType:
		if modified, err = strategicpatch.StrategicMergePatch(original, []byte(patch.Patch), obj); err != nil {
			return err
		}
	default:
		return fmt.Errorf("patch type %q is not supported", patch.Type)
	}

	// reset the object before unmarshalling, since json.Unmarshal does not clear
	// fields that were removed by the patch
	value := reflect.ValueOf(obj)
	value.Elem().Set(reflect.New(value.Type().Elem()).Elem())
	return json.Unmarshal(modified, obj)
}

func getHash(customizations aaqv1.CustomizeComponents) (string, error) {
	values, err := json.Marshal(customizations)
	if err != nil {
		return "", err
	}
	// #nosec G401 -- sha1 is only used to fingerprint the customizations, not for security
	hasher := sha1.New()
	hasher.Write(values)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
