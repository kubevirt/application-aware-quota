package crq_controller

import (
	"errors"
	"testing"

	"k8s.io/client-go/tools/cache"
	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
)

type erroringSharedIndexInformer struct {
	testsutils.FakeSharedIndexInformer
	err error
}

func (i erroringSharedIndexInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return nil, i.err
}

func TestNewCRQControllerPanicsOnAcrqHandlerRegistrationError(t *testing.T) {
	t.Helper()

	crqInformer := testsutils.NewFakeSharedIndexInformer(nil)
	acrqInformer := erroringSharedIndexInformer{
		FakeSharedIndexInformer: testsutils.NewFakeSharedIndexInformer(nil),
		err:                     errors.New("boom"),
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when acrq informer handler registration fails")
		}
	}()

	NewCRQController(nil, crqInformer, acrqInformer, make(chan struct{}))
}
