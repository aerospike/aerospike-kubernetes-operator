/*
Copyright 2024.

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

package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// clusterLabels returns the standard AKO labels for a cluster with the given name.
func clusterLabels(name string) map[string]string {
	return utils.LabelsForAerospikeCluster(name)
}

// TestCheckPreviouslyFailedCluster covers the three paths of
// checkPreviouslyFailedCluster that do not call recoverFailedCreate (which
// would require a full cluster-deletion pipeline).  The paths are:
//
//  1. Non-empty status  → fast-path success (no k8s API calls needed).
//  2. STS found, healthy pod → ReconcileSuccess (early return from pod loop).
//  3. STS found, all pods in grace period → ReconcileRequeueAfter.
func TestCheckPreviouslyFailedCluster(t *testing.T) {
	// reusableSTS is a StatefulSet that carries the cluster labels so it is
	// returned by getClusterSTSList.
	reusableSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-sts",
			Namespace: namespace,
			Labels:    clusterLabels(clusterName),
		},
	}

	t.Run("non-empty status returns success without any k8s calls", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Status: asdbv1.AerospikeClusterStatus{
				AerospikeClusterStatusSpec: asdbv1.AerospikeClusterStatusSpec{
					// Non-nil AerospikeConfig marks the status as non-empty.
					AerospikeConfig: &asdbv1.AerospikeConfigSpec{},
				},
			},
		}
		// The fast path must return before touching the API server. Asserting that with
		// interceptors states the intent, rather than inferring it from a NotFound error.
		r := newTestReconciler(t, aeroCluster, failOnAnyAPIRead(t))

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false for a cluster with non-empty status")
		}

		if !res.IsSuccess {
			t.Errorf("expected ReconcileSuccess, got err=%v requeueAfter=%v", res.Err, res.Result.RequeueAfter)
		}
	})

	t.Run("STS present with healthy pod returns success", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}

		healthyPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName + "-0-0",
				Namespace:         namespace,
				Labels:            clusterLabels(clusterName),
				CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  asdbv1.AerospikeServerContainerName,
						Ready: true,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()},
						},
					},
				},
			},
		}

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, reusableSTS, healthyPod)

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false when a healthy pod is present")
		}

		if !res.IsSuccess {
			t.Errorf("expected ReconcileSuccess, got err=%v requeueAfter=%v", res.Err, res.Result.RequeueAfter)
		}
	})

	t.Run("STS present with pod in grace period returns requeue", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}

		// A pod created only 10 seconds ago that has already entered the Failed
		// phase is within the default 60-second grace period.
		recentFailedPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName + "-0-0",
				Namespace:         namespace,
				Labels:            clusterLabels(clusterName),
				CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Second)),
			},
			Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: "Error",
			},
		}

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, reusableSTS, recentFailedPod)

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false when pods are still within the grace period")
		}

		if res.IsSuccess {
			t.Error("expected a requeue result, not ReconcileSuccess")
		}

		if res.Err != nil {
			t.Errorf("expected no error for grace-period requeue, got %v", res.Err)
		}

		if res.Result.RequeueAfter == 0 {
			t.Error("expected Result.RequeueAfter > 0 for grace-period path")
		}

		wantAfter := time.Duration(asdbv1.RequeueIntervalSeconds10) * time.Second
		if res.Result.RequeueAfter != wantAfter {
			t.Errorf("expected RequeueAfter=%v, got %v", wantAfter, res.Result.RequeueAfter)
		}
	})
}

func TestEnsureSCRoster_NoOpForNonSCCluster(t *testing.T) {
	t.Parallel()

	ac := newTestAerospikeCluster(namespace, clusterName)
	// IsClusterSCEnabled reads rack 0's namespace list with an unchecked type assertion, so the
	// key has to be present. A namespace without strong-consistency is the non-SC case.
	ac.Spec.RackConfig.Racks[0].AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
		map[string]interface{}{"name": "test"},
	}

	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	res := r.ensureSCRoster(context.TODO(), nil, nil, nil)

	if !res.IsSuccess {
		t.Errorf("want success for a non-SC cluster, got err=%v result=%+v", res.Err, res.Result)
	}
}

// ---- writeTerminalStatus ---------------------------------------------------

func TestWriteTerminalStatus(t *testing.T) {
	t.Parallel()

	longErr := strings.Repeat("x", maxConditionMessageLength*2)

	testCases := []struct {
		name          string
		recErr        error
		failureReason string
		wantPhase     asdbv1.AerospikeClusterPhase
		wantReason    string
		wantMessage   string
		// wantMessageBounded asserts truncation rather than an exact string.
		wantMessageBounded bool
	}{
		{
			name:        "records the failure with the generic reason when no stage was recorded",
			recErr:      errors.New("disk full"),
			wantPhase:   asdbv1.AerospikeClusterError,
			wantReason:  asdbv1.AerospikeClusterReasonReconcileFailed,
			wantMessage: "disk full",
		},
		{
			// Reconcile records the stage it bailed out at before returning the error
			name:          "prefers the stage reason Reconcile recorded",
			recErr:        errors.New("reconcile PodDisruptionBudget: nope"),
			failureReason: asdbv1.AerospikeClusterReasonPDBReconcileFailed,
			wantPhase:     asdbv1.AerospikeClusterError,
			wantReason:    asdbv1.AerospikeClusterReasonPDBReconcileFailed,
		},
		{
			// The end-to-end half of TestTruncateConditionMessage: an oversized error must reach
			// the condition already bounded, or the API server rejects the patch and the
			// phase=Error write is lost with it.
			name:               "truncates an oversized error message",
			recErr:             errors.New(longErr),
			wantPhase:          asdbv1.AerospikeClusterError,
			wantReason:         asdbv1.AerospikeClusterReasonReconcileFailed,
			wantMessageBounded: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			r := newTestReconciler(t, ac, &interceptor.Funcs{})
			r.computedState.failureReason = tc.failureReason

			if err := r.writeTerminalStatus(context.TODO(), tc.recErr); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := getCluster(t, r.Client, ac)

			if got.Status.Phase != tc.wantPhase {
				t.Errorf("want phase %s, got %s", tc.wantPhase, got.Status.Phase)
			}

			assertCondition(t, got.Status.Conditions, &metav1.Condition{
				Type:    readyType,
				Status:  metav1.ConditionFalse,
				Reason:  tc.wantReason,
				Message: tc.wantMessage,
			})

			if tc.wantMessageBounded {
				cond := findCondition(t, got.Status.Conditions, readyType)
				if len(cond.Message) > maxConditionMessageLength {
					t.Errorf("message not truncated: %d bytes", len(cond.Message))
				}
			}
		})
	}
}

// TestWriteTerminalStatus_SyncsBackToAeroCluster is separate from the table because it asserts on
// the reconciler's in-memory copy rather than on what reached the API server. Downstream code
// reads r.aeroCluster, so a missing copy-back would go unnoticed by every other test.
func TestWriteTerminalStatus_SyncsBackToAeroCluster(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	if err := r.writeTerminalStatus(context.TODO(), errors.New("timeout")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.aeroCluster.Status.Phase != asdbv1.AerospikeClusterError {
		t.Errorf("r.aeroCluster.Status.Phase not synced back: got %s", r.aeroCluster.Status.Phase)
	}

	cond := apimeta.FindStatusCondition(r.aeroCluster.Status.Conditions,
		string(asdbv1.AerospikeClusterConditionReady))
	if cond == nil {
		t.Fatal("Ready condition not synced back to r.aeroCluster")
	}

	if cond.Status != metav1.ConditionFalse {
		t.Errorf("r.aeroCluster Ready: want False, got %s", cond.Status)
	}
}

// ---- finishReconcile -------------------------------------------------------

// TestFinishReconcile_DeletedClusterLeavesNoPhaseMetric pins the deletion path.
func TestFinishReconcile_DeletedClusterLeavesNoPhaseMetric(t *testing.T) {
	// Not parallel: this asserts on the package-global phase GaugeVec.
	const deletedCluster = "deleted-cluster"

	now := metav1.Now()

	ac := getMinimalCluster()
	ac.Name = deletedCluster
	ac.DeletionTimestamp = &now
	// The fake client rejects an object carrying a DeletionTimestamp with no finalizer.
	ac.Finalizers = []string{finalizerName}
	ac.Status.Phase = asdbv1.AerospikeClusterCompleted

	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	// An ordinary pass registers one series per phase.
	r.addClusterPhaseMetric()

	if got := clusterPhaseSeries(t, deletedCluster); got != len(phases) {
		t.Fatalf("precondition: want %d series, got %d", len(phases), got)
	}

	// handleTerminatingCluster drops them on a successful delete.
	r.removeClusterPhaseMetric()

	if got := clusterPhaseSeries(t, deletedCluster); got != 0 {
		t.Fatalf("precondition: want the gauge cleared, got %d series", got)
	}

	// The deferred finishReconcile then fires with a nil recErr.
	if err := r.finishReconcile(context.TODO(), ctrl.Result{}, nil); err != nil {
		t.Fatalf("finishReconcile: %v", err)
	}

	if got := clusterPhaseSeries(t, deletedCluster); got != 0 {
		t.Errorf("%d phase series left behind for a deleted cluster", got)
	}
}

// TestFinishReconcile_FailedDeletionStillReportsError is the counterpart to the test above. The
// deletion guard is deliberately narrow: a delete that failed leaves the object alive with its
// finalizer intact, so the status write must still run and phase=Error must land.
func TestFinishReconcile_FailedDeletionStillReportsError(t *testing.T) {
	t.Parallel()

	deleteErr := errors.New("delete external resources: nope")
	now := metav1.Now()

	ac := getMinimalCluster()
	ac.DeletionTimestamp = &now
	ac.Finalizers = []string{finalizerName}
	ac.Status.Phase = asdbv1.AerospikeClusterInProgress

	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	r.addClusterPhaseMetric()

	err := r.finishReconcile(context.TODO(), ctrl.Result{}, deleteErr)
	if !errors.Is(err, deleteErr) {
		t.Errorf("want the delete error returned, got %v", err)
	}

	if got := getCluster(t, r.Client, ac).Status.Phase; got != asdbv1.AerospikeClusterError {
		t.Errorf("want phase Error for a failed deletion, got %s", got)
	}

	if got := clusterPhaseSeries(t, ac.Name); got != len(phases) {
		t.Errorf("%d phase series left behind for a deleted failed cluster", got)
	}
}

func TestFinishReconcile(t *testing.T) {
	t.Parallel()

	recErr := errors.New("rack reconcile blew up")

	testCases := []struct {
		recErr error
		name   string
		// wantPhase is checked only when set.
		wantPhase asdbv1.AerospikeClusterPhase
		// wantErrs are the errors that must all be present in the result.
		wantErrs  []error
		failPatch bool
		// wantScalingUpCleared asserts the exit policy actually ran.
		wantScalingUpCleared bool
	}{
		{
			name:                 "clean pass applies the exit policy",
			wantScalingUpCleared: true,
		},
		{
			name:      "error path joins the status write failure onto the reconcile error",
			recErr:    recErr,
			failPatch: true,
			wantErrs:  []error{recErr, errStatusPatch},
		},
		{
			// The ordinary failure path: the reconcile failed and the status write succeeded. recErr
			// must come back unchanged so controller-runtime requeues, and the Error phase must land.
			name:      "error path returns recErr unchanged when the status write succeeds",
			recErr:    recErr,
			wantErrs:  []error{recErr},
			wantPhase: asdbv1.AerospikeClusterError,
		},
		{
			// The phase must stay put: writeTerminalStatus ran with a nil recErr, so it never
			// touched it. This retries without mislabelling the cluster as Error.
			name:      "clean pass surfaces a status write failure without changing the phase",
			failPatch: true,
			wantErrs:  []error{errStatusPatch},
			wantPhase: asdbv1.AerospikeClusterInProgress,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			ac.Status.Phase = asdbv1.AerospikeClusterInProgress
			ac.Status.Conditions = []metav1.Condition{
				opConditionTrue(scalingUp, asdbv1.AerospikeClusterReasonScalingUp),
			}

			funcs := &interceptor.Funcs{}
			if tc.failPatch {
				funcs = failingStatusPatch()
			}

			r := newTestReconciler(t, ac, funcs)
			r.initPendingOpConditionReset() // armed, nothing claimed → the reset will patch

			err := r.finishReconcile(context.TODO(), ctrl.Result{}, tc.recErr)

			for _, want := range tc.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("result must contain %v, got %v", want, err)
				}
			}

			if len(tc.wantErrs) == 0 && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			got := getCluster(t, r.Client, ac)

			if tc.wantPhase != "" && got.Status.Phase != tc.wantPhase {
				t.Errorf("want phase %s, got %s", tc.wantPhase, got.Status.Phase)
			}

			if tc.wantScalingUpCleared {
				cond := findCondition(t, got.Status.Conditions, scalingUp)
				if cond.Status != metav1.ConditionFalse {
					t.Errorf("exit policy not applied: ScalingUp=%s", cond.Status)
				}
			}
		})
	}
}

// ---- handleTerminatingCluster ----------------------------------------------

func TestHandleTerminatingCluster(t *testing.T) {
	t.Parallel()

	errUpdate := errors.New("simulated finalizer removal failure")

	testCases := []struct {
		wantErr    error
		name       string
		finalizers []string
		// failStatus makes the Ready=False/Terminating write fail.
		failStatus bool
		// failUpdate makes finalizer removal fail.
		failUpdate bool
		// wantTerminating asserts the condition reached the API server.
		wantTerminating bool
	}{
		{
			name:            "marks the cluster not ready while it is torn down",
			wantTerminating: true,
		},
		{
			// The object may already be gone, so a NotFound on the condition write is expected
			// here and must never block finalizer removal.
			name:       "continues when the condition write fails",
			failStatus: true,
		},
		{
			// Unlike the condition write, a failure to clean up and drop the finalizer has to
			// surface, or the cluster would be released while its resources still exist.
			name:       "propagates a deletion failure",
			finalizers: []string{finalizerName},
			failUpdate: true,
			wantErr:    errUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			ac.Finalizers = tc.finalizers

			funcs := &interceptor.Funcs{}

			switch {
			case tc.failStatus:
				funcs = failingStatusPatch()
			case tc.failUpdate:
				// Finalizer removal goes through a plain Update.
				funcs = &interceptor.Funcs{
					Update: func(
						_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption,
					) error {
						return errUpdate
					},
				}
			}

			r := newTestReconciler(t, ac, funcs)

			err := r.handleTerminatingCluster(context.TODO())

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v surfaced, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantTerminating {
				assertCondition(t, getCluster(t, r.Client, ac).Status.Conditions, &metav1.Condition{
					Type:   string(asdbv1.AerospikeClusterConditionReady),
					Status: metav1.ConditionFalse,
					Reason: asdbv1.AerospikeClusterReasonTerminating,
				})
			}
		})
	}
}
