package client

import (
	"flag"
	"os"
	"sync"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"kubevirt.io/api/core"
	v1 "kubevirt.io/api/core/v1"
	generatedclient "kubevirt.io/applications-aware-quota/pkg/generated/aaq/clientset/versioned"
	kubevirtclient "kubevirt.io/applications-aware-quota/pkg/generated/kubevirt/clientset/versioned"
	applicationsResourceQuota "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core"
	v1alpha13 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
)

var (
	kubeconfig string
	master     string
)

var (
	SchemeBuilder  runtime.SchemeBuilder
	Scheme         *runtime.Scheme
	Codecs         serializer.CodecFactory
	ParameterCodec runtime.ParameterCodec
)

func init() {
	// This allows consumers of the KubeVirt client go package to
	// customize what version the client uses. Without specifying a
	// version, all versions are registered. While this techincally
	// file to register all versions, so k8s ecosystem libraries
	// do not work well with this. By explicitly setting the env var,
	// consumers of our client go can avoid these scenarios by only
	// registering a single version
	registerVersion := os.Getenv(v1.KubeVirtClientGoSchemeRegistrationVersionEnvVar)
	if registerVersion != "" {
		SchemeBuilder = runtime.NewSchemeBuilder(v1.AddKnownTypesGenerator([]schema.GroupVersion{schema.GroupVersion{Group: core.GroupName, Version: registerVersion}}),
			v1alpha13.AddKnownTypesGenerator([]schema.GroupVersion{schema.GroupVersion{Group: applicationsResourceQuota.GroupName, Version: applicationsResourceQuota.LatestVersion}}))
	} else {
		SchemeBuilder = runtime.NewSchemeBuilder(v1.AddKnownTypesGenerator(v1.GroupVersions),
			v1alpha13.AddKnownTypesGenerator([]schema.GroupVersion{schema.GroupVersion{Group: applicationsResourceQuota.GroupName, Version: applicationsResourceQuota.LatestVersion}}))
	}
	Scheme = runtime.NewScheme()
	AddToScheme := SchemeBuilder.AddToScheme
	Codecs = serializer.NewCodecFactory(Scheme)
	ParameterCodec = runtime.NewParameterCodec(Scheme)
	AddToScheme(Scheme)
	AddToScheme(scheme.Scheme)
}

type RestConfigHookFunc func(*rest.Config)

var restConfigHooks []RestConfigHookFunc
var restConfigHooksLock sync.Mutex

var aaqclient AAQClient
var once sync.Once

