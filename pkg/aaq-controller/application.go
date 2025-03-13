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
	"context"
	goflag "flag"
	"fmt"
	"io/ioutil"
	golog "log"
	"net/http"
	"os"
	"strconv"

	"github.com/emicklei/go-restful/v3"
	flag "github.com/spf13/pflag"
	k8sv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	v14 "k8s.io/client-go/kubernetes/typed/core/v1"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	aaq_evaluator "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-evaluator"
	arq_controller2 "kubevirt.io/application-aware-quota/pkg/aaq-controller/aaq-gate-controller"
	aacrq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/aacrq-controller"
	acrq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/acrq-controller"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/clusterquotamapping"
	crq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/additional-cluster-quota-controllers/crq-controller"
	arq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/arq-controller"
	built_in_usage_calculators "kubevirt.io/application-aware-quota/pkg/aaq-controller/built-in-usage-calculators"
	"kubevirt.io/application-aware-quota/pkg/aaq-controller/leaderelectionconfig"
	rq_controller "kubevirt.io/application-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/application-aware-quota/pkg/certificates/bootstrap"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/generated/aaq/listers/core/v1alpha1"
	"kubevirt.io/application-aware-quota/pkg/informers"
	"kubevirt.io/application-aware-quota/pkg/util"
	v1alpha12 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

type AaqControllerApp struct {
	ctx                           context.Context
	enableClusterQuota            bool
	onOpenshift                   bool
	aaqNs                         string
	host                          string
	LeaderElection                leaderelectionconfig.Configuration
	aaqCli                        client.AAQClient
	arqController                 *arq_controller.ArqController
	acrqController                *acrq_controller.AcrqController
	aacrqController               *aacrq_controller.AacrqController
	clusterQuotaMappingController *clusterquotamapping.ClusterQuotaMappingController
	aaqGateController             *arq_controller2.AaqGateController
	rqController                  *rq_controller.RQController
	crqController                 *crq_controller.CRQController
	podInformer                   cache.SharedIndexInformer
	arqInformer                   cache.SharedIndexInformer
	aaqInformer                   cache.SharedIndexInformer
	rqInformer                    cache.SharedIndexInformer
	aaqjqcInformer                cache.SharedIndexInformer
	crqInformer                   cache.SharedIndexInformer
	acrqInformer                  cache.SharedIndexInformer
	aacrqInformer                 cache.SharedIndexInformer
	nsInformer                    cache.SharedIndexInformer
	recorder                      record.EventRecorder
	calcRegistry                  *aaq_evaluator.AaqEvaluatorRegistry
	readyChan                     chan bool
	leaderElector                 *leaderelection.LeaderElector
}

