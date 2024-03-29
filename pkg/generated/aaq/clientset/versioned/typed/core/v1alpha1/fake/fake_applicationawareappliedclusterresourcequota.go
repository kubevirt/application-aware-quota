/*
Copyright 2023 The AAQ Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
	v1alpha1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// FakeApplicationAwareAppliedClusterResourceQuotas implements ApplicationAwareAppliedClusterResourceQuotaInterface
type FakeApplicationAwareAppliedClusterResourceQuotas struct {
	Fake *FakeAaqV1alpha1
	ns   string
}

var applicationawareappliedclusterresourcequotasResource = v1alpha1.SchemeGroupVersion.WithResource("applicationawareappliedclusterresourcequotas")

var applicationawareappliedclusterresourcequotasKind = v1alpha1.SchemeGroupVersion.WithKind("ApplicationAwareAppliedClusterResourceQuota")

// Get takes name of the applicationAwareAppliedClusterResourceQuota, and returns the corresponding applicationAwareAppliedClusterResourceQuota object, and an error if there is any.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(applicationawareappliedclusterresourcequotasResource, c.ns, name), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuota), err
}

// List takes label and field selectors, and returns the list of ApplicationAwareAppliedClusterResourceQuotas that match those selectors.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(applicationawareappliedclusterresourcequotasResource, applicationawareappliedclusterresourcequotasKind, c.ns, opts), &v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList{ListMeta: obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList).ListMeta}
	for _, item := range obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested applicationAwareAppliedClusterResourceQuotas.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(applicationawareappliedclusterresourcequotasResource, c.ns, opts))

}

// Create takes the representation of a applicationAwareAppliedClusterResourceQuota and creates it.  Returns the server's representation of the applicationAwareAppliedClusterResourceQuota, and an error, if there is any.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Create(ctx context.Context, applicationAwareAppliedClusterResourceQuota *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, opts v1.CreateOptions) (result *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(applicationawareappliedclusterresourcequotasResource, c.ns, applicationAwareAppliedClusterResourceQuota), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuota), err
}

// Update takes the representation of a applicationAwareAppliedClusterResourceQuota and updates it. Returns the server's representation of the applicationAwareAppliedClusterResourceQuota, and an error, if there is any.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Update(ctx context.Context, applicationAwareAppliedClusterResourceQuota *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, opts v1.UpdateOptions) (result *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(applicationawareappliedclusterresourcequotasResource, c.ns, applicationAwareAppliedClusterResourceQuota), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuota), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) UpdateStatus(ctx context.Context, applicationAwareAppliedClusterResourceQuota *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, opts v1.UpdateOptions) (*v1alpha1.ApplicationAwareAppliedClusterResourceQuota, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(applicationawareappliedclusterresourcequotasResource, "status", c.ns, applicationAwareAppliedClusterResourceQuota), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuota), err
}

// Delete takes name of the applicationAwareAppliedClusterResourceQuota and deletes it. Returns an error if one occurs.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(applicationawareappliedclusterresourcequotasResource, c.ns, name, opts), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(applicationawareappliedclusterresourcequotasResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ApplicationAwareAppliedClusterResourceQuotaList{})
	return err
}

// Patch applies the patch and returns the patched applicationAwareAppliedClusterResourceQuota.
func (c *FakeApplicationAwareAppliedClusterResourceQuotas) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ApplicationAwareAppliedClusterResourceQuota, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(applicationawareappliedclusterresourcequotasResource, c.ns, name, pt, data, subresources...), &v1alpha1.ApplicationAwareAppliedClusterResourceQuota{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ApplicationAwareAppliedClusterResourceQuota), err
}
