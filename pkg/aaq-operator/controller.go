package aaq_operator

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	aaqcerts "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/cert"
	"kubevirt.io/applications-aware-quota/staging/src/kubevirt.io/applications-aware-quota-api/pkg/apis/core/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	"github.com/kelseyhightower/envconfig"
	aaqcluster "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/cluster"
	aaqnamespaced "kubevirt.io/applications-aware-quota/pkg/aaq-operator/resources/namespaced"
	"kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/callbacks"
	sdkr "kubevirt.io/controller-lifecycle-operator-sdk/pkg/sdk/reconciler"

	"kubevirt.io/applications-aware-quota/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	finalizerName = "operator.aaq.kubevirt.io"

	createVersionLabel = "operator.aaq.kubevirt.io/createVersion"
	updateVersionLabel = "operator.aaq.kubevirt.io/updateVersion"
	// LastAppliedConfigAnnotation is the annotation that holds the last resource state which we put on resources under our governance
	LastAppliedConfigAnnotation = "operator.aaq.kubevirt.io/lastAppliedConfiguration"

	certPollInterval = 1 * time.Minute
)

var (
	log = logf.Log.WithName("aaq-operator")
)

// Add creates a new AAQ Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr)
	if err != nil {
		return err
	}
	return r.add(mgr)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (*ReconcileAAQ, error) {
	var namespacedArgs aaqnamespaced.FactoryArgs
	namespace := util.GetNamespace()
	restClient := mgr.GetClient()
	onOpenshift := util.OnOpenshift()
	clusterArgs := &aaqcluster.FactoryArgs{
		Namespace:   namespace,
		Client:      restClient,
		Logger:      log,
		OnOpenshift: onOpenshift,
	}

	err := envconfig.Process("", &namespacedArgs)

	if err != nil {
		return nil, err
	}

	namespacedArgs.Namespace = namespace
	namespacedArgs.OnOpenshift = onOpenshift

	log.Info("", "VARS", fmt.Sprintf("%+v", namespacedArgs))

	scheme := mgr.GetScheme()
	uncachedClient, err := client.New(mgr.GetConfig(), client.Options{
		Scheme: scheme,
		Mapper: mgr.GetRESTMapper(),
	})
	if err != nil {
		return nil, err
	}

	recorder := mgr.GetEventRecorderFor("operator-controller")

	r := &ReconcileAAQ{
		client:         restClient,
		uncachedClient: uncachedClient,
		scheme:         scheme,
		recorder:       recorder,
		namespace:      namespace,
		clusterArgs:    clusterArgs,
		namespacedArgs: &namespacedArgs,
	}
	callbackDispatcher := callbacks.NewCallbackDispatcher(log, restClient, uncachedClient, scheme, namespace)
	r.getCache = mgr.GetCache
	r.reconciler = sdkr.NewReconciler(r, log, restClient, callbackDispatcher, scheme, mgr.GetCache, createVersionLabel, updateVersionLabel, LastAppliedConfigAnnotation, certPollInterval, finalizerName, true, recorder)

	r.registerHooks()
	addReconcileCallbacks(r)

	return r, nil
}

var _ reconcile.Reconciler = &ReconcileAAQ{}

// ReconcileAAQ reconciles a AAQ object
type ReconcileAAQ struct {
	// This Client, initialized using mgr.client() above, is a split Client
	// that reads objects from the cache and writes to the apiserver
	client client.Client

	// use this for getting any resources not in the install namespace or cluster scope
	uncachedClient client.Client
	scheme         *runtime.Scheme
	recorder       record.EventRecorder
	controller     controller.Controller

	namespace      string
	clusterArgs    *aaqcluster.FactoryArgs
	namespacedArgs *aaqnamespaced.FactoryArgs

	certManager CertManager
	reconciler  *sdkr.Reconciler
	getCache    func() cache.Cache
}

// SetController sets the controller dependency
func (r *ReconcileAAQ) SetController(controller controller.Controller) {
	r.controller = controller
	r.reconciler.WithController(controller)
}

// Reconcile reads that state of the cluster for a AAQ object and makes changes based on the state read
// and what is in the AAQ.Spec
// Note:
// The Controller will requeue the request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAAQ) Reconcile(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("request.Namespace", request.Namespace, "request.Name", request.Name)
	reqLogger.Info("Reconciling AAQ CR")
	operatorVersion := r.namespacedArgs.OperatorVersion
	cr := &v1alpha1.AAQ{}
	crKey := client.ObjectKey{Namespace: "", Name: request.NamespacedName.Name}
	err := r.client.Get(context.TODO(), crKey, cr)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("AAQ CR does not exist")
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Failed to get AAQ object")
		return reconcile.Result{}, err
	}

	res, err := r.reconciler.Reconcile(request, operatorVersion, reqLogger)
	if err != nil {
		reqLogger.Error(err, "failed to reconcile")
	}
	return res, err
}

func (r *ReconcileAAQ) add(mgr manager.Manager) error {
	// Create a new controller
	c, err := controller.New("aaq-operator-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	r.SetController(c)

	if err = r.reconciler.WatchCR(); err != nil {
		return err
	}

	cm, err := NewCertManager(mgr, r.namespace)
	if err != nil {
		return err
	}

	r.certManager = cm

	return nil
}

// createOperatorConfig creates operator config map
func (r *ReconcileAAQ) createOperatorConfig(cr client.Object) error {
	aaqCR := cr.(*v1alpha1.AAQ)
	installerLabels := util.GetRecommendedInstallerLabelsFromCr(aaqCR)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ConfigMapName,
			Namespace: r.namespace,
			Labels:    map[string]string{"operator.aaq.kubevirt.io": ""},
		},
	}
	util.SetRecommendedLabels(cm, installerLabels, "aaq-operator")

	if err := controllerutil.SetControllerReference(cr, cm, r.scheme); err != nil {
		return err
	}

	return r.client.Create(context.TODO(), cm)
}

func (r *ReconcileAAQ) getConfigMap() (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: util.ConfigMapName, Namespace: r.namespace}

	if err := r.client.Get(context.TODO(), key, cm); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return cm, nil
}

func (r *ReconcileAAQ) getCertificateDefinitions(aaq *v1alpha1.AAQ) []aaqcerts.CertificateDefinition {
	args := &aaqcerts.FactoryArgs{Namespace: r.namespace}

	if aaq != nil && aaq.Spec.CertConfig != nil {
		if aaq.Spec.CertConfig.CA != nil {
			if aaq.Spec.CertConfig.CA.Duration != nil {
				args.SignerDuration = &aaq.Spec.CertConfig.CA.Duration.Duration
			}

			if aaq.Spec.CertConfig.CA.RenewBefore != nil {
				args.SignerRenewBefore = &aaq.Spec.CertConfig.CA.RenewBefore.Duration
			}
		}

		if aaq.Spec.CertConfig.Server != nil {
			if aaq.Spec.CertConfig.Server.Duration != nil {
				args.TargetDuration = &aaq.Spec.CertConfig.Server.Duration.Duration
			}

			if aaq.Spec.CertConfig.Server.RenewBefore != nil {
				args.TargetRenewBefore = &aaq.Spec.CertConfig.Server.RenewBefore.Duration
			}
		}
	}

	return aaqcerts.CreateCertificateDefinitions(args)
}
