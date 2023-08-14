//go:build !noac

// Tests Aerospike network policy settings.

package test

import (
	goctx "context"
	"fmt"
	"net"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
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
	networkOne                   = "ipvlan-conf-1"
	networkTwo                   = "ipvlan-conf-2"
	networkThree                 = "ipvlan-conf-3"
	nsNetworkOne                 = "test1/ipvlan-conf-1"
	nsNetworkTwo                 = "test1/ipvlan-conf-2"
	customNetIPVlanOne           = "10.0.4.70"
	customNetIPVlanTwo           = "10.0.6.70"
	customNetIPVlanThree         = "0.0.0.0"
	networkAnnotationKey         = "k8s.v1.cni.cncf.io/networks"
	networkStatusAnnotationKey   = "k8s.v1.cni.cncf.io/network-status"

	shortRetry = 2 * time.Minute
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
				negativeAerospikeNetworkPolicyTest(ctx, true, true)
			},
		)
	},
)

func negativeAerospikeNetworkPolicyTest(ctx goctx.Context, multiPodPerHost, enableTLS bool) {
	Context(
		"NegativeDeployNetworkPolicyTest", func() {
			negativeDeployNetworkPolicyTest(ctx, multiPodPerHost, enableTLS)
		},
	)

	Context(
		"NegativeUpdateNetworkPolicyTest", func() {
			negativeUpdateNetworkPolicyTest(ctx)
		},
	)
}

func negativeDeployNetworkPolicyTest(ctx goctx.Context, multiPodPerHost, enableTLS bool) {
	Context(
		"Negative cases for customInterface", func() {
			clusterNamespacedName := getNamespacedName("np-custom-interface", namespace)

			It(
				"MissingCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomAlternateAccessNetworkNames: should fail when alternateAccess is set to "+
					"'customInterface' and customAlternateAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSAccessNetworkNames: should fail when tlsAccess is set to 'customInterface' and "+
					"customTLSAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSAlternateAccessNetworkNames: should fail when tlsAlternateAccess is set to"+
					" 'customInterface' and customTLSAlternateAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomFabricNetworkNames: should fail when fabric is set to 'customInterface' and "+
					"customFabricNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSFabricNetworkNames: should fail when tlsFabric is set to 'customInterface' and "+
					"customTLSFabricNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidFabricType: should fail when fabric is set to value other than 'customInterface'",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = "invalid-enum"
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			// Following test-cases are applicable for all custom Interfaces
			// Added test-case for only 'customAccessNetworkNames`, rest of the types will be similar to this only
			It(
				"MissingNetworkNameInPodAnnotations: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is not present in pod annotations",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}

					// define different networks than the ones defined in CustomAccessNetworkNames
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: "ipvlan-conf-2, ipvlan-conf-3",
					}
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"UsingHostNetworkAndCustomInterface: should fail when access is set to 'customInterface' and "+
					"Host network is used",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: networkOne,
					}
					aeroCluster.Spec.PodSpec.HostNetwork = true
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"NetworkNameOfDifferentNamespace: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames present in pod annotations is of different namespace",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{"random/ipvlan-conf-1"}

					// define different networks than the ones defined in CustomAccessNetworkNames
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: networkOne,
					}
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is given and CNI not working (network-status annotation missing)",
				func() {
					aeroCluster := createNonSCDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: networkOne,
					}

					// cluster will crash as there will be no network status annotations
					err := deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
					Expect(err).Should(HaveOccurred())

					err = deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"EmptyCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is set to empty slice",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{}
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)

	Context(
		"Negative cases for configuredIP", func() {
			clusterNamespacedName := getNamespacedName("np-configured-ip", multiClusterNs1)

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

					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
					Expect(err).To(HaveOccurred())
				},
			)
			It(
				"setting configured alternate-access-address without right label", func() {
					err := setNodeLabels(ctx, map[string]string{labelAccessAddress: valueAccessAddress})
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AlternateAccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err = deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
					Expect(err).To(HaveOccurred())
				},
			)
			It(
				"setting configured access-address and alternate-access-address without label", func() {
					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AccessType:             asdbv1.AerospikeNetworkTypeConfigured,
						TLSAccessType:          asdbv1.AerospikeNetworkTypeConfigured,
						AlternateAccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster := getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, networkPolicy, multiPodPerHost,
						enableTLS,
					)

					err := deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
					Expect(err).To(HaveOccurred())
				},
			)
		},
	)
}

