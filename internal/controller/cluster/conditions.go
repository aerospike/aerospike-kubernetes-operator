package cluster

import (
	"context"
	"slices"
	"unicode/utf8"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// maxConditionMessageLength bounds what we write into a condition's message. The CRD caps
// the field at 32768; an AerospikeCluster error chain can embed config diffs and pod lists,
// and an overlong message makes the API server reject the whole patch — which would lose the
// phase=Error write riding along with it.
const maxConditionMessageLength = 2048

// operationConditions lists the "operation in progress" conditions
// paired with the reason used when each is in its resting (False) state.
// Paused is intentionally excluded: it reflects a user action (spec.paused=true),
// not an operator-driven operation.
var operationConditions = []struct {
	condType    string
	falseReason string
}{
	{string(asdbv1.AerospikeClusterConditionScalingUp), asdbv1.AerospikeClusterReasonNotScalingUp},
	{string(asdbv1.AerospikeClusterConditionScalingDown), asdbv1.AerospikeClusterReasonNotScalingDown},
	{string(asdbv1.AerospikeClusterConditionUpgrading), asdbv1.AerospikeClusterReasonNotUpgrading},
	{string(asdbv1.AerospikeClusterConditionRollingRestart), asdbv1.AerospikeClusterReasonNotRollingRestart},
	{
		string(asdbv1.AerospikeClusterConditionRackRevisionRollingOut),
		asdbv1.AerospikeClusterReasonNotRackRevisionRollingOut,
	},
}

// initPendingOpConditionReset takes ownership of the operation conditions for this reconcile: every
// one of them becomes eligible for reset unless a rack function claims it.
// Called immediately before the rack loop, deliberately as late as possible: every stage that runs
// earlier doesn't claim any operation condition.
func (r *SingleClusterReconciler) initPendingOpConditionReset() {
	r.computedState.pendingOpReset = sets.New[string]()

	for _, opCond := range operationConditions {
		r.computedState.pendingOpReset.Insert(opCond.condType)
	}
}

// opConditionAtRest returns the resting (False) form of an operation condition. Shared so the
// first-reconcile seed and the exit-path clear cannot drift on status or reason.
func opConditionAtRest(condType, falseReason string) metav1.Condition {
	return metav1.Condition{
		Type:   condType,
		Status: metav1.ConditionFalse,
		Reason: falseReason,
	}
}

// opConditionsToClear returns the resting form of every operation condition this pass is
// still allowed to clear. A condition claimed by a rack function is omitted, so an operation
// spanning a requeue keeps reporting.
//
// A claimed condition is omitted even when the operation completed: on the success path
// updateStatus clears it, atomically with Ready=True.
func (r *SingleClusterReconciler) opConditionsToClear() []metav1.Condition {
	conditions := make([]metav1.Condition, 0, len(operationConditions))

	for _, opCond := range operationConditions {
		if !r.computedState.pendingOpReset.Has(opCond.condType) {
			continue
		}

		conditions = append(conditions, opConditionAtRest(opCond.condType, opCond.falseReason))
	}

	return conditions
}

// truncateConditionMessage bounds msg, backing off to a rune boundary so the result stays
// valid UTF-8 (invalid UTF-8 in a JSON string is itself rejected).
func truncateConditionMessage(msg string) string {
	if len(msg) <= maxConditionMessageLength {
		return msg
	}

	const suffix = "... (truncated)"

	cut := maxConditionMessageLength - len(suffix)
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}

	return msg[:cut] + suffix
}

// setConditions updates one or more conditions on the AerospikeCluster status using a
// merge patch.
// ObservedGeneration is stamped only when a condition's Status, Reason, or Message actually
// changes — consistent with how LastTransitionTime behaves. Conditions already in the
// desired state are left untouched (no ObservedGeneration bump, no API call).
func (r *SingleClusterReconciler) setConditions(ctx context.Context, conditions ...metav1.Condition) error {
	return r.mergePatchStatus(ctx, nil, conditions...)
}

// initializeConditionsIfNeeded pre-seeds all conditions on the very first reconcile
// so that `kubectl wait --for=condition=Ready` and similar commands do not hang,
// and the status shape is consistent from the start.
func (r *SingleClusterReconciler) initializeConditionsIfNeeded(ctx context.Context) error {
	if len(r.aeroCluster.Status.Conditions) > 0 {
		return nil
	}

	seedConditions := []metav1.Condition{
		{
			Type:    string(asdbv1.AerospikeClusterConditionReady),
			Status:  metav1.ConditionUnknown,
			Reason:  asdbv1.AerospikeClusterReasonInitializing,
			Message: "Cluster conditions not yet evaluated",
		},
		// Paused is not an operation condition but still needs an initial resting state.
		{
			Type:   string(asdbv1.AerospikeClusterConditionPaused),
			Status: metav1.ConditionFalse,
			Reason: asdbv1.AerospikeClusterReasonNotPaused,
		},
	}

	// Every operation condition seeds in the same resting state the exit path clears to.
	for _, opCond := range operationConditions {
		seedConditions = append(seedConditions, opConditionAtRest(opCond.condType, opCond.falseReason))
	}

	return r.setConditions(ctx, seedConditions...)
}

// mergePatchStatus applies an optional phase change and any number of conditions to the
// AerospikeCluster status in a single guarded merge patch. It is the shared primitive behind
// setConditions and writeTerminalStatus, and is called directly wherever a phase change and a
// condition change must land together.
//
// Whether anything changed is decided by SetStatusCondition's own return value, applied to a
// clone of just the conditions slice.
// Phase is applied only when it differs. Only status fields and resourceVersion are copied back
// onto r.aeroCluster, so the spec is never overwritten.
func (r *SingleClusterReconciler) mergePatchStatus(
	ctx context.Context, phase *asdbv1.AerospikeClusterPhase, conditions ...metav1.Condition,
) error {
	candidate := slices.Clone(r.aeroCluster.Status.Conditions)
	condChanged := false

	// Never return early from this loop: every True condition must be claimed even when it
	// needs no patch.
	for i := range conditions {
		if conditions[i].Status == metav1.ConditionTrue && r.computedState.pendingOpReset != nil {
			r.computedState.pendingOpReset.Delete(conditions[i].Type)
		}

		// Copy so the caller's input slice is not mutated.
		cond := conditions[i]
		cond.ObservedGeneration = r.aeroCluster.Generation

		if apimeta.SetStatusCondition(&candidate, cond) {
			condChanged = true
		}
	}

	phaseChanged := phase != nil && r.aeroCluster.Status.Phase != *phase

	if !condChanged && !phaseChanged {
		// This emits metics for scenarios when the CR is in Error phase and the AKO pods are restarted.
		// Without this restarted pod doesn't emit metrics unless the Error phase is recovered.
		r.addClusterPhaseMetric()
		return nil
	}

	patchTarget := r.aeroCluster.DeepCopy()
	patch := client.MergeFrom(r.aeroCluster.DeepCopy())

	patchTarget.Status.Conditions = candidate

	if phaseChanged {
		patchTarget.Status.Phase = *phase
	}

	if err := r.Client.Status().Patch(ctx, patchTarget, patch); err != nil {
		return err
	}

	// Copy back only status and resourceVersion so r.aeroCluster.Spec is never overwritten.
	r.aeroCluster.Status.Conditions = patchTarget.Status.Conditions
	r.aeroCluster.Status.Phase = patchTarget.Status.Phase
	r.aeroCluster.ResourceVersion = patchTarget.ResourceVersion

	if phaseChanged {
		r.addClusterPhaseMetric()
	}

	return nil
}
