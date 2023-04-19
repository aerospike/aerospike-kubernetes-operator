package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	netattach "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

type globalAddressesAndPorts struct {
	globalAccessAddress             []string
	globalAlternateAccessAddress    []string
	globalTLSAccessAddress          []string
	globalTLSAlternateAccessAddress []string
	globalAccessPort                int32
	globalAlternateAccessPort       int32
	globalTLSAccessPort             int32
	globalTLSAlternateAccessPort    int32
}

type networkInfo struct {
	networkPolicy                      asdbv1beta1.AerospikeNetworkPolicy
	hostIP                             string
	podIP                              string
	internalIP                         string
	externalIP                         string
	configureAccessIP                  string
	configuredAlterAccessIP            string
	customAccessNetworkIPs             []string
	customTLSAccessNetworkIPs          []string
	customAlternateAccessNetworkIPs    []string
	customTLSAlternateAccessNetworkIPs []string
	customFabricNetworkIPs             []string
	customTLSFabricNetworkIPs          []string
	globalAddressesAndPorts            globalAddressesAndPorts
	fabricPort                         int32
	fabricTLSPort                      int32
	podPort                            int32
	podTLSPort                         int32
	heartBeatPort                      int32
	heartBeatTLSPort                   int32
	mappedPort                         int32
	mappedTLSPort                      int32
	multiPodPerHost                    bool
	hostNetwork                        bool
}

const (
	configuredAccessIPLabel          = "aerospike.com/configured-access-address"
	configuredAlternateAccessIPLabel = "aerospike.com/configured-alternate-access-address"
	networkStatusAnnotation          = "k8s.v1.cni.cncf.io/network-status"
)

func getNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func getCluster(ctx context.Context, k8sClient client.Client,
	clusterNamespacedName types.NamespacedName) (*asdbv1beta1.AerospikeCluster, error) {
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

func (initp *InitParams) setNetworkInfo(ctx context.Context) error {
	initp.logger.Info("Gathering network related info")

	initp.networkInfo = &networkInfo{
		multiPodPerHost: initp.aeroCluster.Spec.PodSpec.MultiPodPerHost,
		networkPolicy:   initp.aeroCluster.Spec.AerospikeNetworkPolicy,
		hostNetwork:     initp.aeroCluster.Spec.PodSpec.HostNetwork,
		hostIP:          os.Getenv("MY_HOST_IP"),
		podIP:           os.Getenv("MY_POD_IP"),
		internalIP:      os.Getenv("MY_HOST_IP"),
	}

	asConfig := initp.aeroCluster.Spec.AerospikeConfig

	if _, serviceTLSPort := asdbv1beta1.GetServiceTLSNameAndPort(asConfig); serviceTLSPort != nil {
		initp.networkInfo.podTLSPort = int32(*serviceTLSPort)
	}

	if servicePort := asdbv1beta1.GetServicePort(asConfig); servicePort != nil {
		initp.networkInfo.podPort = int32(*servicePort)
	}

	if _, hbTLSPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(asConfig); hbTLSPort != nil {
		initp.networkInfo.heartBeatTLSPort = int32(*hbTLSPort)
	}

	if hbPort := asdbv1beta1.GetHeartbeatPort(asConfig); hbPort != nil {
		initp.networkInfo.heartBeatPort = int32(*hbPort)
	}

	if _, fabricTLSPort := asdbv1beta1.GetFabricTLSNameAndPort(asConfig); fabricTLSPort != nil {
		initp.networkInfo.fabricTLSPort = int32(*fabricTLSPort)
	}

	if fabricPort := asdbv1beta1.GetFabricPort(asConfig); fabricPort != nil {
		initp.networkInfo.fabricPort = int32(*fabricPort)
	}

	if err := initp.setIPAndPorts(ctx); err != nil {
		return err
	}

	initp.logger.Info("Gathered network related info")

	return nil
}

func getNodeIDFromPodName(podName string) (nodeID string, err error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return "", fmt.Errorf("failed to get nodeID from podName %s", podName)
	}
	// Podname format stsname-ordinal
	// stsname ==> clustername-rackid
	nodeID = parts[len(parts)-2] + "a" + parts[len(parts)-1]

	return nodeID, nil
}

