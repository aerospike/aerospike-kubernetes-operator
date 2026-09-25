package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

// buildScaleDownTargets collects scale-down candidate pods from scaledDownRacks
// and racksToDelete.
func (r *SingleClusterReconciler) buildScaleDownTargets(
	ctx context.Context,
	scaledDownRacks []rackWithSTS,
	racksToDelete []asdbv1.Rack,
) ([]*corev1.Pod, common.ReconcileResult) {
	var allTargets []*corev1.Pod

	for idx := range scaledDownRacks {
		removedPods, err := r.getAllScaleDownPods(ctx, scaledDownRacks[idx])
		if err != nil {
			return nil, common.ReconcileError(fmt.Errorf(
				"get scale-down pods for rack %d: %w",
				scaledDownRacks[idx].rackState.Rack.ID, err,
			))
		}

		allTargets = append(allTargets, removedPods...)
	}

	// racksToDelete: nil rackSTS → target size=0, all pods returned.
	for idx := range racksToDelete {
		rack := &racksToDelete[idx]
		entry := rackWithSTS{
			rackSTS:   nil,
			rackState: &RackState{Size: 0, Rack: rack},
		}

		removedPods, err := r.getAllScaleDownPods(ctx, entry)
		if err != nil {
			return nil, common.ReconcileError(fmt.Errorf(
				"get scale-down pods for deleted rack %d: %w",
				rack.ID, err,
			))
		}

		allTargets = append(allTargets, removedPods...)
	}

	return allTargets, common.ReconcileSuccess()
}

// reconcileQuiesceUndo restores quiesced non-target pods to full membership.
// Runs before reconcileRack for non-scaled-down racks so stale quiesces from a
// prior (possibly reverted) scale-down are undone before rolling restarts run.
//
// Annotation fast-exit: if no non-target pod carries QuiesceAnnotation all
// Aerospike calls are skipped. When undo is needed, InfoQuiesceUndoSubset is
// called with annotated non-target pods only (undoHosts) plus all pods
// (allHosts, so InfoRecluster can always reach the principal).
func (r *SingleClusterReconciler) reconcileQuiesceUndo(
	ctx context.Context,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	targetNames := podNamesToSet(allTargets)

	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("list cluster pods for quiesce-undo: %w", err))
	}

	var annotatedNonTargets []corev1.Pod

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if !targetNames.Has(pod.Name) && pod.Annotations[asdbv1.QuiesceAnnotation] ==
			asdbv1.QuiesceAnnotationValue {
			annotatedNonTargets = append(annotatedNonTargets, *pod)
		}
	}

	if len(annotatedNonTargets) == 0 {
		// Nothing for AKO to undo — skip all Aerospike info calls.
		return common.ReconcileSuccess()
	}

	r.Log.Info("Sending quiesce-undo to pods that were quiesced but are no longer scale-down candidates",
		"quiesceUndoPods", len(annotatedNonTargets))

	// ALL pods needed by InfoRecluster inside the management lib to find the principal.
	allHostConns, err := r.newPodsHostConnWithOption(podList.Items, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("build all-host connections for quiesce-undo: %w", err))
	}

	if len(allHostConns) == 0 {
		return common.ReconcileSuccess()
	}

	nonTargetHostConns, err := r.newPodsHostConnWithOption(
		annotatedNonTargets, ignorablePodNames,
	)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("build host connections for quiesce-undo pods: %w", err))
	}

	policy := r.getClientPolicy(ctx)

	if err := deployment.InfoQuiesceUndoSubset(r.Log, policy, allHostConns, nonTargetHostConns); err != nil {
		return common.ReconcileError(fmt.Errorf("send quiesce-undo to scale-down-reverted pods: %w", err))
	}

	// Clear annotations from unquiesced non-target pods only; targets keep
	// theirs for the fast-exit check in reconcileParallelScaleDownQuiesce.
	for i := range annotatedNonTargets {
		if annErr := r.setPodQuiesceAnnotation(ctx, &annotatedNonTargets[i], false); annErr != nil {
			r.Log.Error(annErr, "Failed to remove quiesce annotation from pod; will retry next reconcile",
				"pod", utils.GetNamespacedName(&annotatedNonTargets[i]))
		}
	}

	return common.ReconcileSuccess()
}

