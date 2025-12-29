package cluster

import (
	goctx "context"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	lib "github.com/aerospike/aerospike-management-lib"
)

const (
	clusterNameConfig = "cluster-name"
	adminPort         = 3003
)

var _ = Describe(
	"AerospikeCluster", func() {

		ctx := goctx.TODO()

		// Cluster lifecycle related
		Context(
			"DeployClusterPost570", func() {
				DeployClusterForAllImagesPost570(ctx)
			},
		)
		Context(
			"DeployClusterDiffStorageMultiPodPerHost", func() {
				DeployClusterForDiffStorageTest(ctx, 2, true)
			},
		)
		Context(
			"DeployClusterDiffStorageSinglePodPerHost", func() {
				DeployClusterForDiffStorageTest(ctx, 2, false)
			},
		)
		Context(
			"DeployClusterWithDNSConfiguration", func() {
				DeployClusterWithDNSConfiguration(ctx)
			},
		)
		// Need to setup some syslog related things for this
		// Context(
		// 	"DeployClusterWithSyslog", func() {
		// 		DeployClusterWithSyslog(ctx)
		// 	},
		// )
		Context(
			"DeployClusterWithMaxIgnorablePod", func() {
				clusterWithMaxIgnorablePod(ctx)
			},
		)
		Context(
			"CommonNegativeClusterValidationTest", func() {
				NegativeClusterValidationTest(ctx)
			},
		)
		Context(
			"UpdateAerospikeCluster", func() {
				UpdateClusterTest(ctx)
			},
		)
		Context(
			"RunScaleDownWithMigrateFillDelay", func() {
				ScaleDownWithMigrateFillDelay(ctx)
			},
		)
		Context(
			"ValidateScaleDownWaitForMigrations", func() {
				ValidateScaleDownWaitForMigrations(ctx)
			},
		)
		Context(
			"PauseReconcile", func() {
				PauseReconcileTest(ctx)
			},
		)
		Context(
			"ValidateAerospikeBenchmarkConfigs", func() {
				ValidateAerospikeBenchmarkConfigs(ctx)
			},
		)
		// Network-related tests
		Context(
			asdbv1.ConfKeyNetwork, func() {
				Context(
					"UpdateTLSCluster", func() {
						UpdateTLSClusterTest(ctx)
					},
				)
				Context(
					"NetworkValidation", func() {
						NetworkValidationTest(ctx)
					},
				)
				Context(
					"AdminPort", func() {
						adminPortTests(ctx)
					},
				)
			},
		)
	},
)

func adminPortTests(ctx goctx.Context) {
	aeroCluster := &asdbv1.AerospikeCluster{}

	AfterEach(
		func() {
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)
	It(
		"Should create cluster with admin port and connect successfully",
		func() {
			clusterName := fmt.Sprintf("admin-port-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			// Create cluster with admin port configuration
			aeroCluster = createDummyAerospikeClusterWithAdminPort(clusterNamespacedName, 2, adminPort)

			aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// Verify admin port is configured in Aerospike
			By("Verifying admin port configuration")
			validateAdminPort(ctx, clusterNamespacedName, clusterNamespacedName.Name+"-0-0", adminPort)
		},
	)

	It(
		"Should update cluster by adding admin connection",
		func() {
			var err error

			clusterName := fmt.Sprintf("admin-port-update-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			// Create cluster without admin port initially
			aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// Update cluster to add admin port
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			networkConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
			networkConf[asdbv1.ConfKeyNetworkAdmin] = map[string]interface{}{
				asdbv1.ConfKeyPort: adminPort,
			}
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			// Verify admin port is configured in Aerospike
			By("Verifying admin port configuration")
			validateAdminPort(ctx, clusterNamespacedName, clusterNamespacedName.Name+"-0-0", adminPort)
		},
	)

	It(
		"Should update cluster by removing admin connection",
		func() {
			clusterName := fmt.Sprintf("admin-port-remove-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			// Create cluster with admin port initially
			aeroCluster = createDummyAerospikeClusterWithAdminPort(clusterNamespacedName, 2, adminPort)

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// Verify admin port is initially configured
			By("Verifying admin port configuration")
			validateAdminPort(ctx, clusterNamespacedName, clusterNamespacedName.Name+"-0-0", adminPort)

			// Update cluster to remove admin port
			By("Removing admin port configuration")

			networkConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
			delete(networkConf, asdbv1.ConfKeyNetworkAdmin)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf

			err := updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			// Verify admin port
			By("Verifying admin port configuration")
			validateAdminPort(ctx, clusterNamespacedName, clusterNamespacedName.Name+"-0-0", 0)
		},
	)
}

func validateAdminPort(ctx goctx.Context, clusterNamespacedName types.NamespacedName, podName string, adminPort int) {
	networkPort := asdbv1.ConfKeyNetworkAdmin
	if adminPort == 0 {
		networkPort = asdbv1.ConfKeyNetworkService
	}

	asinfo, err := getASInfo(logger, k8sClient, ctx, clusterNamespacedName, podName, networkPort)
	Expect(err).ToNot(HaveOccurred())

	confs, err := getAsConfig(asinfo, asdbv1.ConfKeyNetwork)
	Expect(err).ToNot(HaveOccurred())

	// Verify admin port is set to adminPort
	network := confs[asdbv1.ConfKeyNetwork].(lib.Stats)
	Expect(network["admin.port"]).To(Equal(int64(adminPort)))
}

// Network-related validation tests
func NetworkValidationTest(ctx goctx.Context) {
	Context(
		"DeployValidation", func() {
			networkDeployValidationTest(ctx)
		},
	)
	Context(
		"UpdateValidation", func() {
			networkUpdateValidationTest(ctx)
		},
	)
}

func networkDeployValidationTest(ctx goctx.Context) {
	Context(
		"Validation", func() {
			clusterName := fmt.Sprintf("invalid-network-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			It(
				"NetworkConf: should fail for setting network conf/tls network conf",
				func() {
					// Network conf
					// asdbv1.ConfKeyPort
					// "access-port"
					// "access-addresses"
					// "alternate-access-port"
					// "alternate-access-addresses"
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					networkConf := map[string]interface{}{
						asdbv1.ConfKeyNetworkService: map[string]interface{}{
							asdbv1.ConfKeyPort: serviceNonTLSPort,
							"access-addresses": []string{"<access_addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-addresses"
					// "tls-alternate-access-port"
					// "tls-alternate-access-addresses"
					aeroCluster = createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					networkConf = map[string]interface{}{
						asdbv1.ConfKeyNetworkService: map[string]interface{}{
							asdbv1.ConfKeyTLSName:  "aerospike-a-0.test-runner",
							asdbv1.ConfKeyTLSPort:  3001,
							"tls-access-addresses": []string{"<tls-access-addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for no tls path in storage volume",
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 1, latestImage,
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/randompath/svc_cluster_chain.pem",
							},
						},
					}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for both ca-file and ca-path in tls",
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 1, latestImage,
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
								"key-file":  "/etc/aerospike/secret/svc_key.pem",
								"ca-file":   "/etc/aerospike/secret/cacert.pem",
								"ca-path":   "/etc/aerospike/secret/cacerts",
							},
						},
					}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for ca-file path pointing to Secret Manager",
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 1, latestImage,
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
								"key-file":  "/etc/aerospike/secret/svc_key.pem",
								"ca-file":   "secrets:Test-secret:Key",
							},
						},
					}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for ca-path pointing to Secret Manager",
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 1, latestImage,
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
								"key-file":  "/etc/aerospike/secret/svc_key.pem",
								"ca-path":   "secrets:Test-secret:Key",
							},
						},
					}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)
		},
	)
}

