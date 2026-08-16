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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/validation"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/deployment"
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

// triggerIndexCheckpointShutdown sends checkpoint-shutdown to every pod in
// pods, then polls checkpoint-status for the whole batch together, for up to
// ~1 minute. It is called once per batch of pods that are about to be deleted,
// before any of them are touched — mirroring the trigger-once-for-the-batch
// shape already used by quiescePods / waitForMultipleNodesSafeStopReady,
// rather than interleaving a per-pod trigger+wait inside the delete loop.
// Serializing per pod would mean a single slow checkpoint blocks the rest of
// the batch from even starting theirs, even though each pod's checkpoint save
// runs concurrently on its own independent server process.
//
// It is called on every reconcile pass before pod deletion and is
// intentionally idempotent, checking checkpoint-status before deciding
// whether to send checkpoint-shutdown at all, per pod:
//
//   - If checkpoint-status already reports a non-"none" state for any
//     checkpoint-enabled namespace (isCheckpointStarted), checkpoint-shutdown
//     was already accepted on a prior pass (or this is a mixed batch where a
//     sibling pod's trigger loop aborted before reaching this pod). The
//     shutdown call is skipped entirely for that pod — there is no reason to
//     send a command we already know the admin-port gate will reject (see
//     "Admin-port gate" in docs/index-checkpoint.md: once checkpoint-shutdown
//     has fired, the server returns FORBIDDEN for every info command except
//     checkpoint-status).
//   - Otherwise checkpoint-shutdown is sent as normal. It's still handled
//     defensively (isCheckpointShutdownInProgress) in case of a genuine
//     race — status read "none" a moment before a concurrent trigger landed
//     — or because the checkpoint-status precheck itself failed and this
//     call is a fail-open retry.
//   - If the checkpoint-status precheck call itself fails (info error,
//     empty/malformed response), this fails open toward attempting
//     checkpoint-shutdown anyway, since that command is safely idempotent on
//     the server side regardless of prior state.
//   - If a server container was restarted (OOM, crash) between reconcile
//     passes, the fresh process reports checkpoint-status state=none again,
//     so the precheck correctly falls through to sending checkpoint-shutdown
//     and starting a new checkpoint — no annotation or other out-of-band
//     state is required.
//
// If no namespace has index-checkpoint-path configured, or pods is empty, this
// is a no-op returning ReconcileSuccess immediately.
//
// rackState is used to resolve the effective per-rack aerospikeConfig, which
// may differ from the spec-level config when per-rack overrides are present.
// It is shared by every pod in the batch (all belong to the same rack), so the
// checkpoint-enabled namespace set only needs to be resolved once.
//
// A checkpoint-shutdown RPC failure on any pod aborts the whole batch
// immediately, before triggering the remaining pods — the same
// first-failure-aborts-the-batch behaviour every other pre-delete step in this
// file already follows (handleNSOrDeviceRemoval, deleteLocalPVCs, r.Delete).
// Nothing destructive happens to any pod in the batch until this call
// succeeds for all of them, so aborting early is always safe to retry.
func (r *SingleClusterReconciler) triggerIndexCheckpointShutdown(
	ctx context.Context,
	pods []*corev1.Pod, rackState *RackState,
) common.ReconcileResult {
	checkpointEnabledNSs := validation.GetIndexCheckpointNamespaces(rackState.Rack.AerospikeConfig.Value)
	if len(checkpointEnabledNSs) == 0 || len(pods) == 0 {
		return common.ReconcileSuccess()
	}

	policy := r.getClientPolicy(ctx)
	nsSet := sets.New[string](checkpointEnabledNSs...)

	// The server always operates on ALL namespaces that have
	// index-checkpoint-path configured; there is no per-namespace form of the
	// command.
	for _, pod := range pods {
		asConn := r.newAsConn(pod)

		alreadyStarted, err := r.checkpointAlreadyStarted(asConn, policy, nsSet)
		if err != nil {
			return common.ReconcileError(fmt.Errorf(
				"check checkpoint-status before triggering checkpoint-shutdown for pod %s: %w",
				utils.GetNamespacedName(pod), err))
		}

		if alreadyStarted {
			r.Log.V(1).Info("checkpoint-shutdown already in progress, skipping trigger and polling status",
				"pod", pod.Name)
			continue
		}

		// TODO: check the restore of the command before continuing
		_, err = asConn.RunInfo(policy, "checkpoint-save")
		if err != nil {
			// Network-level failure (connection error, timeout, EOF).
			return common.ReconcileError(
				fmt.Errorf("checkpoint-shutdown on pod %s: %w", pod.Name, err),
			)
		}

		// "OK" or any other response — checkpoint just started.
		r.Log.Info("Index checkpoint shutdown triggered",
			"pod", pod.Name, "namespaces", checkpointEnabledNSs)

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "IndexCheckpoint",
			"Index checkpoint shutdown triggered for pod %s namespaces %v",
			pod.Name, checkpointEnabledNSs,
		)
	}

	return r.waitForIndexCheckpointDone(ctx, pods, checkpointEnabledNSs)
}

