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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// reconcileCheckpointingPods completes any index-checkpoint park left in flight, cluster-wide.
// A parked node is an obligation rather than a divergence: checkpoint-save is a point of no
// return — the node has left the cluster and shut down its storage, so it cannot serve again
// without a restart, and no comparison of spec against status will say so. Whatever parked it
// may since have finished, been superseded, or been reverted; the pod must still be deleted.
// Running this before the racks reconcile means no later phase ever meets a parked pod: the
// safe-stop checks, scale-down and the batch logic all see either a healthy node or one that
// is on its way back.
func (r *SingleClusterReconciler) reconcileCheckpointingPods(
	ctx context.Context, rackStates []RackState,
) common.ReconcileResult {
	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		return common.ReconcileError(err)
	}

	parked := make([]*corev1.Pod, 0, len(podList.Items))
	rackForPod := make(map[string]*RackState, len(podList.Items))

	for idx := range podList.Items {
		pod := &podList.Items[idx]
		if !utils.IsPodCheckpointing(pod) {
			continue
		}

		rackState := rackStateForPod(rackStates, pod)
		if rackState == nil {
			// Neither configured, mid-migration, nor being deleted — a pod from a rack the
			// operator no longer tracks at all. Nothing here can resolve its storage config,
			// so leave it: its park times out and the container restarts on its own.
			r.Log.Info("Checkpoint-parked Pod belongs to no known rack, leaving its park to time out",
				"pod", utils.GetNamespacedName(pod))

			continue
		}

		parked = append(parked, pod)
		rackForPod[pod.Name] = rackState
	}

	if len(parked) == 0 {
		return common.ReconcileSuccess()
	}

	r.Log.Info("Completing in-flight index checkpoints", "pods", getPodNames(parked))

	deleted, res := r.pollAndDeleteParkedPods(ctx, parked, rackForPod)

	// Record whatever was deleted even when the poll ended in an error or a requeue —
	// otherwise an on-demand operation never sees those restarts and re-parks the pods.
	if err := r.updateOperationStatus(ctx, nil, getPodNames(deleted)); err != nil {
		return common.ReconcileError(err)
	}

	if !res.IsSuccess {
		// Error, or the 60s requeue for pods still copying.
		return res
	}

	// Every park is discharged; settle the replacements before handing back to the racks.
	if result := r.ensurePodsRunningAndReady(ctx, deleted); !result.IsSuccess {
		return result
	}

	return common.ReconcileRequeueAfter(1)
}

// rackStateForPod matches a pod to its rack by the rack ID and revision labels.
func rackStateForPod(rackStates []RackState, pod *corev1.Pod) *RackState {
	rackID := pod.Labels[asdbv1.AerospikeRackIDLabel]
	revision := pod.Labels[asdbv1.AerospikeRackRevisionLabel]

	for idx := range rackStates {
		rackState := &rackStates[idx]
		if strconv.Itoa(rackState.Rack.ID) == rackID && rackState.Rack.Revision == revision {
			return rackState
		}
	}

	return nil
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

		// A parked node has left the cluster and answers only the two checkpoint
		// commands, so including it would fail every caller's info call. Excluding it
		// is also what makes the expected size match the cluster_size the remaining
		// nodes report.
		if utils.IsPodCheckpointing(pod) {
			r.Log.V(1).Info("Excluding checkpoint-parked Pod from cluster info calls",
				"pod", utils.GetNamespacedName(pod))

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
				Operation: patchOperationReplace,
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

// checkpointParkTimeoutSec is the park the server holds after the save, waiting for AKO's SIGTERM
// Passed to CheckpointSave (1..3600 s; the server's own default is 300 s).
// The clock starts once the copy has finished. It only has to cover
// how long AKO takes to notice a finished checkpoint and delete the pod: the discharge
// phase polls 6 × 10 s and then requeues for 60 s, so the copy is seen within ~70 s of
// completing even in the worst alignment, and the delete follows in that same pass.
const checkpointParkTimeoutSec = 300

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

	var anyParked bool

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
			// Running, finished, or failed — the discharge phase distinguishes them, and a
			// failed save gets its IndexCheckpointFailed event from there with the
			// namespace list.
			r.Log.Info("Index checkpoint save already accepted",
				"pod", utils.GetNamespacedName(pod))
			r.markPodCheckpointParked(ctx, pod)

			anyParked = true

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
			r.markPodCheckpointParked(ctx, pod)

			anyParked = true
		}
	}

	// Requeue only when something actually checkpointing. Every pod answering NothingToDo means no
	// node is holding a checkpoint, so falling through lets the caller delete in this pass;
	// requeueing unconditionally would spin forever on a cluster whose running config never
	// resolves a checkpoint path.
	if anyParked {
		return common.ReconcileRequeueAfter(1)
	}

	return common.ReconcileSuccess()
}

