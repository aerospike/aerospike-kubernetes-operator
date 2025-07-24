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
)

// ------------------------------------------------------------------------------------
// Aerospike helper
// ------------------------------------------------------------------------------------

// waitForMultipleNodesSafeStopReady waits until the input pods are safe to stop,
// skipping pods that are not running and present in ignorablePodNames for stability check.
// The ignorablePodNames is the list of failed or pending pods that are either::
// 1. going to be deleted eventually and are safe to ignore in stability checks
// 2. given in ignorePodList by the user and are safe to ignore in stability checks
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if len(pods) == 0 {
		return common.ReconcileSuccess()
	}

	// Remove a node only if the cluster is stable
	if err := r.waitForAllSTSToBeReady(ignorablePodNames); err != nil {
		return common.ReconcileError(fmt.Errorf("failed to wait for cluster to be ready: %v", err))
	}

	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("failed to get hostConn for aerospike cluster nodes: %v", err))
	}

	policy := r.getClientPolicy()

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete", pods[0].Labels[asdbv1.AerospikeRackIDLabel],
	)

	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
		return res
	}

	// Setup roster after migration.
	if err = r.getAndSetRoster(policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
		r.Log.Error(err, "Failed to set roster for cluster")
		return common.ReconcileRequeueAfter(1)
	}

	if err := r.quiescePods(policy, allHostConns, pods, ignorablePodNames); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) quiescePods(
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

	nodesNamespaces, err := deployment.GetClusterNamespaces(r.Log, r.getClientPolicy(), allHostConns)
	if err != nil {
		return err
	}

	return deployment.InfoQuiesce(r.Log, policy, allHostConns, selectedHostConns, r.removedNamespaces(nodesNamespaces))
}

func (r *SingleClusterReconciler) getClusterSecurityConfig(
	policy *as.ClientPolicy, podList *corev1.PodList, ignorablePodNames sets.Set[string],
) (map[string]deployment.InfoResult, error) {
	hostConns, err := r.newPodsHostConnWithOption(podList.Items, ignorablePodNames)
	if err != nil {
		return nil, err
	}

	return deployment.GetInfoOnHosts(r.Log, policy, hostConns, "get-config:context=security")
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
	pod *corev1.Pod, clearPodName string,
) error {
	asConn := r.newAsConn(pod)

	_, heartbeatTLSPort := asdbv1.GetHeartbeatTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatTLSPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			int(*heartbeatTLSPort),
		); err != nil {
			return err
		}
	}

	heartbeatPort := asdbv1.GetHeartbeatPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			int(*heartbeatPort),
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) alumniReset(pod *corev1.Pod) error {
	asConn := r.newAsConn(pod)
	return asConn.AlumniReset(r.getClientPolicy())
}

// newAllHostConnWithOption returns connections to all pods in the cluster skipping pods that are not running and
// present in ignorablePods.
func (r *SingleClusterReconciler) newAllHostConnWithOption(ignorablePodNames sets.Set[string]) (
	[]*deployment.HostConn, error,
) {
	podList, err := r.getClusterPodList()
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("pod list empty")
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
					"Ignoring info call on non-running pod ", "pod", pod.Name,
				)

				continue
			}

			return nil, fmt.Errorf("pod %v is not ready", pod.Name)
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
	tlsName, port := r.getResolvedTLSNameAndPort()

	host := pod.Status.PodIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(*port),
		AerospikeTLSName:  tlsName,
		Log:               r.Log.WithValues("host", pod.Name),
	}

	return asConn
}

func hostID(hostName string, hostPort int) string {
	return fmt.Sprintf("%s:%d", hostName, hostPort)
}

func (r *SingleClusterReconciler) setMigrateFillDelay(
	policy *as.ClientPolicy,
	asConfig *asdbv1.AerospikeConfigSpec, setToZero bool, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	migrateFillDelay, err := asdbv1.GetMigrateFillDelay(asConfig)
	if err != nil {
		common.ReconcileError(err)
	}

	var oldMigrateFillDelay int

	if len(r.aeroCluster.Status.RackConfig.Racks) > 0 {
		oldMigrateFillDelay, err = asdbv1.GetMigrateFillDelay(&r.aeroCluster.Status.RackConfig.Racks[0].AerospikeConfig)
		if err != nil {
			common.ReconcileError(err)
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
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf(
				"failed to get hostConn for aerospike cluster nodes: %v", err,
			),
		)
	}

	if err := deployment.SetMigrateFillDelay(r.Log, policy, allHostConns, migrateFillDelay); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) setDynamicConfig(
	dynamicConfDiffPerPod map[string]asconfig.DynamicConfigMap, pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf(
				"failed to get hostConn for aerospike cluster nodes: %v", err,
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
				"failed to get hostConn for aerospike cluster nodes: %v", err,
			),
		)
	}

	if len(selectedHostConns) == 0 {
		r.Log.Info("No pods selected for dynamic config change")

		return common.ReconcileSuccess()
	}

	for _, host := range selectedHostConns {
		podName := podIPNameMap[host.ASConn.AerospikeHostName]
		asConfCmds, err := asconfig.CreateSetConfigCmdList(r.Log, dynamicConfDiffPerPod[podName],
			host.ASConn, r.getClientPolicy())

		if err != nil {
			// Assuming error returned here will not be a server error.
			return common.ReconcileError(err)
		}

		r.Log.Info("Generated dynamic config commands", "commands", fmt.Sprintf("%v", asConfCmds), "pod", podName)

		if succeededCmds, err := deployment.SetConfigCommandsOnHosts(r.Log, r.getClientPolicy(), allHostConns,
			[]*deployment.HostConn{host}, asConfCmds); err != nil {
			errorStatus := asdbv1.Failed

			// if the len of succeededCmds is not 0 along with error, then it is partially failed.
			if len(succeededCmds) != 0 {
				errorStatus = asdbv1.PartiallyFailed
			}

			var patches []jsonpatch.PatchOperation

			patch := jsonpatch.PatchOperation{
				Operation: "replace",
				Path:      "/status/pods/" + podName + "/dynamicConfigUpdateStatus",
				Value:     errorStatus,
			}
			patches = append(patches, patch)

			if patchErr := r.patchPodStatus(
				context.TODO(), patches,
			); patchErr != nil {
				return common.ReconcileError(
					fmt.Errorf("error updating status: %v, dynamic config command error: %v", patchErr, err))
			}

			return common.ReconcileError(err)
		}

		if err := r.updateAerospikeConfInPod(podName); err != nil {
			return common.ReconcileError(err)
		}
	}

	return common.ReconcileSuccess()
}
