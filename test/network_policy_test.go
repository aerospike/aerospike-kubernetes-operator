//go:build !noac

// Tests Aerospike network policy settings.

package test

import (
	goctx "context"
	"fmt"
	"net"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

const (
	// Use single node cluster so that developer machine tests run in single pod per k8s node configuration.
	networkTestPolicyClusterSize = 3
	labelAccessAddress           = "aerospike.com/configured-access-address"
	valueAccessAddress           = "192.168.1.1"
	labelAlternateAccessAddress  = "aerospike.com/configured-alternate-access-address"
	valueAlternateAccessAddress  = "192.168.1.2"
)

var _ = Describe(
	"NetworkPolicy", func() {
		ctx := goctx.TODO()

		Context(
			"When using TLS", func() {
				Context(
					"When using MultiPodPerHost", func() {
						doTestNetworkPolicy(true, true, ctx)
					},
				)

				Context(
					"When using SinglePodPerHost", func() {
						doTestNetworkPolicy(false, true, ctx)
					},
				)
			},
		)

		Context(
			"When using NonTLS", func() {
				Context(
					"When using MultiPodPerHost", func() {
						doTestNetworkPolicy(true, false, ctx)
					},
				)

				Context(
					"When using SinglePodPerHost", func() {
						doTestNetworkPolicy(false, false, ctx)
					},
				)
			},
		)

		Context(
			"Negative cases for the NetworkPolicy", func() {
				doNegativeTestNetworkPolicy(true, true, ctx)
			},
		)

	},
)

func doNegativeTestNetworkPolicy(
	multiPodPerHost bool, enableTLS bool, ctx goctx.Context,
) {
	Context(
		"Negative cases for the configuredIP", func() {
			clusterNamespacedName := getClusterNamespacedName("np-configured-ip", multiClusterNs1)

			BeforeEach(
				func() {
					err := deleteNodeLabels(ctx, []string{labelAccessAddress, labelAlternateAccessAddress})
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					err = deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"setting configured access-address without right label", func() {
					err := setNodeLabels(
						ctx,
						map[string]string{labelAlternateAccessAddress: valueAlternateAccessAddress},
					)
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).To(HaveOccurred())
				},
			)
			It(
				"setting configured alternate-access-address without right label", func() {
					err := setNodeLabels(ctx, map[string]string{labelAccessAddress: valueAccessAddress})
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AlternateAccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).To(HaveOccurred())
				},
			)
			It(
				"setting configured access-address and alternate-access-address without label", func() {
					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AccessType:             asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAccessType:          asdbv1beta1.AerospikeNetworkTypeConfigured,
						AlternateAccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).To(HaveOccurred())
				},
			)
		},
	)
}

