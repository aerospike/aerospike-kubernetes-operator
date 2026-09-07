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

// buildScaleDownTargets collects all scale-down candidate pods from both
// explicitly scaled-down racks and racks being deleted entirely.
//
// Sources merged:
//  1. scaledDownRacks — configured racks where STS.Replicas > spec.Size.
//  2. racksToDelete — racks removed entirely (rack replacement / deletion).
//     These carry a nil rackSTS; getAllScaleDownPods treats nil as Size=0 and
//     returns all existing pods for the rack.
//
// This function is intentionally a pure collection step — it does not perform
// any pod readiness checks. The readiness gate is applied later by
// checkReadyForBatchQuiesce (Step 2.75 in reconcileRacks), which runs after
// waitForAllRacksReady (Step 2.5) has ensured non-scaled-down racks are fully
// up. This ordering guarantees the check sees the cluster state that will be
// active when quiesce actually runs.
func (r *SingleClusterReconciler) buildScaleDownTargets(
	ctx context.Context,
	scaledDownRacks []scaledDownRack,
	racksToDelete []asdbv1.Rack,
) ([]*corev1.Pod, common.ReconcileResult) {
	// Combine configured scale-down racks and fully-deleted racks in one pass.
	allRacks := make([]scaledDownRack, len(scaledDownRacks), len(scaledDownRacks)+len(racksToDelete))
	copy(allRacks, scaledDownRacks)

	for idx := range racksToDelete {
		rack := &racksToDelete[idx]
		allRacks = append(allRacks, scaledDownRack{
			rackSTS:   nil,
			rackState: &RackState{Size: 0, Rack: rack},
		})
	}

	var allTargets []*corev1.Pod

	for idx := range allRacks {
		removedPods, err := r.getAllScaleDownPods(ctx, allRacks[idx])
		if err != nil {
			return nil, common.ReconcileError(fmt.Errorf(
				"get scale-down pods for rack %d: %w",
				allRacks[idx].rackState.Rack.ID, err,
			))
		}

		allTargets = append(allTargets, removedPods...)
	}

	return allTargets, common.ReconcileSuccess()
}

// checkReadyForBatchQuiesce scans the pods belonging to partially-scaled-down
// racks immediately before reconcileBatchQuiesce.
//
// Why only scaledDownRacks (not racksToDelete):
//
//	getIgnorablePods (very beginning of reconcileRacks) adds ALL non-running
//	pods from racksToDelete to ignorablePodNames unconditionally. Every pod in
//	a deleted rack is also a target (no "remaining" pods). So for racksToDelete:
//	running pods pass the IsAerospikeServerReady check; non-running pods are
//	already in ignorablePodNames. This function is a no-op for them.
//
// Why only pending/initialising pods reach this function:
//
//	handleFailedPodsInRack (lines ~64–90 in reconcileRacks) processes every
//	rack with a *terminal* server failure (CrashLoopBackOff, ErrImagePull,
//	OOMKilled, Failed phase, …). For non-ignorable failures it always returns
//	RequeueAfter — execution never reaches this function until the pod either
//	recovers or enters ignorablePodNames. Pods that are merely pending or still
//	initialising are classified PodHealthy by getServerFailedAndActivePods and
//	pass through handleFailedPodsInRack unchecked.
//
// Within scaledDownRacks two cases are handled:
//
//   - Target (will be removed) + NOT running + no CR status →
//     pod is pending/initialising and never joined the cluster; added to
//     ignorablePodNames so quiescePods skips it safely.
//   - Target (will be removed) + NOT running + has CR status →
//     pod previously joined, currently re-initialising; ReconcileError to
//     wait for recovery before quiesce.
//   - Remaining pod (stays after scale-down) + NOT running →
//     ReconcileError regardless of status — remaining pods must be up for
//     quiesce connections to succeed.
func (r *SingleClusterReconciler) checkReadyForBatchQuiesce(
	ctx context.Context,
	scaledDownRacks []scaledDownRack,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	// Build a fast-lookup set of target pod names (pods being removed).
	targetNames := sets.New[string]()
	for _, pod := range allTargets {
		targetNames.Insert(pod.Name)
	}

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
				// Scale-down target is pending/initialising and not ready.
				if _, hasStatus := r.aeroCluster.Status.Pods[pod.Name]; !hasStatus {
					// Pod never joined the cluster — safe to skip quiesce.
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
				// Remaining pod in a scaled-down rack is not running. These
				// pods must be up for quiesce connections to succeed. Block
				// and wait for recovery — never silently skip them.
				return common.ReconcileError(fmt.Errorf(
					"pod %s in scaled-down rack is not ready; waiting for recovery before quiesce",
					pod.Name,
				))
			}
		}
	}

	return common.ReconcileSuccess()
}