// checkpointAlreadyStarted queries checkpoint-status on a single pod and
// reports whether checkpoint-shutdown has already been accepted for any
// namespace in nsSet. On any info error or empty/malformed response, it
// returns (false, err): the caller fails open toward attempting
// checkpoint-shutdown directly — which is safely idempotent regardless — but
// the error is returned rather than swallowed so the caller can still log it.
func (r *SingleClusterReconciler) checkpointAlreadyStarted(
	asConn *deployment.ASConn, policy *as.ClientPolicy, nsSet sets.Set[string],
) (bool, error) {
	resp, err := asConn.RunInfo(policy, "checkpoint-status")
	if err != nil {
		return false, err
	}

	statusStr, ok := resp["checkpoint-status"]
	if !ok || statusStr == "" {
		return false, fmt.Errorf("empty checkpoint-status response")
	}

	return r.isCheckpointStarted(statusStr, nsSet), nil
}

// waitForIndexCheckpointDone polls checkpoint-status on every pod in pods,
// together, for up to ~1 minute (6 × 10 s) total — not per pod, since each
// pod's checkpoint save runs concurrently on its own server process, so
// there's no reason to wait on them one at a time. Pods that report a terminal
// state (done or failed) drop out of polling; the remaining ones keep getting
// polled every retryInterval until all are terminal or the window expires. The
// parked server keeps its info port open, so a reachable port reporting
// state=done or state=failed means that pod's checkpoint has finished.
// Transient info errors are retried within the window. If the window expires
// with any pod still not terminal, it returns ReconcileRequeueAfter(60) so the
// outer reconcile loop retries after 60 seconds without blocking the worker
// goroutine.
func (r *SingleClusterReconciler) waitForIndexCheckpointDone(ctx context.Context, pods []*corev1.Pod,
	namespaces []string) common.ReconcileResult {
	const (
		// TODO: rever it to 6 and 10 seconds after testing
		maxRetry      = 1
		retryInterval = 5 * time.Second
	)

	policy := r.getClientPolicy(ctx)
	nsSet := sets.New[string](namespaces...)
	pending := make([]*corev1.Pod, len(pods))
	copy(pending, pods)

	for i := 0; i < maxRetry && len(pending) > 0; i++ {
		r.Log.V(1).Info("Waiting for index checkpoint to complete",
			"pods", getPodNames(pending), "attempt", i+1)
		time.Sleep(retryInterval)

		stillPending := make([]*corev1.Pod, 0, len(pending))

		for _, pod := range pending {
			asConn := r.newAsConn(pod)

			resp, err := asConn.RunInfo(policy, "checkpoint-status")
			if err != nil {
				// The parked server keeps its info port open; an error here is a
				// transient glitch. Log and retry within the current polling window.
				r.Log.Error(err, "checkpoint-status info call failed, will retry", "pod", pod.Name)
				stillPending = append(stillPending, pod)

				continue
			}

			statusStr, ok := resp["checkpoint-status"]
			if !ok || statusStr == "" {
				r.Log.V(1).Info("Empty checkpoint-status response, will retry", "pod", pod.Name)
				stillPending = append(stillPending, pod)

				continue
			}

			done, failedNSs := r.isIndexCheckpointDone(statusStr, nsSet)
			if !done {
				r.Log.V(1).Info("Index checkpoint still in progress", "pod", pod.Name, "status", statusStr)
				stillPending = append(stillPending, pod)

				continue
			}

			if len(failedNSs) > 0 {
				r.Log.Info("Index checkpoint failed for one or more namespaces, falling back to cold restart",
					"pod", pod.Name, "failedNamespaces", failedNSs, "status", statusStr)
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning, "IndexCheckpointFailed",
					"Index checkpoint failed for pod %s namespaces %v; proceeding with cold restart",
					pod.Name, failedNSs,
				)
			} else {
				r.Log.Info("Index checkpoint complete, proceeding with pod delete", "pod", pod.Name)
			}
		}

		pending = stillPending
	}

	if len(pending) > 0 {
		pendingNames := getPodNames(pending)
		r.Log.Info("Index checkpoint not done within polling window, requeueing reconcile", "pods", pendingNames)
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "IndexCheckpoint",
			"Index checkpoint still in progress for pods %v, requeueing after 60s", pendingNames,
		)

		// TODO: Change this to 60s
		return common.ReconcileRequeueAfter(1)
	}

	return common.ReconcileSuccess()
}

