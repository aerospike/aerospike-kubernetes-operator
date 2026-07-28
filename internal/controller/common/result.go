package common

import (
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	reconcileResultLogKey       = "result"
	reconcileResultError        = "error"
	reconcileResultRequeue      = "requeue"
	reconcileResultSuccess      = "success"
	reconcileRequeueAfterLogKey = "requeueAfter"
)

type ReconcileResult struct {
	Err       error
	Result    reconcile.Result
	IsSuccess bool
}

func (r ReconcileResult) GetResult() (reconcile.Result, error) {
	return r.Result, r.Err
}

func ReconcileSuccess() ReconcileResult {
	return ReconcileResult{IsSuccess: true, Result: reconcile.Result{}}
}

func ReconcileRequeueAfter(secs int) ReconcileResult {
	t := time.Duration(secs) * time.Second

	return ReconcileResult{
		Result: reconcile.Result{
			RequeueAfter: t,
		},
	}
}

func ReconcileError(e error) ReconcileResult {
	return ReconcileResult{Result: reconcile.Result{}, Err: e}
}

// ReconcileExitLogValues returns structured log key-value pairs for defer exit logging.
// It is the shared building block for each controller's own finishReconcile, which owns
// any controller-specific finish logic (e.g. setting an error status phase).
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