// reconcileQuiesceUndo restores quiesced non-target pods to full cluster
// membership. It runs BEFORE reconcileRack for non-scaled-down racks so that
// any pods left quiesced by a prior batch-quiesce pre-pass (e.g. from a
// since-reverted scale-down) are unquiesced before rolling restarts, config
// updates, or scale-ups execute.
//
// Annotation-based fast-exit:
// AKO marks every pod it quiesces with the BatchQuiesceAnnotation. If no
// non-target pod carries the annotation, nothing needs undoing — we skip all
// Aerospike info calls.
//
// When undo IS needed, InfoQuiesceUndoSubset is called with:
//   - undoHosts: connections to the annotated non-target pods only (the nodes
//     AKO wants to unquiesce).
//   - allHosts: connections to ALL cluster pods (passed through to the internal
//     InfoRecluster call so the principal is always reachable).
//
// Because only non-target pods are passed as undoHosts, target pods that are
// intentionally quiesced are NOT disturbed. Only the annotated non-target pods
// have their BatchQuiesceAnnotation cleared after a successful undo.
func (r *SingleClusterReconciler) reconcileQuiesceUndo(
	ctx context.Context,
	allTargets []*corev1.Pod,
) common.ReconcileResult {
	targetNames := sets.New[string]()
	for _, pod := range allTargets {
		targetNames.Insert(pod.Name)
	}

	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("list cluster pods for quiesce-undo: %w", err))
	}

	// Single pass over the pod list:
	//  - collect annotated non-target pods (annotation fast-exit check)
	//  - build tolerantIgnorable (any currently non-running pod is skipped
	//    rather than causing a hard error; the annotation stays in place and
	//    the next reconcile retries it)
	//
	// tolerantIgnorable uses the live !IsAerospikeServerReady check rather
	// than the upstream ignorablePodNames snapshot: the snapshot was taken at
	// the very start of reconcileRacks; a pod that was not-ready then may have
	// recovered by now. Using a live check ensures recovered pods are included
	// in allHostConns so InfoRecluster can always reach the principal.
	var annotatedNonTargets []*corev1.Pod

	tolerantIgnorable := sets.New[string]()

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if !utils.IsAerospikeServerReady(pod) {
			tolerantIgnorable.Insert(pod.Name)
		}

		if !targetNames.Has(pod.Name) && pod.Annotations[asdbv1.BatchQuiesceAnnotation] ==
			asdbv1.BatchQuiesceAnnotationValue {
			annotatedNonTargets = append(annotatedNonTargets, pod)
		}
	}

	if len(annotatedNonTargets) == 0 {
		// Nothing for AKO to undo — skip all Aerospike info calls.
		return common.ReconcileSuccess()
	}

	r.Log.Info("Sending quiesce-undo to annotated non-target pods",
		"annotatedNonTargetCount", len(annotatedNonTargets))

	// Build connections for ALL pods — required by InfoRecluster inside the
	// management library so the principal is always reachable.
	allHostConns, err := r.newPodsHostConnWithOption(podList.Items, tolerantIgnorable)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("build all-host connections for quiesce-undo: %w", err))
	}

	if len(allHostConns) == 0 {
		return common.ReconcileSuccess()
	}

	// Build connections only for the annotated non-target pods that need
	// unquiescing, using the same tolerant ignorable set so non-running pods
	// in annotatedNonTargets are silently skipped.
	nonTargetHostConns, err := r.newPodsHostConnWithOption(
		podsFromPtrs(annotatedNonTargets), tolerantIgnorable,
	)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("build non-target host connections for quiesce-undo: %w", err))
	}

	policy := r.getClientPolicy(ctx)

	// InfoQuiesceUndoSubset:
	//   - scans and undoes quiesce on nonTargetHostConns only
	//   - calls InfoRecluster with allHostConns so the principal is always found
	if err := deployment.InfoQuiesceUndoSubset(r.Log, policy, nonTargetHostConns, allHostConns); err != nil {
		return common.ReconcileError(fmt.Errorf("send quiesce-undo to non-target pods: %w", err))
	}

	// Clear annotations only from the non-target pods we just unquiesced.
	// Target pods keep their annotations; reconcileBatchQuiesce will re-use
	// them for the fast-exit check.
	for _, pod := range annotatedNonTargets {
		if annErr := r.setPodQuiesceAnnotation(ctx, pod, false); annErr != nil {
			r.Log.Error(annErr, "Failed to remove quiesce annotation from pod; will retry next reconcile",
				"pod", pod.Name)
		}
	}

	return common.ReconcileSuccess()
}

