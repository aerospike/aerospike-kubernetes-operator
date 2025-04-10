/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package restore

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
)

const finalizerName = "asdb.aerospike.com/restore-finalizer"

// AerospikeRestoreReconciler reconciles a AerospikeRestore object
type AerospikeRestoreReconciler struct {
	client.Client
	Scheme   *k8sRuntime.Scheme
	Recorder record.EventRecorder
	Log      logr.Logger
}

//nolint:lll // for readability
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikerestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikerestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikerestores/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AerospikeRestoreReconciler) Reconcile(_ context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("aerospikerestore", request.NamespacedName)

	log.Info("Reconciling AerospikeRestore")

	// Fetch the AerospikeRestore instance
	aeroRestore := &asdbv1beta1.AerospikeRestore{}
	if err := r.Client.Get(context.TODO(), request.NamespacedName, aeroRestore); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after Reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	cr := SingleRestoreReconciler{
		aeroRestore: aeroRestore,
		Client:      r.Client,
		Log:         log,
		Scheme:      r.Scheme,
		Recorder:    r.Recorder,
	}

	return cr.Reconcile()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AerospikeRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1beta1.AerospikeRestore{}).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: common.MaxConcurrentReconciles,
			},
		).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
