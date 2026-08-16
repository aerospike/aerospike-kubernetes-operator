package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// ---- truncateConditionMessage ----------------------------------------------

// TestTruncateConditionMessage covers the bound that exists because the CRD caps
// status.conditions[].message at 32768 (inherited from metav1.Condition's kubebuilder marker).
// An overlong message makes the API server reject the whole patch, which would also lose the
// phase=Error write riding along with it.
func TestTruncateConditionMessage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// in is built from a repeat count so the cases stay readable.
		unit      string
		repeat    int
		unchanged bool
	}{
		{name: "well under the limit", unit: "a", repeat: 5, unchanged: true},
		{name: "one byte under the limit", unit: "a", repeat: maxConditionMessageLength - 1, unchanged: true},
		{name: "exactly at the limit", unit: "a", repeat: maxConditionMessageLength, unchanged: true},
		{name: "one byte over the limit", unit: "a", repeat: maxConditionMessageLength + 1},
		{name: "far over the limit", unit: "a", repeat: maxConditionMessageLength * 3},
		// A naive byte cut would split a 3-byte rune, and invalid UTF-8 in a JSON string is
		// itself rejected by the API server — trading one rejection for another.
		{name: "multi-byte runes over the limit", unit: "→", repeat: maxConditionMessageLength},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := strings.Repeat(tc.unit, tc.repeat)
			got := truncateConditionMessage(in)

			if tc.unchanged {
				if got != in {
					t.Errorf("message of %d bytes should pass through untouched, got %d bytes",
						len(in), len(got))
				}

				return
			}

			if len(got) > maxConditionMessageLength {
				t.Errorf("result exceeds the limit: %d bytes", len(got))
			}

			if !strings.HasSuffix(got, "(truncated)") {
				t.Error("truncation marker missing")
			}

			if !utf8.ValidString(got) {
				t.Error("truncation produced invalid UTF-8")
			}
		})
	}
}

// ---- initializeConditionsIfNeeded ------------------------------------------