// markPodCheckpointParked records on the pod that its Aerospike node is checkpointing, so later
// reconciles can tell it is out of the cluster without asking the server — which a parked node would refuse anyway.
// Written only after checkpoint-save has been accepted, so the annotation can never claim
// a park that did not happen. The reverse — a park with no annotation — is possible if the
// patch cannot be written at all, and degrades to the pre-annotation behaviour: the node is
// treated as a cluster member, so cluster info calls hit it and fail until its park times
// out and it restarts. Failing the reconcile here would fix neither, so it is retried, then
// reported as precisely as we can and left to self-heal.
func (r *SingleClusterReconciler) markPodCheckpointParked(ctx context.Context, pod *corev1.Pod) {
	containerID := utils.GetAerospikeServerContainerID(pod)
	if containerID == "" {
		r.Log.Info("No aerospike-server container ID, cannot record checkpoint park",
			"pod", utils.GetNamespacedName(pod))

		return
	}

	// Capture the base and set the annotation ONCE, before the retry — a base captured
	// inside a retry of an already-mutated pod diffs to an empty patch.
	patch := client.MergeFrom(pod.DeepCopy())

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	pod.Annotations[asdbv1.IndexCheckpointParkedAnnotation] = containerID

	if err := retry.OnError(retry.DefaultBackoff,
		func(err error) bool { return !k8serrors.IsNotFound(err) },
		func() error {
			return r.Patch(ctx, pod, patch)
		}); err != nil {
		if k8serrors.IsNotFound(err) {
			r.Log.Info("Pod deleted before the checkpoint park could be recorded, nothing to track",
				"pod", utils.GetNamespacedName(pod))

			return
		}

		r.Log.Error(err, "Failed to record the index checkpoint park. This node has left the "+
			"cluster but AKO cannot tell, so cluster operations will fail until its park times "+
			"out and the container restarts",
			"pod", utils.GetNamespacedName(pod), "parkTimeoutSeconds", checkpointParkTimeoutSec)
	}
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
// Failures are logged at Error but never fatal: a failed set-config leaves the
// namespace checkpointing, costing a redundant copy that stretches the park — worth
// an operator's attention, not an aborted restart.
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
				r.Log.Error(err, "Could not set skip-checkpoint, namespace will be checkpointed needlessly",
					"pod", utils.GetNamespacedName(pod), "namespace", ns)

				continue
			}

			if respVal := resp[cmd]; info.IsInfoErrorResponse(respVal) {
				r.Log.Error(fmt.Errorf("skip-checkpoint rejected"),
					"Could not set skip-checkpoint, namespace will be checkpointed needlessly",
					"pod", utils.GetNamespacedName(pod), "namespace", ns, "response", respVal)

				continue
			}

			// Why each namespace was excluded is a per-rack decision, logged once by
			// the caller; this line is per pod and only confirms the opt-out landed.
			r.Log.Info("Excluded namespace from the imminent index checkpoint save",
				"pod", pod.Name, "namespace", ns)
		}
	}
}

