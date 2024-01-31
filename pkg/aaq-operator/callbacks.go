package aaq_operator

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/callbacks"
)

const (
	createResourceFailed  = "CreateResourceFailed"
	createResourceSuccess = "CreateResourceSuccess"
)

func addReconcileCallbacks(r *ReconcileAAQ) {
	r.reconciler.AddCallback(&corev1.ServiceAccount{}, reconcileSCC)
}

func reconcileSCC(args *callbacks.ReconcileCallbackArgs) error {
	switch args.State {
	case callbacks.ReconcileStatePreCreate, callbacks.ReconcileStatePostRead:
	default:
		return nil
	}

	sa := args.DesiredObject.(*corev1.ServiceAccount)
	if sa.Name != util.ControllerServiceAccountName {
		return nil
	}

	cr := args.Resource.(runtime.Object)
	if err := ensureSCCExists(context.TODO(), args.Logger, args.Client, args.Namespace, util.ControllerServiceAccountName); err != nil {
		args.Recorder.Event(cr, corev1.EventTypeWarning, createResourceFailed, fmt.Sprintf("Failed to ensure SecurityContextConstraint exists, %v", err))
		return err
	}
	args.Recorder.Event(cr, corev1.EventTypeNormal, createResourceSuccess, "Successfully ensured SecurityContextConstraint exists")

	return nil
}
