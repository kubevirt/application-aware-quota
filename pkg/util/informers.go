package util

import (
	"context"
	v1 "k8s.io/api/core/v1"
	k8sv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	k6tv1 "kubevirt.io/api/core/v1"
	rq_controller "kubevirt.io/applications-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/applications-aware-quota/pkg/client"
	v1alpha13 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

const LauncherLabel = "virt-launcher"

func GetMigrationInformer(aaqCli client.AAQClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.KubevirtClient().KubevirtV1().RESTClient(), "virtualmachineinstancemigrations", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &k6tv1.VirtualMachineInstanceMigration{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetApplicationsResourceQuotaInformer(aaqCli client.AAQClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.RestClient(), "applicationsresourcequotas", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1alpha13.ApplicationsResourceQuota{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetAAQJobQueueConfig(aaqCli client.AAQClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.RestClient(), "aaqjobqueueconfigs", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1alpha13.AAQJobQueueConfig{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetPodInformer(aaqCli client.AAQClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.CoreV1().RESTClient(), "pods", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1.Pod{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetSecretInformer(aaqCli client.AAQClient, ns string) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.CoreV1().RESTClient(), "secrets", ns, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &v1.Secret{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetVMIInformer(aaqCli client.AAQClient) cache.SharedIndexInformer {
	listWatcher := NewListWatchFromClient(aaqCli.KubevirtClient().KubevirtV1().RESTClient(), "virtualmachineinstances", k8sv1.NamespaceAll, fields.Everything(), labels.Everything())
	return cache.NewSharedIndexInformer(listWatcher, &k6tv1.VirtualMachineInstance{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func GetResourceQuotaInformer(aaqCli client.AAQClient) cache.SharedIndexInformer {
	labelSelector, err := labels.Parse(rq_controller.RQLabel)
	if err != nil {
		panic(err)
	}
	listWatcher := NewListWatchFromClient(aaqCli.CoreV1().RESTClient(), "resourcequotas", k8sv1.NamespaceAll, fields.Everything(), labelSelector)
	return cache.NewSharedIndexInformer(listWatcher, &v1.ResourceQuota{}, 1*time.Hour, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
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
