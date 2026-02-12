package main

import (
	"context"
	goflag "flag"
	"github.com/pkg/errors"
	flag "github.com/spf13/pflag"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kubevirt.io/application-aware-quota/pkg/aaq-server"
	"kubevirt.io/application-aware-quota/pkg/certificates/bootstrap"
	"kubevirt.io/application-aware-quota/pkg/client"
	"kubevirt.io/application-aware-quota/pkg/informers"
	tlscryptowatch "kubevirt.io/application-aware-quota/pkg/tls-crypto-watch"
	"kubevirt.io/application-aware-quota/pkg/util"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	flag.CommandLine.AddGoFlag(goflag.CommandLine.Lookup("v"))
	isOnOpenshift := flag.Bool(util.IsOnOpenshift, false, "flag that suggest that we are on Openshift cluster")
	flag.Parse()
	defer klog.Flush()
	aaqNS := util.GetNamespace()

	aaqCli, err := client.GetAAQClient()
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}
	ctx := signals.SetupSignalHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := ctx.Done()

	secretInformer := informers.GetSecretInformer(aaqCli, aaqNS)
	go secretInformer.Run(stop)
	if !cache.WaitForCacheSync(stop, secretInformer.HasSynced) {
		os.Exit(1)
	}

	secretCertManager := bootstrap.NewFallbackCertificateManager(
		bootstrap.NewSecretCertificateManager(
			util.SecretResourceName,
			aaqNS,
			secretInformer.GetStore(),
		),
	)

	secretCertManager.Start()
	defer secretCertManager.Stop()

	tlsWatcher, err := tlscryptowatch.NewAaqConfigTLSWatcher(ctx, aaqCli)
	if err != nil {
		klog.Fatalf("Failed to create TLS watcher: %v\n", errors.WithStack(err))
	}

	aaqServer, err := aaq_server.AaqServer(aaqNS,
		util.DefaultHost,
		util.DefaultPort,
		secretCertManager,
		aaqCli,
		*isOnOpenshift,
		tlsWatcher,
	)
	if err != nil {
		klog.Fatalf("UploadProxy failed to initialize: %v\n", errors.WithStack(err))
	}

	err = aaqServer.Start()
	if err != nil {
		klog.Fatalf("TLS server failed: %v\n", errors.WithStack(err))
	}

}
