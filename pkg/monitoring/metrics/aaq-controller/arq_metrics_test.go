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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rhobs/operator-observability-toolkit/pkg/operatormetrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"

	"kubevirt.io/application-aware-quota/tests/builders"
)

var _ = Describe("domainstats", func() {
	Context("collector functions", func() {

		It("collectAllARQsCreatedMetrics should return a CollectorResult with the correct values", func() {
			creationTimeArq1 := metav1.NewTime(time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC))
			creationTimeArq2 := metav1.NewTime(time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC))

			arq1 := builders.NewArqBuilder().WithNamespace("somens").WithName("arq-test").WithCreationTimestamp(creationTimeArq1).WithResource(corev1.ResourceServices, resource.MustParse("5")).Build()
			arq2 := builders.NewArqBuilder().WithNamespace("somens").WithName("arq-test2").WithCreationTimestamp(creationTimeArq2).WithResource(corev1.ResourceCPU, resource.MustParse("5")).Build()
			CollectorResult := collectAllARQsCreatedMetrics([]*v1alpha1.ApplicationAwareResourceQuota{arq1, arq2})
			Expect(CollectorResult).To(ContainElements(
				operatormetrics.CollectorResult{
					Metric: arqCreatedInfo,
					Labels: []string{
						arq1.Name,
						arq1.Namespace,
					},
					Value: float64(creationTimeArq1.Unix()),
				},
				operatormetrics.CollectorResult{
					Metric: arqCreatedInfo,
					Labels: []string{
						arq2.Name,
						arq2.Namespace,
					},
					Value: float64(creationTimeArq2.Unix()),
				},
			))
		})

		It("collectAllARQsCreatedMetrics should return a CollectorResult with the correct values", func() {
			arq1 := builders.NewArqBuilder().WithNamespace("somens").WithName("arq-test").WithResource(corev1.ResourceServices, resource.MustParse("5")).WithSyncStatusHardEmptyStatusUsed().Build()
			arq2 := builders.NewArqBuilder().WithNamespace("somens").WithName("arq-test2").WithResource(corev1.ResourceCPU, resource.MustParse("5")).WithSyncStatusHardEmptyStatusUsed().Build()
			CollectorResult := collectAllARQsResourceMetrics([]*v1alpha1.ApplicationAwareResourceQuota{arq1, arq2})
			Expect(CollectorResult).To(ContainElements(
				operatormetrics.CollectorResult{
					Metric: arqInfo,
					Labels: []string{
						arq1.Name,
						arq1.Namespace,
						string(corev1.ResourceServices),
						"hard",
						"count",
					},
					Value: float64(5),
				},
				operatormetrics.CollectorResult{
					Metric: arqInfo,
					Labels: []string{
						arq1.Name,
						arq1.Namespace,
						string(corev1.ResourceServices),
						"used",
						"count",
					},
					Value: float64(0),
				},
				operatormetrics.CollectorResult{
					Metric: arqInfo,
					Labels: []string{
						arq2.Name,
						arq2.Namespace,
						string(corev1.ResourceCPU),
						"hard",
						"core",
					},
					Value: float64(5),
				},
				operatormetrics.CollectorResult{
					Metric: arqInfo,
					Labels: []string{
						arq2.Name,
						arq2.Namespace,
						string(corev1.ResourceCPU),
						"used",
						"core",
					},
					Value: float64(0),
				},
			))
		})
	})
})
