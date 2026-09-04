/*
Copyright 2024 The aerospike-operator Authors.
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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/deployment"
	"github.com/aerospike/aerospike-management-lib/info"
)

// ------------------------------------------------------------------------------------
// Aerospike helper
// ------------------------------------------------------------------------------------

// waitForMultipleNodesSafeStopReady waits until the input pods are safe to stop.
// ignorablePodNames are pods whose Aerospike server is unreachable and are
// skipped from cluster-operation queries (host connections, roster, quiesce).
// Pods with a running server but a failing sidecar are not in this set; they are
// included in all cluster-operation calls since their servers are still reachable.
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	ctx context.Context, pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(pods) == 0 {
		return common.ReconcileSuccess()
	}

	// Wait for all non-ignorable pods to have their Aerospike server containers
	// ready before making any cluster-level info calls. This replaces the old
	// waitForAllSTSToBeReady pre-check (which required full pod readiness
	// including sidecars). Server-only readiness is sufficient here — sidecar
	// failures do not prevent the server from accepting info calls. The wait
	// uses the same blocking-retry semantics (up to 18×10s) so that a pod which
	// was just restarted in a previous batch has time to bring its server up
	// before we attempt the migration/quiesce checks.
	if err := r.waitForAllAerospikeServersReady(ctx, ignorablePodNames); err != nil {
		return common.ReconcileError(
			fmt.Errorf("wait for Aerospike server containers across all StatefulSets to be ready: %w", err),
		)
	}

	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(fmt.Errorf(
			"get host connections for cluster nodes: %w", err))
	}

	// Safety guard: if the cluster is degraded (some pods are failed/ignorable) and
	// fewer than 2 reachable nodes remain, every downstream check produces a false
	// positive on a degraded view:
	//   - IsClusterAndStable → true  (Aerospike reforms as a 1-node cluster)
	//   - waitForMigrationToComplete → true  (0 pending migrations on 1 node)
	//   - InfoQuiesce → silent skip  (len(hostIDs) < 2 in management lib)
	// None of those signals is safe to act on when the cluster is degraded.
	// Genuine size-1 clusters are not affected: ignorablePodNames is empty there.
	if len(allHostConns) < 2 && ignorablePodNames.Len() > 0 {
		return common.ReconcileError(fmt.Errorf(
			"cluster is degraded: %d failed/ignorable pod(s) excluded, only %d reachable node(s) remain; "+
				"refusing to proceed to prevent data loss — recover the failed Pods first",
			ignorablePodNames.Len(), len(allHostConns),
		))
	}

	policy := r.getClientPolicy(ctx)

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete", pods[0].Labels[asdbv1.AerospikeRackIDLabel],
	)

	// TODO: can we skip stability checks if all pods are in quiesced state
	// this means that previous reconcile quiesced the pod batch but failed later point in time.
	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
		return res
	}

	// Setup roster after migration.
	if err = r.getAndSetRoster(ctx, policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
		r.Log.Error(err, "Failed to set roster for cluster, will requeue")
		return common.ReconcileRequeueAfter(1)
	}

	if err := r.quiescePods(ctx, policy, allHostConns, pods, ignorablePodNames); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

// waitForMigrationToComplete waits for the migration to complete on all the nodes in the cluster.
func (r *SingleClusterReconciler) waitForMigrationToComplete(ctx context.Context, policy *as.ClientPolicy,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(fmt.Errorf(
			"get host connections for cluster nodes: %w", err))
	}

	r.Log.Info("Waiting for migration to complete")

	return r.waitForClusterStability(policy, allHostConns)
}

func (r *SingleClusterReconciler) quiescePods(
	ctx context.Context,
	policy *as.ClientPolicy, allHostConns []*deployment.HostConn, pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) error {
	podList := make([]corev1.Pod, 0, len(pods))

	for idx := range pods {
		podList = append(podList, *pods[idx])
	}

	selectedHostConns, err := r.newPodsHostConnWithOption(podList, ignorablePodNames)
	if err != nil {
		return err
	}

	nodesNamespaces, err := deployment.GetClusterNamespaces(r.Log, r.getClientPolicy(ctx), allHostConns)
	if err != nil {
		return err
	}

	return deployment.InfoQuiesce(r.Log, policy, allHostConns, selectedHostConns, r.removedNamespaces(nodesNamespaces))
}

// TODO: Check only for migration
func (r *SingleClusterReconciler) waitForClusterStability(
	policy *as.ClientPolicy, allHostConns []*deployment.HostConn,
) common.ReconcileResult {
	const (
		maxRetry      = 6
		retryInterval = time.Second * 10
	)

	var (
		isStable bool
		err      error
	)

	// Wait for migration to finish. Wait for some time...
	for idx := 1; idx <= maxRetry; idx++ {
		r.Log.V(1).Info("Waiting for migrations to be zero")
		time.Sleep(retryInterval)

		// This should fail if coldstart is going on.
		// Info command in cold-starting node should give error, is it? confirm.

		isStable, err = deployment.IsClusterAndStable(
			r.Log, policy, allHostConns,
		)
		if err != nil {
			return common.ReconcileError(err)
		}

		if isStable {
			r.Log.V(1).Info("Cluster is now stable")
			break
		}
	}

	if !isStable {
		return common.ReconcileRequeueAfter(60)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) tipClearHostname(
	ctx context.Context, pod *corev1.Pod, clearPodName string,
) error {
	asConn := r.newAsConn(pod)

	_, heartbeatTLSPort := asdbv1.GetHeartbeatTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatTLSPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(ctx), getFQDNForPod(r.aeroCluster, clearPodName),
			int(*heartbeatTLSPort),
		); err != nil {
			return err
		}
	}

	heartbeatPort := asdbv1.GetHeartbeatPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(ctx), getFQDNForPod(r.aeroCluster, clearPodName),
			int(*heartbeatPort),
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) alumniReset(ctx context.Context, pod *corev1.Pod) error {
	asConn := r.newAsConn(pod)
	return asConn.AlumniReset(r.getClientPolicy(ctx))
}

// newAllHostConnWithOption returns connections to all pods in the cluster skipping pods that are not running and
// present in ignorablePods.
func (r *SingleClusterReconciler) newAllHostConnWithOption(ctx context.Context, ignorablePodNames sets.Set[string]) (
	[]*deployment.HostConn, error,
) {
	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("cluster Pod list is empty")
	}

	return r.newPodsHostConnWithOption(podList.Items, ignorablePodNames)
}

// newPodsHostConnWithOption returns connections to all pods given skipping pods that are not running and
// present in ignorablePods.
func (r *SingleClusterReconciler) newPodsHostConnWithOption(pods []corev1.Pod, ignorablePodNames sets.Set[string]) (
	[]*deployment.HostConn, error,
) {
	hostConns := make([]*deployment.HostConn, 0, len(pods))

	for idx := range pods {
		pod := &pods[idx]
		if utils.IsPodTerminating(pod) {
			continue
		}

		// Only the Aerospike server container needs to be running to accept info calls.
		// Sidecar failures do not prevent the server from being reachable.
		if !utils.IsAerospikeServerReady(pod) {
			if ignorablePodNames.Has(pod.Name) {
				// This pod's aerospike server is not running and it is marked ignorable.
				r.Log.Info(
					"Ignoring info call on Pod with non-running server container", "pod", utils.GetNamespacedName(pod),
				)

				continue
			}

			return nil, fmt.Errorf("pod %s server container is not running", utils.GetNamespacedNameString(pod))
		}

		asConn := r.newAsConn(pod)
		host := hostID(asConn.AerospikeHostName, asConn.AerospikePort)

		hostConn := deployment.NewHostConn(asConn.Log, host, asConn)
		hostConns = append(hostConns, hostConn)
	}

	return hostConns, nil
}

func (r *SingleClusterReconciler) newAsConn(pod *corev1.Pod) *deployment.ASConn {
	// Use pod IP and direct service port from within the operator for info calls.
	tlsName, port := r.getServiceTLSNameAndPortIfConfigured()

	if tlsName == "" || port == nil {
		port = asdbv1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig)
	}

	host := pod.Status.PodIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(*port),
		AerospikeTLSName:  tlsName,
		Log:               r.Log.WithValues("pod", utils.GetNamespacedName(pod)),
	}

	return asConn
}

func hostID(hostName string, hostPort int) string {
	return fmt.Sprintf("%s:%d", hostName, hostPort)
}

func (r *SingleClusterReconciler) setMigrateFillDelay(
	ctx context.Context,
	policy *as.ClientPolicy,
	asConfig *asdbv1.AerospikeConfigSpec, setToZero bool, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	migrateFillDelay, err := asdbv1.GetMigrateFillDelay(asConfig)
	if err != nil {
		return common.ReconcileError(err)
	}

	var oldMigrateFillDelay int

	if len(r.aeroCluster.Status.RackConfig.Racks) > 0 {
		oldMigrateFillDelay, err = asdbv1.GetMigrateFillDelay(&r.aeroCluster.Status.RackConfig.Racks[0].AerospikeConfig)
		if err != nil {
			return common.ReconcileError(err)
		}
	}

	if migrateFillDelay == 0 && oldMigrateFillDelay == 0 {
		r.Log.Info("migrate-fill-delay config not present or 0, skipping it")
		return common.ReconcileSuccess()
	}

	// Set migrate-fill-delay to 0 if setToZero flag is set
	if setToZero {
		migrateFillDelay = 0
	}

	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf(
				"get host connections for cluster nodes: %w", err,
			),
		)
	}

	r.Log.Info("Setting migrate-fill-delay", "migrateFillDelay", migrateFillDelay)

	if err := deployment.SetMigrateFillDelay(r.Log, policy, allHostConns, migrateFillDelay); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) setDynamicConfig(
	ctx context.Context,
	dynamicConfDiffPerPod map[string]asconfig.DynamicConfigMap, pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf(
				"get host connections for cluster nodes: %w", err,
			),
		)
	}

	podList := make([]corev1.Pod, 0, len(pods))
	podIPNameMap := make(map[string]string, len(pods))

	for idx := range pods {
		podIPNameMap[pods[idx].Status.PodIP] = pods[idx].Name
		podList = append(podList, *pods[idx])
	}

	selectedHostConns, err := r.newPodsHostConnWithOption(podList, ignorablePodNames)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf(
				"get host connections for cluster nodes: %w", err,
			),
		)
	}

	if len(selectedHostConns) == 0 {
		r.Log.Info("No Pods selected for dynamic config change")

		return common.ReconcileSuccess()
	}

	for _, host := range selectedHostConns {
		podName := podIPNameMap[host.ASConn.AerospikeHostName]

		asConfCmds, err := asconfig.CreateSetConfigCmdList(r.Log, dynamicConfDiffPerPod[podName],
			host.ASConn, r.getClientPolicy(ctx))
		if err != nil {
			// Assuming error returned here will not be a server error.
			return common.ReconcileError(err)
		}

		r.Log.Info("Generated dynamic config commands",
			"commands", asConfCmds, "pod", utils.NewNamespacedName(r.aeroCluster.Namespace, podName))

		if succeededCmds, err := deployment.SetConfigCommandsOnHosts(r.Log, r.getClientPolicy(ctx), allHostConns,
			[]*deployment.HostConn{host}, asConfCmds); err != nil {
			errorStatus := asdbv1.Failed

			// if the len of succeededCmds is not 0 along with error, then it is partially failed.
			if len(succeededCmds) != 0 {
				errorStatus = asdbv1.PartiallyFailed
			}

			patches := make([]jsonpatch.PatchOperation, 0, 1)

			patch := jsonpatch.PatchOperation{
				Operation: "replace",
				Path:      "/status/pods/" + podName + "/dynamicConfigUpdateStatus",
				Value:     errorStatus,
			}
			patches = append(patches, patch)

			if patchErr := r.patchPodStatus(
				ctx, patches,
			); patchErr != nil {
				return common.ReconcileError(
					errors.Join(
						fmt.Errorf("update status: %w", patchErr),
						fmt.Errorf("apply dynamic config: %w", err),
					),
				)
			}

			return common.ReconcileError(err)
		}

		if err := r.updateAerospikeConfInPod(podName); err != nil {
			return common.ReconcileError(err)
		}
	}

	return common.ReconcileSuccess()
}

// skipCheckpointCmdFmt opts one namespace out of the cluster-wide
// index-checkpoint-path. skip-checkpoint is the only dynamic index-checkpoint key.
const skipCheckpointCmdFmt = "set-config:context=namespace;namespace=%s;skip-checkpoint=true"

// checkpointParkTimeoutSec is the post-save park the server holds while waiting
// for AKO's SIGTERM, passed to CheckpointSave (1..3600 s; the server's own default is 300 s).
// Sized to cover several of AKO's poll+requeue cycles (~120 s each: a ~60 s poll
// window, then ReconcileRequeueAfter(60)) so a slow multi-GB checkpoint is not
// abandoned mid-copy, while keeping the self-heal bound short. On expiry asd
// exits on its own, the container restarts inside the same pod sandbox — the
// SysV segments survive, so it warm-restarts — and reports state=none again.
// That costs one redundant checkpoint but never wedges the reconcile.
// The server treats this as write-once for the in-flight checkpoint.
const checkpointParkTimeoutSec = 600

// triggerIndexCheckpointSave sends checkpoint-save to every pod in pods, then polls
// checkpoint-status for the whole batch together. Called once per batch, before any pod
// is touched.
// Idempotence comes from the server, the save is sent unconditionally, and the response classified,
// so a pod already saving, or fresh save, both resolve correctly with no additional state.
// Checkpointing is skipped for namespace if
// 1. Data-size is changes
// 2. Namespace is not in checkpointEnabledNSs
// 3. Namespace is removed or the checkpoint path is changed
func (r *SingleClusterReconciler) triggerIndexCheckpointSave(
	ctx context.Context,
	pods []*corev1.Pod, rackState *RackState,
) common.ReconcileResult {
	rackStatus := r.getRackStatus(rackState)
	if rackStatus == nil {
		return common.ReconcileSuccess()
	}

	eligibleNSs, skipNSs := r.splitCheckpointNamespaces(rackStatus, rackState.Rack)
	if len(eligibleNSs) == 0 || len(pods) == 0 {
		if len(skipNSs) != 0 {
			r.Log.Info("Skipping index checkpoint save, no namespace's checkpoint would be read",
				"skippedNamespaces", skipNSs)
		}

		return common.ReconcileSuccess()
	}

	policy := r.getClientPolicy(ctx)

	r.skipCheckpointForNamespaces(pods, skipNSs, policy)

	// No checkpoint-status precheck: the server answers a re-issue idempotently so an already-saving pod
	// classifies as CheckpointSaveAccepted below
	for _, pod := range pods {
		verdict, err := r.newAsConn(pod).CheckpointSave(policy, checkpointParkTimeoutSec)
		if err != nil {
			// Network-level failure (connection error, timeout, EOF).
			return common.ReconcileError(
				fmt.Errorf("checkpoint-save on pod %s: %w", pod.Name, err),
			)
		}

		switch verdict {
		case deployment.CheckpointSaveRejected:
			return common.ReconcileError(fmt.Errorf(
				"checkpoint-save rejected by pod %s", utils.GetNamespacedName(pod)))

		case deployment.CheckpointSaveAccepted:
			// Running, finished, or failed — the poll below distinguishes them, and a
			// failed save gets its IndexCheckpointFailed event from there with the
			// namespace list.
			r.Log.Info("Index checkpoint save already accepted, polling status",
				"pod", utils.GetNamespacedName(pod))

		case deployment.CheckpointSaveNothingToDo:
			// The running config disagrees with the rack status AKO derived its
			// namespace set from. Retrying cannot fix it, and no checkpoint was
			// possible, so proceed rather than wedge the restart — but say so
			// loudly, since a shadowless memory namespace loses its data this way.
			r.Log.Info("Server reports no checkpointing namespace, proceeding without a checkpoint",
				"pod", utils.GetNamespacedName(pod), "expectedNamespaces", eligibleNSs)

		case deployment.CheckpointSaveTriggered:
			r.Log.Info("Index checkpoint save triggered",
				"pod", utils.GetNamespacedName(pod), "namespaces", eligibleNSs,
				"parkTimeoutSeconds", checkpointParkTimeoutSec)
		}
	}

	return r.waitForIndexCheckpointDone(ctx, pods)
}

// splitCheckpointNamespaces divides the namespaces the running servers are
// checkpointing into those still worth saving (eligibleNSs) and those to opt out (skipNSs).
//
// A namespace is skipped when the replacement pod will never usably read its checkpoint:
//   - the cluster-wide path moved, so it lands at the abandoned path;
//   - the namespace stopped checkpointing in the spec (removed, skip-checkpoint,);
//   - its in-memory data layout changed — a different data-size, or storage backing
//     gained or lost, so the checkpoint no longer describes the layout the replacement pod will have.
func (r *SingleClusterReconciler) splitCheckpointNamespaces(
	rackStatus, rackSpec *asdbv1.Rack,
) (eligibleNSs, skipNSs []string) {
	statusNSs := asdbv1.GetIndexCheckpointNamespaces(rackStatus.AerospikeConfig.Value)
	if len(statusNSs) == 0 {
		return nil, nil
	}

	// The path is cluster-wide, so a change to it invalidates every namespace's checkpoint
	// at once.
	if asdbv1.GetIndexCheckpointPath(rackStatus.AerospikeConfig.Value) !=
		asdbv1.GetIndexCheckpointPath(rackSpec.AerospikeConfig.Value) {
		r.Log.Info("Excluding every namespace from the imminent index checkpoint save: "+
			"the cluster-wide index-checkpoint-path moved, so every checkpoint would land "+
			"at the abandoned path", "namespaces", statusNSs)

		return nil, statusNSs
	}

	specNSs := sets.New[string](asdbv1.GetIndexCheckpointNamespaces(rackSpec.AerospikeConfig.Value)...)

	oldSizes := asdbv1.GetInMemoryNsDataSizes(rackStatus.AerospikeConfig.Value)
	newSizes := asdbv1.GetInMemoryNsDataSizes(rackSpec.AerospikeConfig.Value)

	for _, ns := range statusNSs {
		oldSize := oldSizes[ns]
		newSize := newSizes[ns]

		switch {
		case !specNSs.Has(ns):
			r.Log.Info("Excluding namespace from the imminent index checkpoint save: it "+
				"stopped checkpointing (removed from the CR, skip-checkpoint set)", "namespace", ns)

			skipNSs = append(skipNSs, ns)

		case oldSize != newSize:
			// data-size changes make the checkpoint incompatible with the replacement pod, so skip it.
			// Moving from pure in-memory data to disk backing makes the checkpoint incompatible with the
			// replacement pod, so skip it.
			r.Log.Info("Excluding namespace from the imminent index checkpoint save: its "+
				"in-memory data layout changed, so the checkpoint save would be rejected",
				"namespace", ns, "oldDataSize", oldSize, "newDataSize", newSize)

			skipNSs = append(skipNSs, ns)

		default:
			eligibleNSs = append(eligibleNSs, ns)
		}
	}

	return eligibleNSs, skipNSs
}

// skipCheckpointForNamespaces opts each namespace out of the imminent save by
// setting skip-checkpoint dynamically on every pod in the batch. Must run BEFORE
// checkpoint-save — once the save fires, set-config is FORBIDDEN for the whole park.
// A FORBIDDEN reply here is expected on a resumed pass: that pod is already parked.
// Failures are logged, never fatal, and that is safe in one direction only: a failed
// set-config leaves the namespace checkpointing.
func (r *SingleClusterReconciler) skipCheckpointForNamespaces(
	pods []*corev1.Pod, skippedNss []string, policy *as.ClientPolicy,
) {
	if len(skippedNss) == 0 {
		return
	}

	for _, pod := range pods {
		asConn := r.newAsConn(pod)

		for _, ns := range skippedNss {
			cmd := fmt.Sprintf(skipCheckpointCmdFmt, ns)

			resp, err := asConn.RunInfo(policy, cmd)
			if err != nil {
				r.Log.V(1).Info("Could not set skip-checkpoint, namespace will be checkpointed needlessly",
					"pod", pod.Name, "namespace", ns, "error", err)

				continue
			}

			if respVal := resp[cmd]; info.IsInfoErrorResponse(respVal) {
				r.Log.V(1).Info("skip-checkpoint rejected, namespace will be checkpointed needlessly",
					"pod", pod.Name, "namespace", ns, "response", respVal)

				continue
			}

			// Why each namespace was excluded is a per-rack decision, logged once by
			// the caller; this line is per pod and only confirms the opt-out landed.
			r.Log.Info("Excluded namespace from the imminent index checkpoint save",
				"pod", pod.Name, "namespace", ns)
		}
	}
}

// waitForIndexCheckpointDone polls checkpoint-status on every pod together
// Pods that report a terminal state (done or failed) drop out of polling; the
// remaining ones keep getting polled until all are terminal or the window
// expires. Transient info errors and server-side rejections are retried
// within the window.
func (r *SingleClusterReconciler) waitForIndexCheckpointDone(
	ctx context.Context, pods []*corev1.Pod,
) common.ReconcileResult {
	const (
		maxRetry      = 6
		retryInterval = 10 * time.Second
	)

	policy := r.getClientPolicy(ctx)
	pending := make([]*corev1.Pod, len(pods))
	copy(pending, pods)

	for i := 0; i < maxRetry && len(pending) > 0; i++ {
		r.Log.V(1).Info("Waiting for index checkpoint to complete",
			"pods", getPodNames(pending), "attempt", i+1)

		stillPending := make([]*corev1.Pod, 0, len(pending))

		for _, pod := range pending {
			statuses, err := r.newAsConn(pod).CheckpointStatus(policy)
			if err != nil {
				r.Log.V(1).Info("checkpoint-status unavailable, will retry",
					"pod", utils.GetNamespacedName(pod), "error", err)
				stillPending = append(stillPending, pod)

				continue
			}

			// An empty map is the server's "no namespace is checkpointing" answer, not an error
			if len(statuses) == 0 {
				r.Log.Info("Server reports no checkpointing namespace, nothing to wait for",
					"pod", utils.GetNamespacedName(pod))

				continue
			}

			done, failedNSs := checkpointDone(statuses)
			if !done {
				r.Log.Info("Index checkpoint in progress", "pod", utils.GetNamespacedName(pod),
					"status", statuses)

				stillPending = append(stillPending, pod)

				continue
			}

			if len(failedNSs) > 0 {
				r.Log.Info("Index checkpoint failed for one or more namespaces, falling back to cold restart",
					"pod", utils.GetNamespacedName(pod), "failedNamespaces", failedNSs, "status", statuses)
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning, "IndexCheckpointFailed",
					"Index checkpoint failed for pod %s namespaces %v; proceeding with cold restart",
					utils.GetNamespacedName(pod), failedNSs,
				)
			} else {
				r.Log.Info("Index checkpoint complete, proceeding with pod delete",
					"pod", utils.GetNamespacedName(pod))
			}
		}

		pending = stillPending

		time.Sleep(retryInterval)
	}

	if len(pending) > 0 {
		pendingNames := getPodNames(pending)
		r.Log.Info("Index checkpoint not done within polling window, requeueing reconcile",
			"pods", pendingNames)

		return common.ReconcileRequeueAfter(60)
	}

	return common.ReconcileSuccess()
}

// checkpointDone reports whether this pod's checkpoint has finished and which namespaces failed.
// failedNSs lists reported namespaces in state=failed, so the caller can warn before proceeding with a cold restart.
func checkpointDone(
	statuses map[string]deployment.CheckpointNamespaceStatus,
) (done bool, failedNSs []string) {
	for ns, status := range statuses {
		if !status.IsTerminal() {
			return false, nil
		}

		if status.State == deployment.CheckpointStateFailed {
			failedNSs = append(failedNSs, ns)
		}
	}

	return true, failedNSs
}

// checkpointStarted reports whether a checkpoint save has been issued to this pod.
// Any reported namespace in a state other than "none" proves it: the trigger sets the
// checkpoint flag on every namespace that resolved a path at once, so a single non-none
// state means this pod's save fired and it is parked.
func checkpointStarted(statuses map[string]deployment.CheckpointNamespaceStatus) bool {
	for _, status := range statuses {
		if status.State != deployment.CheckpointStateNone {
			return true
		}
	}

	return false
}

// filterCheckpointingPods returns the list of pods which are already checkpointing by checking the checkpoint-status.
func (r *SingleClusterReconciler) filterCheckpointingPods(
	ctx context.Context, pods []*corev1.Pod, rackState *RackState,
) []*corev1.Pod {
	rackStatus := r.getRackStatus(rackState)
	if rackStatus == nil {
		return nil
	}

	// This check won't cover scenarios where there are no namespaces to checkpoint i.e. checkpointing is disabled in
	// spec during an update.
	if len(asdbv1.GetIndexCheckpointNamespaces(rackStatus.AerospikeConfig.Value)) == 0 || len(pods) == 0 {
		return nil
	}

	policy := r.getClientPolicy(ctx)

	var parked []*corev1.Pod

	for _, pod := range pods {
		if r.podCheckpointStarted(pod, policy) {
			parked = append(parked, pod)
		}
	}

	return parked
}

// resumeParkedCheckpointPods narrows a batch to its checkpoint-parked subset, if any.
// When there are partial checkpointed pods in a batch, then only those pods are returned and
// remaining pods are deferred.
func (r *SingleClusterReconciler) resumeParkedCheckpointPods(
	ctx context.Context, activePods, eligible []*corev1.Pod, rackState *RackState,
) (batch []*corev1.Pod, deferred int, resuming bool) {
	parked := r.filterCheckpointingPods(ctx, eligible, rackState)
	if len(parked) == 0 {
		return activePods, 0, false
	}

	return parked, len(activePods) - len(parked), true
}

// podCheckpointStarted reports whether this pod has a checkpoint save in flight for
// any namespace in nsSet. Anything short of a clean, affirmative answer is false.
func (r *SingleClusterReconciler) podCheckpointStarted(
	pod *corev1.Pod, policy *as.ClientPolicy,
) bool {
	statuses, err := r.newAsConn(pod).CheckpointStatus(policy)
	if err != nil {
		return false
	}

	return checkpointStarted(statuses)
}
