package common

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/log"
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
	result reconcile.Result,
	recErr error,
	setErrorPhase func(context.Context) error,
) error {
	logger := log.FromContext(ctx)

	logValues := ReconcileExitLogValues(result, recErr)
	if recErr != nil {
		if setErrorPhase != nil {
			if err := setErrorPhase(ctx); err != nil {
				recErr = errors.Join(recErr, err)
			}
		}

		logger.Error(recErr, "Reconcile failed", logValues...)

		return recErr
	}

	logger.Info("Reconcile completed", logValues...)

	return nil
}
