package aaq_operator

import (
	"context"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"kubevirt.io/application-aware-quota/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func (r *ReconcileAAQ) watchAAQCRD() error {
	if err := r.controller.Watch(source.Kind(r.getCache(), &extv1.CustomResourceDefinition{}), handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			name := obj.GetName()
			if name != "aaqs.aaq.kubevirt.io" {
				return nil
			}
			cr, err := util.GetActiveAAQ(r.client)
			if err != nil {
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "",
						Name:      cr.Name,
					},
				},
			}
		},
	)); err != nil {
		return err
	}

	return nil
}