func getRack(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster) (*asdbv1beta1.Rack, error) {
	res := strings.Split(podName, "-")

	//  Assuming podName format stsName-rackID-index
	rackID, err := strconv.Atoi(res[len(res)-2])
	if err != nil {
		return nil, err
	}

	logger.Info("Checking for rack in rackConfig", "rack-id", rackID)

	racks := aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		rack := &racks[idx]
		if rack.ID == rackID {
			return rack, nil
		}
	}

	return nil, fmt.Errorf("rack with rack-id %d not found", rackID)
}

func (initp *InitParams) makeWorkDir() error {
	if initp.workDir != "" {
		defaultWorkDir := filepath.Join("workdir", "filesystem-volumes", initp.workDir)

		requiredDirs := [3]string{"smd", "usr/udf/lua", "xdr"}
		for _, d := range requiredDirs {
			toCreate := filepath.Join(defaultWorkDir, d)
			initp.logger.Info("Creating directory", "dir", toCreate)

			if err := os.MkdirAll(toCreate, 0755); err != nil { //nolint:gocritic // file permission
				return err
			}
		}
	}

	return nil
}

func (initp *InitParams) setIPAndPorts(ctx context.Context) (err error) {
	netInfo := initp.networkInfo

	// Sets up port related variables.
	// User service ports only when MultiPodPerHost is true and node network is defined in NetworkPolicy
	if initp.aeroCluster.Spec.PodSpec.MultiPodPerHost && initp.isNodeNetwork() {
		if netInfo.mappedPort, netInfo.mappedTLSPort, err = getPorts(
			ctx, initp.k8sClient, initp.aeroCluster.Namespace, initp.podName); err != nil {
			return err
		}
	} else {
		// Use the actual ports.
		netInfo.mappedPort = netInfo.podPort
		netInfo.mappedTLSPort = netInfo.podTLSPort
	}

	if initp.isNodeNetwork() {
		if netInfo.internalIP, netInfo.externalIP, netInfo.configureAccessIP,
			netInfo.configuredAlterAccessIP, err = getHostIPS(ctx, initp.k8sClient, netInfo.hostIP); err != nil {
			return err
		}
	}

	pod := &corev1.Pod{}

	err = initp.k8sClient.Get(ctx, types.NamespacedName{
		Name:      initp.podName,
		Namespace: initp.namespace,
	}, pod)
	if err != nil {
		return err
	}

	initp.logger.Info("Gathering custom Interface related info if given")

	// populate custom interface IPs in case of customInterface network type
	if netInfo.customAccessNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.AccessType, pod.Annotations,
		netInfo.networkPolicy.CustomAccessNetworkNames); err != nil {
		return err
	}

	if netInfo.customTLSAccessNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.TLSAccessType, pod.Annotations,
		netInfo.networkPolicy.CustomTLSAccessNetworkNames); err != nil {
		return err
	}

	if netInfo.customAlternateAccessNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.AlternateAccessType,
		pod.Annotations, netInfo.networkPolicy.CustomAlternateAccessNetworkNames); err != nil {
		return err
	}

	if netInfo.customTLSAlternateAccessNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.TLSAlternateAccessType,
		pod.Annotations, netInfo.networkPolicy.CustomTLSAlternateAccessNetworkNames); err != nil {
		return err
	}

	if netInfo.customFabricNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.FabricType, pod.Annotations,
		netInfo.networkPolicy.CustomFabricNetworkNames); err != nil {
		return err
	}

	if netInfo.customTLSFabricNetworkIPs, err = parseCustomNetworkIP(netInfo.networkPolicy.TLSFabricType, pod.Annotations,
		netInfo.networkPolicy.CustomTLSFabricNetworkNames); err != nil {
		return err
	}

	initp.logger.Info("Gathered custom Interface related info")

	return nil
}

