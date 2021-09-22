package test

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	"github.com/aerospike/aerospike-management-lib/info"
	as "github.com/ashishshinde/aerospike-client-go/v5"
)

type CloudProvider string

const (
	CloudProviderUnknown CloudProvider = "Unknown"
	CloudProviderAWS                   = "AWS"
	CloudProviderGCP                   = "GCP"
)

func getServiceForPod(
	pod *corev1.Pod, k8sClient client.Client,
) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := k8sClient.Get(
		context.TODO(),
		types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get service for pod %s: %v", pod.Name, err,
		)
	}
	return service, nil
}

func newAsConn(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, pod *corev1.Pod, k8sClient client.Client,
) (*deployment.ASConn, error) {
	// Use the Kubenetes serice port and IP since the test might run outside the Kubernetes cluster network.
	var port int32

	tlsName := getServiceTLSName(aeroCluster)

	if aeroCluster.Spec.PodSpec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, k8sClient)
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
			port = asdbv1beta1.ServicePort
		} else {
			port = asdbv1beta1.ServiceTLSPort
		}
	}

	host, err := getNodeIP(pod, k8sClient)

	if err != nil {
		return nil, err
	}

	asConn := &deployment.ASConn{
		AerospikeHostName: *host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
		Log:               logger,
	}

	return asConn, nil
}

func getNodeIP(pod *corev1.Pod, k8sClient client.Client) (*string, error) {
	ip := pod.Status.HostIP

	k8sNode := &corev1.Node{}
	err := k8sClient.Get(
		context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName,
			pod.Name, err,
		)
	}

	// TODO: when refactoring this to use this as main code, this might need to be the
	// internal hostIP instead of the external IP. Tests run outside the k8s cluster so
	// we should to use the external IP if present.

	// If externalIP is present than give external ip
	for _, add := range k8sNode.Status.Addresses {
		if add.Type == corev1.NodeExternalIP && add.Address != "" {
			ip = add.Address
		}
	}
	return &ip, nil
}

func newHostConn(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, pod *corev1.Pod, k8sClient client.Client,
) (*deployment.HostConn, error) {
	asConn, err := newAsConn(log, aeroCluster, pod, k8sClient)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(log, host, asConn, nil), nil
}

func getPodList(
	aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func getNodeList(k8sClient client.Client) (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(context.TODO(), nodeList); err != nil {
		return nil, err
	}
	return nodeList, nil
}

func getZones(k8sClient client.Client) ([]string, error) {
	unqZones := map[string]int{}
	nodes, err := getNodeList(k8sClient)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes.Items {
		unqZones[node.Labels["failure-domain.beta.kubernetes.io/zone"]] = 1
	}
	var zones []string
	for zone := range unqZones {
		zones = append(zones, zone)
	}
	return zones, nil
}

func getCloudProvider(k8sClient client.Client) (CloudProvider, error) {
	labelKeys := map[string]struct{}{}
	nodes, err := getNodeList(k8sClient)
	if err != nil {
		return CloudProviderUnknown, err
	}
	for _, node := range nodes.Items {
		for labelKey := range node.Labels {
			if strings.Contains(labelKey, "cloud.google.com") {
				return CloudProviderGCP, nil
			}
			if strings.Contains(labelKey, "eks.amazonaws.com") {
				return CloudProviderAWS, nil
			}
			labelKeys[labelKey] = struct{}{}
		}
	}
	var labelKeysSlice []string
	for labelKey := range labelKeys {
		labelKeysSlice = append(labelKeysSlice, labelKey)
	}
	return CloudProviderUnknown, fmt.Errorf(
		"can't determin cloud platform by node's labels: %v", labelKeysSlice,
	)
}

func newAllHostConn(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) ([]*deployment.HostConn, error) {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("pod list empty")
	}

	var hostConns []*deployment.HostConn
	for _, pod := range podList.Items {
		hostConn, err := newHostConn(log, aeroCluster, &pod, k8sClient)
		if err != nil {
			return nil, err
		}
		hostConns = append(hostConns, hostConn)
	}
	return hostConns, nil
}

func getAeroClusterPVCList(
	aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func getAsConfig(asinfo *info.AsInfo, cmd string) (lib.Stats, error) {
	var confs lib.Stats
	var err error
	for i := 0; i < 10; i++ {
		confs, err = asinfo.GetAsConfig(cmd)
		if err == nil {
			return confs, nil
		}
	}

	return nil, err
}

func runInfo(
	cp *as.ClientPolicy, asConn *deployment.ASConn, cmd string,
) (map[string]string, error) {
	var res map[string]string
	var err error
	for i := 0; i < 10; i++ {
		res, err = asConn.RunInfo(cp, cmd)
		if err == nil {
			return res, nil
		}

		time.Sleep(time.Second)
	}
	return nil, err
}
