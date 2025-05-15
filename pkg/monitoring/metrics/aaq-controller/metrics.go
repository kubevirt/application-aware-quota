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
	"github.com/machadovilaca/operator-observability/pkg/operatormetrics"
	"k8s.io/client-go/tools/cache"
)

type Stores struct {
	ArqStore cache.Store
}

var (
	stores *Stores
)

func SetupMetrics(
	metricsStores *Stores,
) error {
	if metricsStores == nil {
		metricsStores = &Stores{}
	}
	stores = metricsStores

	return operatormetrics.RegisterCollector(
		arqStateCollector,
	)
}
