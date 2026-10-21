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

package backup

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
)

const finalizerName = "asdb.aerospike.com/backup-finalizer"

// AerospikeBackupReconciler reconciles a AerospikeBackup object
type AerospikeBackupReconciler struct {
	client.Client
	Scheme *k8sruntime.Scheme
	// KubeClient and KubeConfig exec into the backup service pods to check that they have the latest ConfigMap
	// before a reload; client.Client cannot run pods/exec.
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Recorder   record.EventRecorder
	Log        logr.Logger
}

//nolint:lll // for readability
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikebackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AerospikeBackupReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("aerospikeBackup", request.NamespacedName)

	log.Info("Reconciling AerospikeBackup")

	// Fetch the AerospikeBackup instance
	aeroBackup := &asdbv1beta1.AerospikeBackup{}
	if err := r.Get(ctx, request.NamespacedName, aeroBackup); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after Reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	cr := SingleBackupReconciler{
		aeroBackup: aeroBackup,
		Client:     r.Client,
		Log:        log,
		Scheme:     r.Scheme,
		Recorder:   r.Recorder,
		KubeClient: r.KubeClient,
		KubeConfig: r.KubeConfig,
	}

	return cr.Reconcile(ctx)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AerospikeBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1beta1.AerospikeBackup{}).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: common.MaxConcurrentReconciles,
			},
		).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
