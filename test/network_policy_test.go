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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

const (
	// Use single node cluster so that developer machine tests run in single pod per k8s node configuration.
	networkTestPolicyClusterSize = 1
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
)

var _ = Describe(
	"NetworkPolicy", func() {
		ctx := goctx.TODO()

		Context(
			"When using TLS", func() {
				Context(
					"When using MultiPodperhost", func() {
						doTestNetworkPolicy(true, true, ctx)
					},
				)

				Context(
					"When using SinglePodperhost", func() {
						doTestNetworkPolicy(false, true, ctx)
					},
				)
			},
		)

		Context(
			"When using NonTLS", func() {
				Context(
					"When using MultiPodperhost", func() {
						doTestNetworkPolicy(true, false, ctx)
					},
				)

				Context(
					"When using SinglePodperhost", func() {
						doTestNetworkPolicy(false, false, ctx)
					},
				)
			},
		)

		Context(
			"NegativeNetworkPolicyTest", func() {
				negativeAerospikeNetworkPolicyTest(ctx)
			},
		)
	},
)

func negativeAerospikeNetworkPolicyTest(ctx goctx.Context) {
	clusterName := "invalid-cluster"
	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	Context(
		"NegativeDeployNetworkPolicyTest", func() {
			negativeDeployNetworkPolicyTest(ctx, clusterNamespacedName)
		},
	)

	Context(
		"NegativeUpdateNetworkPolicyTest", func() {
			negativeUpdateNetworkPolicyTest(ctx, clusterNamespacedName)
		},
	)
}

