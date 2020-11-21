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
	"net"
	"strconv"
	"strings"
	"time"

	log "github.com/inconshreveable/log15"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
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

	res, err := deployment.RunInfo(r.getClientPolicy(aeroCluster), asConn, "build")
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

		isStable, err := deployment.IsClusterAndStable(r.getClientPolicy(aeroCluster), allHostConns)
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
	if err := deployment.InfoQuiesce(r.getClientPolicy(aeroCluster), allHostConns, selectedHostConn); err != nil {
		return err
	}
	return nil
}

func (r *ReconcileAerospikeCluster) tipClearHostname(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod, clearPod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.TipClearHostname(r.getClientPolicy(aeroCluster), asConn, getFQDNForPod(aeroCluster, clearPod.Name), utils.HeartbeatPort)
}

func (r *ReconcileAerospikeCluster) tipHostname(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod, clearPod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.TipHostname(r.getClientPolicy(aeroCluster), asConn, getFQDNForPod(aeroCluster, clearPod.Name), utils.HeartbeatPort)
}

func (r *ReconcileAerospikeCluster) alumniReset(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *v1.Pod) error {
	asConn, err := r.newAsConn(aeroCluster, pod)
	if err != nil {
		return err
	}
	return deployment.AlumniReset(r.getClientPolicy(aeroCluster), asConn)
}

func getRackIDFromPodName(podName string) (*int, error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return nil, fmt.Errorf("Failed to get rackID from podName %s", podName)
	}
	rackStr := parts[len(parts)-2]
	rackID, err := strconv.Atoi(rackStr)
	if err != nil {
		return nil, err
	}
	return &rackID, nil
}

// getIPs returns the pod IP, host internal IP and the host external IP unless there is an error.
// Note: the IPs returned from here should match the IPs generated in the pod intialization script for the init container.
func (r *ReconcileAerospikeCluster) getIPs(pod *corev1.Pod) (string, string, string, error) {
	podIP := pod.Status.PodIP
	hostInternalIP := pod.Status.HostIP
	hostExternalIP := hostInternalIP

	k8sNode := &corev1.Node{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
	if err != nil {
		return "", "", "", fmt.Errorf("Failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName, pod.Name, err)
	}
	// If externalIP is present than give external ip
	for _, add := range k8sNode.Status.Addresses {
		if add.Type == corev1.NodeExternalIP && add.Address != "" {
			hostExternalIP = add.Address
		} else if add.Type == corev1.NodeInternalIP && add.Address != "" {
			hostInternalIP = add.Address
		}
	}

	return podIP, hostInternalIP, hostExternalIP, nil
}

func (r *ReconcileAerospikeCluster) getServiceForPod(pod *corev1.Pod) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
	if err != nil {
		return nil, fmt.Errorf("Failed to get service for pod %s: %v", pod.Name, err)
	}
	return service, nil
}

func (r *ReconcileAerospikeCluster) getServicePortForPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod) (int32, error) {
	var port int32
	tlsName := getServiceTLSName(aeroCluster)

	if aeroCluster.Spec.MultiPodPerHost {
		svc, err := r.getServiceForPod(pod)
		if err != nil {
			return 0, fmt.Errorf("Error getting service port: %v", err)
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
			port = utils.ServiceTLSPort
		}
	}

	return port, nil
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
		if utils.IsTerminating(&pod) {
			continue
		}
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

func (r *ReconcileAerospikeCluster) newAsConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod) (*deployment.ASConn, error) {
	// Use pod IP and direct service port from within the operator for info calls.
	var port int32

	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		port = utils.ServicePort
	} else {
		port = utils.ServiceTLSPort
	}

	host := pod.Status.PodIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
	}

	return asConn, nil
}

func getServiceTLSName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	if networkConfTmp, ok := aeroCluster.Spec.AerospikeConfig["network"]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
			return tlsName.(string)
		}
	}
	return ""
}

func getFQDNForPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, host string) string {
	return fmt.Sprintf("%s.%s.%s", host, aeroCluster.Name, aeroCluster.Namespace)
}

// getEndpointsFromInfo returns the aerospike service endpoints as a slice of host:port elements named addressName from the info endpointsMap. It returns an empty slice if the access address with addressName is not found in endpointsMap.
//
// E.g. addressName are access, alternate-access
func getEndpointsFromInfo(addressName string, endpointsMap map[string]interface{}) []string {
	endpoints := []string{}

	portStr, ok := endpointsMap["service."+addressName+"-port"]
	if !ok {
		return endpoints
	}

	port, err := strconv.ParseInt(fmt.Sprintf("%v", portStr), 10, 32)

	if err != nil || port == 0 {
		return endpoints
	}

	hostsStr, ok := endpointsMap["service."+addressName+"-addresses"]
	if !ok {
		return endpoints
	}

	hosts := strings.Split(fmt.Sprintf("%v", hostsStr), ",")

	for _, host := range hosts {
		endpoints = append(endpoints, net.JoinHostPort(host, strconv.Itoa(int(port))))
	}
	return endpoints
}

// parseInfoIntoMap parses info string into a map.
// TODO adapted from management lib. Should be made public there.
func parseInfoIntoMap(str string, del string, sep string) (map[string]interface{}, error) {
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
			return nil, fmt.Errorf("Error parsing info item %s", item)
		}

		m[kv[0]] = strings.Join(kv[1:len(kv)], sep)
	}

	return m, nil
}