func TestInitializeConditionsIfNeeded_SeedsAllConditions(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	if err := r.initializeConditionsIfNeeded(context.TODO()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Re-fetch to confirm the patch reached the fake API server.
	conds := getCluster(t, r.Client, ac).Status.Conditions

	// Ready + the four operation conditions + Paused.
	expectedCount := 1 + len(operationConditions) + 1
	if len(conds) != expectedCount {
		t.Fatalf("want %d conditions, got %d: %v", expectedCount, len(conds), conds)
	}

	// Ready starts as Unknown/Initializing.
	ready := findCondition(t, conds, string(asdbv1.AerospikeClusterConditionReady))
	if ready.Status != metav1.ConditionUnknown {
		t.Errorf("Ready status: want Unknown, got %s", ready.Status)
	}

	if ready.Reason != asdbv1.AerospikeClusterReasonInitializing {
		t.Errorf("Ready reason: want %s, got %s", asdbv1.AerospikeClusterReasonInitializing, ready.Reason)
	}

	// Every operation condition starts at rest. Driven off operationConditions so a new
	// operation condition cannot be added without being seeded.
	for _, op := range operationConditions {
		cond := findCondition(t, conds, op.condType)

		if cond.Status != metav1.ConditionFalse {
			t.Errorf("condition %s: want False, got %s", op.condType, cond.Status)
		}

		if cond.Reason != op.falseReason {
			t.Errorf("condition %s: want reason %s, got %s", op.condType, op.falseReason, cond.Reason)
		}
	}

	paused := findCondition(t, conds, string(asdbv1.AerospikeClusterConditionPaused))
	if paused.Status != metav1.ConditionFalse {
		t.Errorf("Paused status: want False, got %s", paused.Status)
	}

	if paused.Reason != asdbv1.AerospikeClusterReasonNotPaused {
		t.Errorf("Paused reason: want %s, got %s", asdbv1.AerospikeClusterReasonNotPaused, paused.Reason)
	}
}

func TestInitializeConditionsIfNeeded_ObservedGenerationStamped(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	ac.Generation = 7
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	if err := r.initializeConditionsIfNeeded(context.TODO()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cond := range getCluster(t, r.Client, ac).Status.Conditions {
		if cond.ObservedGeneration != 7 {
			t.Errorf("condition %s: want ObservedGeneration 7, got %d", cond.Type, cond.ObservedGeneration)
		}
	}
}

func TestInitializeConditionsIfNeeded_Idempotent(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	if err := r.initializeConditionsIfNeeded(context.TODO()); err != nil {
		t.Fatalf("first call: %v", err)
	}

	firstRV := getCluster(t, r.Client, ac).ResourceVersion

	// A second call must issue no patch, so ResourceVersion stays put.
	if err := r.initializeConditionsIfNeeded(context.TODO()); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if rv := getCluster(t, r.Client, ac).ResourceVersion; rv != firstRV {
		t.Errorf("ResourceVersion changed on second call (%s → %s): expected no-op", firstRV, rv)
	}
}

// ---- mergePatchStatus ------------------------------------------------------

func TestMergePatchStatus(t *testing.T) {
	t.Parallel()

	readyType := string(asdbv1.AerospikeClusterConditionReady)

	testCases := []struct {
		phase          *asdbv1.AerospikeClusterPhase // proposed
		wantCond       *metav1.Condition             // expected
		name           string
		seedPhase      asdbv1.AerospikeClusterPhase // seed
		wantPhase      asdbv1.AerospikeClusterPhase // expected
		seedConditions []metav1.Condition           // seed
		conditions     []metav1.Condition           // proposed
		failPatch      bool
		wantWrite      bool
	}{
		{
			// The seeded condition carries the cluster's generation deliberately:
			// ObservedGeneration is always stamped, so a condition left at generation 0 is
			// genuinely stale and restamping it would be a real change.
			name:      "no write when neither condition nor phase changes",
			seedPhase: asdbv1.AerospikeClusterCompleted,
			seedConditions: []metav1.Condition{{
				Type:               readyType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: minimalClusterGeneration,
				Reason:             asdbv1.AerospikeClusterReasonReconcileComplete,
			}},
			phase: phasePtr(asdbv1.AerospikeClusterCompleted),
			conditions: []metav1.Condition{{
				Type:   readyType,
				Status: metav1.ConditionTrue,
				Reason: asdbv1.AerospikeClusterReasonReconcileComplete,
			}},
			wantPhase: asdbv1.AerospikeClusterCompleted,
		},
		{
			name: "applies a condition and stamps ObservedGeneration",
			conditions: []metav1.Condition{{
				Type:    readyType,
				Status:  metav1.ConditionFalse,
				Reason:  asdbv1.AerospikeClusterReasonReconcileFailed,
				Message: "something went wrong",
			}},
			wantWrite: true,
			wantCond: &metav1.Condition{
				Type:               readyType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: minimalClusterGeneration,
				Reason:             asdbv1.AerospikeClusterReasonReconcileFailed,
				Message:            "something went wrong",
			},
		},
		{
			name:      "applies a phase with no conditions",
			phase:     phasePtr(asdbv1.AerospikeClusterInProgress),
			wantWrite: true,
			wantPhase: asdbv1.AerospikeClusterInProgress,
		},
		{
			name:  "applies a condition and a phase in one patch",
			phase: phasePtr(asdbv1.AerospikeClusterError),
			conditions: []metav1.Condition{{
				Type:   readyType,
				Status: metav1.ConditionFalse,
				Reason: asdbv1.AerospikeClusterReasonReconcileFailed,
			}},
			wantWrite: true,
			wantPhase: asdbv1.AerospikeClusterError,
			wantCond: &metav1.Condition{
				Type:               readyType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: minimalClusterGeneration,
				Reason:             asdbv1.AerospikeClusterReasonReconcileFailed,
			},
		},
		{
			name: "surfaces a patch failure",
			conditions: []metav1.Condition{{
				Type:   readyType,
				Status: metav1.ConditionFalse,
				Reason: asdbv1.AerospikeClusterReasonReconciling,
			}},
			failPatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			ac.Status.Phase = tc.seedPhase
			ac.Status.Conditions = tc.seedConditions

			funcs := &interceptor.Funcs{}
			if tc.failPatch {
				funcs = failingStatusPatch()
			}

			r := newTestReconciler(t, ac, funcs)
			initialRV := getCluster(t, r.Client, ac).ResourceVersion

			err := r.mergePatchStatus(context.TODO(), tc.phase, tc.conditions...)

			if tc.failPatch {
				if !errors.Is(err, errStatusPatch) {
					t.Fatalf("want the patch error surfaced, got %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := getCluster(t, r.Client, ac)

			if wrote := got.ResourceVersion != initialRV; wrote != tc.wantWrite {
				t.Errorf("wrote=%v, want %v (ResourceVersion %s → %s)",
					wrote, tc.wantWrite, initialRV, got.ResourceVersion)
			}

			if tc.wantPhase != "" && got.Status.Phase != tc.wantPhase {
				t.Errorf("want phase %s, got %s", tc.wantPhase, got.Status.Phase)
			}

			if tc.wantCond != nil {
				assertCondition(t, got.Status.Conditions, tc.wantCond)
			}
		})
	}
}

// TestMergePatchStatus_DoesNotMutateCallerCondition is separate from the table because it
// asserts on the caller's own slice rather than on the resulting status: the internal
// ObservedGeneration stamp must not reach back into the value the caller passed.
func TestMergePatchStatus_DoesNotMutateCallerCondition(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	input := metav1.Condition{
		Type:               string(asdbv1.AerospikeClusterConditionReady),
		Status:             metav1.ConditionFalse,
		Reason:             asdbv1.AerospikeClusterReasonReconcileFailed,
		ObservedGeneration: 0, // caller leaves this at zero
	}

	if err := r.mergePatchStatus(context.TODO(), nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.ObservedGeneration != 0 {
		t.Errorf("caller's condition was mutated: ObservedGeneration = %d, want 0", input.ObservedGeneration)
	}
}

// ---- setConditions / setStatusPhase ----------------------------------------

// Both are thin wrappers over mergePatchStatus, so they are covered only where they add
// something: setConditions batching several conditions into one patch, and setStatusPhase's
// error branch.

func TestSetConditions_SetsMultipleConditionsInOnePatch(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})
	initialRV := getCluster(t, r.Client, ac).ResourceVersion

	if err := r.setConditions(context.TODO(),
		opConditionTrue(string(asdbv1.AerospikeClusterConditionScalingUp), asdbv1.AerospikeClusterReasonScalingUp),
		opConditionAtRest(string(asdbv1.AerospikeClusterConditionReady), asdbv1.AerospikeClusterReasonReconciling),
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getCluster(t, r.Client, ac)

	scalingUp := findCondition(t, got.Status.Conditions, string(asdbv1.AerospikeClusterConditionScalingUp))
	if scalingUp.Status != metav1.ConditionTrue {
		t.Errorf("want ScalingUp=True, got %s", scalingUp.Status)
	}

	ready := findCondition(t, got.Status.Conditions, string(asdbv1.AerospikeClusterConditionReady))
	if ready.Status != metav1.ConditionFalse {
		t.Errorf("want Ready=False, got %s", ready.Status)
	}

	// Both conditions must arrive in a single patch, not one each.
	if rv := got.ResourceVersion; rv == initialRV {
		t.Fatal("no patch was issued")
	} else if bumps := resourceVersionBumps(t, initialRV, rv); bumps != 1 {
		t.Errorf("want 1 patch for 2 conditions, got %d", bumps)
	}
}

func TestSetStatusPhase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		seedPhase asdbv1.AerospikeClusterPhase
		want      asdbv1.AerospikeClusterPhase
		failPatch bool
		wantWrite bool
	}{
		{
			name:      "changes the phase",
			want:      asdbv1.AerospikeClusterInProgress,
			wantWrite: true,
		},
		{
			name:      "no-op when already at that phase",
			seedPhase: asdbv1.AerospikeClusterCompleted,
			want:      asdbv1.AerospikeClusterCompleted,
		},
		{
			name:      "surfaces a patch failure",
			want:      asdbv1.AerospikeClusterInProgress,
			failPatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			ac.Status.Phase = tc.seedPhase

			funcs := &interceptor.Funcs{}
			if tc.failPatch {
				funcs = failingStatusPatch()
			}

			r := newTestReconciler(t, ac, funcs)
			initialRV := getCluster(t, r.Client, ac).ResourceVersion

			err := r.setStatusPhase(context.TODO(), tc.want)

			if tc.failPatch {
				if !errors.Is(err, errStatusPatch) {
					t.Fatalf("want the patch error surfaced, got %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := getCluster(t, r.Client, ac)
			if got.Status.Phase != tc.want {
				t.Errorf("want phase %s, got %s", tc.want, got.Status.Phase)
			}

			if wrote := got.ResourceVersion != initialRV; wrote != tc.wantWrite {
				t.Errorf("wrote=%v, want %v (ResourceVersion %s → %s)",
					wrote, tc.wantWrite, initialRV, got.ResourceVersion)
			}
		})
	}
}

// TestSetConditions_NeverDuplicatesAConditionType is the operator-side guarantee that replaces
// an envtest assertion we had to drop: the CRD marks conditions as a list-map keyed on type, but
// the API server only enforces that on newer versions — k8s 1.23, which CI still targets, accepts
// a duplicate and stores both entries. So uniqueness cannot be delegated to the server.
//
// It holds here because every condition write goes through apimeta.SetStatusCondition, which
// matches on type and updates in place rather than appending.
func TestSetConditions_NeverDuplicatesAConditionType(t *testing.T) {
	t.Parallel()

	readyType := string(asdbv1.AerospikeClusterConditionReady)

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	// Three writes of the same type with differing content, so each is a real change.
	writes := []metav1.Condition{
		{Type: readyType, Status: metav1.ConditionUnknown, Reason: asdbv1.AerospikeClusterReasonInitializing},
		{Type: readyType, Status: metav1.ConditionFalse, Reason: asdbv1.AerospikeClusterReasonReconciling},
		{Type: readyType, Status: metav1.ConditionTrue, Reason: asdbv1.AerospikeClusterReasonReconcileComplete},
	}

	if err := r.setConditions(context.TODO(), writes...); err != nil {
		t.Fatalf("set conditions: %v", err)
	}

	conds := getCluster(t, r.Client, ac).Status.Conditions

	if len(conds) != 1 {
		t.Fatalf("want exactly 1 condition after 3 writes of the same type, got %d: %v", len(conds), conds)
	}

	// The last write must be the one that survived.
	assertCondition(t, conds, &metav1.Condition{
		Type:   readyType,
		Status: metav1.ConditionTrue,
		Reason: asdbv1.AerospikeClusterReasonReconcileComplete,
	})
}

// ---- writeTerminalStatus ---------------------------------------------------

func TestWriteTerminalStatus(t *testing.T) {
	t.Parallel()

	readyType := string(asdbv1.AerospikeClusterConditionReady)
	longErr := strings.Repeat("x", maxConditionMessageLength*2)

	testCases := []struct {
		name        string
		recErr      error
		stageReason string
		wantPhase   asdbv1.AerospikeClusterPhase
		wantReason  string
		wantMessage string
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
			// Reconcile records the stage it bailed out at before returning the error, so a rack
			// problem can be told apart from an access-control or roster problem without parsing
			// the message.
			name:        "prefers the stage reason Reconcile recorded",
			recErr:      errors.New("reconcile PodDisruptionBudget: nope"),
			stageReason: asdbv1.AerospikeClusterReasonPSBReconcileFailed,
			wantPhase:   asdbv1.AerospikeClusterError,
			wantReason:  asdbv1.AerospikeClusterReasonPSBReconcileFailed,
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
			r.stageReason = tc.stageReason

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

// ---- operation condition claim and reset -----------------------------------

// TestClaimSurvivesAlreadyTrueCondition pins the ordering inside mergePatchStatus: the claim
// must be recorded even when the condition is already True and no patch is issued. Batch two
// onwards of a batched operation hits exactly this path, and losing the claim there would let
// the exit path clear a condition whose operation is still running.
func TestClaimSurvivesAlreadyTrueCondition(t *testing.T) {
	t.Parallel()

	scalingUp := string(asdbv1.AerospikeClusterConditionScalingUp)

	ac := getMinimalCluster()
	r := armedReconciler(t, ac)

	cond := opConditionTrue(scalingUp, asdbv1.AerospikeClusterReasonScalingUp)

	// First pass writes it.
	if err := r.setConditions(context.TODO(), cond); err != nil {
		t.Fatalf("first setConditions: %v", err)
	}

	// Next pass: re-arm, then set the same already-True condition. No patch is issued because
	// nothing changed, but the claim must still land.
	r.initPendingOpConditionReset()

	rvBefore := getCluster(t, r.Client, ac).ResourceVersion

	if err := r.setConditions(context.TODO(), cond); err != nil {
		t.Fatalf("second setConditions: %v", err)
	}

	if rv := getCluster(t, r.Client, ac).ResourceVersion; rv != rvBefore {
		t.Errorf("expected no patch for an unchanged condition (%s → %s)", rvBefore, rv)
	}

	if r.pendingOpReset.Has(scalingUp) {
		t.Error("ScalingUp was not claimed when the condition was already True")
	}
}

// TestResetOpConditions covers the whole exit-policy rule in one place. pendingOpReset means
// "the operation conditions this pass may clear": nil clears nothing, and a condition claimed by
// a rack function is removed from the set so it survives.
func TestResetOpConditions(t *testing.T) {
	t.Parallel()

	var (
		scalingUp   = string(asdbv1.AerospikeClusterConditionScalingUp)
		scalingDown = string(asdbv1.AerospikeClusterConditionScalingDown)
	)

	testCases := []struct {
		// wantStatus is the expected status per condition type after the exit path runs.
		wantStatus map[string]metav1.ConditionStatus
		name       string
		seedTrue   []string // True conditions left behind by an earlier pass
		claim      []string // claimed during this pass, as a rack function would
		armed      bool
		wantWrite  bool
	}{
		{
			// Covers every path that returns before initPendingOpConditionReset runs: cluster
			// deletion, spec.paused, and any failure ahead of the rack loop. Conditions must be
			// left exactly as the previous pass left them, and no patch may be issued — the
			// object may be mid-deletion.
			name:       "unarmed pass freezes everything",
			seedTrue:   []string{scalingUp},
			armed:      false,
			wantStatus: map[string]metav1.ConditionStatus{scalingUp: metav1.ConditionTrue},
		},
		{
			// The rack-migration sequence: a previous pass left ScalingUp True and this pass
			// claims ScalingDown. The unclaimed one must clear (the accumulation fix) and the
			// claimed one must survive (freeze-on-interruption).
			name:     "clears unclaimed, keeps claimed",
			seedTrue: []string{scalingUp},
			armed:    true,
			claim:    []string{scalingDown},
			wantStatus: map[string]metav1.ConditionStatus{
				scalingUp:   metav1.ConditionFalse,
				scalingDown: metav1.ConditionTrue,
			},
			wantWrite: true,
		},
		{
			// An operation that ran this pass keeps its condition, so a reconcile that fails
			// mid-upgrade still reports which operation was interrupted.
			name:  "keeps a claimed condition with nothing stale to clear",
			armed: true,
			claim: []string{string(asdbv1.AerospikeClusterConditionUpgrading)},
			wantStatus: map[string]metav1.ConditionStatus{
				string(asdbv1.AerospikeClusterConditionUpgrading): metav1.ConditionTrue,
			},
			wantWrite: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			for _, condType := range tc.seedTrue {
				ac.Status.Conditions = append(ac.Status.Conditions,
					opConditionTrue(condType, reasonForOpCondition(t, condType)))
			}

			r := newTestReconciler(t, ac, &interceptor.Funcs{})
			if tc.armed {
				r.initPendingOpConditionReset()
			}

			for _, condType := range tc.claim {
				if err := r.setConditions(context.TODO(),
					opConditionTrue(condType, reasonForOpCondition(t, condType)),
				); err != nil {
					t.Fatalf("claim %s: %v", condType, err)
				}
			}

			rvBefore := getCluster(t, r.Client, ac).ResourceVersion

			if err := r.writeTerminalStatus(context.TODO(), nil); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := getCluster(t, r.Client, ac)

			if wrote := got.ResourceVersion != rvBefore; wrote != tc.wantWrite {
				t.Errorf("exit path wrote=%v, want %v (ResourceVersion %s → %s)",
					wrote, tc.wantWrite, rvBefore, got.ResourceVersion)
			}

			for condType, want := range tc.wantStatus {
				if cond := findCondition(t, got.Status.Conditions, condType); cond.Status != want {
					t.Errorf("%s: want %s, got %s", condType, want, cond.Status)
				}
			}
		})
	}
}

// TestUpdateStatusClearsCompletedOperation pins the coupling that opConditionsToClear
// deliberately leaves open. It never returns a claimed condition, so an operation interrupted by
// an error keeps reporting. On the success path that job belongs to updateStatus, which clears
// all four atomically with Ready=True. If that loop is ever removed as "redundant", a completed
// operation's condition would stay True forever and this test is what catches it.
func TestUpdateStatusClearsCompletedOperation(t *testing.T) {
	t.Parallel()

	upgrading := string(asdbv1.AerospikeClusterConditionUpgrading)

	// updateStatus runs CopySpecToStatus, so this needs a cluster with a real spec.
	ac := newTestAerospikeCluster(namespace, clusterName)
	r := armedReconciler(t, ac)

	// A rack function claimed Upgrading during this pass and the operation completed.
	if err := r.setConditions(context.TODO(),
		opConditionTrue(upgrading, asdbv1.AerospikeClusterReasonUpgrading),
	); err != nil {
		t.Fatalf("setConditions: %v", err)
	}

	if err := r.updateStatus(context.TODO()); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}

	conds := getCluster(t, r.Client, ac).Status.Conditions

	if cond := findCondition(t, conds, upgrading); cond.Status != metav1.ConditionFalse {
		t.Errorf("updateStatus must clear a completed operation, got Upgrading=%s", cond.Status)
	}

	ready := findCondition(t, conds, string(asdbv1.AerospikeClusterConditionReady))
	if ready.Status != metav1.ConditionTrue {
		t.Errorf("want Ready=True on the success path, got %s", ready.Status)
	}
}

// ---- ObservedGeneration ----------------------------------------------------

// TestObservedGenerationCatchesUpOnGenerationBump pins the three properties that make
// ObservedGeneration usable as a per-generation gate:
//
//  1. a byte-identical condition proposed after a generation bump is restamped, so status always
//     reflects the generation it was evaluated against;
//  2. LastTransitionTime does NOT move for an ObservedGeneration-only change, so the record of
//     when the condition last actually transitioned survives;
//  3. with neither the generation nor the content changed it is still a no-op — restamping must
//     not reintroduce a write on every requeue.
func TestObservedGenerationCatchesUpOnGenerationBump(t *testing.T) {
	t.Parallel()

	paused := string(asdbv1.AerospikeClusterConditionPaused)

	ac := getMinimalCluster() // Generation: 1
	r := newTestReconciler(t, ac, &interceptor.Funcs{})

	cond := metav1.Condition{
		Type:    paused,
		Status:  metav1.ConditionTrue,
		Reason:  asdbv1.AerospikeClusterReasonPausedByUser,
		Message: "Reconciliation is paused via spec.paused=true",
	}

	if err := r.setConditions(context.TODO(), cond); err != nil {
		t.Fatalf("first: %v", err)
	}

	got := findCondition(t, getCluster(t, r.Client, ac).Status.Conditions, paused)
	if got.ObservedGeneration != 1 {
		t.Fatalf("gen 1: want observedGeneration 1, got %d", got.ObservedGeneration)
	}

	firstTransition := got.LastTransitionTime

	// The user edits the spec while paused: generation advances, condition content identical.
	r.aeroCluster.Generation = 2

	if err := r.setConditions(context.TODO(), cond); err != nil {
		t.Fatalf("second: %v", err)
	}

	got = findCondition(t, getCluster(t, r.Client, ac).Status.Conditions, paused)

	if got.ObservedGeneration != 2 {
		t.Errorf("observedGeneration stuck at %d after generation bumped to 2", got.ObservedGeneration)
	}

	if !got.LastTransitionTime.Equal(&firstTransition) {
		t.Errorf("LastTransitionTime moved on an observedGeneration-only change: %v → %v",
			firstTransition, got.LastTransitionTime)
	}

	if got.Status != metav1.ConditionTrue || got.Reason != asdbv1.AerospikeClusterReasonPausedByUser {
		t.Errorf("condition content changed unexpectedly: %+v", got)
	}

	// A third call with nothing changed at all must still be a no-op.
	rv := getCluster(t, r.Client, ac).ResourceVersion
	if err := r.setConditions(context.TODO(), cond); err != nil {
		t.Fatalf("third: %v", err)
	}

	if now := getCluster(t, r.Client, ac).ResourceVersion; now != rv {
		t.Errorf("steady state is no longer a no-op (%s → %s)", rv, now)
	}
}

// ---- finishReconcile -------------------------------------------------------

// TestFinishReconcile covers how the exit path treats a failed status write, which differs by
// path for a reason: on the error path the write carries phase=Error, so losing it means the
// failure was never recorded anywhere. On the clean path there is no phase change to lose, but the
// error is still surfaced — there is no SyncPeriod configured, so controller-runtime's 10-hour
// default applies and "the next pass will retry it" could mean tomorrow.
func TestFinishReconcile(t *testing.T) {
	t.Parallel()

	scalingUp := string(asdbv1.AerospikeClusterConditionScalingUp)
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
			// The phase must stay put: writeTerminalStatus ran with a nil recErr, so it never
			// touched it. This retries without mislabelling the cluster as Error.
			name:      "clean pass surfaces a status write failure without changing the phase",
			failPatch: true,
			wantErrs:  []error{errStatusPatch},
			wantPhase: asdbv1.AerospikeClusterCompleted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ac := getMinimalCluster()
			ac.Status.Phase = asdbv1.AerospikeClusterCompleted
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

// ---- ensureSCRoster --------------------------------------------------------

// TestEnsureSCRoster_NoOpForNonSCCluster covers the path taken by every cluster without strong
// consistency: it must not touch the roster or reach for a host connection, both of which would
// need a live Aerospike server.
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

// ---- helpers ---------------------------------------------------------------

// errStatusPatch is returned by failingStatusPatch so tests can assert on it with errors.Is.
var errStatusPatch = errors.New("simulated status patch failure")

// failingStatusPatch makes every status subresource patch fail, for exercising the error
// branches of the status writers.
func failingStatusPatch() *interceptor.Funcs {
	return &interceptor.Funcs{
		SubResourcePatch: func(
			_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Patch,
			_ ...client.SubResourcePatchOption,
		) error {
			return errStatusPatch
		},
	}
}

// minimalClusterGeneration is the generation getMinimalCluster sets. Named so tables can assert
// on the stamped ObservedGeneration without repeating a magic number.
const minimalClusterGeneration = 1

// assertCondition checks the fields a test cares about, treating a zero value in want as
// "don't care" so tables stay terse.
func assertCondition(t *testing.T, conditions []metav1.Condition, want *metav1.Condition) {
	t.Helper()

	got := findCondition(t, conditions, want.Type)

	if want.Status != "" && got.Status != want.Status {
		t.Errorf("%s status: want %s, got %s", want.Type, want.Status, got.Status)
	}

	if want.Reason != "" && got.Reason != want.Reason {
		t.Errorf("%s reason: want %s, got %s", want.Type, want.Reason, got.Reason)
	}

	if want.Message != "" && got.Message != want.Message {
		t.Errorf("%s message: want %q, got %q", want.Type, want.Message, got.Message)
	}

	if want.ObservedGeneration != 0 && got.ObservedGeneration != want.ObservedGeneration {
		t.Errorf("%s observedGeneration: want %d, got %d",
			want.Type, want.ObservedGeneration, got.ObservedGeneration)
	}
}

// getMinimalCluster returns a cluster with no spec, which is all most condition tests need.
// Generation is set because ObservedGeneration is stamped from it. Use
// newTestAerospikeCluster when the code under test reads the spec (e.g. updateStatus).
func getMinimalCluster() *asdbv1.AerospikeCluster {
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: minimalClusterGeneration,
		},
	}
}

// phasePtr returns a pointer to the given phase, making call sites cleaner.
func phasePtr(p asdbv1.AerospikeClusterPhase) *asdbv1.AerospikeClusterPhase {
	return &p
}

// opConditionTrue builds the True form of an operation condition, as the rack functions do.
func opConditionTrue(condType, reason string) metav1.Condition {
	return metav1.Condition{
		Type:   condType,
		Status: metav1.ConditionTrue,
		Reason: reason,
	}
}

// reasonForOpCondition looks up the True-state reason for an operation condition type, so tables
// can name a condition type without also repeating its reason.
func reasonForOpCondition(t *testing.T, condType string) string {
	t.Helper()

	switch condType {
	case string(asdbv1.AerospikeClusterConditionScalingUp):
		return asdbv1.AerospikeClusterReasonScalingUp
	case string(asdbv1.AerospikeClusterConditionScalingDown):
		return asdbv1.AerospikeClusterReasonScalingDown
	case string(asdbv1.AerospikeClusterConditionUpgrading):
		return asdbv1.AerospikeClusterReasonUpgrading
	case string(asdbv1.AerospikeClusterConditionRollingRestart):
		return asdbv1.AerospikeClusterReasonRollingRestart
	case string(asdbv1.AerospikeClusterConditionRackRevisionRollingOut):
		return asdbv1.AerospikeClusterReasonRackRevisionRollingOut
	default:
		t.Fatalf("no True-state reason known for condition %q", condType)
		return ""
	}
}

// armedReconciler returns a reconciler that has taken ownership of the operation conditions,
// matching the state Reconcile is in once it passes the paused check.
func armedReconciler(t *testing.T, ac *asdbv1.AerospikeCluster) *SingleClusterReconciler {
	t.Helper()

	r := newTestReconciler(t, ac, &interceptor.Funcs{})
	r.initPendingOpConditionReset()

	return r
}

// getCluster fetches ac from the fake API server.
func getCluster(t *testing.T, c client.Client, ac *asdbv1.AerospikeCluster) *asdbv1.AerospikeCluster {
	t.Helper()

	got := &asdbv1.AerospikeCluster{}
	if err := c.Get(t.Context(), types.NamespacedName{Name: ac.Name, Namespace: ac.Namespace}, got); err != nil {
		t.Fatalf("get cluster: %v", err)
	}

	return got
}

// findCondition wraps apimeta.FindStatusCondition and fails the test when absent.
func findCondition(t *testing.T, conditions []metav1.Condition, condType string) metav1.Condition {
	t.Helper()

	c := apimeta.FindStatusCondition(conditions, condType)
	if c == nil {
		t.Fatalf("condition %q not found in %v", condType, conditions)
		return metav1.Condition{}
	}

	return *c
}

// resourceVersionBumps reports how many writes happened between two fake-client
// ResourceVersions. The fake client uses a monotonic integer, so the difference is the count.
func resourceVersionBumps(t *testing.T, from, to string) int {
	t.Helper()

	var a, b int
	if _, err := fmt.Sscanf(from, "%d", &a); err != nil {
		t.Fatalf("parse ResourceVersion %q: %v", from, err)
	}

	if _, err := fmt.Sscanf(to, "%d", &b); err != nil {
		t.Fatalf("parse ResourceVersion %q: %v", to, err)
	}

	return b - a
}
