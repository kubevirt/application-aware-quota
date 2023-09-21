package aaq_operator

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	rq_controller "kubevirt.io/applications-aware-quota/pkg/aaq-controller/rq-controller"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/callbacks"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	deleteResourceFailed  = "DeleteResourceFailed"
	deleteResourceSuccess = "DeleteResourceSuccess"
)

func addReconcileCallbacks(r *ReconcileAAQ) {
	r.reconciler.AddCallback(&appsv1.Deployment{}, reconcileDeleteControllerDeployment)
}

func reconcileDeleteControllerDeployment(args *callbacks.ReconcileCallbackArgs) error {
	switch args.State {
	case callbacks.ReconcileStatePostDelete, callbacks.ReconcileStateOperatorDelete:
	default:
		return nil
	}

	var deployment *appsv1.Deployment
	if args.DesiredObject != nil {
		deployment = args.DesiredObject.(*appsv1.Deployment)
	} else if args.CurrentObject != nil {
		deployment = args.CurrentObject.(*appsv1.Deployment)
	} else {
		args.Logger.Info("Received callback with no desired/current object")
		return nil
	}

	if !isControllerDeployment(deployment) {
		return nil
	}

	args.Logger.Info("Deleting AAQ all managed resourceQuotas")
	cr := args.Resource.(runtime.Object)
	if err := deleteWorkerResources(args.Client); err != nil {
		args.Logger.Error(err, "Error deleting worker resources")
		args.Recorder.Event(cr, corev1.EventTypeWarning, deleteResourceFailed, fmt.Sprintf("Failed to deleted worker resources %v", err))
		return err
	}
	args.Recorder.Event(cr, corev1.EventTypeNormal, deleteResourceSuccess, "Deleted worker resources successfully")

	return nil
}

func isControllerDeployment(d *appsv1.Deployment) bool {
	return d.Name == util.ControllerResourceName
}

func deleteWorkerResources(c client.Client) error {
	listTypes := []client.ObjectList{&corev1.ResourceQuotaList{}}
	ls, err := labels.Parse(rq_controller.RQLabel)

	if err != nil {
		return err
	}

	for _, lt := range listTypes {
		lo := &client.ListOptions{
			LabelSelector: ls,
		}
		if err := BulkDeleteResources(context.TODO(), c, lt, lo); err != nil {
			return err
		}
	}

	return nil
}

// BulkDeleteResources deletes a bunch of resources
func BulkDeleteResources(ctx context.Context, c client.Client, obj client.ObjectList, lo client.ListOption) error {
	if err := c.List(ctx, obj, lo); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}

	sv := reflect.ValueOf(obj).Elem()
	iv := sv.FieldByName("Items")

	for i := 0; i < iv.Len(); i++ {
		obj := iv.Index(i).Addr().Interface().(client.Object)
		if obj.GetDeletionTimestamp().IsZero() {
			klog.V(3).Infof("Deleting type %+v %+v", reflect.TypeOf(obj), obj)
			if err := c.Delete(ctx, obj); err != nil {
				return err
			}
		}
	}

	return nil
}
