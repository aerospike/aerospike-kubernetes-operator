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
	"k8s.io/client-go/util/retry"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

// ------------------------------------------------------------------------------------
// Aerospike helper
// ------------------------------------------------------------------------------------

// waitForMultipleNodesSafeStopReady waits until the input pods are safe to stop,
// skipping pods that are not running and present in ignorablePodNames for stability check.
// The ignorablePodNames is the list of failed or pending pods that are either::
// 1. going to be deleted eventually and are safe to ignore in stability checks
// 2. given in ignorePodList by the user and are safe to ignore in stability checks
//
// mfdDelay is the target migrate-fill-delay value; drainBeforeStability controls whether MFD is
// zeroed before the stability check. Both are computed by mfdDelayForRestart (rolling restart /
// upgrade) or set directly by the scale-down path.
//
//   - drainBeforeStability == true: MFD was transiently raised (override or
//     DeleteLocalStorageOnRestart path); zero it first so that fills held from the previous
//     quiesce can drain, then raise to mfdDelay before the next quiesce.
//   - drainBeforeStability == false: MFD was not transiently raised; skip the drain step and go
//     straight to the stability check. mfdDelay is still raised before quiesce when > 0 (used to
//     restore MFD to the aerospikeConfig value after a prior scale-down zero, or as a no-op when
//     the DynamicMigrateFillDelay guard detects the value is already correct).
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	ctx context.Context, pods []*corev1.Pod, ignorablePodNames sets.Set[string],
	mfdDelay int, drainBeforeStability bool,
) common.ReconcileResult {
	if len(pods) == 0 {
		return common.ReconcileSuccess()
	}

	// Remove a node only if the cluster is stable
	if err := r.waitForAllSTSToBeReady(ctx, ignorablePodNames); err != nil {
		return common.ReconcileError(fmt.Errorf(
			"wait for cluster StatefulSets to be ready: %w", err))
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

	if drainBeforeStability {
		// Zero MFD so that fills held from the previous pod's quiesce (where MFD was transiently
		// raised) can drain before this stability check. Only set on the override,
		// DeleteLocalStorageOnRestart, and scale-down paths.
		if res := r.setMigrateFillDelay(ctx, policy, 0, ignorablePodNames); !res.IsSuccess {
			return res
		}
	}

	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
		return res
	}

	// Setup roster after migration.
	if err = r.getAndSetRoster(ctx, policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
		r.Log.Error(err, "Failed to set roster for cluster, will requeue")
		return common.ReconcileRequeueAfter(1)
	}

	// Raise MFD to mfdDelay before quiesce. This is either the OverrideMigrateFillDelay value
	// (suppresses fills while the pod is absent) or the aerospikeConfig value (restores MFD to
	// the steady-state after a prior scale-down zero). Skipped when mfdDelay==0 (scale-down or
	// configMFD not set) — fills should proceed at full speed in those cases.
	if mfdDelay > 0 {
		if res := r.setMigrateFillDelay(ctx, policy, mfdDelay, ignorablePodNames); !res.IsSuccess {
			return res
		}
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

		// Checking if all the container in the pod are ready or not
		if !utils.IsPodRunningAndReady(pod) {
			if ignorablePodNames.Has(pod.Name) {
				// This pod is not running and ignorable.
				r.Log.Info(
					"Ignoring info call on non-running Pod", "pod", utils.GetNamespacedName(pod),
				)

				continue
			}

			return nil, fmt.Errorf("status for Pod %s: not ready", utils.GetNamespacedNameString(pod))
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

// setMigrateFillDelay sets the migrate-fill-delay on all cluster nodes to delay fill migrations.
// The caller is responsible for resolving delay (e.g. via GetMigrateFillDelay for the
// config-driven value, or a literal 0 for scale-down resets).
//
// The call is skipped when delay equals status.DynamicMigrateFillDelay, which tracks the last
// value AKO successfully applied and is persisted immediately after every server call, making it
// a reliable guard.
func (r *SingleClusterReconciler) setMigrateFillDelay(
	ctx context.Context,
	policy *as.ClientPolicy,
	delay int,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if int64(delay) == r.aeroCluster.Status.DynamicMigrateFillDelay {
		r.Log.Info("migrate-fill-delay already at desired value, skipping", "value", delay)
		return common.ReconcileSuccess()
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

	r.Log.Info("Setting migrate-fill-delay", "migrateFillDelay", delay)

	if err := deployment.SetMigrateFillDelay(r.Log, policy, allHostConns, delay); err != nil {
		return common.ReconcileError(err)
	}

	// Persist DynamicMigrateFillDelay immediately so the value survives a mid-reconcile requeue.
	// Without this, an in-memory-only update would be lost when the next reconcile reads from k8s.
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, utils.GetNamespacedName(r.aeroCluster), r.aeroCluster); err != nil {
			return err
		}

		r.aeroCluster.Status.DynamicMigrateFillDelay = int64(delay)

		return r.Client.Status().Update(ctx, r.aeroCluster)
	}); err != nil {
		return common.ReconcileError(fmt.Errorf("persist dynamic migrate-fill-delay in status: %w", err))
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
