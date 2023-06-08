/*
Copyright 2018 The aerospike-operator Authors.
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

package controllers

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

// ------------------------------------------------------------------------------------
// Aerospike helper
// ------------------------------------------------------------------------------------

// waitForMultipleNodesSafeStopReady waits util the input pods is safe to stop,
// skipping pods that are not running and present in ignorablePods for stability check.
// The ignorablePods list should be a list of failed or pending pods that are going to be
// deleted eventually and are safe to ignore in stability checks.
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	pods []*corev1.Pod, ignorablePods []corev1.Pod, setRoster bool,
) reconcileResult {
	if len(pods) == 0 {
		return reconcileSuccess()
	}

	// Remove a node only if cluster is stable
	if err := r.waitForAllSTSToBeReady(); err != nil {
		return reconcileError(fmt.Errorf("failed to wait for cluster to be ready: %v", err))
	}

	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return reconcileError(fmt.Errorf("failed to get hostConn for aerospike cluster nodes: %v", err))
	}

	policy := r.getClientPolicy()

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete", pods[0].Labels[asdbv1.AerospikeRackIDLabel],
	)

	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.isSuccess {
		return res
	}

	if setRoster {
		// Setup roster after migration.
		if err = r.getAndSetRoster(policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePods); err != nil {
			r.Log.Error(err, "Failed to set roster for cluster")
			return reconcileRequeueAfter(1)
		}
	} else {
		if err := r.validateSCClusterState(policy, ignorablePods); err != nil {
			return reconcileError(err)
		}
	}

	if err := r.quiescePods(policy, allHostConns, pods, ignorablePods); err != nil {
		return reconcileError(err)
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) quiescePods(
	policy *as.ClientPolicy, allHostConns []*deployment.HostConn, pods []*corev1.Pod, ignorablePods []corev1.Pod,
) error {
	removedNSes, err := r.removedNamespaces(allHostConns)
	if err != nil {
		return err
	}

	podList := make([]corev1.Pod, 0, len(pods))

	for idx := range pods {
		podList = append(podList, *pods[idx])
	}

	selectedHostConns, err := r.newPodsHostConnWithOption(podList, ignorablePods)
	if err != nil {
		return err
	}

	return deployment.InfoQuiesce(r.Log, policy, allHostConns, selectedHostConns, removedNSes)
}

// TODO: Check only for migration
func (r *SingleClusterReconciler) waitForClusterStability(
	policy *as.ClientPolicy, allHostConns []*deployment.HostConn,
) reconcileResult {
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
			return reconcileError(err)
		}

		if isStable {
			r.Log.V(1).Info("Cluster is now stable")
			break
		}
	}

	if !isStable {
		return reconcileRequeueAfter(60)
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) tipClearHostname(
	pod *corev1.Pod, clearPodName string,
) error {
	asConn := r.newAsConn(pod)

	_, heartbeatTLSPort := asdbv1.GetHeartbeatTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatTLSPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			*heartbeatTLSPort,
		); err != nil {
			return err
		}
	}

	heartbeatPort := asdbv1.GetHeartbeatPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatPort != nil {
		if err := asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			*heartbeatPort,
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

func (r *SingleClusterReconciler) newAllHostConn() (
	[]*deployment.HostConn, error,
) {
	return r.newAllHostConnWithOption(nil)
}

// newAllHostConnWithOption returns connections to all pods in the cluster skipping pods that are not running and
// present in ignorablePods.
func (r *SingleClusterReconciler) newAllHostConnWithOption(ignorablePods []corev1.Pod) (
	[]*deployment.HostConn, error,
) {
	podList, err := r.getClusterPodList()
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("pod list empty")
	}

	return r.newPodsHostConnWithOption(podList.Items, ignorablePods)
}

// newPodsHostConnWithOption returns connections to all pods given skipping pods that are not running and
// present in ignorablePods.
func (r *SingleClusterReconciler) newPodsHostConnWithOption(pods, ignorablePods []corev1.Pod) (
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
			ignorablePod := utils.GetPod(pod.Name, ignorablePods)
			if ignorablePod != nil {
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
	tlsName, port := asdbv1.GetServiceTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if tlsName == "" || port == nil {
		port = asdbv1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig)
	}

	host := pod.Status.PodIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     *port,
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
	asConfig *asdbv1.AerospikeConfigSpec, setToZero bool, ignorablePods []corev1.Pod,
) reconcileResult {
	migrateFillDelay, err := asdbv1.GetMigrateFillDelay(asConfig)
	if err != nil {
		reconcileError(err)
	}

	var oldMigrateFillDelay int

	if len(r.aeroCluster.Status.RackConfig.Racks) > 0 {
		oldMigrateFillDelay, err = asdbv1.GetMigrateFillDelay(&r.aeroCluster.Status.RackConfig.Racks[0].AerospikeConfig)
		if err != nil {
			reconcileError(err)
		}
	}

	if migrateFillDelay == 0 && oldMigrateFillDelay == 0 {
		r.Log.Info("migrate-fill-delay config not present or 0, skipping it")
		return reconcileSuccess()
	}

	// Set migrate-fill-delay to 0 if setToZero flag is set
	if setToZero {
		migrateFillDelay = 0
	}

	// This doesn't make actual connection, only objects having connection info are created
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return reconcileError(
			fmt.Errorf(
				"failed to get hostConn for aerospike cluster nodes: %v", err,
			),
		)
	}

	if err := deployment.SetMigrateFillDelay(r.Log, policy, allHostConns, migrateFillDelay); err != nil {
		return reconcileError(err)
	}

	return reconcileSuccess()
}
