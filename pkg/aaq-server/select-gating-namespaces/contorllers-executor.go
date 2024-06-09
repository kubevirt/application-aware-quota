package select_gating_namespaces

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

import (
	"context"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/clusterquotamapping"
	"kubevirt.io/application-aware-quota/pkg/aaq-server/select-gating-namespaces/arq-selected-namespaces-controller"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/informers"
	golog "log"
)

type AaqServerControllerExecutor struct {
	enableClusterQuota              bool
	aaqCli                          client.AAQClient
	arqSelectedNamespacesController *arq_selected_namespaces_controller.ArqSelectedNamespacesController
	clusterQuotaMappingController   *clusterquotamapping.ClusterQuotaMappingController
	clusterQuotaMapper              clusterquotamapping.ClusterQuotaMapper
	safeNamespaceSet                *arq_selected_namespaces_controller.NamespaceSet
	arqInformer                     cache.SharedIndexInformer
	acrqInformer                    cache.SharedIndexInformer
	nsInformer                      cache.SharedIndexInformer
	stop                            <-chan struct{}
}

func NewSelectedNamespaceControllerExecutor(clusterQuotaEnabled bool, stop <-chan struct{}) *AaqServerControllerExecutor {
	var err error
	var app = &AaqServerControllerExecutor{}
	app.enableClusterQuota = clusterQuotaEnabled

	app.aaqCli, err = client.GetAAQClient()
	if err != nil {
		golog.Fatalf("AAQClient: %v", err)
	}
	app.arqInformer = informers.GetApplicationAwareResourceQuotaInformer(app.aaqCli)
	app.nsInformer = informers.GetNamespaceInformer(app.aaqCli)

	if app.enableClusterQuota {
		app.acrqInformer = informers.GetApplicationAwareClusterResourceQuotaInformer(app.aaqCli)
		app.initClusterQuotaMappingController()
		app.clusterQuotaMapper = app.clusterQuotaMappingController.GetClusterQuotaMapper()
	}

	app.initArqController()

	return app
}

func (ce *AaqServerControllerExecutor) initArqController() {
	ce.arqSelectedNamespacesController = arq_selected_namespaces_controller.NewArqSelectedNamespacesController(
		ce.arqInformer,
		ce.stop,
	)
}

func (ce *AaqServerControllerExecutor) initClusterQuotaMappingController() {
	ce.clusterQuotaMappingController = clusterquotamapping.NewClusterQuotaMappingController(
		ce.nsInformer,
		ce.acrqInformer,
		ce.stop,
	)
}

func (ce *AaqServerControllerExecutor) Run() {
	go ce.arqInformer.Run(ce.stop)
	go ce.nsInformer.Run(ce.stop)

	if !cache.WaitForCacheSync(ce.stop,
		ce.arqInformer.HasSynced,
		ce.nsInformer.HasSynced,
	) {
		klog.Warningf("failed to wait for caches to sync")
	}
	if ce.enableClusterQuota {
		go ce.acrqInformer.Run(ce.stop)
		if !cache.WaitForCacheSync(ce.stop,
			ce.acrqInformer.HasSynced,
		) {
			klog.Warningf("failed to wait for caches to sync")
		}
		go func() {
			ce.clusterQuotaMappingController.Run(3)
		}()
	}

	go func() {
		ce.arqSelectedNamespacesController.Run(context.Background(), 3)
	}()
}

type QuotaNamespaceChecker interface {
	IsSelectedNamespace(ns string) bool
}

type AAQQuotaNamespaceChecker struct {
	nsSet              *arq_selected_namespaces_controller.NamespaceSet
	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper
	enableClusterQuota bool
}

func (qnsc *AAQQuotaNamespaceChecker) IsSelectedNamespace(ns string) bool {
	if qnsc.nsSet.Contains(ns) {
		return true
	}
	if qnsc.enableClusterQuota {
		quotas, _ := qnsc.clusterQuotaMapper.GetClusterQuotasFor(ns)
		return len(quotas) > 0
	}
	return false
}

func (ce *AaqServerControllerExecutor) GetQuotaNamespaceChecker() QuotaNamespaceChecker {
	return &AAQQuotaNamespaceChecker{
		ce.arqSelectedNamespacesController.GetNSSet(),
		ce.clusterQuotaMapper,
		ce.enableClusterQuota,
	}
}
