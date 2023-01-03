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

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
	corev1 "k8s.io/api/core/v1"
)

//------------------------------------------------------------------------------------
// Aerospike helper
//------------------------------------------------------------------------------------

// waitForMultipleNodesSafeStopReady waits util the input pods is safe to stop,
// skipping pods that are not running and present in ignorablePods for stability check.
// The ignorablePods list should be a list of failed or pending pods that are going to be
// deleted eventually and are safe to ignore in stability checks.
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	pods []*corev1.Pod, ignorablePods []corev1.Pod,
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

	r.Recorder.Eventf(r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete", pods[0].Labels[asdbv1beta1.AerospikeRackIdLabel])

	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.isSuccess {
		return res
	}

	if err := r.validateSCClusterState(policy); err != nil {
		return reconcileError(err)
	}

	if err := r.quiescePods(policy, allHostConns, pods); err != nil {
		return reconcileError(err)
	}
	return reconcileSuccess()
}

func (r *SingleClusterReconciler) quiescePods(policy *as.ClientPolicy, allHostConns []*deployment.HostConn, pods []*corev1.Pod) error {
	removedNSes, err := r.removedNamespaces()
	if err != nil {
		return err
	}

	var selectedHostConns []*deployment.HostConn
	for _, pod := range pods {
		// Quiesce node
		hostConn, err := r.newHostConn(pod)
		if err != nil {
			return fmt.Errorf("failed to get hostConn for aerospike cluster nodes %v: %v",
				pod.Name, err)
		}
		selectedHostConns = append(selectedHostConns, hostConn)
	}

	if err := deployment.InfoQuiesce(
		r.Log, policy, allHostConns, selectedHostConns, removedNSes,
	); err != nil {
		return err
	}

	return nil
}

// TODO: Check only for migration
func (r *SingleClusterReconciler) waitForClusterStability(policy *as.ClientPolicy, allHostConns []*deployment.HostConn) reconcileResult {
	const maxRetry = 6
	const retryInterval = time.Second * 10

	var isStable bool
	var err error
	// Wait for migration to finish. Wait for some time...
	for idx := 1; idx <= maxRetry; idx++ {
		r.Log.V(1).Info("Waiting for migrations to be zero")
		time.Sleep(retryInterval)

		// This should fail if coldstart is going on.
		// Info command in coldstarting node should give error, is it? confirm.

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
	asConn, err := r.newAsConn(pod)
	if err != nil {
		return err
	}

	_, heartbeatTlsPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatTlsPort != nil {
		if err = asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			*heartbeatTlsPort,
		); err != nil {
			return err
		}
	}

	heartbeatPort := asdbv1beta1.GetHeartbeatPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatPort != nil {
		if err = asConn.TipClearHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPodName),
			*heartbeatPort,
		); err != nil {
			return err
		}
	}

	return nil
}

// nolint:unused
func (r *SingleClusterReconciler) tipHostname(
	pod *corev1.Pod, clearPod *corev1.Pod,
) error {
	asConn, err := r.newAsConn(pod)
	if err != nil {
		return err
	}

	_, heartbeatTlsPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatTlsPort != nil {
		if err = asConn.TipHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPod.Name),
			*heartbeatTlsPort,
		); err != nil {
			return err
		}
	}

	heartbeatPort := asdbv1beta1.GetHeartbeatPort(r.aeroCluster.Spec.AerospikeConfig)
	if heartbeatPort != nil {
		if err = asConn.TipHostname(
			r.getClientPolicy(), getFQDNForPod(r.aeroCluster, clearPod.Name),
			*heartbeatPort,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) alumniReset(pod *corev1.Pod) error {
	asConn, err := r.newAsConn(pod)
	if err != nil {
		return err
	}
	return asConn.AlumniReset(r.getClientPolicy())
}

func (r *SingleClusterReconciler) newAllHostConn() (
	[]*deployment.HostConn, error,
) {
	return r.newAllHostConnWithOption(nil)
}

// newAllHostConnWithOption returns connections to all pods in the cluster skipping pods that are not running and present in ignorablePods.
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

	var hostConns []*deployment.HostConn
	for _, pod := range podList.Items {
		if utils.IsPodTerminating(&pod) {
			continue
		}

		// Checking if all the container in the pod are ready or not
		if !utils.IsPodRunningAndReady(&pod) {
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

		hostConn, err := r.newHostConn(&pod)
		if err != nil {
			return nil, err
		}
		hostConns = append(hostConns, hostConn)
	}

	return hostConns, nil
}

func (r *SingleClusterReconciler) newHostConn(pod *corev1.Pod) (
	*deployment.HostConn, error,
) {
	asConn, err := r.newAsConn(pod)
	if err != nil {
		return nil, err
	}
	host := hostID(asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(asConn.Log, host, asConn), nil
}

func (r *SingleClusterReconciler) newAsConn(pod *corev1.Pod) (
	*deployment.ASConn, error,
) {
	// Use pod IP and direct service port from within the operator for info calls.
	tlsName, port := asdbv1beta1.GetServiceTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if tlsName == "" || port == nil {
		port = asdbv1beta1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig)
	}

	host := pod.Status.PodIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     *port,
		AerospikeTLSName:  tlsName,
		Log:               r.Log.WithValues("host", pod.Name),
	}

	return asConn, nil
}

func hostID(hostName string, hostPort int) string {
	return fmt.Sprintf("%s:%d", hostName, hostPort)
}