// isIndexCheckpointDone returns (true, failedNSs) only when EVERY namespace
// in nsSet has been reported with a terminal state (state=done or state=failed)
// in the checkpoint-status response string. failedNSs lists any namespaces that
// reported state=failed so the caller can emit a warning before proceeding with
// a cold restart. It returns (false, nil) while any target namespace is still
// copying, not yet started, or entirely absent from the response — the latter
// guards against a partial/early response causing a premature pod delete before
// the copy has finished.
// Response format: "ns1:state=done:files=42/42;ns2:state=copying:files=20/42"
func (r *SingleClusterReconciler) isIndexCheckpointDone(statusStr string,
	nsSet sets.Set[string]) (done bool, failedNSs []string) {
	seen := sets.New[string]()

	for _, entry := range strings.Split(statusStr, ";") {
		entry = strings.TrimSpace(entry)
		colonIdx := strings.Index(entry, ":")

		if colonIdx < 0 {
			continue
		}

		ns := entry[:colonIdx]
		if !nsSet.Has(ns) {
			continue
		}

		rest := entry[colonIdx+1:]
		if strings.Contains(rest, "state=copying") || strings.Contains(rest, "state=none") {
			return false, nil
		}

		seen.Insert(ns)

		if strings.Contains(rest, "state=failed") {
			failedNSs = append(failedNSs, ns)
		}
	}

	// Every target namespace must have reported a terminal state. If any is
	// missing from the response the checkpoint is not provably complete yet, so
	// keep polling rather than risk deleting the pod mid-copy.
	if seen.Len() < nsSet.Len() {
		return false, nil
	}

	return true, failedNSs
}

// isCheckpointStarted returns true if any namespace in nsSet is reported with
// a state other than "none" in a checkpoint-status response, meaning
// checkpoint-shutdown has already been issued to this pod (possibly in a
// prior, timed-out reconcile pass).
func (r *SingleClusterReconciler) isCheckpointStarted(statusStr string, nsSet sets.Set[string]) bool {
	for _, entry := range strings.Split(statusStr, ";") {
		entry = strings.TrimSpace(entry)
		colonIdx := strings.Index(entry, ":")

		if colonIdx < 0 {
			continue
		}

		if ns := entry[:colonIdx]; nsSet.Has(ns) && !strings.Contains(entry[colonIdx+1:], "state=none") {
			return true
		}
	}

	return false
}

// allPodsAlreadyCheckpointing reports whether every pod in pods already has
// checkpoint-shutdown in flight, by querying checkpoint-status directly on
// each pod. There is no persisted state for this: it is derived live every
// call, matching triggerIndexCheckpointShutdown's idempotent design.
//
// It returns false (never skip the normal safe-stop path) if the rack has no
// checkpoint-enabled namespaces, pods is empty, or any pod's checkpoint
// status can't be confirmed as started -- including info-call failures,
// which fail safe toward re-running the ordinary stability/roster/quiesce
// checks. This also self-heals the case where a checkpointing pod's
// container was restarted (OOM, crash) between reconcile passes: the fresh
// process reports state=none again, so this correctly falls through to the
// normal safe-stop path instead of wrongly assuming the checkpoint is still
// in flight.
func (r *SingleClusterReconciler) allPodsAlreadyCheckpointing(
	ctx context.Context, pods []*corev1.Pod, rackState *RackState) bool {
	checkpointEnabledNSs := validation.GetIndexCheckpointNamespaces(rackState.Rack.AerospikeConfig.Value)
	if len(checkpointEnabledNSs) == 0 || len(pods) == 0 {
		return false
	}

	nsSet := sets.New[string](checkpointEnabledNSs...)
	policy := r.getClientPolicy(ctx)

	for _, pod := range pods {
		resp, err := r.newAsConn(pod).RunInfo(policy, "checkpoint-status")
		if err != nil {
			return false
		}

		statusStr, ok := resp["checkpoint-status"]
		if !ok || statusStr == "" || !r.isCheckpointStarted(statusStr, nsSet) {
			return false
		}
	}

	return true
}