func Execute() {
	flag.CommandLine.AddGoFlag(goflag.CommandLine.Lookup("v"))
	isOnOpenshift := flag.Bool(util.IsOnOpenshift, false, "flag that suggest that we are on Openshift cluster")
	clusterQuotaEnabled := flag.Bool(util.EnableClusterQuota, false, "flag that to let us know if we should enable clusterQuota controllers")
	launcherConfig := flag.String(util.VMICalculatorConfiguration, "", "flag that to let us know how to allocate resource for virtual machines") //todo: should delete this once sidecar evaluators are in
	numberOfRequestedEvaluatorsSidecars := flag.Uint(util.SidecarEvaluatorsNumberFlag, 0, "number of requested evaluators sidecars")

	flag.Parse()
	var err error
	var app = AaqControllerApp{}

	app.LeaderElection = leaderelectionconfig.DefaultLeaderElectionConfiguration()
	app.readyChan = make(chan bool, 1)
	app.onOpenshift = *isOnOpenshift
	app.enableClusterQuota = *clusterQuotaEnabled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.ctx = ctx

	webService := new(restful.WebService)
	webService.Path("/").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	webService.Route(webService.GET("/leader").To(app.leaderProbe).Doc("Leader endpoint"))
	restful.Add(webService)

	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		panic(err)
	}
	app.aaqNs = string(nsBytes)

	host, err := os.Hostname()
	if err != nil {
		golog.Fatalf("unable to get hostname: %v", err)
	}
	app.host = host

	app.aaqCli, err = client.GetAAQClient()
	if err != nil {
		golog.Fatalf("AAQClient: %v", err)
	}
	app.arqInformer = informers.GetApplicationAwareResourceQuotaInformer(app.aaqCli)
	app.rqInformer = informers.GetResourceQuotaInformer(app.aaqCli)
	app.aaqjqcInformer = informers.GetAAQJobQueueConfig(app.aaqCli)
	app.podInformer = informers.GetPodInformer(app.aaqCli)
	app.aaqInformer = informers.GetAAQInformer(app.aaqCli)
	app.nsInformer = informers.GetNamespaceInformer(app.aaqCli)
	// Create event recorder
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: app.aaqCli.CoreV1().Events(v1.NamespaceAll)})
	app.recorder = broadcaster.NewRecorder(scheme.Scheme, k8sv1.EventSource{Component: "aaq-controller"})

	// Block until all requested evaluatorsSidecars are ready
	evaluatorsRegistry := aaq_evaluator.GetAaqEvaluatorsRegistry()
	stop := ctx.Done()

	err = evaluatorsRegistry.Collect(*numberOfRequestedEvaluatorsSidecars, util.DefaultSidecarsEvaluatorsStartTimeout)
	if err != nil {
		panic(err)
	}
	if v1alpha12.VmiCalcConfigName(*launcherConfig) != v1alpha12.IgnoreVmiCalculator {
		vmiInformer := informers.GetVMIInformer(app.aaqCli)
		migrationInformer := informers.GetMigrationInformer(app.aaqCli)
		go migrationInformer.Run(stop)
		go vmiInformer.Run(stop)

		if !cache.WaitForCacheSync(stop,
			migrationInformer.HasSynced,
			vmiInformer.HasSynced,
		) {
			klog.Warningf("failed to wait for caches to sync")
		}
		evaluatorsRegistry.Add(built_in_usage_calculators.NewVirtLauncherCalculator(vmiInformer, migrationInformer, v1alpha12.VmiCalcConfigName(*launcherConfig)))
	}
	switch v1alpha12.VmiCalcConfigName(*launcherConfig) {
	case v1alpha12.VirtualResources, v1alpha12.DedicatedVirtualResources:
		evaluatorsRegistry.Add(built_in_usage_calculators.NewCDIFilterCalculator(v1alpha12.VirtualResources))
	}
	app.calcRegistry = evaluatorsRegistry
	namespaceLister := v12.NewNamespaceLister(app.nsInformer.GetIndexer())

	var clusterQuotaLister v1alpha1.ApplicationAwareClusterResourceQuotaLister
	var clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper

	if app.enableClusterQuota {
		app.acrqInformer = informers.GetApplicationAwareClusterResourceQuotaInformer(app.aaqCli)
		app.aacrqInformer = informers.GetApplicationAwareAppliedClusterResourceQuotaInformer(app.aaqCli)
		if app.onOpenshift {
			app.crqInformer = informers.GetClusterResourceQuotaInformer(app.aaqCli)
			app.initCRQController(stop)
		}
		app.initAacrqController(stop)
		app.initClusterQuotaMappingController(stop)
		app.initAcrqController(stop, app.clusterQuotaMappingController.GetClusterQuotaMapper(), namespaceLister)
		app.clusterQuotaMappingController.GetClusterQuotaMapper().AddListener(app.acrqController)
		clusterQuotaLister = v1alpha1.NewApplicationAwareClusterResourceQuotaLister(app.acrqInformer.GetIndexer())
		clusterQuotaMapper = app.clusterQuotaMappingController.GetClusterQuotaMapper()
	}

	app.initArqController(stop, namespaceLister)
	app.initAaqGateController(stop, clusterQuotaLister, namespaceLister, clusterQuotaMapper)
	app.initRQController(stop, namespaceLister)

	if app.enableClusterQuota {
		app.clusterQuotaMappingController.GetClusterQuotaMapper().AddListener(app.aaqGateController)
	}

	app.Run(stop)

	klog.V(2).Infoln("AAQ controller exited")
}

