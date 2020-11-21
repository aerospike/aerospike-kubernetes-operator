// +build !noac

// Tests Aerospike network policy settings.

package e2e

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/aerospikecluster"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
)

const (
	// Use single node cluster so that developer machine tests run in single pod per k8s node configuration.
	networkTestPolicyClusterSize = 1
)

func TestNetworkPolicy(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Errorf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	t.Run("TLS", func(t *testing.T) {
		t.Run("MultiPodperhost", func(t *testing.T) {
			doTestNetworkPolicy(true, true, ctx, t)
		})

		t.Run("SinglePodperhost", func(t *testing.T) {
			doTestNetworkPolicy(false, true, ctx, t)
		})
	})

	t.Run("NonTLS", func(t *testing.T) {
		t.Run("MultiPodperhost", func(t *testing.T) {
			doTestNetworkPolicy(true, false, ctx, t)
		})

		t.Run("SinglePodperhost", func(t *testing.T) {
			doTestNetworkPolicy(false, false, ctx, t)
		})
	})
}

func doTestNetworkPolicy(multiPodPerHost bool, enableTLS bool, ctx *framework.TestCtx, t *testing.T) {
	var aeroCluster *aerospikev1alpha1.AerospikeCluster = nil

	t.Run("DefaultNetworkPolicy", func(t *testing.T) {
		// Ensures that default network policy is applied.
		defaultNetworkPolicy := aerospikev1alpha1.AerospikeNetworkPolicy{}
		aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(defaultNetworkPolicy, multiPodPerHost, enableTLS, ctx)
		err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}
		validateNetworkPolicy(aeroCluster, t)
	})

	t.Run("PodAndExternal", func(t *testing.T) {
		// Ensures that default network policy is applied.
		networkPolicy := aerospikev1alpha1.AerospikeNetworkPolicy{AccessType: aerospikev1alpha1.AerospikeNetworkTypePod, AlternateAccessType: aerospikev1alpha1.AerospikeNetworkTypeHostExternal, TLSAccessType: aerospikev1alpha1.AerospikeNetworkTypePod, TLSAlternateAccessType: aerospikev1alpha1.AerospikeNetworkTypeHostExternal}
		aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(networkPolicy, multiPodPerHost, enableTLS, ctx)
		err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}
		validateNetworkPolicy(aeroCluster, t)
	})

	if aeroCluster != nil {
		// Cleanup for next run.
		deleteCluster(t, framework.Global, ctx, aeroCluster)
	}
}

// validateNetworkPolicy validates that the new network policy is applied correctly.
func validateNetworkPolicy(desired *aerospikev1alpha1.AerospikeCluster, t *testing.T) {
	client := framework.Global.Client.Client

	current := &aerospikev1alpha1.AerospikeCluster{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		t.Errorf("Error reading cluster spec:%v", err)
		return
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(desired.Spec.AerospikeNetworkPolicy, current.Spec.AerospikeNetworkPolicy) {
		t.Errorf("Cluster state not applied. Desired: %v Current: %v", desired.Spec.AerospikeNetworkPolicy, current.Spec.AerospikeNetworkPolicy)
		return
	}

	podList, err := getPodList(current, &client)
	if err != nil {
		t.Errorf("Failed to list pods: %v", err)
		return
	}

	for _, pod := range podList.Items {
		asConn, err := newAsConn(current, &pod, &client)
		if err != nil {
			t.Errorf("Failed to get aerospike connection: %v", err)
			return
		}

		cp := getClientPolicy(current, &client)
		res, err := deployment.RunInfo(cp, asConn, "endpoints")
		if err != nil {
			t.Errorf("Failed to run Aerospike info command: %v", err)
			return
		}

		endpointsStr, ok := res["endpoints"]
		if !ok {
			t.Errorf("Failed to get aerospike endpoints from pod %v", pod.Name)
			return
		}

		endpointsMap, err := aerospikecluster.ParseInfoIntoMap(endpointsStr, ";", "=")
		if err != nil {
			t.Errorf("Failed to parse aerospike endpoints from pod %v: %v", pod.Name, err)
			return
		}

		networkPolicy := current.Spec.AerospikeNetworkPolicy

		// Validate the returned endpoints.
		validatePodEndpoint(&pod, current, networkPolicy.AccessType, false, aerospikecluster.GetEndpointsFromInfo("access", endpointsMap), t)
		validatePodEndpoint(&pod, current, networkPolicy.AlternateAccessType, false, aerospikecluster.GetEndpointsFromInfo("alternate-access", endpointsMap), t)

		tlsName := getServiceTLSName(current)

		if tlsName != "" {
			validatePodEndpoint(&pod, current, networkPolicy.TLSAccessType, true, aerospikecluster.GetEndpointsFromInfo("tls-access", endpointsMap), t)
			validatePodEndpoint(&pod, current, networkPolicy.TLSAlternateAccessType, true, aerospikecluster.GetEndpointsFromInfo("tls-alternate-access", endpointsMap), t)
		}
	}
}

func validatePodEndpoint(pod *corev1.Pod, aeroCluster *aerospikev1alpha1.AerospikeCluster, networkType aerospikev1alpha1.AerospikeNetworkType, isTLS bool, actual []string, t *testing.T) {
	podIP, hostInternalIP, hostExternalIP, _ := getIPs(pod)
	endpoint := actual[0]
	host, portStr, err := net.SplitHostPort(endpoint)
	t.Logf("For pod:%v for accessType:%v Actual endpoint:%v", pod.Name, networkType, endpoint)

	if err != nil {
		t.Errorf("Invalid endpoint %v", endpoint)
		return
	}

	// Validate the IP address.
	switch networkType {
	case aerospikev1alpha1.AerospikeNetworkTypePod:
		if podIP != host {
			t.Errorf("Expected podIP %v got %v", podIP, host)
			return
		}

	case aerospikev1alpha1.AerospikeNetworkTypeHostInternal:
		if hostInternalIP != host {
			t.Errorf("Expected host internal IP %v got %v", hostInternalIP, host)
			return
		}

	case aerospikev1alpha1.AerospikeNetworkTypeHostExternal:
		if hostExternalIP != host {
			t.Errorf("Expected host external IP %v got %v", hostExternalIP, host)
			return
		}

	default:
		t.Errorf("Unknowk network type %v", networkType)
		return
	}

	// Validate port.
	expectedPort, _ := getExpectedServicePortForPod(aeroCluster, pod, networkType, isTLS)

	if portStr != fmt.Sprintf("%v", expectedPort) {
		t.Errorf("Incorrect port expected: %v actual: %v", expectedPort, portStr)
	}
}

func getExpectedServicePortForPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod, networkType aerospikev1alpha1.AerospikeNetworkType, isTLS bool) (int32, error) {
	var port int32

	if networkType == aerospikev1alpha1.AerospikeNetworkTypePod {
		if !isTLS {
			port = utils.ServicePort
		} else {
			port = utils.ServiceTLSPort
		}
	} else if aeroCluster.Spec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, &framework.Global.Client.Client)
		if err != nil {
			return 0, fmt.Errorf("Error getting service port: %v", err)
		}
		if !isTLS {
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
		if !isTLS {
			port = utils.ServicePort
		} else {
			port = utils.ServiceTLSPort
		}
	}

	return port, nil
}

