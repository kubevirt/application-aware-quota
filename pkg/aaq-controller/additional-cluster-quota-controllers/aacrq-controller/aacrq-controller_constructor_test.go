package aacrq_controller

import (
	"errors"
	"testing"

	testsutils "kubevirt.io/application-aware-quota/pkg/tests-utils"
)

func TestNewAacrqControllerPanicsOnAcrqHandlerRegistrationError(t *testing.T) {
	t.Helper()

	aacrqInformer := testsutils.NewFakeSharedIndexInformer(nil)
	acrqInformer := testsutils.NewFakeSharedIndexInformer(nil)
	acrqInformer.AddEventHandlerErr = errors.New("boom")

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when acrq informer handler registration fails")
		}
	}()

	NewAacrqController(nil, aacrqInformer, acrqInformer, make(chan struct{}))
}