func (mca *AaqControllerApp) leaderProbe(_ *restful.Request, response *restful.Response) {
	res := map[string]interface{}{}
	select {
	case _, opened := <-mca.readyChan:
		if !opened {
			res["apiserver"] = map[string]interface{}{"leader": "true"}
			if err := response.WriteHeaderAndJson(http.StatusOK, res, restful.MIME_JSON); err != nil {
				klog.Warningf("failed to return 200 OK reply: %v", err)
			}
			return
		}
	default:
	}
	res["apiserver"] = map[string]interface{}{"leader": "false"}
	if err := response.WriteHeaderAndJson(http.StatusOK, res, restful.MIME_JSON); err != nil {
		klog.Warningf("failed to return 200 OK reply: %v", err)
	}
}

func (mca *AaqControllerApp) initArqController(
	stop <-chan struct{},
	namespaceLister v12.NamespaceLister,
) {
	mca.arqController = arq_controller.NewArqController(mca.aaqCli,
		mca.podInformer,
		mca.arqInformer,
		mca.rqInformer,
		mca.aaqjqcInformer,
		mca.calcRegistry,
		namespaceLister,
		stop,
	)
}

func (mca *AaqControllerApp) initAcrqController(
	stop <-chan struct{},
	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper,
	namespaceLister v12.NamespaceLister,
) {
	mca.acrqController = acrq_controller.NewAcrqController(mca.aaqCli,
		clusterQuotaMapper,
		mca.acrqInformer,
		mca.crqInformer,
		mca.podInformer,
		mca.aaqjqcInformer,
		mca.calcRegistry,
		namespaceLister,
		stop,
		mca.onOpenshift,
	)
}

func (mca *AaqControllerApp) initAacrqController(stop <-chan struct{}) {
	mca.aacrqController = aacrq_controller.NewAacrqController(mca.aaqCli,
		mca.aacrqInformer,
		mca.acrqInformer,
		stop,
	)
}

func (mca *AaqControllerApp) initClusterQuotaMappingController(stop <-chan struct{}) {
	mca.clusterQuotaMappingController = clusterquotamapping.NewClusterQuotaMappingController(
		mca.nsInformer,
		mca.acrqInformer,
		stop,
	)
}

func (mca *AaqControllerApp) initAaqGateController(stop <-chan struct{},
	clusterQuotaLister v1alpha1.ApplicationAwareClusterResourceQuotaLister,
	namespaceLister v12.NamespaceLister,
	clusterQuotaMapper clusterquotamapping.ClusterQuotaMapper,
) {
	mca.aaqGateController = arq_controller2.NewAaqGateController(mca.aaqCli,
		mca.podInformer,
		mca.arqInformer,
		mca.aaqjqcInformer,
		mca.acrqInformer,
		mca.calcRegistry,
		clusterQuotaLister,
		namespaceLister,
		clusterQuotaMapper,
		mca.recorder,
		mca.enableClusterQuota,
		stop,
	)
}

func (mca *AaqControllerApp) initRQController(stop <-chan struct{},
	namespaceLister v12.NamespaceLister,
) {
	mca.rqController = rq_controller.NewRQController(mca.aaqCli,
		mca.rqInformer,
		mca.arqInformer,
		namespaceLister,
		stop,
	)
}

func (mca *AaqControllerApp) initCRQController(stop <-chan struct{}) {
	mca.crqController = crq_controller.NewCRQController(mca.aaqCli,
		mca.crqInformer,
		mca.acrqInformer,
		stop,
	)
}