// reconcileParallelScaleDownQuiesce quiesces ALL scale-down candidates across every rack in a
// single pre-pass, triggering one concurrent migration round instead of N
// sequential rounds. Runs before
// reconcileRack for scaled-down racks.
//
//   - Annotation fast-exit: if all targets carry QuiesceAnnotation, all
//     Aerospike info calls are skipped (steady-state path is free of network I/O).
//   - Delegates to waitForMultipleNodesSafeStopReady (MFD=0, drain path) which
//     handles: server readiness, degraded-cluster guard, MFD zeroing, stability
//     wait, SC roster management, and quiesce.
func (r *SingleClusterReconciler) reconcileParallelScaleDownQuiesce(
	ctx context.Context,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(allTargets) == 0 {
		return common.ReconcileSuccess()
	}

	// Filter ignorable targets; check annotation fast-exit.
	effectiveTargets := make([]*corev1.Pod, 0, len(allTargets))
	allAnnotated := true

	for _, pod := range allTargets {
		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		effectiveTargets = append(effectiveTargets, pod)

		if pod.Annotations[asdbv1.QuiesceAnnotation] != asdbv1.QuiesceAnnotationValue {
			allAnnotated = false
		}
	}

	if len(effectiveTargets) == 0 {
		r.Log.V(1).Info("All scale-down candidates are ignorable; skipping parallel quiesce pre-pass")
		return common.ReconcileSuccess()
	}

	allTargets = effectiveTargets

	if allAnnotated {
		r.Log.V(1).Info("All scale-down candidates already quiesced by AKO, skipping parallel quiesce pre-pass",
			"scaleDownCandidates", len(allTargets))

		return common.ReconcileSuccess()
	}

	r.Log.Info("Running cross-rack parallel quiesce pre-pass", "scaleDownCandidates", len(allTargets))

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "ParallelQuiesceStarted",
		"Quiescing %d pod(s) across racks before scale-down", len(allTargets),
	)

	if err := r.setConditions(ctx, metav1.Condition{
		Type:    string(asdbv1.AerospikeClusterConditionScalingDown),
		Status:  metav1.ConditionTrue,
		Reason:  asdbv1.AerospikeClusterReasonScalingDown,
		Message: fmt.Sprintf("Quiescing %d pod(s) across racks before scale-down", len(allTargets)),
	}); err != nil {
		return common.ReconcileError(err)
	}

	if res := r.waitForMultipleNodesSafeStopReady(ctx, allTargets, ignorablePodNames, 0, true); !res.IsSuccess {
		return res
	}

	// Stamp annotation for fast-exit; failure is non-fatal (next reconcile retries).
	for _, pod := range allTargets {
		if pod.Annotations[asdbv1.QuiesceAnnotation] != asdbv1.QuiesceAnnotationValue {
			if annErr := r.setPodQuiesceAnnotation(ctx, pod, true); annErr != nil {
				r.Log.Error(annErr, "Failed to set quiesce annotation on pod; will try quiesce in next reconcile",
					"pod", utils.GetNamespacedName(pod))
			}
		}
	}

	r.Log.Info("Cross-rack parallel quiesce pre-pass completed", "podsQuiesced", len(allTargets))

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "ParallelQuiesceCompleted",
		"Quiesced %d pod(s) across racks, ready for scale-down", len(allTargets),
	)

	return common.ReconcileSuccess()
}

// clearStaleQuiesceAnnotations removes QuiesceAnnotation from every pod
// that still carries it. Single list call; only patches annotated pods, so
// the common case (no stale annotations) costs exactly one API round-trip.
func (r *SingleClusterReconciler) clearStaleQuiesceAnnotations(ctx context.Context) error {
	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return fmt.Errorf("list pods to clear stale quiesce annotations: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Annotations[asdbv1.QuiesceAnnotation] != asdbv1.QuiesceAnnotationValue {
			continue
		}

		if err := r.setPodQuiesceAnnotation(ctx, pod, false); err != nil {
			return err
		}
	}

	return nil
}

// setPodQuiesceAnnotation adds (add=true) or removes (add=false) the
// QuiesceAnnotation on the pod via a merge-patch.
func (r *SingleClusterReconciler) setPodQuiesceAnnotation(
	ctx context.Context, pod *corev1.Pod, add bool,
) error {
	if add && pod.Annotations[asdbv1.QuiesceAnnotation] == asdbv1.QuiesceAnnotationValue {
		return nil // already set
	}

	if !add && pod.Annotations[asdbv1.QuiesceAnnotation] != asdbv1.QuiesceAnnotationValue {
		return nil // already absent
	}

	base := pod.DeepCopy()
	patch := client.MergeFrom(base)

	if add {
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}

		pod.Annotations[asdbv1.QuiesceAnnotation] = asdbv1.QuiesceAnnotationValue
	} else {
		delete(pod.Annotations, asdbv1.QuiesceAnnotation)
	}

	if err := r.Patch(ctx, pod, patch); err != nil {
		return fmt.Errorf("patch pod %s quiesce annotation (add=%v): %w", utils.GetNamespacedNameString(pod), add, err)
	}

	return nil
}

// getAllScaleDownPods returns all pods to be removed for the given rack.
// When rackSTS is nil (rack in racksToDelete) all existing pods are returned.
func (r *SingleClusterReconciler) getAllScaleDownPods(
	ctx context.Context, rack rackWithSTS,
) ([]*corev1.Pod, error) {
	rackState := rack.rackState

	orderedPods, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return nil, err
	}

	if rack.rackSTS == nil {
		return orderedPods, nil
	}

	diffPods := *rack.rackSTS.Spec.Replicas - rackState.Size
	if diffPods <= 0 {
		return nil, nil
	}

	return orderedPods[:diffPods], nil
}
