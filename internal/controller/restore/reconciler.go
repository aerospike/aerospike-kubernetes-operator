package restore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/pkg/backup-service"
)

// SingleRestoreReconciler reconciles a single AerospikeRestore
type SingleRestoreReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	aeroRestore *asdbv1beta1.AerospikeRestore
	KubeConfig  *rest.Config
	Scheme      *k8sRuntime.Scheme
	Log         logr.Logger
}

func (r *SingleRestoreReconciler) Reconcile() (result ctrl.Result, recErr error) {
	if !r.aeroRestore.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleted AerospikeRestore")
		r.Recorder.Eventf(
			r.aeroRestore, corev1.EventTypeNormal, "Deleted",
			"Deleted AerospikeRestore %s/%s", r.aeroRestore.Namespace,
			r.aeroRestore.Name,
		)
		// Stop reconciliation as the Aerospike restore is being deleted
		return reconcile.Result{}, nil
	}

	if err := r.setStatusPhase(asdbv1beta1.AerospikeRestoreInProgress); err != nil {
		return ctrl.Result{}, err
	}

	if res := r.reconcileRestore(); !res.IsSuccess {
		if res.Err != nil {
			r.Log.Error(res.Err, "Failed to reconcile restore")
			r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeWarning, "RestoreReconcileFailed",
				"Failed to reconcile restore %s/%s", r.aeroRestore.Namespace, r.aeroRestore.Name)

			return res.Result, res.Err
		}

		return res.Result, nil
	}

	if err := r.checkRestoreStatus(); err != nil {
		r.Log.Error(err, "Failed to check restore status")
		r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeWarning, "CheckStatusFailed",
			"Failed to check restore status %s/%s", r.aeroRestore.Namespace, r.aeroRestore.Name)

		return ctrl.Result{}, err
	}

	if r.aeroRestore.Status.Phase == asdbv1beta1.AerospikeRestoreInProgress {
		return ctrl.Result{RequeueAfter: r.aeroRestore.Spec.PollingPeriod.Duration}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SingleRestoreReconciler) reconcileRestore() common.ReconcileResult {
	if r.aeroRestore.Status.JobID != nil {
		r.Log.Info("Restore already running, checking the restore status")
		return common.ReconcileSuccess()
	}

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroRestore.Spec.BackupService)
	if err != nil {
		return common.ReconcileError(err)
	}

	var (
		jobID      *int64
		statusCode *int
	)

	switch r.aeroRestore.Spec.Type {
	case asdbv1beta1.Full:
		jobID, statusCode, err = serviceClient.TriggerRestoreWithType(r.Log, string(asdbv1beta1.Full),
			r.aeroRestore.Spec.Config.Raw)

	case asdbv1beta1.Incremental:
		jobID, statusCode, err = serviceClient.TriggerRestoreWithType(r.Log, string(asdbv1beta1.Incremental),
			r.aeroRestore.Spec.Config.Raw)

	case asdbv1beta1.Timestamp:
		jobID, statusCode, err = serviceClient.TriggerRestoreWithType(r.Log, string(asdbv1beta1.Timestamp),
			r.aeroRestore.Spec.Config.Raw)

	default:
		return common.ReconcileError(fmt.Errorf("unsupported restore type"))
	}

	if err != nil {
		if statusCode != nil && *statusCode == http.StatusBadRequest {
			r.Log.Error(err, fmt.Sprintf("Failed to trigger restore with status code %d", *statusCode))

			r.aeroRestore.Status.Phase = asdbv1beta1.AerospikeRestoreFailed

			if err = r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
				r.Log.Error(err, fmt.Sprintf("Failed to update restore status to %+v", err))
				return common.ReconcileError(err)
			}

			// Don't requeue if the error is due to bad request.
			return common.ReconcileError(reconcile.TerminalError(err))
		}

		return common.ReconcileError(err)
	}

	r.aeroRestore.Status.JobID = jobID

	if err = r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update restore status to %+v", err))
		return common.ReconcileError(err)
	}

	return common.ReconcileRequeueAfter(1)
}

func (r *SingleRestoreReconciler) checkRestoreStatus() error {
	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroRestore.Spec.BackupService)
	if err != nil {
		return err
	}

	restoreStatus, err := serviceClient.CheckRestoreStatus(r.aeroRestore.Status.JobID)
	if err != nil {
		return err
	}

	r.Log.Info(fmt.Sprintf("Restore status: %+v", restoreStatus))

	if status, ok := restoreStatus["status"]; ok {
		r.aeroRestore.Status.Phase = statusToPhase(status.(string))
	}

	statusBytes, err := json.Marshal(restoreStatus)
	if err != nil {
		return err
	}

	r.aeroRestore.Status.RestoreResult.Raw = statusBytes

	if err = r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update restore status to %+v", err))
		return err
	}

	return nil
}

func (r *SingleRestoreReconciler) setStatusPhase(phase asdbv1beta1.AerospikeRestorePhase) error {
	if r.aeroRestore.Status.Phase != phase {
		r.aeroRestore.Status.Phase = phase

		if err := r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to set restore status to %s", phase))
			return err
		}
	}

	return nil
}

func statusToPhase(status string) asdbv1beta1.AerospikeRestorePhase {
	switch status {
	case "Done":
		return asdbv1beta1.AerospikeRestoreCompleted

	case "Running":
		return asdbv1beta1.AerospikeRestoreInProgress

	case "Failed":
		return asdbv1beta1.AerospikeRestoreFailed
	}

	return ""
}
