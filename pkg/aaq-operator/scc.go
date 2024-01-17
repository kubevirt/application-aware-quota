package aaq_operator

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"kubevirt.io/applications-aware-quota/pkg/util"
	aaqv1 "kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	secv1 "github.com/openshift/api/security/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"

	sdk "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk"
)

const sccName = "applications-aware-quota"

func setSCC(scc *secv1.SecurityContextConstraints) {
	scc.AllowHostDirVolumePlugin = true
	scc.AllowHostIPC = true
	scc.AllowHostNetwork = true
	scc.AllowHostPID = true
	scc.AllowHostPorts = true
	scc.AllowPrivilegeEscalation = pointer.Bool(true)
	scc.AllowPrivilegedContainer = true
	scc.AllowedCapabilities = []corev1.Capability{
		"*",
	}
	scc.AllowedUnsafeSysctls = []string{
		"*",
	}
	scc.DefaultAddCapabilities = nil
	scc.RunAsUser = secv1.RunAsUserStrategyOptions{
		Type: secv1.RunAsUserStrategyRunAsAny,
	}
	scc.SELinuxContext = secv1.SELinuxContextStrategyOptions{
		Type: secv1.SELinuxStrategyRunAsAny,
	}
	scc.SeccompProfiles = []string{
		"*",
	}
	scc.SupplementalGroups = secv1.SupplementalGroupsStrategyOptions{
		Type: secv1.SupplementalGroupsStrategyRunAsAny,
	}
	scc.Volumes = []secv1.FSType{
		"*",
	}
}

func ensureSCCExists(ctx context.Context, logger logr.Logger, c client.Client, saNamespace, saName string) error {
	scc := &secv1.SecurityContextConstraints{}
	userName := fmt.Sprintf("system:serviceaccount:%s:%s", saNamespace, saName)

	err := c.Get(ctx, client.ObjectKey{Name: sccName}, scc)
	if meta.IsNoMatchError(err) {
		// not in openshift
		logger.V(3).Info("No match error for SCC, must not be in openshift")
		return nil
	} else if errors.IsNotFound(err) {
		cr, err := util.GetActiveAAQ(c)
		if err != nil {
			return err
		}
		if cr == nil {
			return fmt.Errorf("no active MTQ")
		}
		installerLabels := util.GetRecommendedInstallerLabelsFromCr(cr)

		scc = &secv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: sccName,
				Labels: map[string]string{
					"aaq.kubevirt.io": "",
				},
			},
			Users: []string{
				userName,
			},
		}

		setSCC(scc)

		util.SetRecommendedLabels(scc, installerLabels, "aaq-operator")

		if err = SetOwnerRuntime(c, scc); err != nil {
			return err
		}

		return c.Create(ctx, scc)
	} else if err != nil {
		return err
	}

	origSCC := scc.DeepCopy()

	setSCC(scc)

	if !sdk.ContainsStringValue(scc.Users, userName) {
		scc.Users = append(scc.Users, userName)
	}

	if !apiequality.Semantic.DeepEqual(origSCC, scc) {
		return c.Update(context.TODO(), scc)
	}

	return nil
}

func (r *ReconcileAAQ) watchSecurityContextConstraints() error {
	err := r.uncachedClient.List(context.TODO(), &secv1.SecurityContextConstraintsList{}, &client.ListOptions{
		Limit: 1,
	})
	if err == nil {
		return r.controller.Watch(source.Kind(r.getCache(), &secv1.SecurityContextConstraints{}), enqueueAAQ(r.client))
	}
	if meta.IsNoMatchError(err) {
		log.Info("Not watching SecurityContextConstraints")
		return nil
	}

	log.Info("GOODBYE SCC")

	return err
}

func enqueueAAQ(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, _ client.Object) []reconcile.Request {
			var rrs []reconcile.Request
			aaqList := &aaqv1.AAQList{}

			if err := c.List(context.TODO(), aaqList, &client.ListOptions{}); err != nil {
				log.Error(err, "Error listing all AAQ objects")
				return nil
			}

			for _, aaq := range aaqList.Items {
				rr := reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: aaq.Namespace, Name: aaq.Name},
				}
				rrs = append(rrs, rr)
			}

			return rrs
		},
	)
}

// SetOwnerRuntime makes the current "active" AAQ CR the owner of the object using runtime lib client
func SetOwnerRuntime(client client.Client, object metav1.Object) error {
	namespace := util.GetNamespace()
	configMap := &corev1.ConfigMap{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: util.ConfigMapName, Namespace: namespace}, configMap); err != nil {
		klog.Warningf("ConfigMap %s does not exist, so not assigning owner", util.ConfigMapName)
		return nil
	}
	return SetConfigAsOwner(configMap, object)
}

// SetConfigAsOwner sets the passed in config map as owner of the object
func SetConfigAsOwner(configMap *corev1.ConfigMap, object metav1.Object) error {
	configMapOwner := getController(configMap.GetOwnerReferences())

	if configMapOwner == nil {
		return fmt.Errorf("configmap has no owner")
	}

	for _, o := range object.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller {
			if o.UID == configMapOwner.UID {
				// already set to current obj
				return nil
			}

			return fmt.Errorf("object %+v already owned by %+v", object, o)
		}
	}

	object.SetOwnerReferences(append(object.GetOwnerReferences(), *configMapOwner))

	return nil
}

func getController(owners []metav1.OwnerReference) *metav1.OwnerReference {
	for _, owner := range owners {
		if owner.Controller != nil && *owner.Controller {
			return &owner
		}
	}
	return nil
}