func negativeUpdateNetworkPolicyTest(ctx goctx.Context) {
	Context("Negative cases for customInterface", func() {
		clusterNamespacedName := getNamespacedName("np-custom-interface", namespace)
		Context(
			"InvalidAerospikeCustomInterface", func() {
				BeforeEach(
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

				It(
					"MissingCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
						"customAccessNetworkNames is not given",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"MissingCustomAlternateAccessNetworkNames: should fail when alternateAccess is set to "+
						"'customInterface' and customAlternateAccessNetworkNames is not given",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"MissingCustomTLSAccessNetworkNames: should fail when tlsAccess is set to 'customInterface' and "+
						"customTLSAccessNetworkNames is not given",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"MissingCustomTLSAlternateAccessNetworkNames: should fail when tlsAlternateAccess is set "+
						"to 'customInterface' and customTLSAlternateAccessNetworkNames is not given",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"UpdatingFabricTypeInNetworkPolicy: should fail when fabric type is changed",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1.AerospikeNetworkTypeCustomInterface
						aeroCluster.Spec.AerospikeNetworkPolicy.CustomFabricNetworkNames = []string{networkOne}
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							networkAnnotationKey: networkOne,
						}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"UpdatingTLSFabricTypeInNetworkPolicy: should fail when TLS fabric type is changed",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1.AerospikeNetworkTypeCustomInterface
						aeroCluster.Spec.AerospikeNetworkPolicy.CustomTLSFabricNetworkNames = []string{networkOne}
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							networkAnnotationKey: networkOne,
						}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				// Following test-cases are applicable for all custom Interfaces
				// Added test-case for only 'customAccessNetworkNames`, rest of the types will be similar to this only
				It(
					"MissingNetworkNameInPodAnnotations: should fail when access is set to 'customInterface' and "+
						"customAccessNetworkNames is not present in pod annotations",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}

						// define different networks than the ones defined in CustomAccessNetworkNames
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							networkAnnotationKey: "ipvlan-conf-2, ipvlan-conf-3",
						}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"UsingHostNetworkAndCustomInterface: should fail when access is set to 'customInterface' and "+
						"Host network is used",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							networkAnnotationKey: networkOne,
						}
						aeroCluster.Spec.PodSpec.HostNetwork = true
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"NetworkNameOfDifferentNamespace: should fail when access is set to 'customInterface' and "+
						"customAccessNetworkNames present in pod annotations is of different namespace",
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{"random/ipvlan-conf-1"}

						// define different networks than the ones defined in CustomAccessNetworkNames
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							networkAnnotationKey: networkOne,
						}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)
			},
		)

		Context("InvalidFabricNetworkNamesUpdate", func() {
			var (
				aeroCluster        *asdbv1.AerospikeCluster
				fabricNetStatusOne = `[{
    "name": "test1/ipvlan-conf-1",
    "interface": "net1",
    "ips": [
        "0.0.0.0"
    ],
    "mac": "06:18:e7:3e:50:65",
    "dns": {}
}]`
				fabricNetStatusTwo = `[{
    "name": "test1/ipvlan-conf-2",
    "interface": "net2",
    "ips": [
        "0.0.0.0"
    ],
    "mac": "06:18:e7:3e:50:65",
    "dns": {}
}]`
			)

			AfterEach(
				func() {
					err := deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"UpdateCustomFabricNetworkNames: should fail when fabric is set to 'customInterface' and "+
					"customFabricNetworkNames list is updated",
				func() {
					aeroCluster = createDummyAerospikeCluster(
						clusterNamespacedName, 2,
					)
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomFabricNetworkNames = []string{nsNetworkOne}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey:       nsNetworkOne,
						networkStatusAnnotationKey: fabricNetStatusOne,
					}

					By("Creating cluster with custom fabric interface")
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Updating custom fabric interface network list")
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomFabricNetworkNames = []string{nsNetworkTwo}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey:       nsNetworkTwo,
						networkStatusAnnotationKey: fabricNetStatusTwo,
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"UpdateCustomTLSFabricNetworkNames: should fail when tlsFabric is set to 'customInterface' and "+
					"customTLSFabricNetworkNames list is updated",
				func() {
					aeroCluster = createDummyAerospikeCluster(
						clusterNamespacedName, 2,
					)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomTLSFabricNetworkNames = []string{nsNetworkOne}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey:       nsNetworkOne,
						networkStatusAnnotationKey: fabricNetStatusOne,
					}

					By("Creating cluster with custom tlsfabric interface")
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Updating custom tlsFabric interface network list")
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomTLSFabricNetworkNames = []string{nsNetworkTwo}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey:       nsNetworkTwo,
						networkStatusAnnotationKey: fabricNetStatusTwo,
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		})
	})
}