// Get tls, info port
func getPorts(ctx context.Context, k8sClient client.Client, namespace,
	podName string) (infoPort, tlsPort int32, err error) {
	serviceList := &corev1.ServiceList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err = k8sClient.List(ctx, serviceList, listOps)
	if err != nil {
		return infoPort, tlsPort, err
	}

	for idx := range serviceList.Items {
		service := &serviceList.Items[idx]
		if service.Name == podName {
			for _, port := range service.Spec.Ports {
				switch port.Name {
				case "service":
					infoPort = port.NodePort
				case "tls-service":
					tlsPort = port.NodePort
				}
			}

			break
		}
	}

	return infoPort, tlsPort, err
}

func (initp *InitParams) isNodeNetwork() bool {
	networkSet := sets.NewString(
		string(asdbv1beta1.AerospikeNetworkTypePod),
		string(asdbv1beta1.AerospikeNetworkTypeCustomInterface),
		string(initp.aeroCluster.Spec.AerospikeNetworkPolicy.AccessType),
		string(initp.aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType),
		string(initp.aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType),
		string(initp.aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType),
	)

	// If len of set is more than 2, it means network type different from "pod" and  "customInterface" are present.
	return networkSet.Len() > 2
}

// Note: the IPs returned from here should match the IPs used in the node summary.
func getHostIPS(ctx context.Context, k8sClient client.Client, hostIP string) (
	internalIP, externalIP, configuredAccessIP, configuredAlternateAccessIP string, err error) {
	internalIP = hostIP
	externalIP = hostIP
	nodeList := &corev1.NodeList{}

	// Get External IP
	if err := k8sClient.List(ctx, nodeList); err != nil {
		return internalIP, externalIP, configuredAccessIP, configuredAlternateAccessIP, err
	}

	for idx := range nodeList.Items {
		node := &nodeList.Items[idx]
		nodeInternalIP := ""
		nodeExternalIP := ""
		matchFound := false

		for _, add := range node.Status.Addresses {
			if add.Address == hostIP {
				matchFound = true
			}

			if add.Type == corev1.NodeInternalIP {
				nodeInternalIP = add.Address
			} else if add.Type == corev1.NodeExternalIP {
				nodeExternalIP = add.Address
			}
		}

		if matchFound {
			if nodeInternalIP != "" {
				internalIP = nodeInternalIP
			}

			if nodeExternalIP != "" {
				externalIP = nodeExternalIP
			}

			if ip, exists := node.Labels[configuredAccessIPLabel]; exists {
				configuredAccessIP = ip
			}

			if ip, exists := node.Labels[configuredAlternateAccessIPLabel]; exists {
				configuredAlternateAccessIP = ip
			}

			break
		}
	}

	return internalIP, externalIP, configuredAccessIP, configuredAlternateAccessIP, nil
}

// parseCustomNetworkIP function parses the network IPs for the given list of network names
// It parses network status info from pod annotations key `k8s.v1.cni.cncf.io/network-status` which is added by CNI
func parseCustomNetworkIP(networkType asdbv1beta1.AerospikeNetworkType,
	annotations map[string]string, networks []string,
) ([]string, error) {
	if networkType != asdbv1beta1.AerospikeNetworkTypeCustomInterface {
		return nil, nil
	}

	if _, exists := annotations[networkStatusAnnotation]; !exists {
		return nil, fmt.Errorf("required pod network status annotation key %s is missing", networkStatusAnnotation)
	}

	var (
		networkIPs  []string
		netStatuses []netattach.NetworkStatus
	)

	if err := json.Unmarshal([]byte(annotations[networkStatusAnnotation]), &netStatuses); err != nil {
		return nil, fmt.Errorf("%s json unmarshal failed, error: %s", networkStatusAnnotation, err.Error())
	}

	networkSet := sets.NewString(networks...)

	for idx := range netStatuses {
		net := &netStatuses[idx]
		if networkSet.Has(net.Name) {
			if len(net.IPs) == 0 {
				return networkIPs, fmt.Errorf("ips list empty for network %s in pod annotations key %s",
					net.Name, networkStatusAnnotation)
			}

			networkIPs = append(networkIPs, net.IPs...)
		}
	}

	if len(networkIPs) == 0 {
		return networkIPs, fmt.Errorf("networks %+v not found in pod annotations key %s",
			networks, networkStatusAnnotation)
	}

	return networkIPs, nil
}
