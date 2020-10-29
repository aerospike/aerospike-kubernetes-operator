package aerospikecluster

import (
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcileResult struct {
	isContinue bool
	result     reconcile.Result
	err        error
}

func (r reconcileResult) getResult() (reconcile.Result, error) {
	return r.result, r.err
}

func reconcileContinue() reconcileResult {
	return reconcileResult{isContinue: true, result: reconcile.Result{}}
}

func reconcileDone() reconcileResult {
	return reconcileResult{result: reconcile.Result{}}
}

func reconcileRequeueAfter(secs int) reconcileResult {
	t := time.Duration(secs) * time.Second
	return reconcileResult{result: reconcile.Result{Requeue: true, RequeueAfter: t}}
}

func reconcileError(e error) reconcileResult {
	return reconcileResult{result: reconcile.Result{}, err: e}
}
