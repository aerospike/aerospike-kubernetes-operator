package common

import (
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
			Requeue: true, RequeueAfter: t,
		},
	}
}

func ReconcileError(e error) ReconcileResult {
	return ReconcileResult{Result: reconcile.Result{}, Err: e}
}
