package crq_controller

import (
	"errors"
	"testing"

	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
)

func TestNewCRQControllerPanicsOnAcrqHandlerRegistrationError(t *testing.T) {
	t.Helper()

	crqInformer := testsutils.NewFakeSharedIndexInformer(nil)
	acrqInformer := testsutils.NewFakeSharedIndexInformer(nil)
	acrqInformer.AddEventHandlerErr = errors.New("boom")

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when acrq informer handler registration fails")
		}
	}()

	NewCRQController(nil, crqInformer, acrqInformer, make(chan struct{}))
}
