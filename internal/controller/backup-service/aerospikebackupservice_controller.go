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

package backupservice

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
)

// AerospikeBackupServiceReconciler reconciles a AerospikeBackupService object
type AerospikeBackupServiceReconciler struct {
	Scheme *k8sruntime.Scheme
	client.Client
	Recorder record.EventRecorder
	Log      logr.Logger
}

//nolint:lll // for readability
//+kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackupservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackupservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackupservices/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AerospikeBackupServiceReconciler) Reconcile(_ context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("aerospikebackupservice", request.NamespacedName)

	log.Info("Reconciling AerospikeBackupService")

	// Fetch the AerospikeBackupService instance
	aeroBackupService := &asdbv1beta1.AerospikeBackupService{}
	if err := r.Client.Get(context.TODO(), request.NamespacedName, aeroBackupService); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Deleted AerospikeBackupService")

			objectRef := &corev1.ObjectReference{
				Kind:      "AerospikeBackupService",
				Namespace: request.Namespace,
				Name:      request.Name,
			}
			r.Recorder.Eventf(
				objectRef, corev1.EventTypeNormal, "Deleted",
				"Deleted AerospikeBackupService %s/%s", objectRef.Namespace,
				objectRef.Name,
			)

			// Request object not found, could have been deleted after Reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	cr := SingleBackupServiceReconciler{
		aeroBackupService: aeroBackupService,
		Client:            r.Client,
		Log:               log,
		Scheme:            r.Scheme,
		Recorder:          r.Recorder,
	}

	return cr.Reconcile()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AerospikeBackupServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1beta1.AerospikeBackupService{}).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: common.MaxConcurrentReconciles,
			},
		).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
