// +build !noac

// Tests Aerospike network policy settings.

package test

import (
	"context"
	goctx "context"
	"fmt"
	"net"
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/aerospikecluster"
)

const (
	// Use single node cluster so that developer machine tests run in single pod per k8s node configuration.
	networkTestPolicyClusterSize = 1
)

var _ = Describe("TestNetworkPolicy", func() {
	ctx := goctx.TODO()

	Context("TLS", func() {
		Context("MultiPodperhost", func() {
			doTestNetworkPolicy(true, true, ctx)
		})

		Context("SinglePodperhost", func() {
			doTestNetworkPolicy(false, true, ctx)
		})
	})

	Context("NonTLS", func() {
		Context("MultiPodperhost", func() {
			doTestNetworkPolicy(true, false, ctx)
		})

		Context("SinglePodperhost", func() {
			doTestNetworkPolicy(false, false, ctx)
		})
	})
})

func doTestNetworkPolicy(multiPodPerHost bool, enableTLS bool, ctx goctx.Context) {
	var aeroCluster *asdbv1alpha1.AerospikeCluster

	It("DefaultNetworkPolicy", func() {
		// Ensures that default network policy is applied.
		defaultNetworkPolicy := asdbv1alpha1.AerospikeNetworkPolicy{}
		aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(defaultNetworkPolicy, multiPodPerHost, enableTLS, ctx)
		err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
		Expect(err).ToNot(HaveOccurred())

		err = validateNetworkPolicy(aeroCluster)
		Expect(err).ToNot(HaveOccurred())

	})

	It("PodAndExternal", func() {
		// Ensures that default network policy is applied.
		networkPolicy := asdbv1alpha1.AerospikeNetworkPolicy{AccessType: asdbv1alpha1.AerospikeNetworkTypePod, AlternateAccessType: asdbv1alpha1.AerospikeNetworkTypeHostExternal, TLSAccessType: asdbv1alpha1.AerospikeNetworkTypePod, TLSAlternateAccessType: asdbv1alpha1.AerospikeNetworkTypeHostExternal}
		aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(networkPolicy, multiPodPerHost, enableTLS, ctx)
		err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
		Expect(err).ToNot(HaveOccurred())

		err = validateNetworkPolicy(aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})
	It("cleanup", func() {
		// Cleanup for next run.
		deleteCluster(k8sClient, ctx, aeroCluster)
	})
}

// validateNetworkPolicy validates that the new network policy is applied correctly.
func validateNetworkPolicy(desired *asdbv1alpha1.AerospikeCluster) error {
	current := &asdbv1alpha1.AerospikeCluster{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		return fmt.Errorf("Error reading cluster spec:%v", err)
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(desired.Spec.AerospikeNetworkPolicy, current.Spec.AerospikeNetworkPolicy) {
		return fmt.Errorf("Cluster state not applied. Desired: %v Current: %v", desired.Spec.AerospikeNetworkPolicy, current.Spec.AerospikeNetworkPolicy)
	}

	podList, err := getPodList(current, k8sClient)
	if err != nil {
		return fmt.Errorf("Failed to list pods: %v", err)
	}

	for _, pod := range podList.Items {
		asConn, err := newAsConn(current, &pod, k8sClient)
		if err != nil {
			return fmt.Errorf("Failed to get aerospike connection: %v", err)
		}

		cp := getClientPolicy(current, k8sClient)
		res, err := runInfo(cp, asConn, "endpoints")

		if err != nil {
			return fmt.Errorf("Failed to run Aerospike info command: %v", err)
		}

		endpointsStr, ok := res["endpoints"]
		if !ok {
			return fmt.Errorf("Failed to get aerospike endpoints from pod %v", pod.Name)
		}

		endpointsMap, err := aerospikecluster.ParseInfoIntoMap(endpointsStr, ";", "=")
		if err != nil {
			return fmt.Errorf("Failed to parse aerospike endpoints from pod %v: %v", pod.Name, err)
		}

		networkPolicy := current.Spec.AerospikeNetworkPolicy

		// Validate the returned endpoints.
		err = validatePodEndpoint(&pod, current, networkPolicy.AccessType, false, aerospikecluster.GetEndpointsFromInfo("access", endpointsMap))
		if err != nil {
			return err
		}

		err = validatePodEndpoint(&pod, current, networkPolicy.AlternateAccessType, false, aerospikecluster.GetEndpointsFromInfo("alternate-access", endpointsMap))
		if err != nil {
			return err
		}

		tlsName := getServiceTLSName(current)

		if tlsName != "" {
			err = validatePodEndpoint(&pod, current, networkPolicy.TLSAccessType, true, aerospikecluster.GetEndpointsFromInfo("tls-access", endpointsMap))
			if err != nil {
				return err
			}

			err = validatePodEndpoint(&pod, current, networkPolicy.TLSAlternateAccessType, true, aerospikecluster.GetEndpointsFromInfo("tls-alternate-access", endpointsMap))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePodEndpoint(pod *corev1.Pod, aeroCluster *asdbv1alpha1.AerospikeCluster, networkType asdbv1alpha1.AerospikeNetworkType, isTLS bool, actual []string) error {
	podIP, hostInternalIP, hostExternalIP, _ := getIPs(pod)
	endpoint := actual[0]
	host, portStr, err := net.SplitHostPort(endpoint)
	// t.Logf("For pod:%v for accessType:%v Actual endpoint:%v", pod.Name, networkType, endpoint)

	if err != nil {
		return fmt.Errorf("Invalid endpoint %v", endpoint)
	}

	// Validate the IP address.
	switch networkType {
	case asdbv1alpha1.AerospikeNetworkTypePod:
		if podIP != host {
			return fmt.Errorf("Expected podIP %v got %v", podIP, host)
		}

	case asdbv1alpha1.AerospikeNetworkTypeHostInternal:
		if hostInternalIP != host {
			return fmt.Errorf("Expected host internal IP %v got %v", hostInternalIP, host)
		}

	case asdbv1alpha1.AerospikeNetworkTypeHostExternal:
		if hostExternalIP != host {
			return fmt.Errorf("Expected host external IP %v got %v", hostExternalIP, host)
		}

	default:
		return fmt.Errorf("Unknowk network type %v", networkType)
	}

	// Validate port.
	expectedPort, _ := getExpectedServicePortForPod(aeroCluster, pod, networkType, isTLS)

	if portStr != fmt.Sprintf("%v", expectedPort) {
		return fmt.Errorf("Incorrect port expected: %v actual: %v", expectedPort, portStr)
	}
	return nil
}

func getExpectedServicePortForPod(aeroCluster *asdbv1alpha1.AerospikeCluster, pod *corev1.Pod, networkType asdbv1alpha1.AerospikeNetworkType, isTLS bool) (int32, error) {
	var port int32

	if networkType == asdbv1alpha1.AerospikeNetworkTypePod {
		if !isTLS {
			port = asdbv1alpha1.ServicePort
		} else {
			port = asdbv1alpha1.ServiceTLSPort
		}
	} else if aeroCluster.Spec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, k8sClient)
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
			port = asdbv1alpha1.ServicePort
		} else {
			port = asdbv1alpha1.ServiceTLSPort
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
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
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
func getAerospikeClusterSpecWithNetworkPolicy(networkPolicy asdbv1alpha1.AerospikeNetworkPolicy, multiPodPerHost bool, enableTLS bool, ctx goctx.Context) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	kubeNs := namespace
	cascadeDelete := true

	var networkConf map[string]interface{} = map[string]interface{}{}

	if enableTLS {
		networkConf = map[string]interface{}{
			"service": map[string]interface{}{
				"tls-name": "aerospike-a-0.test-runner",
			},
			"tls": []interface{}{
				map[string]interface{}{
					"name":      "aerospike-a-0.test-runner",
					"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
					"key-file":  "/etc/aerospike/secret/svc_key.pem",
					"ca-file":   "/etc/aerospike/secret/cacert.pem",
				},
			},
		}
	}

	return &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aeroclustertest",
			Namespace: kubeNs,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  networkTestPolicyClusterSize,
			Image: "aerospike/aerospike-server-enterprise:5.0.0.4",
			Storage: asdbv1alpha1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				BlockVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
					{
						Path:         "/opt/aerospike/data",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			AerospikeAccessControl: &asdbv1alpha1.AerospikeAccessControlSpec{
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{
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
								"type":     "device",
								"files":    []interface{}{"/opt/aerospike/data/test.dat"},
								"filesize": 2000955200,
							},
						},
					},
				},
			},
			AerospikeNetworkPolicy: networkPolicy,
		},
	}
}
