package pkg

import (
	goctx "context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

type globalAddressesAndPorts struct {
	globalAccessAddress             string
	globalAlternateAccessAddress    string
	globalTLSAccessAddress          string
	globalTLSAlternateAccessAddress string
	globalAccessPort                int32
	globalAlternateAccessPort       int32
	globalTLSAccessPort             int32
	globalTLSAlternateAccessPort    int32
}

type networkInfo struct {
	networkPolicy           asdbv1beta1.AerospikeNetworkPolicy
	hostIP                  string
	podIP                   string
	internalIP              string
	externalIP              string
	globalAddressesAndPorts globalAddressesAndPorts
	fabricPort              int32
	fabricTLSPort           int32
	podPort                 int32
	podTLSPort              int32
	heartBeatPort           int32
	heartBeatTLSPort        int32
	mappedPort              int32
	mappedTLSPort           int32
	multiPodPerHost         bool
	hostNetwork             bool
}

func getNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func getCluster(ctx goctx.Context, k8sClient client.Client,
	clusterNamespacedName types.NamespacedName) (*asdbv1beta1.AerospikeCluster, error) {
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

func getNetworkInfo(k8sClient client.Client, podName string,
	aeroCluster *asdbv1beta1.AerospikeCluster) (*networkInfo, error) {
	networkInfo := &networkInfo{
		multiPodPerHost: aeroCluster.Spec.PodSpec.MultiPodPerHost,
		networkPolicy:   aeroCluster.Spec.AerospikeNetworkPolicy,
		hostNetwork:     aeroCluster.Spec.PodSpec.HostNetwork,
		hostIP:          os.Getenv("MY_HOST_IP"),
		podIP:           os.Getenv("MY_POD_IP"),
	}

	asConfig := aeroCluster.Spec.AerospikeConfig

	if _, serviceTLSPort := asdbv1beta1.GetServiceTLSNameAndPort(asConfig); serviceTLSPort != nil {
		networkInfo.podTLSPort = int32(*serviceTLSPort)
	}

	if servicePort := asdbv1beta1.GetServicePort(asConfig); servicePort != nil {
		networkInfo.podPort = int32(*servicePort)
	}

	if _, hbTLSPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(asConfig); hbTLSPort != nil {
		networkInfo.heartBeatTLSPort = int32(*hbTLSPort)
	}

	if hbPort := asdbv1beta1.GetHeartbeatPort(asConfig); hbPort != nil {
		networkInfo.heartBeatPort = int32(*hbPort)
	}

	if _, fabricTLSPort := asdbv1beta1.GetFabricTLSNameAndPort(asConfig); fabricTLSPort != nil {
		networkInfo.fabricTLSPort = int32(*fabricTLSPort)
	}

	if fabricPort := asdbv1beta1.GetFabricPort(asConfig); fabricPort != nil {
		networkInfo.fabricPort = int32(*fabricPort)
	}

	if err := setHostPortEnv(k8sClient, podName, aeroCluster.Namespace, networkInfo); err != nil {
		return nil, err
	}

	return networkInfo, nil
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

func setHostPortEnv(k8sClient client.Client, podName, namespace string, networkInfo *networkInfo) error {
	// Sets up port related variables.
	infoPort, tlsPort, err := getPorts(goctx.TODO(), k8sClient, namespace, podName)
	if err != nil {
		return err
	}

	networkInfo.internalIP, networkInfo.externalIP, err = getHostIPS(goctx.TODO(), k8sClient, networkInfo.hostIP)
	if err != nil {
		return err
	}

	// Compute the mapped access ports based on config.
	if networkInfo.multiPodPerHost {
		// Use mapped service ports
		networkInfo.mappedPort = infoPort
		networkInfo.mappedTLSPort = tlsPort
	} else {
		// Use the actual ports.
		networkInfo.mappedPort = networkInfo.podPort
		networkInfo.mappedTLSPort = networkInfo.podTLSPort
	}

	return nil
}

// Get tls, info port
func getPorts(ctx goctx.Context, k8sClient client.Client, namespace,
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

// Note: the IPs returned from here should match the IPs used in the node summary.
func getHostIPS(ctx goctx.Context, k8sClient client.Client, hostIP string) (internalIP, externalIP string, err error) {
	internalIP = hostIP
	externalIP = hostIP
	nodeList := &corev1.NodeList{}

	// Get External IP
	if err := k8sClient.List(ctx, nodeList); err != nil {
		return internalIP, externalIP, err
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

			break
		}
	}

	return internalIP, externalIP, nil
}
