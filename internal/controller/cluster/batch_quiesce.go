package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

// classifyTargetPods adds never-joined targets to ignorablePodNames and returns
// ReconcileError for previously-joined targets that are currently not running.
func (r *SingleClusterReconciler) classifyTargetPods(
	ctx context.Context,
	targetNames sets.Set[string],
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("list cluster pods for batch quiesce target classification: %w", err))
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if utils.IsAerospikeServerReady(pod) || ignorablePodNames.Has(pod.Name) || !targetNames.Has(pod.Name) {
			continue
		}

		// Target pod not running.
		if _, hasStatus := r.aeroCluster.Status.Pods[pod.Name]; !hasStatus {
			// Never joined the cluster — safe to skip quiesce entirely.
			r.Log.Info("Scale-down target has no CR status entry; never joined cluster, skipping quiesce",
				"pod", pod.Name)
			ignorablePodNames.Insert(pod.Name)
		} else {
			return common.ReconcileError(fmt.Errorf(
				"pod %s is not ready; waiting for recovery before scale-down to prevent data loss",
				pod.Name,
			))
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
// sequential rounds. Runs after classifyTargetPods and before
// reconcileRack for scaled-down racks.
//
//   - Annotation fast-exit: if all targets carry BatchQuiesceAnnotation, all
//     Aerospike info calls are skipped (steady-state path is free of network I/O).
//   - Delegates to waitForMultipleNodesSafeStopReady (MFD=0, drain path) which
//     handles: server readiness, degraded-cluster guard, MFD zeroing, stability
//     wait, SC roster management, and quiesce.
func (r *SingleClusterReconciler) reconcileBatchQuiesce(
	ctx context.Context,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(allTargets) == 0 {
		return common.ReconcileSuccess()
	}

	// Filter ignorable targets; check annotation fast-exit.
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

	// Local copy: mutations by classifyTargetPods must not bleed into Step 4's reconcileRack.
	localIgnorable := ignorablePodNames.Union(sets.New[string]())

	// Never-joined targets → localIgnorable; previously-joined non-running → ReconcileError.
	targetNames := sets.New(getPodNames(allTargets)...)
	if res := r.classifyTargetPods(ctx, targetNames, localIgnorable); !res.IsSuccess {
		return res
	}

	if res := r.waitForMultipleNodesSafeStopReady(ctx, allTargets, localIgnorable, 0, true); !res.IsSuccess {
		return res
	}

	// Stamp annotation for fast-exit; failure is non-fatal (next reconcile retries).
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
