package cluster

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	goctx "context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	"github.com/aerospike/aerospike-management-lib/info"
)

func newAsConn(
	_ logr.Logger, aeroCluster *asdbv1.AerospikeCluster, pod *corev1.Pod,
	k8sClient client.Client,
) (*deployment.ASConn, error) {
	// Use the Kubernetes service port and IP since the test might run outside
	// the Kubernetes cluster network.
	var port int32

	tlsName := getServiceTLSName(aeroCluster)

	networkType := asdbv1.AerospikeNetworkType(*defaultNetworkType)
	if asdbv1.GetBool(aeroCluster.Spec.PodSpec.MultiPodPerHost) && networkType != asdbv1.AerospikeNetworkTypePod &&
		networkType != asdbv1.AerospikeNetworkTypeCustomInterface {
		svc, err := getServiceForPod(pod, k8sClient)
		if err != nil {
			return nil, err
		}

		if tlsName == "" {
			port = svc.Spec.Ports[0].NodePort
		} else {
			for _, portInfo := range svc.Spec.Ports {
				if portInfo.Name == asdbv1.ServiceTLSPortName {
					port = portInfo.NodePort
					break
				}
			}
		}
	} else {
		if tlsName == "" {
			port = *asdbv1.GetServicePort(
				aeroCluster.Spec.
					AerospikeConfig,
			)
		} else {
			_, portP := asdbv1.GetServiceTLSNameAndPort(
				aeroCluster.Spec.
					AerospikeConfig,
			)
			port = *portP
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
	networkType asdbv1.AerospikeNetworkType,
) (string, error) {
	switch networkType {
	case asdbv1.AerospikeNetworkTypePod:
		if pod.Status.PodIP == "" {
			return "", fmt.Errorf(
				"pod ip is not assigned yet for the pod %s", pod.Name,
			)
		}

		return pod.Status.PodIP, nil
	case asdbv1.AerospikeNetworkTypeHostInternal:
		if pod.Status.HostIP == "" {
			return "", fmt.Errorf(
				"host ip is not assigned yet for the pod %s", pod.Name,
			)
		}

		return pod.Status.HostIP, nil
	case asdbv1.AerospikeNetworkTypeHostExternal:
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
	case asdbv1.AerospikeNetworkTypeConfigured:
		// configured IP is a fake IP used for testing, therefor this can not be used to connect
		return "", fmt.Errorf(
			"can not use configured network type: %s", networkType,
		)
	case asdbv1.AerospikeNetworkTypeCustomInterface:
		return "", fmt.Errorf(
			"%s not support yet", networkType,
		)
	case asdbv1.AerospikeNetworkTypeUnspecified:
		return "", fmt.Errorf(
			"unknown network type: %s", networkType,
		)
	}

	return "", fmt.Errorf("unknown network type: %s", networkType)
}

func createHost(pod *asdbv1.AerospikePodStatus, network string) (*as.Host, error) {
	var host string

	networkType := asdbv1.AerospikeNetworkType(*defaultNetworkType)
	switch networkType {
	case asdbv1.AerospikeNetworkTypePod:
		if pod.PodIP == "" {
			return nil, fmt.Errorf(
				"pod ip is not defined in pod status yet: %+v", pod,
			)
		}

		return &as.Host{
			Name: pod.PodIP, Port: pod.PodPort, TLSName: pod.Aerospike.TLSName,
		}, nil
	case asdbv1.AerospikeNetworkTypeHostInternal:
		if pod.HostInternalIP == "" {
			return nil, fmt.Errorf(
				"internal host ip is not defined in pod status yet: %+v", pod,
			)
		}

		host = pod.HostInternalIP
	case asdbv1.AerospikeNetworkTypeHostExternal:
		if pod.HostExternalIP == "" {
			return nil, fmt.Errorf(
				"external host ip is not defined in pod status yet: %+v", pod,
			)
		}

		host = pod.HostExternalIP
	case asdbv1.AerospikeNetworkTypeConfigured:
		// configured IP is a fake IP used for testing, therefor this can not be used to connect
		return nil, fmt.Errorf(
			"can not use configured network type: %s", networkType,
		)
	case asdbv1.AerospikeNetworkTypeCustomInterface:
		return nil, fmt.Errorf(
			"%s not support yet", networkType,
		)
	case asdbv1.AerospikeNetworkTypeUnspecified:
		return nil, fmt.Errorf(
			"unknown network type: %s", networkType,
		)
	default:
		return nil, fmt.Errorf("unknown network type: %s", networkType)
	}

	port := pod.ServicePort
	if network == asdbv1.ConfKeyNetworkAdmin {
		port = pod.ServiceAdminPort
	}

	return &as.Host{
		Name: host, Port: int(port), TLSName: pod.Aerospike.TLSName,
	}, nil
}

func newHostConn(
	log logr.Logger, aeroCluster *asdbv1.AerospikeCluster, pod *corev1.Pod,
	k8sClient client.Client,
) (*deployment.HostConn, error) {
	asConn, err := newAsConn(log, aeroCluster, pod, k8sClient)
	if err != nil {
		return nil, err
	}

	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)

	return deployment.NewHostConn(log, host, asConn), nil
}

func newAllHostConn(
	log logr.Logger, aeroCluster *asdbv1.AerospikeCluster,
	k8sClient client.Client,
) ([]*deployment.HostConn, error) {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("pod list empty")
	}

	hostConns := make([]*deployment.HostConn, 0, len(podList.Items))

	for index := range podList.Items {
		hostConn, err := newHostConn(log, aeroCluster, &podList.Items[index], k8sClient)
		if err != nil {
			return nil, err
		}

		hostConns = append(hostConns, hostConn)
	}

	return hostConns, nil
}

func getAsConfig(asinfo *info.AsInfo, cmd string) (lib.Stats, error) {
	var (
		confs lib.Stats
		err   error
	)

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
	var (
		res map[string]string
		err error
	)

	for i := 0; i < 10; i++ {
		res, err = asConn.RunInfo(cp, cmd)
		if err == nil {
			return res, nil
		}

		time.Sleep(time.Second)
	}

	return nil, err
}