// podsFromPtrs converts a slice of *corev1.Pod to []corev1.Pod so it can be
// passed to helpers that operate on value slices.
func podsFromPtrs(ptrs []*corev1.Pod) []corev1.Pod {
	out := make([]corev1.Pod, len(ptrs))
	for i, p := range ptrs {
		out[i] = *p
	}

	return out
}

// reconcileBatchQuiesce is the cross-rack batch quiesce pre-pass. It runs
// AFTER checkReadyForBatchQuiesce (Step 2.75) and BEFORE reconcileRack for
// scaled-down racks, quiescing ALL scale-down target pods across every rack at
// once — triggering a single concurrent migration round instead of N sequential
// rounds (one per rack / batch in the legacy flow).
//
// Design notes:
//   - ALL diff pods are quiesced upfront, not just the first ScaleDownBatchSize
//     batch. The per-rack scaleDownRack loop still reduces the STS in batches;
//     this pre-pass only triggers the single migration round early.
//   - Annotation-based fast-exit: if every target pod already carries the
//     BatchQuiesceAnnotation (set by AKO on the previous pass), all Aerospike
//     info calls are skipped. This makes the steady-state path free of network
//     round-trips.
//   - Connections are built fresh here because reconcileRack for non-scaled-down
//     racks (which runs before this function) may restart pods and change IPs.
//   - Non-running targets that never joined the cluster are already in
//     ignorablePodNames (added by checkReadyForBatchQuiesce) and are skipped
//     by quiescePods.
//   - validateSCClusterState gates on SC partition health before any quiesce.
//   - waitForClusterStability ensures no migrations are in flight before
//     quiescing, preventing stalled migrations on quiesced nodes.
func (r *SingleClusterReconciler) reconcileBatchQuiesce(
	ctx context.Context,
	allTargets []*corev1.Pod,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(allTargets) == 0 {
		return common.ReconcileSuccess()
	}

	// Filter ignorable pods out of the target list.
	//
	// A target pod can end up in ignorablePodNames from two independent sources:
	//
	//  1. getIgnorablePods (beginning of reconcileRacks) — server-failed pods
	//     within the maxIgnorablePods budget are added BEFORE buildScaleDownTargets
	//     runs, so they can already be ignorable when we arrive here.
	//
	//  2. checkReadyForBatchQuiesce (Step 2.75) — never-joined target pods
	//     (not running, no CR status) are added AFTER buildScaleDownTargets
	//     collected them.
	//
	// Both cases must be filtered out so that:
	//   a. The annotation fast-exit below correctly detects that all remaining
	//      (actually-quiesceable) targets are already annotated.
	//   b. quiescePods is not asked to build a connection to an unreachable pod.
	//   c. The annotation loop at the end does not stamp ignorable pods.
	effectiveTargets := allTargets[:0:0] // reuse backing-array hint but start empty

	for _, pod := range allTargets {
		if !ignorablePodNames.Has(pod.Name) {
			effectiveTargets = append(effectiveTargets, pod)
		}
	}

	if len(effectiveTargets) == 0 {
		r.Log.V(1).Info("All scale-down targets are ignorable; skipping batch quiesce pre-pass")
		return common.ReconcileSuccess()
	}

	allTargets = effectiveTargets

	// Annotation-based fast-exit: if every target is already marked as
	// quiesced by AKO, skip all Aerospike info calls.
	allAnnotated := true

	for _, pod := range allTargets {
		if pod.Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
			allAnnotated = false
			break
		}
	}

	if allAnnotated {
		r.Log.V(1).Info("All scale-down targets already quiesced by AKO, skipping batch quiesce pre-pass",
			"targetCount", len(allTargets))

		return common.ReconcileSuccess()
	}

	r.Log.Info("Running cross-rack batch quiesce pre-pass", "targetCount", len(allTargets))

	policy := r.getClientPolicy(ctx)

	// SC cluster state pre-check.
	// If the cluster already has unavailable or dead partitions, quiescing
	// additional nodes would deepen the degradation. Requeue and wait for
	// the SC state to be healthy before proceeding.
	if err := r.validateSCClusterState(ctx, policy, ignorablePodNames); err != nil {
		r.Log.Error(err, "SC cluster state not healthy, deferring batch quiesce pre-pass")
		return common.ReconcileRequeueAfter(10)
	}

	// Build connections fresh — pods may have restarted during reconcileRack
	// for non-scaled-down racks (which runs before this function), so any
	// connections built earlier would carry stale IPs.
	// errEmptyPodList means no pods exist yet (new cluster); treat as a no-op.
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		if errors.Is(err, errEmptyPodList) {
			return common.ReconcileSuccess()
		}

		return common.ReconcileError(fmt.Errorf("build host connections for batch quiesce: %w", err))
	}

	// Wait for any in-flight migrations to complete before quiescing.
	// Quiescing while migrations are active could stall them indefinitely,
	// because quiesced nodes stop accepting new partition assignments.
	if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
		return res
	}

	// Quiesce all running target pods — idempotent (re-quiescing an already-
	// quiesced node is safe; the management lib verifies pending_quiesce after
	// sending the quiesce: command).
	if err := r.quiescePods(ctx, policy, allHostConns, allTargets, ignorablePodNames); err != nil {
		return common.ReconcileError(fmt.Errorf("batch quiesce scale-down target pods: %w", err))
	}

	// Mark all targets with the BatchQuiesceAnnotation so subsequent reconcile
	// cycles can skip re-quiescing them (fast-exit above).
	for _, pod := range allTargets {
		if pod.Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
			if annErr := r.setPodQuiesceAnnotation(ctx, pod, true); annErr != nil {
				r.Log.Error(annErr, "Failed to set quiesce annotation on pod; will re-quiesce next reconcile",
					"pod", pod.Name)
				// Non-fatal: the annotation is an optimisation. Next reconcile
				// will re-quiesce this pod (idempotent) and retry the annotation.
			}
		}
	}

	r.Log.Info("Cross-rack batch quiesce pre-pass completed", "quiesceTargets", len(allTargets))

	return common.ReconcileSuccess()
}

// setPodQuiesceAnnotation adds (add=true) or removes (add=false) the
// BatchQuiesceAnnotation on the given pod using a merge-patch so only the
// annotation map is touched.
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

// getAllScaleDownPods returns ALL pods that will be removed for the given rack.
// Unlike the per-rack scaleDownRack loop which processes one ScaleDownBatchSize
// chunk at a time, the pre-pass quiesces the entire removal set upfront so all
// migrations happen concurrently.
//
// When rackSTS is nil (racks sourced from racksToDelete whose STS was not
// fetched) all existing pods are returned — the target size is 0 so every pod
// is a removal candidate.
func (r *SingleClusterReconciler) getAllScaleDownPods(
	ctx context.Context, rack scaledDownRack,
) ([]*corev1.Pod, error) {
	rackState := rack.rackState

	orderedPods, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return nil, err
	}

	// No STS object: rack is being deleted entirely (Size=0), return all pods.
	if rack.rackSTS == nil {
		return orderedPods, nil
	}

	diffPods := *rack.rackSTS.Spec.Replicas - rackState.Size
	if diffPods <= 0 {
		return nil, nil
	}

	return orderedPods[:diffPods], nil
}
