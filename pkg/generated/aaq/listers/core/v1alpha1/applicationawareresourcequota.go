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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	v1alpha1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
)

// ApplicationAwareResourceQuotaLister helps list ApplicationAwareResourceQuotas.
// All objects returned here must be treated as read-only.
type ApplicationAwareResourceQuotaLister interface {
	// List lists all ApplicationAwareResourceQuotas in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ApplicationAwareResourceQuota, err error)
	// ApplicationAwareResourceQuotas returns an object that can list and get ApplicationAwareResourceQuotas.
	ApplicationAwareResourceQuotas(namespace string) ApplicationAwareResourceQuotaNamespaceLister
	ApplicationAwareResourceQuotaListerExpansion
}

// applicationAwareResourceQuotaLister implements the ApplicationAwareResourceQuotaLister interface.
type applicationAwareResourceQuotaLister struct {
	indexer cache.Indexer
}

// NewApplicationAwareResourceQuotaLister returns a new ApplicationAwareResourceQuotaLister.
func NewApplicationAwareResourceQuotaLister(indexer cache.Indexer) ApplicationAwareResourceQuotaLister {
	return &applicationAwareResourceQuotaLister{indexer: indexer}
}

// List lists all ApplicationAwareResourceQuotas in the indexer.
func (s *applicationAwareResourceQuotaLister) List(selector labels.Selector) (ret []*v1alpha1.ApplicationAwareResourceQuota, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ApplicationAwareResourceQuota))
	})
	return ret, err
}

// ApplicationAwareResourceQuotas returns an object that can list and get ApplicationAwareResourceQuotas.
func (s *applicationAwareResourceQuotaLister) ApplicationAwareResourceQuotas(namespace string) ApplicationAwareResourceQuotaNamespaceLister {
	return applicationAwareResourceQuotaNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ApplicationAwareResourceQuotaNamespaceLister helps list and get ApplicationAwareResourceQuotas.
// All objects returned here must be treated as read-only.
type ApplicationAwareResourceQuotaNamespaceLister interface {
	// List lists all ApplicationAwareResourceQuotas in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ApplicationAwareResourceQuota, err error)
	// Get retrieves the ApplicationAwareResourceQuota from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ApplicationAwareResourceQuota, error)
	ApplicationAwareResourceQuotaNamespaceListerExpansion
}

// applicationAwareResourceQuotaNamespaceLister implements the ApplicationAwareResourceQuotaNamespaceLister
// interface.
type applicationAwareResourceQuotaNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ApplicationAwareResourceQuotas in the indexer for a given namespace.
func (s applicationAwareResourceQuotaNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.ApplicationAwareResourceQuota, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ApplicationAwareResourceQuota))
	})
	return ret, err
}

// Get retrieves the ApplicationAwareResourceQuota from the indexer for a given namespace and name.
func (s applicationAwareResourceQuotaNamespaceLister) Get(name string) (*v1alpha1.ApplicationAwareResourceQuota, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("applicationawareresourcequota"), name)
	}
	return obj.(*v1alpha1.ApplicationAwareResourceQuota), nil
}
