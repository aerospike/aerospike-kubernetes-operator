package common

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	reconcileResultLogKey       = "result"
	reconcileResultError        = "error"
	reconcileResultRequeue      = "requeue"
	reconcileResultSuccess      = "success"
	reconcileRequeueAfterLogKey = "requeueAfter"
)

// ReconcileExitLogValues returns structured log key-value pairs for defer exit logging.
func ReconcileExitLogValues(result reconcile.Result, recErr error) []interface{} {
	if recErr != nil {
		return []interface{}{reconcileResultLogKey, reconcileResultError}
	}

	if result.RequeueAfter > 0 {
		return []interface{}{
			reconcileResultLogKey, reconcileResultRequeue,
			reconcileRequeueAfterLogKey, result.RequeueAfter.String(),
		}
	}

	return []interface{}{reconcileResultLogKey, reconcileResultSuccess}
}

// FinishReconcile logs reconcile exit and optionally runs setErrorPhase when recErr != nil.
// Returns the error to assign to Reconcile's named recErr return in defer.
func FinishReconcile(
	ctx context.Context,
	log logr.Logger,
	result reconcile.Result,
	recErr error,
	setErrorPhase func(context.Context) error,
) error {
	logValues := ReconcileExitLogValues(result, recErr)
	if recErr != nil {
		if setErrorPhase != nil {
			if err := setErrorPhase(ctx); err != nil {
				recErr = errors.Join(recErr, err)
			}
		}

		log.Error(recErr, "Reconcile failed", logValues...)

		return recErr
	}

	log.Info("Reconcile completed", logValues...)

	return nil
}
