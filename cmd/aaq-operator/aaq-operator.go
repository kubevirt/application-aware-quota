package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"

	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"go.uber.org/zap/zapcore"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	controller "kubevirt.io/application-aware-quota/pkg/aaq-operator"
	"kubevirt.io/application-aware-quota/pkg/client"
	tlscryptowatch "kubevirt.io/application-aware-quota/pkg/tls-crypto-watch"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"net/http"
	"os"
	"runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strconv"
)

var log = logf.Log.WithName("cmd")
var defVerbose = fmt.Sprintf("%d", 1) // note flag values are strings

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func init() {
	// Define a flag named "v" with a default value of false and a usage message.
	flag.String("v", defVerbose, "Verbosity level")
}

func main() {
	flag.Parse()
	verbose := defVerbose
	// visit actual flags passed in and if passed check -v and set verbose
	if verboseEnvVarVal := os.Getenv("VERBOSITY"); verboseEnvVarVal != "" {
		verbose = verboseEnvVarVal
	}
	if verbose == defVerbose {
		log.V(1).Info(fmt.Sprintf("Note: increase the -v level in the controller deployment for more detailed logging, eg. -v=%d or -v=%d\n", 2, 3))
	}
	verbosityLevel, err := strconv.Atoi(verbose)
	debug := false
	if err == nil && verbosityLevel > 1 {
		debug = true
	}

	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.New(zap.Level(zapcore.Level(-1*verbosityLevel)), zap.UseDevMode(debug)))

	printVersion()

	util.PrintVersion()
	namespace := util.GetNamespace()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	aaqClient, err := client.GetAAQClientFromRESTConfig(cfg)
	if err != nil {
		log.Error(err, "Failed to create AAQ client")
		os.Exit(1)
	}

	tlsWatcher, err := tlscryptowatch.NewAaqConfigTLSWatcher(context.Background(), aaqClient)
	if err != nil {
		log.Error(err, "Failed to create TLS watcher")
		os.Exit(1)
	}

	managerOpts := manager.Options{
		Cache:                      cache.Options{DefaultNamespaces: getCacheConfig([]string{namespace})},
		LeaderElection:             true,
		LeaderElectionNamespace:    namespace,
		LeaderElectionID:           "aaq-operator-leader-election-helper",
		LeaderElectionResourceLock: "leases",
		HealthProbeBindAddress:     ":8081",
		Metrics: metricsserver.Options{
			BindAddress:   ":8443",
			SecureServing: true,
			TLSOpts: []func(*tls.Config){func(c *tls.Config) {
				// Disable HTTP/2 to prevent rapid reset vulnerability
				// See CVE-2023-44487, CVE-2023-39325
				c.NextProtos = []string{"http/1.1"}

				c.GetConfigForClient = func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
					config := c.Clone()
					cryptoConfig := tlsWatcher.GetTLSConfig()
					config.MinVersion = cryptoConfig.MinVersion
					config.CipherSuites = cryptoConfig.CipherSuites
					return config, nil
				}
			}},
		},
	}

	// Create a new Manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, managerOpts)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	_ = mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		return nil
	})
	_ = mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		return nil
	})

	log.Info("Registering Components.")
	if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := secv1.Install(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := extv1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := promv1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Setup the controller
	if err := controller.Add(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Starting the Manager.")

	// Start the Manager
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}
}

func getCacheConfig(namespaces []string) map[string]cache.Config {
	cacheConfig := map[string]cache.Config{}

	for _, namespace := range namespaces {
		cacheConfig[namespace] = cache.Config{}
	}

	return cacheConfig
}
