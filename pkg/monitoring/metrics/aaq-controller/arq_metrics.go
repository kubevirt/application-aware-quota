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
 * Copyright 2023,Red Hat, Inc.
 *
 */

package aaq_controller

import (
	"strings"

	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

var (
	arqMetrics = []operatormetrics.Metric{
		arqInfo,
		arqCreatedInfo,
	}

	arqCreatedInfo = operatormetrics.NewGaugeVec(
		operatormetrics.MetricOpts{
			Name: "kube_application_aware_resourcequota_creation_timestamp_seconds",
			Help: "Unix creation timestamp",
		},
		[]string{
			"resourcequota", "namespace",
		},
	)

	arqInfo = operatormetrics.NewGaugeVec(
		operatormetrics.MetricOpts{
			Name: "kube_application_aware_resourcequota",
			Help: "Reports usage and hard limits for each resource in ApplicationAwareResourceQuota",
		},
		[]string{
			"resourcequota", "namespace", "resource", "type", "unit",
		},
	)

	arqStateCollector = operatormetrics.Collector{
		Metrics:         append(arqMetrics),
		CollectCallback: arqStateCollectorCallback,
	}
)

func collectARQMetricInfo(resourcequota, namespace, resource, reasouretype string, value float64) operatormetrics.CollectorResult {
	var unit string

	switch {
	case v1.ResourceName(resource) == v1alpha1.ResourceRequestsVmiMemory,
		v1.ResourceName(resource) == v1.ResourceMemory,
		v1.ResourceName(resource) == v1.ResourceStorage,
		v1.ResourceName(resource) == v1.ResourceEphemeralStorage,
		v1.ResourceName(resource) == v1.ResourceRequestsMemory,
		v1.ResourceName(resource) == v1.ResourceRequestsStorage,
		v1.ResourceName(resource) == v1.ResourceRequestsEphemeralStorage,
		v1.ResourceName(resource) == v1.ResourceLimitsMemory,
		v1.ResourceName(resource) == v1.ResourceLimitsEphemeralStorage,
		strings.HasPrefix(resource, v1.ResourceRequestsHugePagesPrefix):
		unit = "bytes"
	case v1.ResourceName(resource) == v1alpha1.ResourceRequestsVmiCPU,
		v1.ResourceName(resource) == v1.ResourceLimitsCPU,
		v1.ResourceName(resource) == v1.ResourceRequestsCPU,
		v1.ResourceName(resource) == v1.ResourceCPU:
		unit = "core"
	default:
		unit = "count"
	}
	return operatormetrics.CollectorResult{
		Metric: arqInfo,
		Labels: []string{
			resourcequota,
			namespace,
			resource,
			reasouretype,
			unit,
		},
		Value: value,
	}
}

func collectAllARQsResourceMetrics(arqs []*v1alpha1.ApplicationAwareResourceQuota) []operatormetrics.CollectorResult {
	var results []operatormetrics.CollectorResult
	for _, arq := range arqs {
		if arq.Status.Hard != nil {
			for res, qty := range arq.Status.Hard {
				results = append(results, collectARQMetricInfo(arq.Name, arq.Namespace, string(res), "hard", convertValueToFloat64(&qty)))
			}
		}
		if arq.Status.Used != nil {
			for res, qty := range arq.Status.Used {
				results = append(results, collectARQMetricInfo(arq.Name, arq.Namespace, string(res), "used", convertValueToFloat64(&qty)))
			}
		}
	}
	return results
}

func collectAllARQsCreatedMetrics(arqs []*v1alpha1.ApplicationAwareResourceQuota) []operatormetrics.CollectorResult {
	var results []operatormetrics.CollectorResult
	for _, arq := range arqs {
		results = append(results, operatormetrics.CollectorResult{
			Metric: arqCreatedInfo,
			Labels: []string{
				arq.Name,
				arq.Namespace,
			},
			Value: float64(arq.CreationTimestamp.Unix()),
		})
	}
	return results
}

func arqStateCollectorCallback() []operatormetrics.CollectorResult {
	cachedObjs := stores.ArqStore.List()
	if len(cachedObjs) == 0 {
		log.Log.V(4).Infof("No ARQs detected")
		return []operatormetrics.CollectorResult{}
	}

	arqs := make([]*v1alpha1.ApplicationAwareResourceQuota, len(cachedObjs))

	for i, obj := range cachedObjs {
		arqs[i] = obj.(*v1alpha1.ApplicationAwareResourceQuota)
	}

	var results []operatormetrics.CollectorResult
	results = append(results, collectAllARQsResourceMetrics(arqs)...)
	results = append(results, collectAllARQsCreatedMetrics(arqs)...)

	return results
}

// convertValueToFloat64 converts a resource.Quantity to a float64 and checks for a possible overflow in the value.
func convertValueToFloat64(q *resource.Quantity) float64 {
	if q.Value() > resource.MaxMilliValue {
		return float64(q.Value())
	}
	return float64(q.MilliValue()) / 1000
}
