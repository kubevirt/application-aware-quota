package aaq_operator

import (
	"fmt"
	"github.com/go-logr/logr"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"golang.org/x/net/context"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources/namespaced"
	"kubevirt.io/application-aware-quota/pkg/util"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ruleName = "prometheus-aaq-rules"
)

func ensurePrometheusResourcesExist(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object) error {
	namespace := owner.GetNamespace()

	cr, err := util.GetActiveAAQ(c)
	if err != nil {
		return err
	}
	if cr == nil {
		return fmt.Errorf("no active AAQ")
	}
	installerLabels := util.GetRecommendedInstallerLabelsFromCr(cr)

	prometheusResources := []client.Object{
		namespaced.NewPrometheusServiceMonitor(namespace),
		namespaced.NewPrometheusRole(namespace),
		namespaced.NewPrometheusRoleBinding(namespace),
		namespaced.NewPrometheusService(namespace),
	}

	for _, desired := range prometheusResources {
		if err := sdk.SetLastAppliedConfiguration(desired, LastAppliedConfigAnnotation); err != nil {
			return err
		}
		util.SetRecommendedLabels(desired, installerLabels, "aaq-operator")
		if err := controllerutil.SetControllerReference(owner, desired, scheme); err != nil {
			return err
		}

		if err := c.Create(ctx, desired); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				current := sdk.NewDefaultInstance(desired)
				nn := client.ObjectKeyFromObject(desired)
				if err := c.Get(ctx, nn, current); err != nil {
					return err
				}
				current, err = sdk.StripStatusFromObject(current)
				if err != nil {
					return err
				}
				currentObjCopy := current.DeepCopyObject()
				sdk.MergeLabelsAndAnnotations(desired, current)
				merged, err := sdk.MergeObject(desired, current, LastAppliedConfigAnnotation)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(currentObjCopy, merged) {
					if err := c.Update(ctx, merged); err != nil {
						return err
					}
				}
			} else {
				return err
			}
		}
	}

	return nil
}

func isPrometheusDeployed(logger logr.Logger, c client.Client, namespace string) (bool, error) {
	rule := &promv1.PrometheusRule{}
	key := client.ObjectKey{Namespace: namespace, Name: ruleName}
	if err := c.Get(context.TODO(), key, rule); err != nil {
		if meta.IsNoMatchError(err) {
			logger.V(3).Info("No match error for PrometheusRule, must not have prometheus deployed")
			return false, nil
		} else if !k8serrors.IsNotFound(err) {
			return false, err
		}
	}

	return true, nil
}
