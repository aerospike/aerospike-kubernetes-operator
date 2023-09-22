package controllers

import (
	"context"
	"runtime"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

const patchFieldOwner = "aerospike-kuberneter-operator"
const finalizerName = "asdb.aerospike.com/storage-finalizer"

// Number of Reconcile threads to run Reconcile operations
var maxConcurrentReconciles = runtime.NumCPU() * 2

var (
	updateOption = &client.UpdateOptions{
		FieldManager: "aerospike-operator",
	}
	createOption = &client.CreateOptions{
		FieldManager: "aerospike-operator",
	}
)

var PDBbGvk = policyv1beta1.SchemeGroupVersion.WithKind("PodDisruptionBudget")

// AerospikeClusterReconciler reconciles AerospikeClusters
type AerospikeClusterReconciler struct {
	client.Client
	Recorder   record.EventRecorder
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Scheme     *k8sRuntime.Scheme
	Log        logr.Logger
}

// SetupWithManager sets up the controller with the Manager
func (r *AerospikeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.setupPdbAPI(r.KubeConfig); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1.AerospikeCluster{}).
		Owns(
			&appsv1.StatefulSet{}, builder.WithPredicates(
				predicate.Funcs{
					CreateFunc: func(e event.CreateEvent) bool {
						return false
					},
					UpdateFunc: func(e event.UpdateEvent) bool {
						return false
					},
				},
			),
		).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: maxConcurrentReconciles,
			},
		).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r)
}

// setupPdbAPI sets up the pdb api version to use as per the k8s version.
// TODO: Move to v1 when minimum supported k8s version is 1.21
func (r *AerospikeClusterReconciler) setupPdbAPI(config *rest.Config) error {
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(config)

	resources, err := discoveryClient.ServerResourcesForGroupVersion("policy/v1")
	if err != nil {
		r.Log.Info("Could not get ServerResourcesForGroupVersion for policy/v1, falling back to policy/v1beta1")
		return nil
	}

	for i := range resources.APIResources {
		if resources.APIResources[i].Kind == "PodDisruptionBudget" {
			PDBbGvk = policyv1.SchemeGroupVersion.WithKind("PodDisruptionBudget")
			return nil
		}
	}

	return nil
}

// RackState contains the rack configuration and rack size.
type RackState struct {
	Rack *asdbv1.Rack
	Size int
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;create;update;patch
//nolint:lll // marker
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters/finalizers,verbs=update

// Reconcile AerospikeCluster object
func (r *AerospikeClusterReconciler) Reconcile(
	_ context.Context, request reconcile.Request,
) (ctrl.Result, error) {
	log := r.Log.WithValues("aerospikecluster", request.NamespacedName)

	log.Info("Reconciling AerospikeCluster")

	// Fetch the AerospikeCluster instance
	aeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(context.TODO(), request.NamespacedName, aeroCluster); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after Reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	cr := SingleClusterReconciler{
		aeroCluster: aeroCluster,
		Client:      r.Client,
		KubeClient:  r.KubeClient,
		KubeConfig:  r.KubeConfig,
		Log:         log,
		Scheme:      r.Scheme,
		Recorder:    r.Recorder,
	}

	return cr.Reconcile()
}
