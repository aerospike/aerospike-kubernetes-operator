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
	as "github.com/ashishshinde/aerospike-client-go/v6"
	corev1 "k8s.io/api/core/v1"
)

//------------------------------------------------------------------------------------
// Aerospike helper
//------------------------------------------------------------------------------------

//func (r *SingleClusterReconciler) waitForNodeSafeStopReady(
//	pod *corev1.Pod, ignorablePods []corev1.Pod,
//) reconcileResult {
//	// Skip SC namespace in blocked node
//	// If node not blocked then normal flow
//	// If node blocked then skip SC namespaces
//	// Find non SC namespace, if not found then go out. No need for quiesce
//	r.Log.Info("Do quiesce for node", "node", pod.Name)
//
//	hostConn, err := r.newHostConn(pod)
//	if err != nil {
//		return reconcileError(err)
//	}
//
//	policy := r.getClientPolicy()
//
//	namespaces, err := r.getNamespaces(policy, hostConn)
//	if err != nil {
//		return reconcileError(err)
//	}
//
//	r.Log.Info("Namespace list", "namespaces", namespaces)
//
//	var scNamespaces []string
//	for _, ns := range namespaces {
//		isEnabled, err := r.isNamespaceSCEnabled(policy, hostConn, ns)
//		if err != nil {
//			return reconcileError(err)
//		}
//
//		if isEnabled {
//			scNamespaces = append(scNamespaces, ns)
//		}
//	}
//
//	return r.waitForNodeSafeStopReadyAndQuiesce(policy, pod, ignorablePods, scNamespaces)
//}

//func (r *SingleClusterReconciler) getSCNamespaces(policy *as.ClientPolicy, pod *corev1.Pod) ([]string, error) {
//	hostConn, err := r.newHostConn(pod)
//	if err != nil {
//		return nil, err
//	}
//
//	namespaces, err := r.getNamespaces(policy, hostConn)
//	if err != nil {
//		return nil, err
//	}
//
//	r.Log.Info("Namespace list", "namespaces", namespaces)
//
//	var scNamespaces []string
//	for _, ns := range namespaces {
//		isEnabled, err := r.isNamespaceSCEnabled(policy, hostConn, ns)
//		if err != nil {
//			return nil, err
//		}
//
//		if isEnabled {
//			scNamespaces = append(scNamespaces, ns)
//		}
//	}
//	return scNamespaces, nil
//}

// waitForNodeSafeStopReady waits util the input pod is safe to stop,
// skipping pods that are not running and present in ignorablePods for stability check.
// The ignorablePods list should be a list of failed or pending pods that are going to be
// deleted eventually and are safe to ignore in stability checks.
func (r *SingleClusterReconciler) waitForNodeSafeStopReady(
	pod *corev1.Pod, ignorablePods []corev1.Pod,
) reconcileResult {
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
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "WaitMigration",
		"[rack-%s] Waiting for migrations to complete",
		pod.Labels[asdbv1beta1.AerospikeRackIdLabel],
	)

	policy := r.getClientPolicy()

	// Check for cluster stability
	if res := r.waitForClusterStability(policy, allHostConns); !res.isSuccess {
		return res
	}

	removedNSes, err := r.removedNamespaces()
	if err != nil {
		return reconcileError(err)
	}

	// Quiesce node
	selectedHostConn, err := r.newHostConn(pod)
	if err != nil {
		return reconcileError(
			fmt.Errorf(
				"failed to get hostConn for aerospike cluster nodes %v: %v",
				pod.Name, err,
			),
		)
	}

	//scNamespaces, err := r.getSCNamespaces([]*deployment.HostConn{selectedHostConn}, policy)
	//if err != nil {
	//	return reconcileError(err)
	//}
	//
	//blockedPodList, err := r.podsForRosterBlockList()
	//if err != nil {
	//	return reconcileError(err)
	//}
	//
	//var blockedPodsHostIDs []string
	//for _, pod := range blockedPodList {
	//	blockedPodsHostIDs = append(blockedPodsHostIDs, r.hostIDForPod(&pod))
	//}
	//
	//r.Log.V(1).Info("Doing quiesce", "removedNSes", removedNSes, "rosterBlockedPodsHostIDs", blockedPodsHostIDs, "rosterBlockList", r.aeroCluster.Spec.RosterBlockList, "scNamespaces", scNamespaces)
	//
	r.Log.V(1).Info("Doing quiesce", "removedNSes", removedNSes, "rosterBlockList", r.aeroCluster.Spec.RosterBlockList)

	if err := deployment.InfoQuiesce(
		r.Log, r.getClientPolicy(), allHostConns, []*deployment.HostConn{selectedHostConn}, removedNSes,
	); err != nil {
		return reconcileError(err)
	}

	return reconcileSuccess()
}

func getNodeIDFromPodName(podName string) string {
	// POD_ORDINAL="{name[-1]}"
	// RACK_ID="{name[-2]}"
	// NODE_ID="${RACK_ID}a${POD_ORDINAL}"
	nameSection := strings.Split(podName, "-")
	rackID := nameSection[len(nameSection)-2]
	idx := nameSection[len(nameSection)-1]
	nodeID := "A" + idx
	if rackID != "0" {
		nodeID = rackID + nodeID
	}
	return nodeID
}

func (r *SingleClusterReconciler) podsForRosterBlockList() ([]corev1.Pod, error) {
	podList, err := r.getClusterPodList()
	if err != nil {
		return nil, err
	}

	var blockedPods []corev1.Pod
	for _, pod := range podList.Items {
		nodeID := getNodeIDFromPodName(pod.Name)
		if utils.ContainsString(r.aeroCluster.Spec.RosterBlockList, nodeID) {
			blockedPods = append(blockedPods, pod)
		}
	}
	return blockedPods, nil
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
	//host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	host := hostID(asConn.AerospikeHostName, asConn.AerospikePort)
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

func hostID(hostName string, hostPort int) string {
	return fmt.Sprintf("%s:%d", hostName, hostPort)
}

func (r *SingleClusterReconciler) hostIDForPod(pod *corev1.Pod) string {
	// Use pod IP and direct service port from within the operator for info calls.
	tlsName, port := asdbv1beta1.GetServiceTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if tlsName == "" || port == nil {
		port = asdbv1beta1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig)
	}

	return hostID(pod.Status.PodIP, *port)
}

// ParseInfoIntoMap parses info string into a map.
// TODO adapted from management lib. Should be made public there.
func ParseInfoIntoMap(str string, del string, sep string) (map[string]string, error) {
	m := map[string]string{}
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
