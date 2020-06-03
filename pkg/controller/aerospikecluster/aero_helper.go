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

package aerospikecluster

import (
	"context"
	"fmt"
	"time"

	log "github.com/inconshreveable/log15"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/citrusleaf/aerospike-management-lib/deployment"
	"github.com/travelaudience/aerospike-operator/pkg/meta"
)

//------------------------------------------------------------------------------------
// Aerospike helper
//------------------------------------------------------------------------------------

func (r *ReconcileAerospikeCluster) getAerospikeServerVersionFromPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod) (string, error) {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return "", err
	}

	res, err := deployment.RunInfo(r.getClientPolicyFromStatus(aeroCluster), asConn, "build")
	if err != nil {
		return "", err
	}
	version, ok := res["build"]
	if !ok {
		return "", fmt.Errorf("Failed to get aerospike version from pod %v", meta.Key(pod))
	}
	return version, nil
}

func (r *ReconcileAerospikeCluster) waitForNodeSafeStopReady(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	allHostConns, err := r.newAllHostConn(aeroCluster)
	if err != nil {
		return fmt.Errorf("Failed to get hostConn for aerospike cluster nodes: %v", err)
	}

	// Wait for migration to finish
	for {
		logger.Info("Waiting for migrations to be zero")
		time.Sleep(time.Second * 2)

		isStable, err := deployment.IsClusterAndStable(r.getClientPolicyFromStatus(aeroCluster), allHostConns)
		if err != nil {
			return err
		}
		if isStable {
			break
		}
	}
	time.Sleep(time.Second * 10)

	// Quiesce node
	selectedHostConn, err := r.newHostConn(aeroCluster, pod)
	if err != nil {
		return fmt.Errorf("Failed to get hostConn for aerospike cluster nodes %v: %v", pod.Name, err)
	}
	if err := deployment.InfoQuiesce(r.getClientPolicyFromStatus(aeroCluster), allHostConns, selectedHostConn); err != nil {
		return err
	}
	return nil
}

func (r *ReconcileAerospikeCluster) tipClearHostname(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod, clearPod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.TipClearHostname(r.getClientPolicyFromStatus(aeroCluster), asConn, hostNameForTip(aeroCluster, clearPod.Name), utils.HeartbeatPort)
}

func (r *ReconcileAerospikeCluster) tipHostname(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod, clearPod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.TipHostname(r.getClientPolicyFromStatus(aeroCluster), asConn, hostNameForTip(aeroCluster, clearPod.Name), utils.HeartbeatPort)
}

func (r *ReconcileAerospikeCluster) alumniReset(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.AlumniReset(r.getClientPolicyFromStatus(aeroCluster), asConn)
}

func (r *ReconcileAerospikeCluster) getAerospikeClusterNodeSummary(aeroCluster *aerospikev1alpha1.AerospikeCluster) ([]aerospikev1alpha1.AerospikeNodeSummary, error) {
	podList, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return nil, fmt.Errorf("Failed to list pods: %v", err)
	}
	var summary []aerospikev1alpha1.AerospikeNodeSummary
	for _, pod := range podList.Items {
		asConn, err := r.newAsConn(aeroCluster, &pod)
		if err != nil {
			return nil, err
		}
		// This func is only called while updating status at the end.
		// Cluster will have updated auth according to spec hence use spec auth info
		cp := r.getClientPolicyFromSpec(aeroCluster)

		res, err := deployment.RunInfo(cp, asConn, "build", "cluster-name", "name")
		if err != nil {
			return nil, err
		}
		build, ok := res["build"]
		if !ok {
			return nil, fmt.Errorf("Failed to get aerospike build from pod %v", pod.Name)
		}
		clName, ok := res["cluster-name"]
		if !ok {
			return nil, fmt.Errorf("Failed to get aerospike cluster-name from pod %v", pod.Name)
		}
		nodeID, ok := res["name"]
		if !ok {
			return nil, fmt.Errorf("Failed to get aerospike nodeId from pod %v", pod.Name)
		}

		ip, err := r.getNodeIP(&pod)
		if err != nil {
			return nil, err
		}
		//return version, nil
		nodeSummary := aerospikev1alpha1.AerospikeNodeSummary{
			IP:          *ip,
			Build:       build,
			ClusterName: clName,
			NodeID:      nodeID,
			Port:        asConn.AerospikePort,
			TLSName:     asConn.AerospikeTLSName,
		}
		summary = append(summary, nodeSummary)
	}
	return summary, nil
}

func (r *ReconcileAerospikeCluster) getNodeIP(pod *corev1.Pod) (*string, error) {
	ip := pod.Status.HostIP

	k8sNode := &corev1.Node{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
	if err != nil {
		return nil, fmt.Errorf("Failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName, pod.Name, err)
	}
	// If externalIP is present than give external ip
	for _, add := range k8sNode.Status.Addresses {
		if add.Type == corev1.NodeExternalIP && add.Address != "" {
			ip = add.Address
		}
	}
	return &ip, nil
}

func (r *ReconcileAerospikeCluster) getServiceForPod(pod *corev1.Pod) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
	if err != nil {
		return nil, fmt.Errorf("Failed to get service for pod %s: %v", pod.Name, err)
	}
	return service, nil
}

func (r *ReconcileAerospikeCluster) newAllHostConn(aeroCluster *aerospikev1alpha1.AerospikeCluster) ([]*deployment.HostConn, error) {
	podList, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("Pod list empty")
	}

	var hostConns []*deployment.HostConn
	for _, pod := range podList.Items {
		hostConn, err := r.newHostConn(aeroCluster, &pod)
		if err != nil {
			return nil, err
		}
		hostConns = append(hostConns, hostConn)
	}
	return hostConns, nil
}

func (r *ReconcileAerospikeCluster) newHostConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod) (*deployment.HostConn, error) {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(host, asConn, nil), nil
}

func getServiceTLSName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	networkConf := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})
	if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
		return tlsName.(string)
	}
	return ""
}

func (r *ReconcileAerospikeCluster) newAsConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod) (*deployment.ASConn, error) {
	var port int32

	tlsName := getServiceTLSName(aeroCluster)

	if aeroCluster.Spec.MultiPodPerHost {
		svc, err := r.getServiceForPod(pod)
		if err != nil {
			return nil, err
		}
		if tlsName == "" {
			port = svc.Spec.Ports[0].NodePort
		} else {
			for _, portInfo := range svc.Spec.Ports {
				if portInfo.Name == "tls" {
					port = portInfo.NodePort
					break
				}
			}
		}
	} else {
		if tlsName == "" {
			port = utils.ServicePort
		} else {
			port = utils.ServiceTlsPort
		}
	}
	host := pod.Status.HostIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
	}

	return asConn, nil
}

func hostNameForTip(aeroCluster *aerospikev1alpha1.AerospikeCluster, host string) string {
	return fmt.Sprintf("%s.%s.%s", host, aeroCluster.Name, aeroCluster.Namespace)
}

// For load balancer service
// func (r *ReconcileAerospikeCluster) getServiceClusterIPForPod(pod *corev1.Pod) (string, error) {
// 	service := &corev1.Service{}
// 	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
// 	if err != nil {
// 		return "", fmt.Errorf("Failed to get service for pod %s: %v", pod.Name, err)
// 	}
// 	// ClusterIP for NodePort service
// 	return service.Spec.ClusterIP, nil

// 	// ClusterIP for LoadBalancer service
// 	// return service.Status.LoadBalancer.Ingress[0].IP, nil
// }
