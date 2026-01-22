package cluster

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
)

const patchFieldOwner = "aerospike-kuberneter-operator"
const finalizerName = "asdb.aerospike.com/storage-finalizer"

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
	// Create predicate for pods with eviction-blocked annotation or effective-rack-id annotation changes
	podPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, ok := e.ObjectOld.(*corev1.Pod)
			if !ok {
				return false
			}

			newPod, ok := e.ObjectNew.(*corev1.Pod)
			if !ok {
				return false
			}

			oldAnnotations := oldPod.Annotations
			newAnnotations := newPod.Annotations

			// Check if the eviction-blocked annotation was added in this update
			_, hadEvictionBlocked := oldAnnotations[asdbv1.EvictionBlockedAnnotation]
			_, hasEvictionBlocked := newAnnotations[asdbv1.EvictionBlockedAnnotation]
			evictionBlockedAdded := !hadEvictionBlocked && hasEvictionBlocked

			// Check if the effective-rack-id annotation value changed
			oldEffectiveRackID := oldAnnotations[asdbv1.EffectiveRackIDAnnotation]
			newEffectiveRackID := newAnnotations[asdbv1.EffectiveRackIDAnnotation]
			effectiveRackIDChanged := oldEffectiveRackID != newEffectiveRackID

			return evictionBlockedAdded || effectiveRackIDChanged
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false // Don't process delete events
		},
	}

	podMapFunc := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		pod := obj.(*corev1.Pod)

		clusterName, exists := pod.Labels[asdbv1.AerospikeCustomResourceLabel]
		if !exists || clusterName == "" {
			// Pod is not part of any AerospikeCluster, skip
			return nil
		}

		// Reconcile request for the owning AerospikeCluster
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      clusterName,
			},
		}}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1.AerospikeCluster{}).
		Owns(
			&appsv1.StatefulSet{}, builder.WithPredicates(
				predicate.Funcs{
					CreateFunc: func(_ event.CreateEvent) bool {
						return false
					},
					UpdateFunc: func(_ event.UpdateEvent) bool {
						return false
					},
				},
			),
		).
		Watches(
			&corev1.Pod{},
			podMapFunc,
			builder.WithPredicates(podPredicate),
		).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: common.MaxConcurrentReconciles,
			},
		).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{})).
		Complete(r)
}

// RackState contains the rack configuration and rack size.
type RackState struct {
	Rack *asdbv1.Rack
	Size int32
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;create;update;patch;delete
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
	if err := r.Get(context.TODO(), request.NamespacedName, aeroCluster); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after Reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
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
