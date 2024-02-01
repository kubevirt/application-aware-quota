package main

import (
	"flag"
	"fmt"
	secv1 "github.com/openshift/api/security/v1"
	"go.uber.org/zap/zapcore"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	controller "kubevirt.io/application-aware-quota/pkg/aaq-operator"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"os"
	"runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
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

	managerOpts := manager.Options{
		Cache:                      cache.Options{DefaultNamespaces: getCacheConfig([]string{namespace})},
		LeaderElection:             true,
		LeaderElectionNamespace:    namespace,
		LeaderElectionID:           "aaq-operator-leader-election-helper",
		LeaderElectionResourceLock: "leases",
	}

	// Create a new Manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, managerOpts)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

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
