package restore

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	"github.com/go-logr/logr"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/pkg/backup-service"
)

// SingleClusterReconciler reconciles a single AerospikeRestore
type SingleRestoreReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	aeroRestore *asdbv1beta1.AerospikeRestore
	KubeConfig  *rest.Config
	Scheme      *k8sRuntime.Scheme
	Log         logr.Logger
}

type ReconcileResult struct {
	Err       error
	Result    reconcile.Result
	IsSuccess bool
}

func (r *SingleRestoreReconciler) Reconcile() (result ctrl.Result, recErr error) {
	if err := r.setStatusPhase(asdbv1beta1.AerospikeRestoreInProgress); err != nil {
		return ctrl.Result{}, err
	}

	if res := r.ReconcileRestore(); !res.IsSuccess {
		if res.Err != nil {
			return res.Result, res.Err
		}

		return res.Result, nil
	}

	if err := r.CheckRestoreStatus(); err != nil {
		return ctrl.Result{}, err
	}

	if r.aeroRestore.Status.Phase == asdbv1beta1.AerospikeRestoreInProgress {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SingleRestoreReconciler) ReconcileRestore() ReconcileResult {
	if r.aeroRestore.Status.JobID != 0 {
		r.Log.Info("Restore already running, checking the restore status")
		return reconcileSuccess()
	}

	serviceClient := backup_service.GetBackupServiceClient(r.aeroRestore.Spec.ServiceConfig)

	var (
		jobID int64
		err   error
	)

	switch r.aeroRestore.Spec.RestoreConfig.Type {
	case asdbv1beta1.Full:
		jobID, err = serviceClient.TriggerFullRestore(r.Log, &r.aeroRestore.Spec.RestoreConfig.RestoreRequest)

	case asdbv1beta1.Incremental:
		jobID, err = serviceClient.TriggerIncrementalRestore(r.Log, &r.aeroRestore.Spec.RestoreConfig.RestoreRequest)

	case asdbv1beta1.TimeStamp:
		var timeStampRequest *model.RestoreTimestampRequest
		timeStampRequest.Time = r.aeroRestore.Spec.RestoreConfig.Time
		timeStampRequest.Routine = r.aeroRestore.Spec.RestoreConfig.Routine
		timeStampRequest.Policy = r.aeroRestore.Spec.RestoreConfig.Policy
		timeStampRequest.DestinationCuster = r.aeroRestore.Spec.RestoreConfig.DestinationCuster
		timeStampRequest.SecretAgent = r.aeroRestore.Spec.RestoreConfig.SecretAgent

		jobID, err = serviceClient.TriggerRestoreByTimeStamp(r.Log, timeStampRequest)
	default:
		return reconcileError(fmt.Errorf("unsupported restore type"))
	}

	if err != nil {
		reconcileError(err)
	}

	r.aeroRestore.Status.JobID = jobID

	if err = r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update restore status to %+v", err))
		return reconcileError(err)
	}

	return reconcileRequeueAfter(1)
}

func (r *SingleRestoreReconciler) CheckRestoreStatus() error {
	serviceClient := backup_service.GetBackupServiceClient(r.aeroRestore.Spec.ServiceConfig)

	restoreStatus, err := serviceClient.CheckRestoreStatus(r.aeroRestore.Status.JobID)
	if err != nil {
		return err
	}

	r.aeroRestore.Status.RestoreResult = &restoreStatus.RestoreResult
	r.aeroRestore.Status.Error = restoreStatus.Error
	r.aeroRestore.Status.Phase = statusToPhase(restoreStatus.Status)

	if err := r.Client.Status().Update(context.Background(), r.aeroRestore); err != nil {
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

func statusToPhase(status model.JobStatus) asdbv1beta1.AerospikeRestorePhase {
	switch status {
	case model.JobStatusDone:
		return asdbv1beta1.AerospikeRestoreCompleted

	case model.JobStatusRunning:
		return asdbv1beta1.AerospikeRestoreInProgress

	case model.JobStatusFailed:
		return asdbv1beta1.AerospikeRestoreFailed
	}

	return ""
}

func reconcileSuccess() ReconcileResult {
	return ReconcileResult{IsSuccess: true, Result: reconcile.Result{}}
}

func reconcileRequeueAfter(secs int) ReconcileResult {
	t := time.Duration(secs) * time.Second

	return ReconcileResult{
		Result: reconcile.Result{
			Requeue: true, RequeueAfter: t,
		},
	}
}

func reconcileError(e error) ReconcileResult {
	return ReconcileResult{Result: reconcile.Result{}, Err: e}
}
