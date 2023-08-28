package main

import (
	"context"
	"github.com/pkg/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/namespaced"
	"kubevirt.io/applications-aware-quota/pkg/aaq-server"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/kubevirt/pkg/certificates/bootstrap"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	defer klog.Flush()
	aaqNS := util.GetNamespace()

	virtCli, err := util.GetVirtCli()
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}
	ctx := signals.SetupSignalHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := ctx.Done()
	if err := util.CreateReadyFile(); err != nil {
		klog.Fatalf("Error creating ready file: %+v", err)
	}

	secretInformer := util.GetSecretInformer(virtCli, aaqNS)
	go secretInformer.Run(stop)
	if !cache.WaitForCacheSync(stop, secretInformer.HasSynced) {
		os.Exit(1)
	}

	secretCertManager := bootstrap.NewFallbackCertificateManager(
		bootstrap.NewSecretCertificateManager(
			namespaced.SecretResourceName,
			aaqNS,
			secretInformer.GetStore(),
		),
	)

	secretCertManager.Start()
	defer secretCertManager.Stop()

	aaqServer, err := aaq_server.AaqServer(util.DefaultHost,
		util.DefaultPort,
		secretCertManager,
	)
	if err != nil {
		klog.Fatalf("UploadProxy failed to initialize: %v\n", errors.WithStack(err))
	}

	err = aaqServer.Start()
	if err != nil {
		klog.Fatalf("TLS server failed: %v\n", errors.WithStack(err))
	}

}