// Init adds the default `kubeconfig` and `master` flags. It is not added by default to allow integration into
// the different controller generators which normally add these flags too.
func Init() {
	if flag.CommandLine.Lookup("kubeconfig") == nil {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	if flag.CommandLine.Lookup("master") == nil {
		flag.StringVar(&master, "master", "", "master url")
	}
}

func RegisterRestConfigHook(fn RestConfigHookFunc) {
	restConfigHooksLock.Lock()
	defer restConfigHooksLock.Unlock()

	restConfigHooks = append(restConfigHooks, fn)
}

func executeRestConfigHooks(config *rest.Config) {
	restConfigHooksLock.Lock()
	defer restConfigHooksLock.Unlock()

	for _, hookFn := range restConfigHooks {
		hookFn(config)
	}
}

func FlagSet() *flag.FlagSet {
	set := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	set.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	set.StringVar(&master, "master", "", "master url")
	return set
}

// DefaultClientConfig creates a clientcmd.ClientConfig with the following hierarchy:
//
//  1. Use the kubeconfig builder.  The number of merges and overrides here gets a little crazy.  Stay with me.
//
//  1. Merge the kubeconfig itself.  This is done with the following hierarchy rules:
//
//  1. CommandLineLocation - this parsed from the command line, so it must be late bound.  If you specify this,
//     then no other kubeconfig files are merged.  This file must exist.
//
//  2. If $KUBECONFIG is set, then it is treated as a list of files that should be merged.
//
//  3. HomeDirectoryLocation
//     Empty filenames are ignored.  Files with non-deserializable content produced errors.
//     The first file to set a particular value or map key wins and the value or map key is never changed.
//     This means that the first file to set CurrentContext will have its context preserved.  It also means
//     that if two files specify a "red-user", only values from the first file's red-user are used.  Even
//     non-conflicting entries from the second file's "red-user" are discarded.
//
//  2. Determine the context to use based on the first hit in this chain
//
//  1. command line argument - again, parsed from the command line, so it must be late bound
//
//  2. CurrentContext from the merged kubeconfig file
//
//  3. Empty is allowed at this stage
//
//  3. Determine the cluster info and auth info to use.  At this point, we may or may not have a context.  They
//     are built based on the first hit in this chain.  (run it twice, once for auth, once for cluster)
//
//  1. command line argument
//
//  2. If context is present, then use the context value
//
//  3. Empty is allowed
//
//  4. Determine the actual cluster info to use.  At this point, we may or may not have a cluster info.  Build
//     each piece of the cluster info based on the chain:
//
//  1. command line argument
//
//  2. If cluster info is present and a value for the attribute is present, use it.
//
//  3. If you don't have a server location, bail.
//
//  5. Auth info is build using the same rules as cluster info, EXCEPT that you can only have one authentication
//     technique per auth info.  The following conditions result in an error:
//
//  1. If there are two conflicting techniques specified from the command line, fail.
//
//  2. If the command line does not specify one, and the auth info has conflicting techniques, fail.
//
//  3. If the command line specifies one and the auth info specifies another, honor the command line technique.
//
//  2. Use default values and potentially prompt for auth information
//
//     However, if it appears that we're running in a kubernetes cluster
//     container environment, then run with the auth info kubernetes mounted for
//     us. Specifically:
//     The env vars KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are
//     set, and the file /var/run/secrets/kubernetes.io/serviceaccount/token
//     exists and is not a directory.
//
// Initially copied from https://github.com/kubernetes/kubernetes/blob/09f321c80bfc9bca63a5530b56d7a1a3ba80ba9b/pkg/kubectl/cmd/util/factory_client_access.go#L174
func DefaultClientConfig(flags *pflag.FlagSet) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// use the standard defaults for this client command
	// DEPRECATED: remove and replace with something more accurate
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig

	flags.StringVar(&loadingRules.ExplicitPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")

	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}

	flagNames := clientcmd.RecommendedConfigOverrideFlags("")
	// short flagnames are disabled by default.  These are here for compatibility with existing scripts
	flagNames.ClusterOverrideFlags.APIServer.ShortName = "s"

	clientcmd.BindOverrideFlags(overrides, flags, flagNames)
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, os.Stdin)

	return clientConfig
}

// this function is defined as a closure so iut could be overwritten by unit tests
var GetAAQClientFromClientConfig = func(cmdConfig clientcmd.ClientConfig) (AAQClient, error) {
	config, err := cmdConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return GetAAQClientFromRESTConfig(config)

}

func GetAAQClientFromRESTConfig(config *rest.Config) (AAQClient, error) {
	shallowCopy := *config
	shallowCopy.GroupVersion = &v1alpha13.SchemeGroupVersion
	shallowCopy.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: Codecs}
	shallowCopy.APIPath = "/apis"
	shallowCopy.ContentType = runtime.ContentTypeJSON
	if config.UserAgent == "" {
		config.UserAgent = restclient.DefaultKubernetesUserAgent()
	}

	executeRestConfigHooks(&shallowCopy)

	restClient, err := rest.RESTClientFor(&shallowCopy)
	if err != nil {
		return nil, err
	}

	coreClient, err := kubernetes.NewForConfig(&shallowCopy)
	if err != nil {
		return nil, err
	}

	generatedAAQClient, err := generatedclient.NewForConfig(&shallowCopy)
	if err != nil {
		return nil, err
	}

	kubevirtClient, err := kubevirtclient.NewForConfig(&shallowCopy)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(&shallowCopy)
	if err != nil {
		return nil, err
	}

	return &aaq{
		master,
		kubeconfig,
		restClient,
		&shallowCopy,
		generatedAAQClient,
		kubevirtClient,
		dynamicClient,
		coreClient,
	}, nil
}

func GetAAQClientFromFlags(master string, kubeconfig string) (AAQClient, error) {
	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		return nil, err
	}
	return GetAAQClientFromRESTConfig(config)
}

func GetAAQClient() (AAQClient, error) {
	var err error
	once.Do(func() {
		aaqclient, err = GetAAQClientFromFlags(master, kubeconfig)
	})
	return aaqclient, err
}

// Deprecated: Use GetKubevirtClientConfig instead
func GetConfig() (*restclient.Config, error) {
	return clientcmd.BuildConfigFromFlags(master, kubeconfig)
}

func GetAAQClientConfig() (*rest.Config, error) {
	return GetConfig()
}
