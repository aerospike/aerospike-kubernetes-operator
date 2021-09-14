package controllers

import (
	"context"
	"runtime"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

// SetupWithManager sets up the controller with the Manager
func (r *AerospikeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1beta1.AerospikeCluster{}).
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
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// AerospikeClusterReconciler reconciles AerospikeClusters
type AerospikeClusterReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Log        logr.Logger
	Scheme     *k8sRuntime.Scheme
}

// RackState contains the rack configuration and rack size.
type RackState struct {
	Rack asdbv1beta1.Rack
	Size int
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
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
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, aeroCluster)
	if err != nil {
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
	}

	return cr.Reconcile()
}
