package util

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8sv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	k6tv1 "kubevirt.io/api/core/v1"
	v1alpha12 "kubevirt.io/applications-aware-quota/pkg/generated/clientset/versioned/typed/core/v1alpha1"
	v1alpha13 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"time"
)

const launcherLabel = "virt-launcher"

func GetVirtCli() (kubecli.KubevirtClient, error) {
	clientConfig, err := kubecli.GetKubevirtClientConfig()
	if err != nil {
		return nil, err
	}

	virtCli, err := kubecli.GetKubevirtClientFromRESTConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return virtCli, err
}

func GetAAQCli() v1alpha12.AaqV1alpha1Client {
	cfg, err := kubecli.GetKubevirtClientConfig()
	if err != nil {
		klog.Fatalf("Unable to get kube config: %v\n", errors.WithStack(err))
	}
	AAQCli := v1alpha12.NewForConfigOrDie(cfg)
	return *AAQCli
}

func GetMigrationInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(virtCli.RestClient(), "virtualmachineinstancemigrations", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &k6tv1.VirtualMachineInstanceMigration{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetApplicationsResourceQuotaInformer(aaqCli v1alpha12.AaqV1alpha1Client) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.RESTClient(), "applicationsresourcequotas", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1alpha13.ApplicationsResourceQuota{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetLauncherPodInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	labelSelector, err := labels.Parse(fmt.Sprintf(k6tv1.AppLabel+" in (%s)", launcherLabel))
	if err != nil {
		panic(err)
	}
	listWatcher := NewListWatchFromClient(virtCli.CoreV1().RESTClient(), "pods", k8sv1.NamespaceAll, fields.Everything(), labelSelector)
	return cache.NewSharedIndexInformer(listWatcher, &v1.Pod{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetSecretInformer(virtCli kubecli.KubevirtClient, ns string) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(virtCli.CoreV1().RESTClient(), "secrets", ns, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1.Secret{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetVMIInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(virtCli.RestClient(), "virtualmachineinstances", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &k6tv1.VirtualMachineInstance{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func KubeVirtInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(virtCli.RestClient(), "kubevirts", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &k6tv1.KubeVirt{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func CRDInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	ext, err := extclient.NewForConfig(virtCli.Config())
	if err != nil {
		panic(err)
	}
	restClient := ext.ApiextensionsV1().RESTClient()
	lw := cache.NewListWatchFromClient(restClient, "customresourcedefinitions", k8sv1.NamespaceAll, fields.Everything())
	return cache.NewSharedIndexInformer(lw, &extv1.CustomResourceDefinition{}, 1*time.Hour, cache.Indexers{})
}

func GetResourceQuotaInformer(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(virtCli.CoreV1().RESTClient(), "resourcequotas", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1.ResourceQuota{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func PersistentVolumeClaim(virtCli kubecli.KubevirtClient) cache.SharedIndexInformer {
	lw := cache.NewListWatchFromClient(virtCli.CoreV1().RESTClient(), "persistentvolumeclaims", k8sv1.NamespaceAll, fields.Everything())
	return cache.NewSharedIndexInformer(lw, &v1.PersistentVolumeClaim{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

// NewListWatchFromClient creates a new ListWatch from the specified client, resource, kubevirtNamespace and field selector.
func NewListWatchFromClient(c cache.Getter, resource string, namespace string, fieldSelector fields.Selector, labelSelector labels.Selector) *cache.ListWatch {
	listFunc := func(options k8sv1.ListOptions) (runtime.Object, error) {
		options.FieldSelector = fieldSelector.String()
		options.LabelSelector = labelSelector.String()
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, k8sv1.ParameterCodec).
			Do(context.Background()).
			Get()
	}
	watchFunc := func(options k8sv1.ListOptions) (watch.Interface, error) {
		options.FieldSelector = fieldSelector.String()
		options.LabelSelector = labelSelector.String()
		options.Watch = true
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, k8sv1.ParameterCodec).
			Watch(context.Background())
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}