func doTestNetworkPolicy(
	multiPodPerHost bool, enableTLS bool, ctx goctx.Context,
) {
	var aeroCluster *asdbv1.AerospikeCluster

	AfterEach(func() {
		err := deleteCluster(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	It(
		"DefaultNetworkPolicy", func() {
			clusterNamespacedName := getNamespacedName(
				"np-default", multiClusterNs1,
			)

			// Ensures that default network policy is applied.
			defaultNetworkPolicy := asdbv1.AerospikeNetworkPolicy{}
			aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, &defaultNetworkPolicy, multiPodPerHost,
				enableTLS,
			)

			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	It(
		"PodAndExternal", func() {
			clusterNamespacedName := getNamespacedName(
				"np-pod-external", multiClusterNs1,
			)

			// Ensures that default network policy is applied.
			networkPolicy := asdbv1.AerospikeNetworkPolicy{
				AccessType:             asdbv1.AerospikeNetworkTypePod,
				AlternateAccessType:    asdbv1.AerospikeNetworkTypeHostExternal,
				TLSAccessType:          asdbv1.AerospikeNetworkTypePod,
				TLSAlternateAccessType: asdbv1.AerospikeNetworkTypeHostExternal,
			}
			aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, &networkPolicy, multiPodPerHost,
				enableTLS,
			)

			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	// test-case valid only for multiPodPerHost true
	if multiPodPerHost {
		It("OnlyPodNetwork: should create cluster without nodePort service", func() {
			clusterNamespacedName := getNamespacedName(
				"pod-network-cluster", multiClusterNs1)

			networkPolicy := asdbv1.AerospikeNetworkPolicy{
				AccessType:             asdbv1.AerospikeNetworkTypePod,
				AlternateAccessType:    asdbv1.AerospikeNetworkTypePod,
				TLSAccessType:          asdbv1.AerospikeNetworkTypePod,
				TLSAlternateAccessType: asdbv1.AerospikeNetworkTypePod,
			}

			aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, &networkPolicy, multiPodPerHost,
				enableTLS,
			)

			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			validateSvcExistence := func(aeroCluster *asdbv1.AerospikeCluster, shouldExist bool) {
				for podName := range aeroCluster.Status.Pods {
					svc := &corev1.Service{}
					err = k8sClient.Get(ctx, types.NamespacedName{
						Namespace: aeroCluster.Namespace,
						Name:      podName,
					}, svc)

					if !shouldExist {
						Expect(err).To(HaveOccurred())
						Expect(errors.IsNotFound(err)).To(BeTrue())
						Expect(aeroCluster.Status.Pods[podName].ServicePort).To(Equal(int32(0)))
						Expect(aeroCluster.Status.Pods[podName].HostExternalIP).To(Equal(""))
					} else {
						Expect(err).NotTo(HaveOccurred())
						Expect(aeroCluster.Status.Pods[podName].ServicePort).NotTo(Equal(int32(0)))
						Expect(aeroCluster.Status.Pods[podName].HostExternalIP).NotTo(Equal(""))
					}
				}
			}

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			validateSvcExistence(aeroCluster, false)

			By("Updating AccessType to hostInternal")
			aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypeHostExternal
			err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateSvcExistence(aeroCluster, true)

			By("Reverting AccessType to pod")
			aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkTypePod
			err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			validateSvcExistence(aeroCluster, false)
		})
	}

	Context(
		"When using configuredIP", func() {
			clusterNamespacedName := getNamespacedName("np-configured-ip", multiClusterNs1)
			BeforeEach(
				func() {
					err := deleteNodeLabels(ctx, []string{labelAccessAddress, labelAlternateAccessAddress})
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"setting configured access-address", func() {
					err := setNodeLabels(ctx, map[string]string{labelAccessAddress: valueAccessAddress})
					Expect(err).ToNot(HaveOccurred())

					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
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

					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AlternateAccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
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

					networkPolicy := &asdbv1.AerospikeNetworkPolicy{
						AccessType:             asdbv1.AerospikeNetworkTypeConfigured,
						AlternateAccessType:    asdbv1.AerospikeNetworkTypeConfigured,
						TLSAccessType:          asdbv1.AerospikeNetworkTypeConfigured,
						TLSAlternateAccessType: asdbv1.AerospikeNetworkTypeConfigured,
					}
					aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
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

	// All the custom interface related test-cases are tested by mocking the behavior of CNI plugins.
	// Mocking is done by adding k8s.v1.cni.cncf.io/network-status manually in pod metadata annotations
	// which is ideally added by CNI at runtime.
	// Test cases with NetworkAttachmentDefinition of different namespaces can't be tested with current mocking.
	Context("customInterface", func() {
		clusterNamespacedName := getNamespacedName(
			"np-custom-interface", multiClusterNs1,
		)

		// Skip this test when multiPodPerHost is true and enabledTLS true because Network Policy contains all
		// customInterface type, so no nodePort service will be created hence no port mapping with k8s host node
		if !(multiPodPerHost && enableTLS) {
			It(
				"Should add all custom interface IPs in aerospike.conf file", func() {
					networkPolicy := asdbv1.AerospikeNetworkPolicy{
						AccessType:                        asdbv1.AerospikeNetworkTypeCustomInterface,
						CustomAccessNetworkNames:          []string{networkOne, networkTwo},
						AlternateAccessType:               asdbv1.AerospikeNetworkTypeCustomInterface,
						CustomAlternateAccessNetworkNames: []string{networkTwo},
						FabricType:                        asdbv1.AerospikeNetworkTypeCustomInterface,
						CustomFabricNetworkNames:          []string{networkThree},
					}

					if enableTLS {
						networkPolicy.TLSAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						networkPolicy.CustomTLSAccessNetworkNames = []string{networkOne, networkTwo}
						networkPolicy.TLSAlternateAccessType = asdbv1.AerospikeNetworkTypeCustomInterface
						networkPolicy.CustomTLSAlternateAccessNetworkNames = []string{networkTwo}
						networkPolicy.TLSFabricType = asdbv1.AerospikeNetworkTypeCustomInterface
						networkPolicy.CustomTLSFabricNetworkNames = []string{networkThree}
					}

					aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
						clusterNamespacedName, &networkPolicy, multiPodPerHost,
						enableTLS,
					)

					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						// Network Interfaces to be used
						networkAnnotationKey: "test1/ipvlan-conf-1, ipvlan-conf-2, ipvlan-conf-3",
						// CNI updates this network-status in the pod annotations
						networkStatusAnnotationKey: `[{
    "name": "aws-cni",
    "interface": "eth0",
    "ips": [
        "10.0.2.114"
    ],
    "default": true,
    "dns": {}
},{
    "name": "test1/ipvlan-conf-1",
    "interface": "net1",
    "ips": [
        "10.0.4.70"
    ],
    "mac": "06:18:e7:3e:50:65",
    "dns": {}
},{
    "name": "test1/ipvlan-conf-2",
    "interface": "net2",
    "ips": [
        "10.0.6.70"
    ],
    "mac": "06:0d:8f:a4:be:f9",
    "dns": {}
},{
    "name": "test1/ipvlan-conf-3",
    "interface": "net3",
    "ips": [
        "0.0.0.0"
    ],
    "mac": "06:0d:8f:a4:be:f8",
    "dns": {}
}]`,
					}

					err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
					Expect(err).ToNot(HaveOccurred())

					err = validateNetworkPolicy(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		}

		It("Should recover when correct custom network names are updated", func() {
			networkPolicy := asdbv1.AerospikeNetworkPolicy{
				AccessType:               asdbv1.AerospikeNetworkTypeCustomInterface,
				CustomAccessNetworkNames: []string{"missing-network"},
			}

			aeroCluster = getAerospikeClusterSpecWithNetworkPolicy(
				clusterNamespacedName, &networkPolicy, multiPodPerHost,
				enableTLS,
			)
			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
				networkAnnotationKey: "missing-network, test1/ipvlan-conf-1, test1/ipvlan-conf-2",
				networkStatusAnnotationKey: `[{
    "name": "aws-cni",
    "interface": "eth0",
    "ips": [
        "10.0.2.114"
    ],
    "default": true,
    "dns": {}
},{
    "name": "test1/ipvlan-conf-1",
    "interface": "net1",
    "ips": [
        "10.0.4.70"
    ],
    "mac": "06:18:e7:3e:50:65",
    "dns": {}
},{
    "name": "test1/ipvlan-conf-2",
    "interface": "net2",
    "ips": [
        "10.0.6.70"
    ],
    "mac": "06:0d:8f:a4:be:f9",
    "dns": {}
}]`,
			}

			By("Creating cluster with wrong custom network name")
			// cluster will crash as wrong custom interface is passed in CustomAccessNetworkNames
			err := deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
			Expect(err).Should(HaveOccurred())

			aeroCluster, err = getCluster(
				k8sClient, ctx, clusterNamespacedName,
			)
			Expect(err).ToNot(HaveOccurred())

			By("Updating correct custom network name")
			aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{nsNetworkOne, nsNetworkTwo}
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})
}

// validateNetworkPolicy validates that the new network policy is applied correctly.
func validateNetworkPolicy(
	ctx goctx.Context, desired *asdbv1.AerospikeCluster,
) error {
	current := &asdbv1.AerospikeCluster{}
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
			aerospikecluster.GetEndpointsFromInfo("service", "access", endpointsMap),
			[]string{customNetIPVlanOne, customNetIPVlanTwo}, valueAccessAddress, 0)
		if err != nil {
			return err
		}

		err = validatePodEndpoint(
			ctx, &podList.Items[podIndex], current, networkPolicy.AlternateAccessType, false,
			aerospikecluster.GetEndpointsFromInfo("service", "alternate-access", endpointsMap),
			[]string{customNetIPVlanTwo}, valueAlternateAccessAddress, 0)
		if err != nil {
			return err
		}

		if networkPolicy.FabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.FabricType, false,
				aerospikecluster.GetEndpointsFromInfo("fabric", "", endpointsMap),
				[]string{customNetIPVlanThree}, "", int32(*asdbv1.GetFabricPort(desired.Spec.AerospikeConfig)))
			if err != nil {
				return err
			}
		}

		tlsName := getServiceTLSName(current)

		if tlsName != "" {
			if err := validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAccessType, true,
				aerospikecluster.GetEndpointsFromInfo("service", "tls-access", endpointsMap),
				[]string{customNetIPVlanOne, customNetIPVlanTwo}, valueAccessAddress, 0); err != nil {
				return err
			}

			if err := validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAlternateAccessType, true,
				aerospikecluster.GetEndpointsFromInfo("service", "tls-alternate-access",
					endpointsMap), []string{customNetIPVlanTwo}, valueAlternateAccessAddress, 0); err != nil {
				return err
			}

			if networkPolicy.TLSFabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
				_, port := asdbv1.GetFabricTLSNameAndPort(desired.Spec.AerospikeConfig)
				if err := validatePodEndpoint(
					ctx, &podList.Items[podIndex], current, networkPolicy.TLSFabricType, false,
					aerospikecluster.GetEndpointsFromInfo("fabric", "tls", endpointsMap),
					[]string{customNetIPVlanThree}, "", int32(*port)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// TODO: refactor this func to reduce number of params
func validatePodEndpoint(
	ctx goctx.Context, pod *corev1.Pod, aeroCluster *asdbv1.AerospikeCluster,
	networkType asdbv1.AerospikeNetworkType, isTLS bool, actual, customNetIP []string, configuredIP string,
	customPort int32,
) error {
	podIP, hostInternalIP, hostExternalIP, _ := getIPs(ctx, pod)

	hostIPList := make([]string, 0, len(actual))

	var port string

	for idx := range actual {
		host, portStr, err := net.SplitHostPort(actual[idx])
		if err != nil {
			return fmt.Errorf("invalid endpoint %v", actual[idx])
		}

		hostIPList = append(hostIPList, host)
		// port will be same for all hosts
		port = portStr
	}

	// Validate the IP address.
	switch networkType {
	case asdbv1.AerospikeNetworkTypePod:
		if podIP != hostIPList[0] {
			return fmt.Errorf("expected podIP %v got %v", podIP, hostIPList[0])
		}
	case asdbv1.AerospikeNetworkTypeHostInternal:
		if hostInternalIP != hostIPList[0] {
			return fmt.Errorf(
				"expected host internal IP %v got %v", hostInternalIP, hostIPList[0],
			)
		}
	case asdbv1.AerospikeNetworkTypeHostExternal:
		if hostExternalIP != hostIPList[0] {
			return fmt.Errorf(
				"expected host external IP %v got %v", hostExternalIP, hostIPList[0],
			)
		}
	case asdbv1.AerospikeNetworkTypeConfigured:
		if configuredIP != hostIPList[0] {
			return fmt.Errorf(
				"expected host configured IP %v got %v", configuredIP, hostIPList[0],
			)
		}
	case asdbv1.AerospikeNetworkTypeCustomInterface:
		if !reflect.DeepEqual(customNetIP, hostIPList) {
			return fmt.Errorf(
				"expected custom network IP %v got %v", customNetIP, hostIPList,
			)
		}
	case asdbv1.AerospikeNetworkTypeUnspecified:
		return fmt.Errorf(
			"network type cannot be unspecified",
		)

	default:
		return fmt.Errorf("unknowk network type %v", networkType)
	}

	expectedPort := customPort

	if customPort == 0 {
		expectedPort, _ = getExpectedServicePortForPod(
			aeroCluster, pod, networkType, isTLS,
		)
	}

	// Validate port.
	if port != fmt.Sprintf("%v", expectedPort) {
		return fmt.Errorf(
			"incorrect port expected: %v actual: %v", expectedPort, port,
		)
	}

	return nil
}

func getExpectedServicePortForPod(
	aeroCluster *asdbv1.AerospikeCluster, pod *corev1.Pod,
	networkType asdbv1.AerospikeNetworkType, isTLS bool,
) (int32, error) {
	var port int32

	if (networkType != asdbv1.AerospikeNetworkTypePod &&
		networkType != asdbv1.AerospikeNetworkTypeCustomInterface) && aeroCluster.Spec.PodSpec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, k8sClient)
		if err != nil {
			return 0, fmt.Errorf("error getting service port: %v", err)
		}

		if !isTLS {
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
		if !isTLS {
			port = int32(*asdbv1.GetServicePort(aeroCluster.Spec.AerospikeConfig))
		} else {
			_, tlsPort := asdbv1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig)
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
	networkPolicy *asdbv1.AerospikeNetworkPolicy, multiPodPerHost bool,
	enableTLS bool,
) *asdbv1.AerospikeCluster {
	cascadeDelete := true

	var networkConf map[string]interface{}

	var operatorClientCertSpec *asdbv1.AerospikeOperatorClientCertSpec

	if enableTLS {
		networkConf = getNetworkTLSConfig()

		operatorClientCertSpec = getOperatorCert()
	} else {
		networkConf = getNetworkConfig()
	}

	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  networkTestPolicyClusterSize,
			Image: latestImage,
			Storage: asdbv1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: "ns",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike/data",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},

			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
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
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: multiPodPerHost,
			},
			OperatorClientCertSpec: operatorClientCertSpec,
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
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
			AerospikeNetworkPolicy: *networkPolicy,
		},
	}
}

func setNodeLabels(ctx goctx.Context, labels map[string]string) error {
	nodeList, err := getNodeList(ctx, k8sClient)
	if err != nil {
		return err
	}

	for idx := range nodeList.Items {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			node := &nodeList.Items[idx]

			if err := k8sClient.Get(
				ctx, types.NamespacedName{Name: node.Name}, node); err != nil {
				return err
			}

			for key, val := range labels {
				node.Labels[key] = val
			}

			return k8sClient.Update(ctx, node)
		}); err != nil {
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
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			node := &nodeList.Items[idx]

			if err := k8sClient.Get(
				ctx, types.NamespacedName{Name: node.Name}, node); err != nil {
				return err
			}

			for _, key := range keys {
				delete(node.Labels, key)
			}

			return k8sClient.Update(ctx, node)
		}); err != nil {
			return err
		}
	}

	return nil
}
