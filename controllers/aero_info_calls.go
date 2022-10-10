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
	"strings"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
	corev1 "k8s.io/api/core/v1"
)

//------------------------------------------------------------------------------------
// Aerospike helper
//------------------------------------------------------------------------------------

func (r *SingleClusterReconciler) getAerospikeServerVersionFromPod(pod *corev1.Pod) (
	string, error,
) {
	asConn, err := r.newAsConn(pod)
	if err != nil {
		return "", err
	}

	res, err := asConn.RunInfo(
		r.getClientPolicy(), "build",
	)
	if err != nil {
		return "", err
	}
	version, ok := res["build"]
	if !ok {
		return "", fmt.Errorf(
			"failed to get aerospike version from pod %v", pod.Name,
		)
	}
	return version, nil
}

// waitForMultipleNodesSafeStopReady waits util the input pods are safe to stop, skipping pods that are not running and present in ignorablePods for stability check. The ignorablePods list should be a list of failed or pending pods that are going to be deleted eventually and are safe to ignore in stability checks.
func (r *SingleClusterReconciler) waitForMultipleNodesSafeStopReady(
	pods []*corev1.Pod, ignorablePods []corev1.Pod,
) reconcileResult {
	// TODO: Check post quiesce recluster conditions first.
	// If they pass the node is safe to remove and cluster is stable ignoring migration this node is safe to shut down.

	if len(pods) == 0 {
		return reconcileSuccess()
	}
	// Remove a node only if cluster is stable
	err := r.waitForAllSTSToBeReady()
	if err != nil {
		return reconcileError(
			fmt.Errorf(
				"failed to wait for cluster to be ready: %v", err,
			),
		)
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
	r.Recorder.Eventf(r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete", pods[0].Labels[asdbv1beta1.AerospikeRackIdLabel])

	const maxRetry = 6
	const retryInterval = time.Second * 10

	var isStable bool
	// Wait for migration to finish. Wait for some time...
	for idx := 1; idx <= maxRetry; idx++ {
		r.Log.V(1).Info("Waiting for migrations to be zero before stopping pods", "pods", getPodNames(pods))

		time.Sleep(retryInterval)

		// This should fail if cold start is going on.
		// Info command in cold starting node should give error, is it? confirm
		//.

		isStable, err = deployment.IsClusterAndStable(
			r.Log, r.getClientPolicy(), allHostConns,
		)
		if err != nil {
			return reconcileError(err)
		}
		if isStable {
			break
		}
	}
	// TODO: Requeue after how much time. 1 min for now
	if !isStable {
		return reconcileRequeueAfter(60)
	}

	removedNSes, err := r.removedNamespaces()
	if err != nil {
		return reconcileError(err)
	}

	var selectedHostConns []*deployment.HostConn
	for _, pod := range pods {
		// Quiesce node
		hostConn, err := r.newHostConn(pod)
		if err != nil {
			return reconcileError(
				fmt.Errorf(
					"failed to get hostConn for aerospike cluster nodes %v: %v",
					pod.Name, err,
				),
			)
		}
		selectedHostConns = append(selectedHostConns, hostConn)
	}

	if err := deployment.InfoQuiesce(
		r.Log, r.getClientPolicy(), allHostConns, selectedHostConns, removedNSes,
	); err != nil {
		return reconcileError(err)
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
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(r.Log, host, asConn), nil
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
		Log:               r.Log,
	}

	return asConn, nil
}

// ParseInfoIntoMap parses info string into a map.
// TODO adapted from management lib. Should be made public there.
func ParseInfoIntoMap(
	str string, del string, sep string,
) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	if str == "" {
		return m, nil
	}
	items := strings.Split(str, del)
	for _, item := range items {
		if item == "" {
			continue
		}
		kv := strings.Split(item, sep)
		if len(kv) < 2 {
			return nil, fmt.Errorf("error parsing info item %s", item)
		}

		m[kv[0]] = strings.Join(kv[1:], sep)
	}

	return m, nil
}
