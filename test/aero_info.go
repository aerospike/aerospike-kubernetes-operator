package test

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	goctx "context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	"github.com/aerospike/aerospike-management-lib/info"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

type CloudProvider int

const (
	CloudProviderUnknown CloudProvider = iota
	CloudProviderAWS
	CloudProviderGCP
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
	_ logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, pod *corev1.Pod,
	k8sClient client.Client,
) (*deployment.ASConn, error) {
	// Use the Kubernetes service port and IP since the test might run outside
	//the Kubernetes cluster network.
	var port int32

	tlsName := getServiceTLSName(aeroCluster)

	networkType := asdbv1beta1.AerospikeNetworkType(*defaultNetworkType)
	if aeroCluster.Spec.PodSpec.MultiPodPerHost && networkType != asdbv1beta1.AerospikeNetworkTypePod {
		svc, err := getServiceForPod(pod, k8sClient)
		if err != nil {
			return nil, err
		}
		if tlsName == "" {
			port = svc.Spec.Ports[0].NodePort
		} else {
			for _, portInfo := range svc.Spec.Ports {
				if portInfo.Name == asdbv1beta1.ServiceTLSPortName {
					port = portInfo.NodePort
					break
				}
			}
		}
	} else {
		if tlsName == "" {
			port = int32(
				*asdbv1beta1.GetServicePort(
					aeroCluster.Spec.
						AerospikeConfig,
				),
			)
		} else {
			_, portP := asdbv1beta1.GetServiceTLSNameAndPort(
				aeroCluster.Spec.
					AerospikeConfig,
			)
			port = int32(*portP)
		}
	}

	host, err := getEndpointIP(pod, k8sClient, networkType)

	if err != nil {
		return nil, err
	}

	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
		Log:               logger,
	}

	return asConn, nil
}

func getEndpointIP(
	pod *corev1.Pod, k8sClient client.Client,
	networkType asdbv1beta1.AerospikeNetworkType,
) (string, error) {
	switch networkType {
	case asdbv1beta1.AerospikeNetworkTypePod:
		if pod.Status.PodIP == "" {
			return "", fmt.Errorf(
				"pod ip is not assigned yet for the pod %s", pod.Name,
			)
		}
		return pod.Status.PodIP, nil
	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		if pod.Status.HostIP == "" {
			return "", fmt.Errorf(
				"host ip is not assigned yet for the pod %s", pod.Name,
			)
		}
		return pod.Status.HostIP, nil
	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		k8sNode := &corev1.Node{}
		err := k8sClient.Get(
			goctx.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode,
		)
		if err != nil {
			return "", fmt.Errorf(
				"failed to get k8s node %s for pod %v: %w", pod.Spec.NodeName,
				pod.Name, err,
			)
		}

		// TODO: when refactoring this to use this as main code, this might need to be the
		// internal hostIP instead of the external IP. Tests run outside the k8s cluster so
		// we should to use the external IP if present.

		// If externalIP is present than give external ip
		for _, add := range k8sNode.Status.Addresses {
			if add.Type == corev1.NodeExternalIP && add.Address != "" {
				return add.Address, nil
			}
		}
		return "", fmt.Errorf(
			"failed to find %s address in the node %s for pod %s: nodes addresses are %v",
			networkType, pod.Spec.NodeName, pod.Name, k8sNode.Status.Addresses,
		)
	}
	return "", fmt.Errorf("anknown network type: %s", networkType)
}

func createHost(pod asdbv1beta1.AerospikePodStatus) (*as.Host, error) {
	var host string
	networkType := asdbv1beta1.AerospikeNetworkType(*defaultNetworkType)
	switch networkType {
	case asdbv1beta1.AerospikeNetworkTypePod:
		if pod.PodIP == "" {
			return nil, fmt.Errorf(
				"pod ip is not defined in pod status yet: %+v", pod,
			)
		}
		return &as.Host{
			Name: pod.PodIP, Port: pod.PodPort, TLSName: pod.Aerospike.TLSName,
		}, nil
	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		if pod.HostInternalIP == "" {
			return nil, fmt.Errorf(
				"internal host ip is not defined in pod status yet: %+v", pod,
			)
		}
		host = pod.HostInternalIP
	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		if pod.HostExternalIP == "" {
			return nil, fmt.Errorf(
				"external host ip is not defined in pod status yet: %+v", pod,
			)
		}
		host = pod.HostExternalIP
	default:
		return nil, fmt.Errorf("unknown network type: %s", networkType)
	}
	return &as.Host{
		Name: host, Port: int(pod.ServicePort), TLSName: pod.Aerospike.TLSName,
	}, nil
}

func newHostConn(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, pod *corev1.Pod,
	k8sClient client.Client,
) (*deployment.HostConn, error) {
	asConn, err := newAsConn(log, aeroCluster, pod, k8sClient)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(log, host, asConn), nil
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

func getSTSList(
	aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) (*appsv1.StatefulSetList, error) {
	stsList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(context.TODO(), stsList, listOps); err != nil {
		return nil, err
	}
	return stsList, nil
}

func getNodeList(ctx goctx.Context, k8sClient client.Client) (
	*corev1.NodeList, error,
) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList); err != nil {
		return nil, err
	}
	return nodeList, nil
}

func getZones(ctx goctx.Context, k8sClient client.Client) ([]string, error) {
	unqZones := map[string]int{}
	nodes, err := getNodeList(ctx, k8sClient)
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

func getRegion(ctx goctx.Context, k8sClient client.Client) (string, error) {
	nodes, err := getNodeList(ctx, k8sClient)
	if err != nil {
		return "", err
	}
	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("node list empty: %v", nodes.Items)
	}
	return nodes.Items[0].Labels["failure-domain.beta.kubernetes.io/region"], nil
}

func getCloudProvider(
	ctx goctx.Context, k8sClient client.Client,
) (CloudProvider, error) {
	labelKeys := map[string]struct{}{}
	nodes, err := getNodeList(ctx, k8sClient)
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
		provider := determineByProviderId(&node)
		if provider != CloudProviderUnknown {
			return provider, nil
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

func determineByProviderId(node *corev1.Node) CloudProvider {
	if strings.Contains(node.Spec.ProviderID, "gce") {
		return CloudProviderGCP
	} else if strings.Contains(node.Spec.ProviderID, "aws") {
		return CloudProviderAWS
	}
	// TODO add cloud provider detection for Azure
	return CloudProviderUnknown
}

func newAllHostConn(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster,
	k8sClient client.Client,
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
