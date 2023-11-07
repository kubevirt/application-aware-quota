package tests_utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"time"
)

type FakeSharedIndexInformer struct {
	indexer            cache.Indexer
	InternalGetIndexer func(cache.Indexer) cache.Indexer
}

func NewFakeSharedIndexInformer(objs []metav1.Object) FakeSharedIndexInformer {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, obj := range objs {
		indexer.Add(obj)
	}
	return FakeSharedIndexInformer{indexer: indexer, InternalGetIndexer: selfGetIndexer}
}

func (i FakeSharedIndexInformer) RemoveEventHandler(handle cache.ResourceEventHandlerRegistration) error {
	//TODO implement me
	panic("implement me")
}

func (i FakeSharedIndexInformer) IsStopped() bool {
	panic("implement me")
}

func (i FakeSharedIndexInformer) AddIndexers(indexers cache.Indexers) error { return nil }
func (i FakeSharedIndexInformer) GetIndexer() cache.Indexer {
	return i.InternalGetIndexer(i.indexer)

}
func (i FakeSharedIndexInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return nil, nil
}
func (i FakeSharedIndexInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return nil, nil
}
func (i FakeSharedIndexInformer) GetStore() cache.Store           { return nil }
func (i FakeSharedIndexInformer) GetController() cache.Controller { return nil }
func (i FakeSharedIndexInformer) Run(stopCh <-chan struct{})      {}
func (i FakeSharedIndexInformer) HasSynced() bool                 { panic("implement me") }
func (i FakeSharedIndexInformer) LastSyncResourceVersion() string { return "" }
func (i FakeSharedIndexInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	return nil
}
func (i FakeSharedIndexInformer) SetTransform(f cache.TransformFunc) error {
	return nil
}

func selfGetIndexer(i cache.Indexer) cache.Indexer { //for testing
	return i
}
