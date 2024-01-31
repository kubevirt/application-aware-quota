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
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
	v1 "kubevirt.io/application-aware-quota/pkg/generated/cluster-resource-quota/clientset/versioned/typed/quota/v1"
)

type FakeQuotaV1 struct {
	*testing.Fake
}

func (c *FakeQuotaV1) AppliedClusterResourceQuotas(namespace string) v1.AppliedClusterResourceQuotaInterface {
	return &FakeAppliedClusterResourceQuotas{c, namespace}
}

func (c *FakeQuotaV1) ClusterResourceQuotas() v1.ClusterResourceQuotaInterface {
	return &FakeClusterResourceQuotas{c}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeQuotaV1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