// pollAndDeleteParkedPods polls checkpoint-status across the parked pods and deletes each
// pod as its save completes, acting on the same response that proved it.
// Deleting per pod rather than after the whole batch so that skewed/slow sibling node doesn't slow the
// whole batch leading to park timeout and asd exit.
// Pods still copying stay pending; whatever remains at the window's end requeues. Deleted
// pods are returned even alongside an error, so the caller's bookkeeping never loses a
// delete that happened.
func (r *SingleClusterReconciler) pollAndDeleteParkedPods(
	ctx context.Context, pods []*corev1.Pod, rackForPod map[string]*RackState,
) (deleted []*corev1.Pod, res common.ReconcileResult) {
	const (
		maxRetry      = 6
		retryInterval = 10 * time.Second
	)

	policy := r.getClientPolicy(ctx)
	pending := make([]*corev1.Pod, len(pods))
	copy(pending, pods)

	for i := 0; i < maxRetry && len(pending) > 0; i++ {
		r.Log.V(1).Info("Waiting for index checkpoints to complete",
			"pods", getPodNames(pending), "attempt", i+1)

		time.Sleep(retryInterval)

		stillPending := make([]*corev1.Pod, 0, len(pending))

		for _, pod := range pending {
			resp, err := r.newAsConn(pod).CheckpointStatus(policy)

			switch {
			case errors.Is(err, deployment.ErrCheckpointNotConfigured):
				// The running server resolves no checkpoint path at all — its config
				// disagrees with the rack status AKO derived its namespace set from.
				// Retrying cannot change that, and there is no park to discharge.
				r.Log.Info("Server reports index checkpoint is not configured, leaving Pod to the normal flow",
					"pod", utils.GetNamespacedName(pod))

				continue

			case err != nil:
				r.Log.V(1).Info("checkpoint-status unavailable, will retry",
					"pod", utils.GetNamespacedName(pod), "error", err)
				stillPending = append(stillPending, pod)

				continue

			case len(resp.Namespaces) == 0 && !resp.IsParked:
				// Configured, checkpointing nothing, and NOT parked: a RESTARTED asd.
				// Nothing to wait for and nothing to delete; the annotation check
				// reclassifies it next pass. The PARKED form of an empty response falls
				// through instead — see checkpointDone.
				r.Log.Info("Server reports no checkpointing namespace and no park, leaving Pod to the normal flow",
					"pod", utils.GetNamespacedName(pod))

				continue
			}

			done, failedNSs := checkpointDone(resp)
			if !done {
				r.Log.Info("Index checkpoint in progress", "pod", utils.GetNamespacedName(pod),
					"status", resp)

				stillPending = append(stillPending, pod)

				continue
			}

			if len(failedNSs) > 0 {
				r.Log.Info("Index checkpoint failed for one or more namespaces, falling back to cold restart",
					"pod", utils.GetNamespacedName(pod), "failedNamespaces", failedNSs, "status", resp)
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning, "IndexCheckpointFailed",
					"Index checkpoint failed for pod %s namespaces %v; proceeding with cold restart",
					utils.GetNamespacedName(pod), failedNSs,
				)
			}

			// isFailureRecovery is false by construction: the checkpoint save is only
			// triggered on the planned path, so a parked pod is never a failure-recovery pod.
			if err := r.deletePodWithLocalPVCs(ctx, rackForPod[pod.Name], pod, false); err != nil {
				return deleted, common.ReconcileError(err)
			}

			deleted = append(deleted, pod)
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeNormal, "IndexCheckpoint",
				"Index checkpoint complete, restarting Pod %s", utils.GetNamespacedName(pod),
			)
		}

		pending = stillPending
	}

	if len(pending) > 0 {
		r.Log.Info("Index checkpoint not done within polling window, requeueing reconcile",
			"pods", getPodNames(pending))

		return deleted, common.ReconcileRequeueAfter(60)
	}

	return deleted, common.ReconcileSuccess()
}

// checkpointDone reports whether this pod's checkpoint has finished and which namespaces failed.
// failedNSs lists reported namespaces in state=failed, so the caller can warn before proceeding with a cold restart.
// A parked node reporting NO namespaces is done: a node whose namespaces have all opted
// out parks without writing anything, so there is nothing to wait for and it still has to
// be deleted — it has left the cluster and cannot serve again. The caller screens out the
// empty-and-not-parked response, which is a restarted asd rather than a finished save.
func checkpointDone(
	resp deployment.CheckpointResponse,
) (done bool, failedNSs []string) {
	for ns, status := range resp.Namespaces {
		if !status.IsTerminal() {
			return false, nil
		}

		if status.State == deployment.CheckpointStateFailed {
			failedNSs = append(failedNSs, ns)
		}
	}

	return true, failedNSs
}