// getIPs returns the pod IP, host internal IP and the host external IP unless there is an error.
// Note: the IPs returned from here should match the IPs generated in the pod intialization script for the init container.
func getIPs(pod *corev1.Pod) (string, string, string, error) {
	podIP := pod.Status.PodIP
	hostInternalIP := pod.Status.HostIP
	hostExternalIP := hostInternalIP

	k8sNode := &corev1.Node{}
	err := framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
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

// getAerospikeClusterSpecWithNetworkPolicy create a spec with input network policy.
func getAerospikeClusterSpecWithNetworkPolicy(networkPolicy aerospikev1alpha1.AerospikeNetworkPolicy, multiPodPerHost bool, enableTLS bool, ctx *framework.TestCtx) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	kubeNs, _ := ctx.GetNamespace()
	cascadeDelete := true

	var networkConf map[string]interface{} = map[string]interface{}{}

	if enableTLS {
		networkConf = map[string]interface{}{
			"service": map[string]interface{}{
				"tls-name": "bob-cluster-a",
			},
			"tls": []interface{}{
				map[string]interface{}{
					"name":      "bob-cluster-a",
					"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
					"key-file":  "/etc/aerospike/secret/svc_key.pem",
					"ca-file":   "/etc/aerospike/secret/cacert.pem",
				},
			},
		}
	}

	return &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aeroclustertest",
			Namespace: kubeNs,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  networkTestPolicyClusterSize,
			Image: "aerospike/aerospike-server-enterprise:5.0.0.4",
			Storage: aerospikev1alpha1.AerospikeStorageSpec{
				FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aerospike/data",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			AerospikeAccessControl: &aerospikev1alpha1.AerospikeAccessControlSpec{
				Users: []aerospikev1alpha1.AerospikeUserSpec{
					aerospikev1alpha1.AerospikeUserSpec{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			MultiPodPerHost: multiPodPerHost,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: map[string]interface{}{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
					"migrate-threads":  4,
				},

				"network": networkConf,

				"security": map[string]interface{}{"enable-security": true},
				"namespaces": []interface{}{
					map[string]interface{}{
						"name":               "test",
						"replication-factor": networkTestPolicyClusterSize,
						"memory-size":        3000000000,
						"migrate-sleep":      0,
						"storage-engine": map[string]interface{}{
							"files":    []interface{}{"/opt/aerospike/data/test.dat"},
							"filesize": 2000955200,
						},
					},
				},
			},
			AerospikeNetworkPolicy: networkPolicy,
		},
	}
}
