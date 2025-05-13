package aaq_operator

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
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
	r.reconciler.AddCallback(&appsv1.Deployment{}, reconcileCreatePrometheusInfra)
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

func isControllerDeployment(d *appsv1.Deployment) bool {
	return d.Name == "aaq-controller"
}

func reconcileCreatePrometheusInfra(args *callbacks.ReconcileCallbackArgs) error {
	if args.State != callbacks.ReconcileStatePostRead {
		return nil
	}

	deployment := args.CurrentObject.(*appsv1.Deployment)
	// we don't check sdk.CheckDeploymentReady(deployment) since we want Prometheus to cover NotReady state as well
	if !isControllerDeployment(deployment) {
		return nil
	}

	cr := args.Resource.(runtime.Object)
	namespace := deployment.GetNamespace()
	if namespace == "" {
		return fmt.Errorf("cluster scoped owner not supported")
	}

	if deployed, err := isPrometheusDeployed(args.Logger, args.Client, namespace); err != nil {
		return err
	} else if !deployed {
		return nil
	}

	if err := ensurePrometheusResourcesExist(context.TODO(), args.Client, args.Scheme, deployment); err != nil {
		args.Recorder.Event(cr, corev1.EventTypeWarning, createResourceFailed, fmt.Sprintf("Failed to ensure prometheus resources exists, %v", err))
		return err
	}

	return nil
}
