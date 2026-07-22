package restore

import (
	"context"
	"encoding/json"
	"errors"
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

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
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

func (r *SingleRestoreReconciler) Reconcile(ctx context.Context) (result ctrl.Result, recErr error) {
	defer func() {
		// finishReconcile returns the error to assign here so we avoid *error params; recErr is Reconcile's named return.
		recErr = common.FinishReconcile(ctx, r.Log, result, recErr, nil)
	}()

	if !r.aeroRestore.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleting AerospikeRestore")

		if err := r.cleanUpAndRemoveFinalizer(ctx, finalizerName); err != nil {
			return reconcile.Result{}, err
		}

		r.Recorder.Eventf(
			r.aeroRestore, corev1.EventTypeNormal, "Deleted",
			"Successfully deleted restore resources",
		)

		// Stop reconciliation as the Aerospike restore is being deleted
		return reconcile.Result{}, nil
	}

	if r.aeroRestore.Status.Phase == asdbv1beta1.AerospikeRestoreCompleted {
		// Stop reconciliation as the Aerospike restore is already completed
		r.Log.Info("Restore already completed, skipped reconciliation")
		return reconcile.Result{}, nil
	}

	if err := r.setStatusPhase(ctx, asdbv1beta1.AerospikeRestoreInProgress); err != nil {
		return ctrl.Result{}, err
	}

	// The restore is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(ctx, finalizerName); err != nil {
		return reconcile.Result{}, err
	}

	if res := r.reconcileRestore(ctx); !res.IsSuccess {
		if res.Err != nil {
			r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeWarning, "ReconcileFailed",
				"Failed to reconcile AerospikeRestore")

			return res.Result, res.Err
		}

		return res.Result, nil
	}

	if err := r.checkRestoreStatus(ctx); err != nil {
		r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeWarning, "StatusCheckFailed",
			"Failed to check AerospikeRestore status")

		return ctrl.Result{}, err
	}

	if r.aeroRestore.Status.Phase == asdbv1beta1.AerospikeRestoreInProgress {
		return ctrl.Result{RequeueAfter: r.aeroRestore.Spec.PollingPeriod.Duration}, nil
	}

	r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeNormal, "Completed",
		"Restore completed")

	return ctrl.Result{}, nil
}

func (r *SingleRestoreReconciler) reconcileRestore(ctx context.Context) common.ReconcileResult {
	backupSvcID := r.aeroRestore.Spec.BackupService.String()

	if r.aeroRestore.Status.JobID != nil {
		r.Log.Info("Restore already running, checked restore status", "jobID", *r.aeroRestore.Status.JobID)
		return common.ReconcileSuccess()
	}

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroRestore.Spec.BackupService)
	if err != nil {
		return common.ReconcileError(fmt.Errorf(
			"get backup service client (backup service %s): %w",
			backupSvcID, err,
		))
	}

	var (
		jobID      *int64
		statusCode *int
	)

	restoreType := r.aeroRestore.Spec.Type

	switch restoreType {
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
		return common.ReconcileError(fmt.Errorf(
			"unsupported restore type %q", restoreType,
		))
	}

	if err != nil {
		if statusCode != nil && *statusCode == http.StatusBadRequest {
			r.aeroRestore.Status.Phase = asdbv1beta1.AerospikeRestoreFailed

			if statusUpdateErr := r.Client.Status().Update(ctx, r.aeroRestore); statusUpdateErr != nil {
				return common.ReconcileError(errors.Join(
					err,
					fmt.Errorf(
						"set AerospikeRestore status phase to %s: %w",
						asdbv1beta1.AerospikeRestoreFailed, statusUpdateErr,
					),
				))
			}

			// Don't requeue if the error is due to bad request.
			return common.ReconcileError(reconcile.TerminalError(fmt.Errorf(
				"trigger restore type %q: %w",
				restoreType, err,
			)))
		}

		return common.ReconcileError(fmt.Errorf(
			"trigger restore type %q: %w",
			restoreType, err,
		))
	}

	r.Recorder.Eventf(r.aeroRestore, corev1.EventTypeNormal, "Triggered",
		"Triggered restore")

	r.aeroRestore.Status.JobID = jobID

	if err = r.Client.Status().Update(ctx, r.aeroRestore); err != nil {
		return common.ReconcileError(fmt.Errorf(
			"update AerospikeRestore status with jobID %d: %w",
			*jobID, err,
		))
	}

	return common.ReconcileRequeueAfter(1)
}