func (mca *AaqControllerApp) Run(stop <-chan struct{}) {
	secretInformer := informers.GetSecretInformer(mca.aaqCli, mca.aaqNs)
	go secretInformer.Run(stop)
	if !cache.WaitForCacheSync(stop, secretInformer.HasSynced) {
		os.Exit(1)
	}

	secretCertManager := bootstrap.NewFallbackCertificateManager(
		bootstrap.NewSecretCertificateManager(
			util.SecretResourceName,
			mca.aaqNs,
			secretInformer.GetStore(),
		),
	)

	secretCertManager.Start()
	defer secretCertManager.Stop()

	tlsConfig := util.SetupTLS(secretCertManager)

	go func() {
		server := http.Server{
			Addr:      fmt.Sprintf("%s:%s", util.DefaultHost, strconv.Itoa(util.DefaultPort)),
			Handler:   http.DefaultServeMux,
			TLSConfig: tlsConfig,
		}
		if err := server.ListenAndServeTLS("", ""); err != nil {
			golog.Fatal(err)
		}
	}()
	if err := mca.setupLeaderElector(); err != nil {
		golog.Fatal(err)
	}
	mca.leaderElector.Run(mca.ctx)
	panic("unreachable")
}

func (mca *AaqControllerApp) setupLeaderElector() (err error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v14.EventSinkImpl{Interface: mca.aaqCli.CoreV1().Events(v1.NamespaceAll)})
	rl, err := resourcelock.New(mca.LeaderElection.ResourceLock,
		mca.aaqNs,
		"aaq-controller",
		mca.aaqCli.CoreV1(),
		mca.aaqCli.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      mca.host,
			EventRecorder: eventBroadcaster.NewRecorder(scheme.Scheme, k8sv1.EventSource{Component: "aaq-controller"}),
		})

	if err != nil {
		return
	}

	mca.leaderElector, err = leaderelection.NewLeaderElector(
		leaderelection.LeaderElectionConfig{
			Lock:          rl,
			LeaseDuration: mca.LeaderElection.LeaseDuration.Duration,
			RenewDeadline: mca.LeaderElection.RenewDeadline.Duration,
			RetryPeriod:   mca.LeaderElection.RetryPeriod.Duration,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: mca.onStartedLeading(),
				OnStoppedLeading: func() {
					golog.Fatal("leaderelection lost")
				},
			},
		})

	return
}

func (mca *AaqControllerApp) onStartedLeading() func(ctx context.Context) {
	return func(ctx context.Context) {
		stop := ctx.Done()
		go mca.podInformer.Run(stop)
		go mca.arqInformer.Run(stop)
		go mca.rqInformer.Run(stop)
		go mca.aaqjqcInformer.Run(stop)
		go mca.aaqInformer.Run(stop)
		go mca.nsInformer.Run(stop)

		if !cache.WaitForCacheSync(stop,
			mca.podInformer.HasSynced,
			mca.arqInformer.HasSynced,
			mca.aaqjqcInformer.HasSynced,
			mca.rqInformer.HasSynced,
			mca.aaqInformer.HasSynced,
			mca.nsInformer.HasSynced,
		) {
			klog.Warningf("failed to wait for caches to sync")
		}
		if mca.enableClusterQuota {
			go mca.acrqInformer.Run(stop)
			go mca.aacrqInformer.Run(stop)
			if !cache.WaitForCacheSync(stop,
				mca.acrqInformer.HasSynced,
				mca.aacrqInformer.HasSynced,
			) {
				klog.Warningf("failed to wait for caches to sync")
			}
			if mca.onOpenshift {
				go mca.crqInformer.Run(stop)
				if !cache.WaitForCacheSync(stop,
					mca.crqInformer.HasSynced,
				) {
					klog.Warningf("failed to wait for caches to sync")
				}
				go func() {
					mca.crqController.Run(3)
				}()
			}
			go func() {
				mca.clusterQuotaMappingController.Run(3)
			}()
			go func() {
				mca.acrqController.Run(context.Background(), 3)
			}()
			go func() {
				mca.aacrqController.Run(3)
			}()
		}

		go func() {
			mca.arqController.Run(context.Background(), 3)
		}()
		go func() {
			mca.aaqGateController.Run(context.Background(), 3)
		}()
		go func() {
			mca.rqController.Run(3)
		}()
		close(mca.readyChan)
	}
}