func negativeDeployNetworkPolicyTest(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	Context(
		"InvalidAerospikeNetworkPolicy", func() {
			It(
				"MissingCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomAlternateAccessNetworkNames: should fail when alternateAccess is set to "+
					"'customInterface' and customAlternateAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSAccessNetworkNames: should fail when tlsAccess is set to 'customInterface' and "+
					"customTLSAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSAlternateAccessNetworkNames: should fail when tlsAlternateAccess is set to"+
					" 'customInterface' and customTLSAlternateAccessNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomFabricNetworkNames: should fail when fabric is set to 'customInterface' and "+
					"customFabricNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"MissingCustomTLSFabricNetworkNames: should fail when tlsFabric is set to 'customInterface' and "+
					"customTLSFabricNetworkNames is not given",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{networkOne}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: networkOne,
					}

					// cluster will crash as there will be no network status annotations
					err := deployClusterWithTO(
						k8sClient, ctx, aeroCluster, retryInterval, 2*time.Minute,
					)
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{}
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"TwoCustomAccessNetworkNames: should fail when access is set to 'customInterface' and "+
					"customAccessNetworkNames is added with 2 entries",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{
						nsNetworkOne, nsNetworkTwo,
					}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
						networkAnnotationKey: "test1/ipvlan-conf-1, test1/ipvlan-conf-2",
					}
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

func negativeUpdateNetworkPolicyTest(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	Context(
		"InvalidAerospikeNetworkPolicy", func() {
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
			aeroCluster        *asdbv1beta1.AerospikeCluster
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
				aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
				aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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
}

func doTestNetworkPolicy(
	multiPodPerHost bool, enableTLS bool, ctx goctx.Context,
) {
	var aeroCluster *asdbv1beta1.AerospikeCluster

	AfterEach(func() {
		err := deleteCluster(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	It(
		"DefaultNetworkPolicy", func() {
			clusterNamespacedName := getClusterNamespacedName(
				"np-default", multiClusterNs1,
			)

			// Ensures that default network policy is applied.
			defaultNetworkPolicy := asdbv1beta1.AerospikeNetworkPolicy{}
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

	Context("customInterface", func() {
		clusterNamespacedName := getClusterNamespacedName(
			"np-custom-interface", multiClusterNs1,
		)

		It(
			"Should add all custom interface IPs in aerospike.conf file", func() {
				networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
					AccessType:                        asdbv1beta1.AerospikeNetworkTypeCustomInterface,
					CustomAccessNetworkNames:          []string{networkOne},
					AlternateAccessType:               asdbv1beta1.AerospikeNetworkTypeCustomInterface,
					CustomAlternateAccessNetworkNames: []string{networkTwo},
					FabricType:                        asdbv1beta1.AerospikeNetworkTypeCustomInterface,
					CustomFabricNetworkNames:          []string{networkThree},
				}

				if enableTLS {
					networkPolicy.TLSAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					networkPolicy.CustomTLSAccessNetworkNames = []string{networkOne}
					networkPolicy.TLSAlternateAccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
					networkPolicy.CustomTLSAlternateAccessNetworkNames = []string{networkTwo}
					networkPolicy.TLSFabricType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
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

		It("Should recover when correct custom network names are updated", func() {
			aeroCluster = createNonSCDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1beta1.AerospikeNetworkTypeCustomInterface
			aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{"missing-network"}
			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
				networkAnnotationKey: "missing-network, test1/ipvlan-conf-1 ",
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
}]`,
			}

			By("Creating cluster with wrong custom network name")
			// cluster will crash as wrong custom interface is passed in CustomAccessNetworkNames
			err := deployClusterWithTO(
				k8sClient, ctx, aeroCluster, retryInterval, 2*time.Minute,
			)
			Expect(err).Should(HaveOccurred())

			aeroCluster, err = getCluster(
				k8sClient, ctx, clusterNamespacedName,
			)
			Expect(err).ToNot(HaveOccurred())

			By("Updating correct custom network name")
			aeroCluster.Spec.AerospikeNetworkPolicy.CustomAccessNetworkNames = []string{nsNetworkOne}
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = validateNetworkPolicy(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})
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
			aerospikecluster.GetEndpointsFromInfo(
				"service", "access", endpointsMap), customNetIPVlanOne, 0,
		)
		if err != nil {
			return err
		}

		err = validatePodEndpoint(
			ctx, &podList.Items[podIndex], current, networkPolicy.AlternateAccessType, false,
			aerospikecluster.GetEndpointsFromInfo(
				"service", "alternate-access", endpointsMap), customNetIPVlanTwo, 0,
		)
		if err != nil {
			return err
		}

		if networkPolicy.FabricType == asdbv1beta1.AerospikeNetworkTypeCustomInterface {
			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.FabricType, false,
				aerospikecluster.GetEndpointsFromInfo(
					"fabric", "", endpointsMap), customNetIPVlanThree,
				int32(*asdbv1beta1.GetFabricPort(desired.Spec.AerospikeConfig)),
			)
			if err != nil {
				return err
			}
		}

		tlsName := getServiceTLSName(current)

		if tlsName != "" {
			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAccessType, true,
				aerospikecluster.GetEndpointsFromInfo(
					"service", "tls-access", endpointsMap), customNetIPVlanOne, 0,
			)
			if err != nil {
				return err
			}

			err = validatePodEndpoint(
				ctx, &podList.Items[podIndex], current, networkPolicy.TLSAlternateAccessType, true,
				aerospikecluster.GetEndpointsFromInfo(
					"service", "tls-alternate-access", endpointsMap), customNetIPVlanTwo, 0,
			)
			if err != nil {
				return err
			}

			if networkPolicy.TLSFabricType == asdbv1beta1.AerospikeNetworkTypeCustomInterface {
				err = validatePodEndpoint(
					ctx, &podList.Items[podIndex], current, networkPolicy.TLSFabricType, false,
					aerospikecluster.GetEndpointsFromInfo(
						"fabric", "tls", endpointsMap), customNetIPVlanThree,
					int32(*asdbv1beta1.GetFabricTLSPort(desired.Spec.AerospikeConfig)),
				)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// TODO: refactor this func to reduce number of params
func validatePodEndpoint(
	ctx goctx.Context, pod *corev1.Pod,
	aeroCluster *asdbv1beta1.AerospikeCluster,
	networkType asdbv1beta1.AerospikeNetworkType, isTLS bool, actual []string, customNetIP string, customPort int32,
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
	case asdbv1beta1.AerospikeNetworkTypeCustomInterface:
		if customNetIP != host {
			return fmt.Errorf(
				"expected custom network IP %v got %v", customNetIP, host,
			)
		}
	case asdbv1beta1.AerospikeNetworkTypeUnspecified:
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

	if (networkType != asdbv1beta1.AerospikeNetworkTypePod &&
		networkType != asdbv1beta1.AerospikeNetworkTypeCustomInterface) && aeroCluster.Spec.PodSpec.MultiPodPerHost {
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
	networkPolicy *asdbv1beta1.AerospikeNetworkPolicy, multiPodPerHost bool,
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
			AerospikeNetworkPolicy: *networkPolicy,
		},
	}
}
