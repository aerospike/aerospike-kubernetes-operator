package pkg

import (
	goctx "context"
	"os"

	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
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

type InitParams struct {
	k8sClient               client.Client
	aeroCluster             *asdbv1beta1.AerospikeCluster
	podName                 string
	namespace               string
	hostIP                  string
	internalIP              string
	externalIP              string
	rackID                  string
	nodeID                  string
	globalAddressesAndPorts globalAddressesAndPorts
	initTemplateInput       initializeTemplateInput
	mappedPort              int32
	mappedTLSPort           int32
}

func PopulateInitParams() (*InitParams, error) {
	var (
		k8sClient client.Client
		cfg       = ctrl.GetConfigOrDie()
	)

	if err := clientgoscheme.AddToScheme(clientgoscheme.Scheme); err != nil {
		return nil, err
	}

	if err := asdbv1beta1.AddToScheme(clientgoscheme.Scheme); err != nil {
		return nil, err
	}

	var err error

	if k8sClient, err = client.New(
		cfg, client.Options{Scheme: clientgoscheme.Scheme},
	); err != nil {
		return nil, err
	}

	podName := os.Getenv("MY_POD_NAME")
	namespace := os.Getenv("MY_POD_NAMESPACE")
	hostIP := os.Getenv("MY_HOST_IP")
	clusterName := getClusterName(podName)
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	aeroCluster, err := getCluster(goctx.TODO(), k8sClient, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return nil, err
	}

	initTemplateInput := getBaseConfData(aeroCluster, rack)

	rackID, nodeID, err := GetRackIDNodeIDFromPodName(podName)
	if err != nil {
		return nil, err
	}

	initp := InitParams{
		aeroCluster:       aeroCluster,
		k8sClient:         k8sClient,
		podName:           podName,
		namespace:         namespace,
		hostIP:            hostIP,
		rackID:            rackID,
		nodeID:            nodeID,
		initTemplateInput: initTemplateInput,
	}
	if err := setHostPortEnv(&initp); err != nil {
		return nil, err
	}

	return &initp, nil
}

func setHostPortEnv(initp *InitParams) error {
	infoPort, tlsPort, err := getPortString(goctx.TODO(), initp.k8sClient, initp.namespace, initp.podName)
	if err != nil {
		return err
	}

	initp.internalIP, initp.externalIP, err = getHostIPS(goctx.TODO(), initp.k8sClient, initp.hostIP)
	if err != nil {
		return err
	}

	if initp.initTemplateInput.MultiPodPerHost {
		// Use mapped service ports
		initp.mappedPort = infoPort
		initp.mappedTLSPort = tlsPort
	} else {
		// Use the actual ports.
		initp.mappedPort = initp.initTemplateInput.PodPort
		initp.mappedTLSPort = initp.initTemplateInput.PodTLSPort
	}

	return nil
}

func getPortString(ctx goctx.Context, k8sClient client.Client, namespace,
	podName string) (infoport, tlsport int32, err error) {
	serviceList := &corev1.ServiceList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err = k8sClient.List(ctx, serviceList, listOps)
	if err != nil {
		return infoport, tlsport, err
	}

	for idx := range serviceList.Items {
		service := &serviceList.Items[idx]
		if service.Name == podName {
			for _, port := range service.Spec.Ports {
				switch port.Name {
				case "service":
					infoport = port.NodePort
				case "tls-service":
					tlsport = port.NodePort
				}
			}

			break
		}
	}

	return infoport, tlsport, err
}

func getHostIPS(ctx goctx.Context, k8sClient client.Client, hostIP string) (internalIP, externalIP string, err error) {
	internalIP = hostIP
	externalIP = hostIP
	nodeList := &corev1.NodeList{}

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

			if add.Type == "InternalIP" {
				nodeInternalIP = add.Address
			} else if add.Type == "ExternalIP" {
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