func (r *SingleRestoreReconciler) checkRestoreStatus(ctx context.Context) error {
	backupSvcID := r.aeroRestore.Spec.BackupService.String()

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroRestore.Spec.BackupService)
	if err != nil {
		return fmt.Errorf(
			"get backup service client (backup service %s): %w",
			backupSvcID, err,
		)
	}

	jobID := *r.aeroRestore.Status.JobID

	restoreStatus, err := serviceClient.CheckRestoreStatus(jobID)
	if err != nil {
		return fmt.Errorf(
			"check restore status for jobID %d: %w",
			jobID, err,
		)
	}

	r.Log.Info("Restore status received", "status", restoreStatus, "jobID", jobID)

	if status, ok := restoreStatus["status"]; ok {
		r.aeroRestore.Status.Phase = statusToPhase(status.(string))
	}

	statusBytes, err := json.Marshal(restoreStatus)
	if err != nil {
		return fmt.Errorf("marshal restore status: %w", err)
	}

	r.aeroRestore.Status.RestoreResult.Raw = statusBytes

	if err = r.Client.Status().Update(ctx, r.aeroRestore); err != nil {
		return fmt.Errorf(
			"update AerospikeRestore status (phase %s, jobID %d): %w",
			r.aeroRestore.Status.Phase, jobID, err,
		)
	}

	return nil
}

func (r *SingleRestoreReconciler) setStatusPhase(
	ctx context.Context, phase asdbv1beta1.AerospikeRestorePhase,
) error {
	if r.aeroRestore.Status.Phase != phase {
		r.aeroRestore.Status.Phase = phase

		if err := r.Client.Status().Update(ctx, r.aeroRestore); err != nil {
			return fmt.Errorf(
				"set AerospikeRestore status phase to %s: %w",
				phase, err,
			)
		}
	}

	return nil
}

func (r *SingleRestoreReconciler) addFinalizer(ctx context.Context, finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object.
	if !utils.ContainsString(
		r.aeroRestore.Finalizers, finalizerName,
	) {
		r.aeroRestore.Finalizers = append(
			r.aeroRestore.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroRestore); err != nil {
			return fmt.Errorf(
				"add finalizer %q: %w",
				finalizerName, err,
			)
		}
	}

	return nil
}

func (r *SingleRestoreReconciler) cleanUpAndRemoveFinalizer(ctx context.Context, finalizerName string) error {
	if utils.ContainsString(r.aeroRestore.Finalizers, finalizerName) {
		r.Log.Info("Removing finalizer")

		if r.aeroRestore.Status.JobID != nil {
			if err := r.cancelRestoreJob(); err != nil {
				return err
			}
		}

		// Remove finalizer from the list
		r.aeroRestore.Finalizers = utils.RemoveString(
			r.aeroRestore.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroRestore); err != nil {
			return fmt.Errorf(
				"remove finalizer %q: %w",
				finalizerName, err,
			)
		}

		r.Log.Info("Removed finalizer")
	}

	return nil
}

func (r *SingleRestoreReconciler) cancelRestoreJob() error {
	backupSvcID := r.aeroRestore.Spec.BackupService.String()

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroRestore.Spec.BackupService)
	if err != nil {
		return fmt.Errorf(
			"get backup service client (backup service %s): %w",
			backupSvcID, err,
		)
	}

	jobID := *r.aeroRestore.Status.JobID

	if statusCode, err := serviceClient.CancelRestoreJob(jobID); err != nil {
		if statusCode == http.StatusNotFound {
			r.Log.Info("Restore job not found, skipping cancel",
				"jobID", jobID, "statusCode", statusCode, "err", err)

			return nil
		}

		return fmt.Errorf("cancel restore job %d: %w", jobID, err)
	}

	r.Log.Info("Restore job cancelled successfully", "jobID", jobID)

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
