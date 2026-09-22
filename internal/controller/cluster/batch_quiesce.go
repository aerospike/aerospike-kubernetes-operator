package cluster

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

// buildScaleDownTargets collects all scale-down candidate pods from
// scaledDownRacks (STS.Replicas > spec.Size) and racksToDelete (target size=0).
// Pure collection only — readiness checks run later in checkReadyForBatchQuiesce.
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

	// racksToDelete: nil rackSTS signals target size=0, so all pods are returned.
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

// checkReadyForBatchQuiesce gates entry into reconcileBatchQuiesce by checking
// pod readiness in scaledDownRacks (racksToDelete are skipped — all their
// non-running pods are already in ignorablePodNames via getIgnorablePods).
//
// For each non-ready pod in a scaled-down rack:
//   - Target + no CR status → never joined; added to ignorablePodNames.
//   - Target + has CR status → re-initialising; ReconcileError (wait for recovery).
//   - Remaining (non-target) pod → ReconcileError (must be up for connections).
func (r *SingleClusterReconciler) checkReadyForBatchQuiesce(
	ctx context.Context,
	scaledDownRacks []rackWithSTS,
	targetNames sets.Set[string],
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	for idx := range scaledDownRacks {
		rack := scaledDownRacks[idx]

		// Fetch all pods for this rack (not just the diff — remaining pods are
		// also checked for readiness).
		orderedPods, err := r.getOrderedRackPodList(
			ctx, rack.rackState.Rack.ID, rack.rackState.Rack.Revision,
		)
		if err != nil {
			return common.ReconcileError(fmt.Errorf(
				"list pods for scaled-down rack %d readiness check: %w",
				rack.rackState.Rack.ID, err,
			))
		}

		for _, pod := range orderedPods {
			if utils.IsAerospikeServerReady(pod) || ignorablePodNames.Has(pod.Name) {
				continue
			}

			if targetNames.Has(pod.Name) {
				if _, hasStatus := r.aeroCluster.Status.Pods[pod.Name]; !hasStatus {
					// Never joined — safe to skip quiesce.
					r.Log.Info("Scale-down target has no CR status entry; never joined cluster, skipping quiesce",
						"pod", pod.Name)
					ignorablePodNames.Insert(pod.Name)
				} else {
					return common.ReconcileError(fmt.Errorf(
						"pod %s is not ready; waiting for recovery before scale-down to prevent data loss",
						pod.Name,
					))
				}
			} else {
				// Remaining pod must be up for quiesce connections.
				return common.ReconcileError(fmt.Errorf(
					"pod %s in scaled-down rack is not ready; waiting for recovery before quiesce",
					pod.Name,
				))
			}
		}
	}

	return common.ReconcileSuccess()
}

// reconcileQuiesceUndo restores quiesced non-target pods to full membership.
// Runs before reconcileRack for non-scaled-down racks so stale quiesces from a
// prior (possibly reverted) scale-down are undone before rolling restarts run.
//
// Annotation fast-exit: if no non-target pod carries BatchQuiesceAnnotation all
// Aerospike calls are skipped. When undo is needed, InfoQuiesceUndoSubset is
// called with annotated non-target pods only (undoHosts) plus all pods
// (allHosts, so InfoRecluster can always reach the principal).
func (r *SingleClusterReconciler) reconcileQuiesceUndo(
	ctx context.Context,
	targetNames sets.Set[string],
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("list cluster pods for quiesce-undo: %w", err))
	}

	var annotatedNonTargets []corev1.Pod

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if !targetNames.Has(pod.Name) && pod.Annotations[asdbv1.BatchQuiesceAnnotation] ==
			asdbv1.BatchQuiesceAnnotationValue {
			annotatedNonTargets = append(annotatedNonTargets, *pod)
		}
	}

	if len(annotatedNonTargets) == 0 {
		// Nothing for AKO to undo — skip all Aerospike info calls.
		return common.ReconcileSuccess()
	}

	r.Log.Info("Sending quiesce-undo to annotated non-target pods",
		"annotatedNonTargetCount", len(annotatedNonTargets))

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
		return common.ReconcileError(fmt.Errorf("build non-target host connections for quiesce-undo: %w", err))
	}

	policy := r.getClientPolicy(ctx)

	if err := deployment.InfoQuiesceUndoSubset(r.Log, policy, nonTargetHostConns, allHostConns); err != nil {
		return common.ReconcileError(fmt.Errorf("send quiesce-undo to non-target pods: %w", err))
	}

	// Clear annotations from unquiesced non-target pods only; targets keep
	// theirs for the fast-exit check in reconcileBatchQuiesce.
	for i := range annotatedNonTargets {
		if annErr := r.setPodQuiesceAnnotation(ctx, &annotatedNonTargets[i], false); annErr != nil {
			r.Log.Error(annErr, "Failed to remove quiesce annotation from pod; will retry next reconcile",
				"pod", annotatedNonTargets[i].Name)
		}
	}

	return common.ReconcileSuccess()
}

