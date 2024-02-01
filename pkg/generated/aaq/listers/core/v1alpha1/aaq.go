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
	v1alpha1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

// AAQLister helps list AAQs.
// All objects returned here must be treated as read-only.
type AAQLister interface {
	// List lists all AAQs in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.AAQ, err error)
	// Get retrieves the AAQ from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.AAQ, error)
	AAQListerExpansion
}

// aAQLister implements the AAQLister interface.
type aAQLister struct {
	indexer cache.Indexer
}

// NewAAQLister returns a new AAQLister.
func NewAAQLister(indexer cache.Indexer) AAQLister {
	return &aAQLister{indexer: indexer}
}

// List lists all AAQs in the indexer.
func (s *aAQLister) List(selector labels.Selector) (ret []*v1alpha1.AAQ, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.AAQ))
	})
	return ret, err
}

// Get retrieves the AAQ from the index for a given name.
func (s *aAQLister) Get(name string) (*v1alpha1.AAQ, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("aaq"), name)
	}
	return obj.(*v1alpha1.AAQ), nil
}