func networkUpdateValidationTest(ctx goctx.Context) {
	Context(
		"Validation", func() {
			clusterName := fmt.Sprintf("invalid-network-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 3,
					)

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"UpdateService: should fail for updating non-tls to tls in single step. Cannot be updated",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network := getNetworkTLSConfig()
					serviceNetwork := network[asdbv1.ConfKeyNetworkService].(map[string]interface{})
					delete(serviceNetwork, asdbv1.ConfKeyPort)
					network[asdbv1.ConfKeyNetworkService] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					aeroCluster.Spec.OperatorClientCertSpec = &asdbv1.AerospikeOperatorClientCertSpec{
						AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
							SecretCertSource: &asdbv1.AerospikeSecretCertSource{
								SecretName:         test.AerospikeSecretName,
								CaCertsFilename:    "cacert.pem",
								ClientCertFilename: "svc_cluster_chain.pem",
								ClientKeyFilename:  "svc_key.pem",
							},
						},
					}
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"NetworkConf: should fail for setting network conf, should fail for setting tls network conf",
				func() {
					// Network conf
					// asdbv1.ConfKeyPort
					// "access-port"
					// "access-addresses"
					// "alternate-access-port"
					// "alternate-access-addresses"
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					networkConf := map[string]interface{}{
						asdbv1.ConfKeyNetworkService: map[string]interface{}{
							asdbv1.ConfKeyPort: serviceNonTLSPort,
							"access-addresses": []string{"<access_addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-addresses"
					// "tls-alternate-access-port"
					// "tls-alternate-access-addresses"
					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					networkConf = map[string]interface{}{
						asdbv1.ConfKeyNetworkService: map[string]interface{}{
							asdbv1.ConfKeyTLSName:  "aerospike-a-0.test-runner",
							asdbv1.ConfKeyTLSPort:  3001,
							"tls-access-addresses": []string{"<tls-access-addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkConf
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for no tls path in storage volumes",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/randompath/svc_cluster_chain.pem",
							},
						},
					}
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

func PauseReconcileTest(ctx goctx.Context) {
	clusterNamespacedName := test.GetNamespacedName(
		"pause-reconcile", namespace,
	)

	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	It(
		"Should pause reconcile", func() {
			// Testing over upgrade as it is a long-running operation
			By("1. Start upgrade and pause at partial upgrade")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = UpdateClusterImage(aeroCluster, nextImage)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Update(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			Eventually(
				func() bool {
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Check if at least one pod is upgraded
					podUpgraded := false

					for podName := range aeroCluster.Status.Pods {
						podStatus := aeroCluster.Status.Pods[podName]
						if podStatus.Image == nextImage {
							pkgLog.Info("One Pod upgraded", "pod", podName, "image", podStatus.Image)

							podUpgraded = true

							break
						}
					}

					return podUpgraded
				}, 2*time.Minute, 1*time.Second,
			).Should(BeTrue())

			By("Pause reconcile")

			err = setPauseFlag(ctx, clusterNamespacedName, ptr.To(true))
			Expect(err).ToNot(HaveOccurred())

			By("2. Upgrade should fail")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(1), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)
			Expect(err).To(HaveOccurred())

			// Resume reconcile and Wait for all pods to be upgraded
			By("3. Resume reconcile and upgrade should succeed")

			err = setPauseFlag(ctx, clusterNamespacedName, nil)
			Expect(err).ToNot(HaveOccurred())

			By("Upgrade should succeed")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func setPauseFlag(ctx goctx.Context, clusterNamespacedName types.NamespacedName, pause *bool) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Paused = pause

	return k8sClient.Update(ctx, aeroCluster)
}

// Historically, all benchmark configurations were represented as single literal fields in the configuration.
// The presence of these fields indicated that the corresponding benchmark was enabled.
// However, AER-6767(https://aerospike.atlassian.net/browse/AER-6767) restricted a two-literal benchmark
// configuration format (e.g., "enable-benchmarks-read true")
// Here validating that benchmark configurations are working as expected with all server versions.
func ValidateAerospikeBenchmarkConfigs(ctx goctx.Context) {
	Context(
		"ValidateAerospikeBenchmarkConfigs", func() {
			clusterNamespacedName := test.GetNamespacedName(
				"deploy-cluster-benchmark", namespace,
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)
			It(
				"benchmarking should work in all aerospike server version", func() {
					By("Deploying cluster which does not have the fix for AER-6767")

					imageBeforeFix := fmt.Sprintf("%s:%s", baseImage, "7.1.0.2")
					aeroCluster := createAerospikeClusterPost640(clusterNamespacedName, 2, imageBeforeFix)
					namespaceConfig :=
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
					namespaceConfig["enable-benchmarks-read"] = false
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					By("Validating benchmarking is disabled")

					nsConfs, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
						asdbv1.ConfKeyNamespace, aeroCluster.Name+"-0-0")
					Expect(err).ToNot(HaveOccurred())
					Expect(nsConfs["test"].(lib.Stats)["enable-benchmarks-read"]).To(BeFalse())

					By("Updating cluster to enable benchmarking")

					namespaceConfig =
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
					namespaceConfig["enable-benchmarks-read"] = true
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					nsConfs, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
						asdbv1.ConfKeyNamespace, aeroCluster.Name+"-0-0")
					Expect(err).ToNot(HaveOccurred())
					Expect(nsConfs["test"].(lib.Stats)["enable-benchmarks-read"]).To(BeTrue())

					By("Updating cluster server to version which has the fix for AER-6767")

					imageAfterFix := fmt.Sprintf("%s:%s", baseImage, "7.1.0.10")
					aeroCluster.Spec.Image = imageAfterFix

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					nsConfs, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
						asdbv1.ConfKeyNamespace, aeroCluster.Name+"-0-0")
					Expect(err).ToNot(HaveOccurred())
					Expect(nsConfs["test"].(lib.Stats)["enable-benchmarks-read"]).To(BeTrue())

					By("Updating cluster to disable benchmarking")

					namespaceConfig =
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})

					namespaceConfig["enable-benchmarks-read"] = false
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					nsConfs, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
						asdbv1.ConfKeyNamespace, aeroCluster.Name+"-0-0")
					Expect(err).ToNot(HaveOccurred())
					Expect(nsConfs["test"].(lib.Stats)["enable-benchmarks-read"]).To(BeFalse())
				},
			)
		},
	)
}

func ScaleDownWithMigrateFillDelay(ctx goctx.Context) {
	Context(
		"ScaleDownWithMigrateFillDelay", func() {
			clusterNamespacedName := test.GetNamespacedName(
				"migrate-fill-delay-cluster", namespace,
			)
			migrateFillDelay := int64(120)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["migrate-fill-delay"] =
						migrateFillDelay
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"Should ignore migrate-fill-delay while scaling down", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size -= 2
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// verify that migrate-fill-delay is set to 0 while scaling down
					firstPodName := aeroCluster.Name + "-" + strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[0].ID) + "-0"

					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, 0,
						nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())

					err = waitForAerospikeCluster(
						k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
						getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
					)
					Expect(err).ToNot(HaveOccurred())

					// verify that migrate-fill-delay is reverted to original value after scaling down
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, migrateFillDelay,
						nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

func ValidateScaleDownWaitForMigrations(ctx goctx.Context) {
	Context(
		"ValidateScaleDownWaitForMigrations", func() {
			clusterNamespacedName := test.GetNamespacedName(
				"wait-for-migrations-cluster", namespace,
			)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"Should wait for migrations to complete before pod deletion", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size -= 1
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					seenQuiesced := false
					seenMigrationsComplete := false

					Eventually(func() bool {
						podList, lErr := getPodList(aeroCluster, k8sClient)
						Expect(lErr).ToNot(HaveOccurred())

						info, iErr := requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test",
							podList.Items[0].Name)
						Expect(iErr).ToNot(HaveOccurred())

						var nodesQuiesced string

						confs := strings.Split(info["namespace/test"], ";")
						for _, conf := range confs {
							if strings.Contains(conf, "nodes_quiesced") {
								nodesQuiesced = strings.Split(conf, "=")[1]
								break
							}
						}

						migrations := getMigrationsInProgress(ctx, k8sClient, clusterNamespacedName, podList)

						podCount := utils.Len32(podList.Items)
						// Track quiesced
						if podCount == 4 && nodesQuiesced == "1" {
							seenQuiesced = true
						}

						// Track migrations complete after quiesce
						if seenQuiesced && podCount == 4 && migrations == 0 {
							seenMigrationsComplete = true
						}

						// Fail condition: if scale down happens before migrations finish
						if podCount < 4 && migrations != 0 {
							Fail(fmt.Sprintf("Pods scaledown while migrations still running (pods=%d, migrations=%d)",
								podCount, migrations))
						}

						// Success condition: pod scale down only after migrations complete
						return podCount == aeroCluster.Spec.Size && seenQuiesced && seenMigrationsComplete
					}, 5*time.Minute, 1*time.Second).Should(BeTrue(), "Pods should only scaledown after migrations complete")

					err = waitForAerospikeCluster(
						k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
						getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
					)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

func clusterWithMaxIgnorablePod(ctx goctx.Context) {
	var (
		err            error
		nodeList       = &v1.NodeList{}
		podList        = &v1.PodList{}
		expectedPhases = []asdbv1.AerospikeClusterPhase{
			asdbv1.AerospikeClusterInProgress, asdbv1.AerospikeClusterCompleted,
		}
	)

	clusterName := fmt.Sprintf("ignore-pod-cluster-%d", GinkgoParallelProcess())
	clusterNamespacedName := test.GetNamespacedName(
		clusterName, namespace,
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	Context(
		"UpdateClusterWithMaxIgnorablePodAndPendingPod", func() {
			BeforeEach(
				func() {
					var aeroCluster *asdbv1.AerospikeCluster

					nodeList, err = test.GetNodeList(ctx, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					size := utils.Len32(nodeList.Items)

					deployClusterForMaxIgnorablePods(ctx, clusterNamespacedName, size)

					By("Scale up 1 pod to make that pod pending due to lack of k8s nodes")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size++
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"Should allow cluster operations with pending pod", func() {
					By("Set MaxIgnorablePod and Rolling restart cluster")

					var aeroCluster *asdbv1.AerospikeCluster

					// As pod is in pending state, CR object will be updated continuously
					// This is put in eventually to retry Object Conflict error
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							val := intstr.FromInt32(1)
							aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
							aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeySecurity].(map[string]interface{})["enable-quotas"] = true

							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, time.Minute, time.Second,
					).ShouldNot(HaveOccurred())

					By("Upgrade version")
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.Image = nextImage
							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, time.Minute, time.Second,
					).ShouldNot(HaveOccurred())

					By("Verify pending pod")

					podList, err = getPodList(aeroCluster, k8sClient)

					var counter int

					for idx := range podList.Items {
						if podList.Items[idx].Status.Phase == v1.PodPending {
							counter++
						}
					}
					// There should be only one pending pod
					Expect(counter).To(Equal(1))

					By("Executing on-demand operation")
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							operations := []asdbv1.OperationSpec{
								{
									Kind: asdbv1.OperationWarmRestart,
									ID:   "1",
								},
							}
							aeroCluster.Spec.Operations = operations
							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, time.Minute, time.Second,
					).ShouldNot(HaveOccurred())

					By("Verify pending pod")

					podList, err = getPodList(aeroCluster, k8sClient)

					counter = 0

					for idx := range podList.Items {
						if podList.Items[idx].Status.Phase == v1.PodPending {
							counter++
						}
					}
					// There should be only one pending pod
					Expect(counter).To(Equal(1))

					By("Scale down 1 pod")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size--
					// As pod is in pending state, CR object won't reach the final phase.
					// So expectedPhases can be InProgress or Completed
					err = updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
					Expect(err).ToNot(HaveOccurred())

					By("Verify if all pods are running")

					podList, err = getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					for idx := range podList.Items {
						Expect(utils.IsPodRunningAndReady(&podList.Items[idx])).To(BeTrue())
					}
				},
			)
		},
	)

	Context(
		"UpdateClusterWithMaxIgnorablePodAndFailedPod", func() {
			BeforeEach(
				func() {
					deployClusterForMaxIgnorablePods(ctx, clusterNamespacedName, 4)
				},
			)

			It(
				"Should allow rack deletion with failed pods in different rack", func() {
					By("Fail 1-1 aerospike pod")

					ignorePodName := clusterNamespacedName.Name + "-1-1"
					err = markPodAsFailed(ctx, k8sClient, ignorePodName, clusterNamespacedName.Namespace)
					Expect(err).ToNot(HaveOccurred())

					// Underlying kubernetes cluster should have atleast 6 nodes to run this test successfully.
					By("Delete rack with id 2")

					aeroCluster, gErr := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(gErr).ToNot(HaveOccurred())

					val := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
					aeroCluster.Spec.RackConfig.Racks = getDummyRackConf(1)
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By(fmt.Sprintf("Verify if failed pod %s is automatically recovered", ignorePodName))

					pod := &v1.Pod{}

					Eventually(
						func() bool {
							err = k8sClient.Get(
								ctx, types.NamespacedName{
									Name:      ignorePodName,
									Namespace: clusterNamespacedName.Namespace,
								}, pod,
							)

							return len(pod.Status.ContainerStatuses) != 0 && *pod.Status.ContainerStatuses[0].Started &&
								pod.Status.ContainerStatuses[0].Ready
						}, time.Minute, time.Second,
					).Should(BeTrue())

					Eventually(
						func() error {
							return InterceptGomegaFailure(
								func() {
									validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
								},
							)
						}, 5*time.Minute, 10*time.Second,
					).Should(Succeed())
				},
			)

			It(
				"Should allow namespace addition and removal with failed pod", func() {
					By("Fail 1-1 aerospike pod")

					ignorePodName := clusterNamespacedName.Name + "-1-1"

					err = markPodAsFailed(ctx, k8sClient, ignorePodName, clusterNamespacedName.Namespace)
					Expect(err).ToNot(HaveOccurred())

					By("Set MaxIgnorablePod and Rolling restart by removing namespace")

					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					val := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
					nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsList = nsList[:len(nsList)-1]
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					err = validateDirtyVolumes(ctx, k8sClient, clusterNamespacedName, []string{"bar"})
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart by re-using previously removed namespace storage")

					nsList = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsList = append(nsList, getNonSCNamespaceConfig("barnew", "/test/dev/xvdf1"))
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

func deployClusterForMaxIgnorablePods(ctx goctx.Context, clusterNamespacedName types.NamespacedName, size int32) {
	By("Deploying cluster")

	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, size)

	// Add a nonsc namespace. This will be used to test dirty volumes
	nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
	nsList = append(nsList, getNonSCNamespaceConfig("bar", "/test/dev/xvdf1"))
	aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes,
		asdbv1.VolumeSpec{
			Name: "bar",
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					Size:         resource.MustParse("1Gi"),
					StorageClass: storageClass,
					VolumeMode:   v1.PersistentVolumeBlock,
				},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
				Path: "/test/dev/xvdf1",
			},
		},
	)
	racks := getDummyRackConf(1, 2)
	aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
		Namespaces: []string{scNamespace}, Racks: racks,
	}
	aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)

	randomizeServicePorts(aeroCluster, false, GinkgoParallelProcess())

	Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
}

// Test cluster deployment with all image post 5.7.0 except the latest version
func DeployClusterForAllImagesPost570(ctx goctx.Context) {
	versions := []string{
		"8.0.0.2", "7.2.0.6", "7.1.0.12", "7.0.0.20", "6.4.0.7", "6.3.0.13", "6.2.0.9", "6.1.0.14", "6.0.0.16",
	}

	aeroCluster := &asdbv1.AerospikeCluster{}

	AfterEach(
		func() {
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	for _, v := range versions {
		It(
			fmt.Sprintf("Deploy-%s", v), func() {
				var err error

				clusterName := fmt.Sprintf("deploy-cluster-%d", GinkgoParallelProcess())
				clusterNamespacedName := test.GetNamespacedName(
					clusterName, namespace,
				)

				image := fmt.Sprintf(
					"aerospike/aerospike-server-enterprise:%s", v,
				)
				aeroCluster, err = getAeroClusterConfig(
					clusterNamespacedName, image,
				)
				Expect(err).ToNot(HaveOccurred())

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				By("Validating Readiness probe")

				err = validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceTLSPort)
				Expect(err).ToNot(HaveOccurred())
			},
		)
	}
}

// Test cluster deployment with different namespace storage
func DeployClusterForDiffStorageTest(
	ctx goctx.Context, nHosts int32, multiPodPerHost bool,
) {
	clusterSz := nHosts
	if multiPodPerHost {
		clusterSz++
	}

	repFact := nHosts
	//nolint:wsl //Comments are for test-case description
	Context(
		"Positive", func() {
			aeroCluster := &asdbv1.AerospikeCluster{}

			AfterEach(
				func() {
					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)
			// Cluster with n nodes, enterprise can be more than 8
			// Cluster with resources
			// Verify: Connect with cluster

			// Namespace storage configs
			//
			// SSD Storage Engine
			It(
				"SSDStorageCluster", func() {
					clusterName := fmt.Sprintf("ssdstoragecluster-%d", GinkgoParallelProcess())
					clusterNamespacedName := test.GetNamespacedName(
						clusterName, namespace,
					)
					aeroCluster = createSSDStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					if !multiPodPerHost {
						randomizeServicePorts(aeroCluster, true, GinkgoParallelProcess())
					}

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			// HDD Storage Engine with Data in Memory
			It(
				"HDDAndDataInMemStorageCluster", func() {
					clusterName := fmt.Sprintf("inmemstoragecluster-%d", GinkgoParallelProcess())
					clusterNamespacedName := test.GetNamespacedName(
						clusterName, namespace,
					)

					aeroCluster = createHDDAndDataInMemStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					if !multiPodPerHost {
						randomizeServicePorts(aeroCluster, true, GinkgoParallelProcess())
					}

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)
			// Data in Memory Without Persistence
			It(
				"DataInMemWithoutPersistentStorageCluster", func() {
					clusterName := fmt.Sprintf("nopersistentcluster-%d", GinkgoParallelProcess())
					clusterNamespacedName := test.GetNamespacedName(
						clusterName, namespace,
					)

					aeroCluster = createDataInMemWithoutPersistentStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					if !multiPodPerHost {
						randomizeServicePorts(aeroCluster, true, GinkgoParallelProcess())
					}

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)
			// Shadow Device
			// It("ShadowDeviceStorageCluster", func() {
			// 	aeroCluster := createShadowDeviceStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)
			// 	if err := deployCluster(k8sClient, ctx, aeroCluster); err != nil {
			// 		t.Fatal(err)
			// 	}
			// 	// make info call

			// 	deleteCluster(k8sClient, ctx, aeroCluster)
			// })

			// Persistent Memory (pmem) Storage Engine
		},
	)
}

func DeployClusterWithDNSConfiguration(ctx goctx.Context) {
	var aeroCluster *asdbv1.AerospikeCluster

	It(
		"deploy with dnsPolicy 'None' and dnsConfig given",
		func() {
			clusterNamespacedName := test.GetNamespacedName(
				"dns-config-cluster", namespace,
			)
			aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)
			noneDNS := v1.DNSNone
			aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS

			var kubeDNSSvc v1.Service

			// fetch kube-dns service to get the DNS server IP for DNS lookup
			// This service name is same for both kube-dns and coreDNS DNS servers
			Expect(
				k8sClient.Get(
					ctx, types.NamespacedName{
						Namespace: "kube-system",
						Name:      "kube-dns",
					}, &kubeDNSSvc,
				),
			).ShouldNot(HaveOccurred())

			dnsConfig := &v1.PodDNSConfig{
				Nameservers: kubeDNSSvc.Spec.ClusterIPs,
				Searches:    []string{"svc.cluster.local", "cluster.local"},
				Options: []v1.PodDNSConfigOption{
					{
						Name:  "ndots",
						Value: func(input string) *string { return &input }("5"),
					},
				},
			}
			aeroCluster.Spec.PodSpec.DNSConfig = dnsConfig

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())

			sts, err := getSTSFromRackID(aeroCluster, 0, "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(sts.Spec.Template.Spec.DNSConfig).To(Equal(dnsConfig))
		},
	)

	AfterEach(
		func() {
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)
}

func DeployClusterWithSyslog(ctx goctx.Context) {
	It(
		"deploy with syslog logging config", func() {
			clusterNamespacedName := test.GetNamespacedName(
				"logging-config-cluster", namespace,
			)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			loggingConf := []interface{}{
				map[string]interface{}{
					"name":     "syslog",
					"any":      "INFO",
					"path":     "/dev/log",
					"tag":      "asd",
					"facility": "local0",
				},
			}

			aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)
}

func UpdateTLSClusterTest(ctx goctx.Context) {
	clusterName := fmt.Sprintf("update-tls-cluster-%d", GinkgoParallelProcess())
	clusterNamespacedName := test.GetNamespacedName(
		clusterName, namespace,
	)

	BeforeEach(
		func() {
			aeroCluster := CreateBasicTLSCluster(clusterNamespacedName, 3)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	Context(
		"When doing valid operations", func() {
			It(
				"Try update operations", func() {
					By("Adding new TLS configuration")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList := network["tls"].([]interface{})
					tlsList = append(
						tlsList, map[string]interface{}{
							"name":      "aerospike-a-0.test-runner1",
							"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/svc_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
					)
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Modifying unused TLS configuration")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					unusedTLS := tlsList[1].(map[string]interface{})
					unusedTLS["name"] = "aerospike-a-0.test-runner2"
					unusedTLS["ca-file"] = "/etc/aerospike/secret/fb_cert.pem"
					tlsList[1] = unusedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Removing unused TLS configuration")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					network["tls"] = tlsList[:1]
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Changing ca-file to ca-path in TLS configuration")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS := tlsList[0].(map[string]interface{})
					usedTLS["ca-path"] = "/etc/aerospike/secret/cacerts"
					delete(usedTLS, "ca-file")
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					secretVolume := asdbv1.VolumeSpec{
						Name: test.TLSCacertSecretName,
						Source: asdbv1.VolumeSource{
							Secret: &v1.SecretVolumeSource{
								SecretName: test.TLSCacertSecretName,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret/cacerts",
						},
					}
					aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, secretVolume)
					operatorClientCertSpec := getOperatorCert()
					operatorClientCertSpec.SecretCertSource.CaCertsFilename = ""
					cacertPath := &asdbv1.CaCertsSource{
						SecretName: test.TLSCacertSecretName,
					}
					operatorClientCertSpec.SecretCertSource.CaCertsSource = cacertPath
					aeroCluster.Spec.OperatorClientCertSpec = operatorClientCertSpec
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)

	Context(
		"When doing invalid operations", func() {
			It(
				"Try update operations", func() {
					By("Modifying name of used TLS configuration")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList := network["tls"].([]interface{})
					usedTLS := tlsList[0].(map[string]interface{})
					usedTLS["name"] = "aerospike-a-0.test-runner2"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Modifying ca-file of used TLS configuration")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS = tlsList[0].(map[string]interface{})
					usedTLS["ca-file"] = "/etc/aerospike/secret/fb_cert.pem"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating both ca-file and ca-path in TLS configuration")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS = tlsList[0].(map[string]interface{})
					usedTLS["ca-path"] = "/etc/aerospike/secret/cacerts"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating tls-name in service network config")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					serviceNetwork := network[asdbv1.ConfKeyNetworkService].(map[string]interface{})
					serviceNetwork["tls-name"] = "unknown-tls"
					network[asdbv1.ConfKeyNetworkService] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating tls-port in service network config")

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					serviceNetwork = network[asdbv1.ConfKeyNetworkService].(map[string]interface{})
					serviceNetwork["tls-port"] = float64(4000)
					network[asdbv1.ConfKeyNetworkService] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					// Should fail when changing network config from tls to non-tls in a single step.
					// Ideally first tls and non-tls config both has to set and then remove tls config.
					By("Updating tls to non-tls in single step in service network config")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					serviceNetwork = network[asdbv1.ConfKeyNetworkService].(map[string]interface{})
					delete(serviceNetwork, asdbv1.ConfKeyPort)
					network[asdbv1.ConfKeyNetworkService] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork].(map[string]interface{})
					serviceNetwork = network[asdbv1.ConfKeyNetworkService].(map[string]interface{})
					delete(serviceNetwork, "tls-port")
					delete(serviceNetwork, "tls-name")
					delete(serviceNetwork, "tls-authenticate-client")
					serviceNetwork[asdbv1.ConfKeyPort] = float64(serviceNonTLSPort)
					network[asdbv1.ConfKeyNetworkService] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

// Test cluster cr update
func UpdateClusterTest(ctx goctx.Context) {
	// Note: this storage will be used by dynamically added namespace after deployment of cluster
	dynamicNsPath := "/test/dev/dynamicns"
	dynamicNsVolume := asdbv1.VolumeSpec{
		Name: "dynamicns",
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   v1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: dynamicNsPath,
		},
	}
	dynamicNsPath1 := "/test/dev/dynamicns1"
	dynamicNsVolume1 := asdbv1.VolumeSpec{
		Name: "dynamicns1",
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   v1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: dynamicNsPath1,
		},
	}
	dynamicNs := map[string]interface{}{
		"name":               "dynamicns",
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{dynamicNsPath, dynamicNsPath1},
		},
	}

	clusterName := fmt.Sprintf("update-cluster-%d", GinkgoParallelProcess())
	clusterNamespacedName := test.GetNamespacedName(
		clusterName, namespace,
	)

	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, dynamicNsVolume, dynamicNsVolume1,
			)

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	Context(
		"When doing valid operations", func() {
			It(
				"Try update operations", func() {
					By("ScaleUp")

					err := scaleUpClusterTest(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("ScaleDown")

					// TODO:
					// How to check if it is checking cluster stability before killing node
					// Check if tip-clear, alumni-reset is done or not
					err = scaleDownClusterTest(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart")

					// TODO: How to check if it is checking cluster stability before killing node
					err = rollingRestartClusterTest(
						logger, k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Updating NamespaceStorage")

					err = rollingRestartClusterByUpdatingNamespaceStorageTest(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Adding Namespace Dynamically")

					err = rollingRestartClusterByAddingNamespaceDynamicallyTest(
						k8sClient, ctx, dynamicNs, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Scaling up along with modifying Namespace storage Dynamically")

					err = scaleUpClusterTestWithNSDeviceHandling(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Scaling down along with modifying Namespace storage Dynamically")

					err = scaleDownClusterTestWithNSDeviceHandling(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Removing Namespace Dynamically")

					err = rollingRestartClusterByRemovingNamespaceDynamicallyTest(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Re-using Previously Removed Namespace Storage")

					err = rollingRestartClusterByAddingNamespaceDynamicallyTest(
						k8sClient, ctx, dynamicNs, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By changing non-tls to tls")

					err = rollingRestartClusterByEnablingTLS(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceTLSPort})
					Expect(err).ToNot(HaveOccurred())

					err = validatePodPortsUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceTLSPort})
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By changing tls to non-tls")

					err = rollingRestartClusterByDisablingTLS(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceNonTLSPort})
					Expect(err).ToNot(HaveOccurred())

					err = validatePodPortsUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceNonTLSPort})
					Expect(err).ToNot(HaveOccurred())

					By("Upgrade/Downgrade")

					// TODO: How to check if it is checking cluster stability before killing node
					// dont change image, it upgrade, check old version
					err = upgradeClusterTest(
						k8sClient, ctx, clusterNamespacedName, nextImage,
					)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)

	Context(
		"When doing invalid operations", func() {
			Context(
				"ValidateUpdate", func() {
					// TODO: No jump version yet but will be used
					// It("Image", func() {
					// 	old := aeroCluster.Spec.Image
					// 	aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:4.0.0.5"
					// 	err = k8sClient.Update(ctx, aeroCluster)
					// 	validateError(err, "should fail for upgrading to jump version")
					// 	aeroCluster.Spec.Image = old
					// })
					It(
						"MultiPodPerHost: should fail for updating MultiPodPerHost. Cannot be updated",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							multiPodPerHost := !*aeroCluster.Spec.PodSpec.MultiPodPerHost
							aeroCluster.Spec.PodSpec.MultiPodPerHost = &multiPodPerHost

							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"Should fail for Re-using Namespace Storage Dynamically",
						func() {
							err := rollingRestartClusterByReusingNamespaceStorageTest(
								k8sClient, ctx, clusterNamespacedName, dynamicNs,
							)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"StorageValidation: should fail for updating Storage. Cannot be updated",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							newVolumeSpec := []asdbv1.VolumeSpec{
								{
									Name: "ns",
									Source: asdbv1.VolumeSource{
										PersistentVolume: &asdbv1.PersistentVolumeSpec{
											StorageClass: storageClass,
											VolumeMode:   v1.PersistentVolumeBlock,
											Size:         resource.MustParse("1Gi"),
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/dev/xvdf2",
									},
								},
								{
									Name: "workdir",
									Source: asdbv1.VolumeSource{
										PersistentVolume: &asdbv1.PersistentVolumeSpec{
											StorageClass: storageClass,
											VolumeMode:   v1.PersistentVolumeFilesystem,
											Size:         resource.MustParse("1Gi"),
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/opt/aeropsike/ns1",
									},
								},
							}
							aeroCluster.Spec.Storage.Volumes = newVolumeSpec

							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					Context(
						asdbv1.ConfKeyNamespace, func() {
							It(
								"UpdateReplicationFactor: should fail for updating namespace"+
									"replication-factor on SC namespace. Cannot be updated",
								func() {
									aeroCluster, err := getCluster(
										k8sClient, ctx,
										clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									namespaceConfig :=
										aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
									namespaceConfig["replication-factor"] = 5
									aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig

									err = k8sClient.Update(
										ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())
								},
							)

							It(
								"UpdateReplicationFactor: should fail for updating namespace"+
									"replication-factor on non-SC namespace. Cannot be updated",
								func() {
									By("RollingRestart By Adding Namespace Dynamically")

									err := rollingRestartClusterByAddingNamespaceDynamicallyTest(
										k8sClient, ctx, dynamicNs, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster, err := getCluster(
										k8sClient, ctx,
										clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
									namespaceConfig := nsList[len(nsList)-1].(map[string]interface{})
									namespaceConfig["replication-factor"] = 3
									aeroCluster.Spec.AerospikeConfig.
										Value[asdbv1.ConfKeyNamespace].([]interface{})[len(nsList)-1] = namespaceConfig

									err = k8sClient.Update(
										ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())
								},
							)
						},
					)
				},
			)
		},
	)
}

// Test cluster validation Common for deployment and update both
func NegativeClusterValidationTest(ctx goctx.Context) {
	Context(
		"NegativeDeployClusterValidationTest", func() {
			negativeDeployClusterValidationTest(ctx)
		},
	)

	Context(
		"NegativeUpdateClusterValidationTest", func() {
			negativeUpdateClusterValidationTest(ctx)
		},
	)
}

func negativeDeployClusterValidationTest(
	ctx goctx.Context,
) {
	Context(
		"Validation", func() {
			clusterName := fmt.Sprintf("invalid-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			It(
				"EmptyClusterName: should fail for EmptyClusterName", func() {
					cName := test.GetNamespacedName(
						"", clusterNamespacedName.Namespace,
					)

					aeroCluster := createDummyAerospikeCluster(cName, 1)
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"EmptyNamespaceName: should fail for EmptyNamespaceName",
				func() {
					cName := test.GetNamespacedName("validclustername", "")

					aeroCluster := createDummyAerospikeCluster(cName, 1)
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"InvalidImage: should fail for InvalidImage", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					aeroCluster.Spec.Image = "InvalidImage"
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())

					aeroCluster.Spec.Image = invalidImage
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			It(
				"InvalidSize: should fail for zero size", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 0,
					)
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
				},
			)

			Context(
				"InvalidOperatorClientCertSpec: should fail for invalid OperatorClientCertSpec", func() {
					It(
						"MultipleCertSource: should fail if both SecretCertSource and CertPathInOperator is set",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator = &asdbv1.AerospikeCertPathInOperatorSource{}
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					It(
						"MissingClientKeyFilename: should fail if ClientKeyFilename is missing",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.ClientKeyFilename = ""

							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					It(
						"Should fail if both CaCertsFilename and CaCertsSource is set",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.CaCertsSource = &asdbv1.CaCertsSource{}

							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					It(
						"MissingClientCertPath: should fail if clientCertPath is missing",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource = nil
							aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator =
								&asdbv1.AerospikeCertPathInOperatorSource{
									CaCertsPath:    "cacert.pem",
									ClientKeyPath:  "svc_key.pem",
									ClientCertPath: "",
								}

							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfig: should fail for empty/invalid aerospikeConfig",
				func() {
					It(
						"should fail for empty/invalid aerospikeConfig",
						func() {
							aeroCluster := createDummyAerospikeCluster(
								clusterNamespacedName, 1,
							)
							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())

							aeroCluster = createDummyAerospikeCluster(
								clusterNamespacedName, 1,
							)
							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
								Value: map[string]interface{}{
									asdbv1.ConfKeyNamespace: "invalidConf",
								},
							}
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					It(
						"ServiceConf: should fail for setting advertise-ipv6",
						func() {
							aeroCluster := createDummyAerospikeCluster(
								clusterNamespacedName, 1,
							)
							aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["advertise-ipv6"] = true
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					Context(
						"InvalidNamespace", func() {
							It(
								"NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace",
								func() {
									aeroCluster := createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil
									Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
								},
							)

							// Should we test for overridden fields
							Context(
								"InvalidStorage", func() {
									It(
										"NilStorageEngine: should fail for nil storage-engine",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
											namespaceConfig["storage-engine"] = nil
											aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
											Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
										},
									)

									It(
										"NilStorageEngineDevice: should fail for nil storage-engine.device",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})

											if _, ok :=
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = nil
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
											}
										},
									)

									It(
										"InvalidStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot have 3 devices in single device string",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})

											if _, ok :=
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												aeroCluster.Spec.Storage.Volumes = []asdbv1.VolumeSpec{
													{
														Name: "nsvol1",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf1",
														},
													},
													{
														Name: "nsvol2",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf2",
														},
													},
													{
														Name: "nsvol3",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf3",
														},
													},
												}

												namespaceConfig :=
													aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] =
													[]string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
											}
										},
									)

									It(
										"NilStorageEngineFile: should fail for nil storage-engine.file",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})

											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["files"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["files"] = nil
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
											}
										},
									)

									It(
										"ExtraStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot use a device which doesn't exist in storage",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})

											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
												devList = append(
													devList, "andRandomDevice",
												)
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
											}
										},
									)

									It(
										"DuplicateStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot use a device which already exist in another namespace",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											secondNs := map[string]interface{}{
												"name":               "ns1",
												"replication-factor": 2,
												"storage-engine": map[string]interface{}{
													"type":    "device",
													"devices": []interface{}{"/test/dev/xvdf"},
												},
											}

											nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
											nsList = append(nsList, secondNs)
											aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
											Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
										},
									)
								},
							)
						},
					)

					Context(
						"ChangeDefaultConfig", func() {
							It(
								"NsConf", func() {
									// Ns conf
									// Rack-id
									// aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
									// aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].
									// ([]interface{})[0].(map[string]interface{})["rack-id"] = 1
									// aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
									// err := deployCluster(k8sClient, ctx, aeroCluster)
									// validateError(err, "should fail for setting rack-id")
								},
							)

							It(
								"ServiceConf: should fail for setting node-id/cluster-name",
								func() {
									// Service conf
									// 	"node-id"
									// 	"cluster-name"
									aeroCluster := createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a1"
									Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())

									aeroCluster = createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.
										Value[asdbv1.ConfKeyService].(map[string]interface{})[clusterNameConfig] = clusterNameConfig
									Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
								},
							)
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfigSecret", func() {
					It(
						"WhenFeatureKeyExist: should fail for no feature-key-file path in storage volume",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService] = map[string]interface{}{
								"feature-key-file": "/randompath/features.conf",
							}
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidDNSConfiguration", func() {
					It(
						"InvalidDnsPolicy: should fail when dnsPolicy is set to 'Default'",
						func() {
							aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
							defaultDNS := v1.DNSDefault
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)

					It(
						"MissingDnsConfig: should fail when dnsPolicy is set to 'None' and no dnsConfig given",
						func() {
							aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
							noneDNS := v1.DNSNone
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
						},
					)
				},
			)

			It(
				"InvalidLogging: should fail for using syslog param with file or console logging", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}

					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
					err := k8sClient.Create(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

func negativeUpdateClusterValidationTest(
	ctx goctx.Context,
) {
	// Will be used in Update
	Context(
		"Validation", func() {
			clusterName := fmt.Sprintf("invalid-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 3,
					)

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"InvalidImage: should fail for InvalidImage, should fail for image lower than base",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Image = "InvalidImage"
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Image = invalidImage
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidSize: should fail for zero size", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size = 0
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			Context(
				"InvalidDNSConfiguration", func() {
					It(
						"InvalidDnsPolicy: should fail when dnsPolicy is set to 'Default'",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							defaultDNS := v1.DNSDefault
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS
							err = updateCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"MissingDnsConfig: Should fail when dnsPolicy is set to 'None' and no dnsConfig given",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							noneDNS := v1.DNSNone
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS
							err = updateCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfig: should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig",
				func() {
					It(
						"should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())

							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
								Value: map[string]interface{}{
									asdbv1.ConfKeyNamespace: "invalidConf",
								},
							}
							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					Context(
						"InvalidNamespace", func() {
							It(
								"NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace",
								func() {
									aeroCluster, err := getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())
								},
							)

							// Should we test for overridden fields
							Context(
								"InvalidStorage", func() {
									It(
										"NilStorageEngine: should fail for nil storage-engine",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
											namespaceConfig["storage-engine"] = nil
											aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] =
												namespaceConfig
											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"NilStorageEngineDevice: should fail for nil storage-engine.device",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = nil
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"NilStorageEngineFile: should fail for nil storage-engine.file",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["files"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["files"] = nil
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"ExtraStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot add a device which doesn't exist in BlockStorage",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
												devList = append(
													devList, "andRandomDevice",
												)
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
												aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"DuplicateStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot add a device which already exist in another namespace",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											secondNs := map[string]interface{}{
												"name":               "ns1",
												"replication-factor": 2,
												"storage-engine": map[string]interface{}{
													"type":    "device",
													"devices": []interface{}{"/test/dev/xvdf"},
												},
											}

											nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
											nsList = append(nsList, secondNs)
											aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)
								},
							)
						},
					)

					Context(
						"ChangeDefaultConfig", func() {
							It(
								"ServiceConf: should fail for setting node-id, should fail for setting cluster-name",
								func() {
									// Service conf
									// 	"node-id"
									// 	"cluster-name"
									aeroCluster, err := getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a10"
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())

									aeroCluster, err = getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.
										Value[asdbv1.ConfKeyService].(map[string]interface{})[clusterNameConfig] = clusterNameConfig
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())
								},
							)
						},
					)
				},
			)

			It(
				"InvalidLogging: should fail for using syslog param with file or console logging", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}

					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)

	Context(
		"InvalidAerospikeConfigSecret", func() {
			clusterName := fmt.Sprintf("invalid-cluster-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			BeforeEach(
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 2, latestImage,
					)

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"WhenFeatureKeyExist: should fail for no feature-key-file path in storage volumes",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService] = map[string]interface{}{
						"feature-key-file": "/randompath/features.conf",
					}
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)

	Context(
		"FailedPodGracePeriodConfiguration", func() {
			clusterName := fmt.Sprintf("grace-period-%d", GinkgoParallelProcess())
			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"Should wait for grace period before recreating pods in fresh cluster deployment", func() {
					By("Creating fresh cluster with unschedulable resource requirements")

					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					// Make pods unschedulable by requesting excessive memory
					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

					err := k8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for pods to be created and enter pending state")

					var (
						podList *v1.PodList
						uuidMap = make(map[string]types.UID)
					)

					Eventually(
						func() error {
							podList, err = getPodList(aeroCluster, k8sClient)
							if err != nil {
								return err
							}

							if len(podList.Items) != int(aeroCluster.Spec.Size) {
								return fmt.Errorf("expected %d pods, found %d",
									aeroCluster.Spec.Size, len(podList.Items))
							}

							for idx := range podList.Items {
								pod := &podList.Items[idx]
								if pod.Status.Phase != v1.PodPending {
									return fmt.Errorf("pod %s is not in pending state", pod.Name)
								}

								uuidMap[pod.Name] = pod.UID
							}

							return nil
						}, 5*time.Minute, 10*time.Second,
					).Should(Succeed())

					By("Waiting within grace period and verifying operator does not recreate pods")
					pkgLog.Info("Waiting 20 seconds within grace period (grace period = 60s)...")
					time.Sleep(20 * time.Second)

					podList, err = getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					By("Verifying pods are not recreated within grace period")

					for idx := range podList.Items {
						pod := &podList.Items[idx]
						Expect(pod.Status.Phase).To(Equal(v1.PodPending))
						Expect(uuidMap[pod.Name]).To(Equal(pod.UID))
					}

					By("Verifying pods are recreated after grace period")

					Eventually(
						func() error {
							podList, err = getPodList(aeroCluster, k8sClient)
							if err != nil {
								return err
							}

							if len(podList.Items) != int(aeroCluster.Spec.Size) {
								return fmt.Errorf("expected %d pods, found %d",
									aeroCluster.Spec.Size, len(podList.Items))
							}

							for idx := range podList.Items {
								pod := &podList.Items[idx]
								if uuidMap[pod.Name] == pod.UID {
									return fmt.Errorf("pod %s was not recreated yet", pod.Name)
								}
							}

							return nil
						}, 5*time.Minute, 10*time.Second).Should(Succeed())
				},
			)

			It(
				"Should wait for grace period before recreating pods in an existing cluster", func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

					err := updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					var (
						podList    *v1.PodList
						pendingPod *v1.Pod
					)

					Eventually(
						func() error {
							podList, err = getPodList(aeroCluster, k8sClient)
							if err != nil {
								return err
							}

							for idx := range podList.Items {
								pod := &podList.Items[idx]
								if pod.Status.Phase == v1.PodPending {
									pendingPod = pod
									return nil
								}
							}

							return fmt.Errorf("no pod in pending state")
						}, 5*time.Minute, 10*time.Second,
					).Should(Succeed())

					By("Waiting within grace period and verifying operator does not recreate pods")
					pkgLog.Info("Waiting 20 seconds within grace period (grace period = 60s)...")
					time.Sleep(20 * time.Second)

					podList, err = getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					By("Verifying pods are not recreated within grace period")

					for idx := range podList.Items {
						pod := &podList.Items[idx]
						if pod.Name == pendingPod.Name {
							Expect(pod.Status.Phase).To(Equal(v1.PodPending))
							Expect(pod.UID).To(Equal(pendingPod.UID))

							break
						}
					}

					By("Verifying pods are recreated after grace period")
					Eventually(
						func() bool {
							podList, err = getPodList(aeroCluster, k8sClient)
							if err != nil {
								pkgLog.Error(err, "Failed to get pod list")
								return false
							}

							for idx := range podList.Items {
								pod := &podList.Items[idx]
								if pod.Name == pendingPod.Name {
									return pod.UID != pendingPod.UID
								}
							}

							return false
						}, 5*time.Minute, 10*time.Second).Should(BeTrue())
				},
			)
		},
	)

	Context("Validate ImageUpdate", func() {
		clusterName := fmt.Sprintf("image-update-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

		AfterEach(
			func() {
				aeroCluster := &asdbv1.AerospikeCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterNamespacedName.Name,
						Namespace: clusterNamespacedName.Namespace,
					},
				}

				Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
			},
		)

		It("Should fail if server image updated from enterprise to federal", func() {
			aeroCluster := CreateAdminTLSCluster(
				clusterNamespacedName, 2,
			)
			err := DeployCluster(
				k8sClient, ctx, aeroCluster,
			)
			Expect(err).ToNot(HaveOccurred())
			By("Updating cluster image from enterprise to federal")

			aeroCluster.Spec.Image = federalImage
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("enterprise image cannot be updated to federal image"))
		})

		It("Should fail if server image updated from federal to enterprise", func() {
			aeroCluster := CreateAdminTLSCluster(
				clusterNamespacedName, 2,
			)
			aeroCluster.Spec.Image = federalImage
			err := DeployCluster(
				k8sClient, ctx, aeroCluster,
			)
			Expect(err).ToNot(HaveOccurred())
			By("Updating cluster image from federal to enterprise")

			aeroCluster.Spec.Image = latestImage
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("federal image cannot be updated to enterprise image"))
		})
	})
}