// reconcileBatchQuiesce quiesces ALL scale-down targets across every rack in a
// single pre-pass, triggering one concurrent migration round instead of N
// sequential rounds. Runs after checkReadyForBatchQuiesce and before
// reconcileRack for scaled-down racks.
//
//   - Annotation fast-exit: if all targets carry BatchQuiesceAnnotation, all
//     Aerospike info calls are skipped (steady-state path is free of network I/O).
//   - Connections built fresh — prior reconcileRack pass may have changed IPs.
//   - validateSCClusterState + waitForClusterStability gate before any quiesce.
func (r *SingleClusterReconciler) reconcileBatchQuiesce(
	ctx context.Context,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(allTargets) == 0 {
		return common.ReconcileSuccess()
	}

	// Single pass: filter ignorable pods and check annotation fast-exit.
	// Targets may be ignorable from getIgnorablePods (server-failed within budget)
	// or checkReadyForBatchQuiesce (never-joined pods). allTargets[:0] reuses
	// the backing array safely (effectiveTargets is a left-to-right subset).
	effectiveTargets := allTargets[:0]
	allAnnotated := true

	for _, pod := range allTargets {
		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		effectiveTargets = append(effectiveTargets, pod)

		if pod.Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
			allAnnotated = false
		}
	}

	if len(effectiveTargets) == 0 {
		r.Log.V(1).Info("All scale-down targets are ignorable; skipping batch quiesce pre-pass")
		return common.ReconcileSuccess()
	}

	allTargets = effectiveTargets

	if allAnnotated {
		r.Log.V(1).Info("All scale-down targets already quiesced by AKO, skipping batch quiesce pre-pass",
			"targetCount", len(allTargets))

		return common.ReconcileSuccess()
	}

	r.Log.Info("Running cross-rack batch quiesce pre-pass", "targetCount", len(allTargets))

	policy := r.getClientPolicy(ctx)

	// Abort if SC partitions are already unavailable — quiescing would deepen it.
	if err := r.validateSCClusterState(ctx, policy, ignorablePodNames); err != nil {
		r.Log.Error(err, "SC cluster state not healthy, deferring batch quiesce pre-pass")
		return common.ReconcileRequeueAfter(10)
	}

	// Build connections fresh; prior reconcileRack pass may have restarted pods.
	// errEmptyPodList (new cluster) is treated as a no-op.
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		if errors.Is(err, errEmptyPodList) {
			return common.ReconcileSuccess()
		}

		return common.ReconcileError(fmt.Errorf("build host connections for batch quiesce: %w", err))
	}

	// Zero MFD before the stability check so fills from any prior elevated MFD
	// (e.g. a rolling-restart override) drain freely. Scale-down never raises MFD
	// before quiesce — once nodes are permanently removed, fills must proceed at
	// full speed. The DynamicMigrateFillDelay guard skips the call when already 0.
	if res := r.setMigrateFillDelay(ctx, policy, 0, ignorablePodNames, allHostConns, false); !res.IsSuccess {
		return res
	}

	// Quiescing during active migrations stalls them; wait for stability first.
	if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
		return res
	}

	// Quiesce all targets — idempotent.
	if err := r.quiescePods(ctx, policy, allHostConns, allTargets, ignorablePodNames); err != nil {
		return common.ReconcileError(fmt.Errorf("batch quiesce scale-down target pods: %w", err))
	}

	// Stamp annotation for fast-exit on subsequent reconciles. Non-fatal if it
	// fails — next reconcile will re-quiesce idempotently and retry the stamp.
	for _, pod := range allTargets {
		if pod.Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
			if annErr := r.setPodQuiesceAnnotation(ctx, pod, true); annErr != nil {
				r.Log.Error(annErr, "Failed to set quiesce annotation on pod; will re-quiesce next reconcile",
					"pod", pod.Name)
			}
		}
	}

	r.Log.Info("Cross-rack batch quiesce pre-pass completed", "quiesceTargets", len(allTargets))

	return common.ReconcileSuccess()
}

// setPodQuiesceAnnotation adds (add=true) or removes (add=false) the
// BatchQuiesceAnnotation on the pod via a merge-patch.
func (r *SingleClusterReconciler) setPodQuiesceAnnotation(
	ctx context.Context, pod *corev1.Pod, add bool,
) error {
	if add && pod.Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
		return nil // already set
	}

	if !add && pod.Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
		return nil // already absent
	}

	base := pod.DeepCopy()
	patch := client.MergeFrom(base)

	if add {
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}

		pod.Annotations[asdbv1.BatchQuiesceAnnotation] = asdbv1.BatchQuiesceAnnotationValue
	} else {
		delete(pod.Annotations, asdbv1.BatchQuiesceAnnotation)
	}

	if err := r.Patch(ctx, pod, patch); err != nil {
		return fmt.Errorf("patch pod %s quiesce annotation (add=%v): %w", pod.Name, add, err)
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