func doTestNetworkPolicy(
	multiPodPerHost bool, enableTLS bool, ctx goctx.Context,
) {
	It(
		"DefaultNetworkPolicy", func() {
			clusterNamespacedName := getClusterNamespacedName(
				"np-default", multiClusterNs1,
			)

			// Ensures that default network policy is applied.
			defaultNetworkPolicy := asdbv1beta1.AerospikeNetworkPolicy{}
			aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, defaultNetworkPolicy, multiPodPerHost,
				enableTLS,
			)

			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	It(
		"PodAndExternal", func() {
			clusterNamespacedName := getClusterNamespacedName(
				"np-pod-external", multiClusterNs1,
			)

			// Ensures that default network policy is applied.
			networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
				AccessType:             asdbv1beta1.AerospikeNetworkTypePod,
				AlternateAccessType:    asdbv1beta1.AerospikeNetworkTypeHostExternal,
				TLSAccessType:          asdbv1beta1.AerospikeNetworkTypePod,
				TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkTypeHostExternal,
			}
			aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, networkPolicy, multiPodPerHost,
				enableTLS,
			)

			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	Context(
		"When using configuredIP", func() {
			clusterNamespacedName := getClusterNamespacedName("np-configured-ip", multiClusterNs1)

			BeforeEach(
				func() {
					err := deleteNodeLabels(ctx, []string{labelAccessAddress, labelAlternateAccessAddress})
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					err = deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"setting configured access-address", func() {
					err := setNodeLabels(ctx, map[string]string{labelAccessAddress: valueAccessAddress})
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).ToNot(HaveOccurred())

					err = validateNetworkPolicy(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
			It(
				"setting configured alternate-access-address", func() {
					err := setNodeLabels(
						ctx, map[string]string{
							labelAlternateAccessAddress: valueAlternateAccessAddress,
						},
					)
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AlternateAccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).ToNot(HaveOccurred())

					err = validateNetworkPolicy(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
			It(
				"setting configured access-address and alternate-access-address", func() {
					err := setNodeLabels(
						ctx, map[string]string{
							labelAccessAddress:          valueAccessAddress,
							labelAlternateAccessAddress: valueAlternateAccessAddress,
						},
					)
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
						AccessType:             asdbv1beta1.AerospikeNetworkTypeConfigured,
						AlternateAccessType:    asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAccessType:          asdbv1beta1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).ToNot(HaveOccurred())

					err = validateNetworkPolicy(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

// validateNetworkPolicy validates that the new network policy is applied correctly.
func validateNetworkPolicy(
	ctx goctx.Context, desired *asdbv1beta1.AerospikeCluster,
) error {
	current := &asdbv1beta1.AerospikeCluster{}
	err := k8sClient.Get(
		ctx,
		types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace},
		current,
	)

	if err != nil {
		return fmt.Errorf("error reading cluster spec:%v", err)
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(
		desired.Spec.AerospikeNetworkPolicy,
		current.Spec.AerospikeNetworkPolicy,
	) {
		return fmt.Errorf(
			"cluster state not applied. Desired: %v Current: %v",
			desired.Spec.AerospikeNetworkPolicy,
			current.Spec.AerospikeNetworkPolicy,
		)
	}

	podList, err := getPodList(current, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for podIndex := range podList.Items {
		asConn, err := newAsConn(logger, current, &podList.Items[podIndex], k8sClient)
		if err != nil {
			return fmt.Errorf("failed to get aerospike connection: %v", err)
		}

		cp := getClientPolicy(current, k8sClient)
		res, err := runInfo(cp, asConn, "endpoints")

		if err != nil {
			return fmt.Errorf("failed to run Aerospike info command: %v", err)
		}

		endpointsStr, ok := res["endpoints"]
		if !ok {
			return fmt.Errorf(
				"failed to get aerospike endpoints from pod %v", podList.Items[podIndex].Name,
			)
		}

		endpointsMap, err := deployment.ParseInfoIntoMap(
			endpointsStr, ";", "=",
		)
		if err != nil {
			return fmt.Errorf(
				"failed to parse aerospike endpoints from pod %v: %v", podList.Items[podIndex].Name,
				err,
			)
		}

		networkPolicy := current.Spec.AerospikeNetworkPolicy

		// Validate the returned endpoints.
		err = validatePodEndpoint(
			ctx, &podList.Items[podIndex], current, networkPolicy.AccessType, false,
			aerospikecluster.GetEndpointsFromInfo("access", endpointsMap), valueAccessAddress,
		)
		if err != nil {
			return err
		}

		err = validatePodEndpoint(
			ctx, &podList.Items[podIndex], current, networkPolicy.AlternateAccessType, false,
			aerospikecluster.GetEndpointsFromInfo(
				"alternate-access", endpointsMap,
			), valueAlternateAccessAddress,
		)
		if err != nil {
			return err
		}

		tlsName := getServiceTLSName(current)

		if tlsName != "" {
			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAccessType, true,
				aerospikecluster.GetEndpointsFromInfo(
					"tls-access", endpointsMap,
				), valueAccessAddress,
			)
			if err != nil {
				return err
			}

			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAlternateAccessType, true,
				aerospikecluster.GetEndpointsFromInfo(
					"tls-alternate-access", endpointsMap,
				), valueAlternateAccessAddress,
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func validatePodEndpoint(
	ctx goctx.Context, pod *corev1.Pod,
	aeroCluster *asdbv1beta1.AerospikeCluster,
	networkType asdbv1beta1.AerospikeNetworkType, isTLS bool, actual []string, configuredIP string,
) error {
	podIP, hostInternalIP, hostExternalIP, _ := getIPs(ctx, pod)
	endpoint := actual[0]
	host, portStr, err := net.SplitHostPort(endpoint)

	if err != nil {
		return fmt.Errorf("invalid endpoint %v", endpoint)
	}

	// Validate the IP address.
	switch networkType {
	case asdbv1beta1.AerospikeNetworkTypePod:
		if podIP != host {
			return fmt.Errorf("expected podIP %v got %v", podIP, host)
		}

	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		if hostInternalIP != host {
			return fmt.Errorf(
				"expected host internal IP %v got %v", hostInternalIP, host,
			)
		}

	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		if hostExternalIP != host {
			return fmt.Errorf(
				"expected host external IP %v got %v", hostExternalIP, host,
			)
		}

	case asdbv1beta1.AerospikeNetworkTypeConfigured:
		if configuredIP != host {
			return fmt.Errorf(
				"expected host configured IP %v got %v", configuredIP, host,
			)
		}

	case asdbv1beta1.AerospikeNetworkTypeUnspecified:
		return fmt.Errorf(
			"network type cannot be unspecified",
		)

	default:
		return fmt.Errorf("unknowk network type %v", networkType)
	}

	// Validate port.
	expectedPort, _ := getExpectedServicePortForPod(
		aeroCluster, pod, networkType, isTLS,
	)

	if portStr != fmt.Sprintf("%v", expectedPort) {
		return fmt.Errorf(
			"incorrect port expected: %v actual: %v", expectedPort, portStr,
		)
	}

	return nil
}

func getExpectedServicePortForPod(
	aeroCluster *asdbv1beta1.AerospikeCluster, pod *corev1.Pod,
	networkType asdbv1beta1.AerospikeNetworkType, isTLS bool,
) (int32, error) {
	var port int32

	if networkType != asdbv1beta1.AerospikeNetworkTypePod && aeroCluster.Spec.PodSpec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, k8sClient)
		if err != nil {
			return 0, fmt.Errorf("error getting service port: %v", err)
		}

		if !isTLS {
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
		if !isTLS {
			port = int32(*asdbv1beta1.GetServicePort(aeroCluster.Spec.AerospikeConfig))
		} else {
			_, tlsPort := asdbv1beta1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig)
			port = int32(*tlsPort)
		}
	}

	return port, nil
}

// getIPs returns the pod IP, host internal IP and the host external IP unless there is an error.
// Note: the IPs returned from here should match the IPs generated in the pod
// initialization script for the init container.
func getIPs(ctx goctx.Context, pod *corev1.Pod) (
	podIP string, hostInternalIP string, hostExternalIP string, err error,
) {
	podIP = pod.Status.PodIP
	hostInternalIP = pod.Status.HostIP
	hostExternalIP = hostInternalIP

	k8sNode := &corev1.Node{}
	err = k8sClient.Get(
		ctx, types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode,
	)

	if err != nil {
		return "", "", "", fmt.Errorf(
			"failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName,
			pod.Name, err,
		)
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
func getAerospikeClusterSpecWithNetworkPolicy(
	clusterNamespacedName types.NamespacedName,
	networkPolicy asdbv1beta1.AerospikeNetworkPolicy, multiPodPerHost bool,
	enableTLS bool,
) *asdbv1beta1.AerospikeCluster {
	cascadeDelete := true

	var networkConf map[string]interface{}

	var operatorClientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec

	if enableTLS {
		networkConf = getNetworkTLSConfig()

		operatorClientCertSpec = getOperatorCert()
	} else {
		networkConf = getNetworkConfig()
	}

	return &asdbv1beta1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:  networkTestPolicyClusterSize,
			Image: latestImage,
			Storage: asdbv1beta1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1beta1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: "ns",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike/data",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1beta1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},

			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
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
			PodSpec: asdbv1beta1.AerospikePodSpec{
				MultiPodPerHost: multiPodPerHost,
			},
			OperatorClientCertSpec: operatorClientCertSpec,
			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"migrate-threads":  4,
					},

					"network": networkConf,

					"security": map[string]interface{}{},
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

func setNodeLabels(ctx goctx.Context, labels map[string]string) error {
	nodeList, err := getNodeList(ctx, k8sClient)
	if err != nil {
		return err
	}

	for idx := range nodeList.Items {
		node := &nodeList.Items[idx]
		for key, val := range labels {
			node.Labels[key] = val
		}

		if err := k8sClient.Update(ctx, node); err != nil {
			return err
		}
	}

	return nil
}

func deleteNodeLabels(ctx goctx.Context, keys []string) error {
	nodeList, err := getNodeList(ctx, k8sClient)
	if err != nil {
		return err
	}

	for idx := range nodeList.Items {
		node := &nodeList.Items[idx]
		for _, key := range keys {
			delete(node.Labels, key)
		}

		if err := k8sClient.Update(ctx, node); err != nil {
			return err
		}
	}

	return nil
}
