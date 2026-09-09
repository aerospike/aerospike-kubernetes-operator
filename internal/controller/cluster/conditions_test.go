package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	readyType   = string(asdbv1.AerospikeClusterConditionReady)
	scalingUp   = string(asdbv1.AerospikeClusterConditionScalingUp)
	scalingDown = string(asdbv1.AerospikeClusterConditionScalingDown)
	upgrading   = string(asdbv1.AerospikeClusterConditionUpgrading)
	paused      = string(asdbv1.AerospikeClusterConditionPaused)
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

	// Ready + the five operation conditions + Paused.
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

// ---- setConditions / mergePatchStatus --------------------------------------

// setConditions is a thin wrapper over mergePatchStatus, so it is covered only where it adds
// something: batching several conditions into one patch. The phase half of mergePatchStatus is
// covered separately, including its error branch.
func TestSetConditions_SetsMultipleConditionsInOnePatch(t *testing.T) {
	t.Parallel()

	// Counting patches asserts the batching directly. It counts the number of status update done.
	patches := 0

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{
		SubResourcePatch: func(
			ctx context.Context, c client.Client, sub string, obj client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption,
		) error {
			patches++

			return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
		},
	})

	if err := r.setConditions(context.TODO(),
		opConditionTrue(string(asdbv1.AerospikeClusterConditionScalingUp), asdbv1.AerospikeClusterReasonScalingUp),
		opConditionAtRest(string(asdbv1.AerospikeClusterConditionReady), asdbv1.AerospikeClusterReasonReconciling),
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getCluster(t, r.Client, ac)

	scalingUpCond := findCondition(t, got.Status.Conditions, string(asdbv1.AerospikeClusterConditionScalingUp))
	if scalingUpCond.Status != metav1.ConditionTrue {
		t.Errorf("want ScalingUp=True, got %s", scalingUpCond.Status)
	}

	ready := findCondition(t, got.Status.Conditions, string(asdbv1.AerospikeClusterConditionReady))
	if ready.Status != metav1.ConditionFalse {
		t.Errorf("want Ready=False, got %s", ready.Status)
	}

	// Both conditions must arrive in a single patch, not one each.
	if patches != 1 {
		t.Errorf("want 1 patch for 2 conditions, got %d", patches)
	}
}

func TestMergePatchStatus_Phase(t *testing.T) {
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

			err := r.mergePatchStatus(context.TODO(), &tc.want)

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

func TestSetConditions_NeverDuplicatesAConditionType(t *testing.T) {
	t.Parallel()

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

// ---- operation condition claim and reset -----------------------------------

// TestClaimSurvivesAlreadyTrueCondition pins the ordering inside mergePatchStatus: the claim
// must be recorded even when the condition is already True and no patch is issued. Batch two
// onwards of a batched operation hits exactly this path, and losing the claim there would let
// the exit path clear a condition whose operation is still running.
func TestMergePatchStatus_ClaimsUnchangedTrueCondition(t *testing.T) {
	t.Parallel()

	ac := getMinimalCluster()
	r := newTestReconciler(t, ac, &interceptor.Funcs{})
	r.initPendingOpConditionReset()

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

	if r.computedState.pendingOpReset.Has(scalingUp) {
		t.Error("ScalingUp was not claimed when the condition was already True")
	}
}

// TestResetOpConditions covers the whole exit-policy rule in one place. pendingOpReset means
// "the operation conditions this pass may clear": nil clears nothing, and a condition claimed by
// a rack function is removed from the set so it survives.
func TestResetOpConditions(t *testing.T) {
	t.Parallel()

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
			// Different operation per reconcile pass. The unclaimed one must clear (the accumulation fix) and the
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

// Update Status resets all five operations conditions
func TestUpdateStatusClearsCompletedOperation(t *testing.T) {
	t.Parallel()

	// updateStatus runs CopySpecToStatus, so this needs a cluster with a real spec.
	ac := newTestAerospikeCluster(namespace, clusterName)
	r := newTestReconciler(t, ac, &interceptor.Funcs{})
	r.initPendingOpConditionReset()

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
