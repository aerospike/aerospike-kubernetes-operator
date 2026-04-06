package cluster

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeCluster validation", func() {
	const (
		clusterName = "invalid-cluster"
	)

	ctx := context.TODO()

	// Create namespaced name for cluster
	clusterNamespacedName := uniqueNamespacedName(clusterName)

	AfterEach(func() {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterNamespacedName.Name,
				Namespace: clusterNamespacedName.Namespace,
			},
		}
		// Delete the cluster after each test
		Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
	})

	Context("Deploy validation", func() {
		Context("spec.size", func() {
			Context("negative", func() {
				It("rejects zero size", func() {
					// Create AerospikeCluster with invalid size (0)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 0)

					// Deploy the cluster and expect an error due to invalid cluster size.
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)

					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"invalid cluster size 0").
						Validate(err)
				})

				It("rejects negative size", func() {
					// Create AerospikeCluster with invalid size (-1)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, -1)
					aeroCluster.Spec.Size = -1

					// Deploy the cluster and expect an error due to invalid cluster size (negative integer).
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)

					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithCauses(metav1.StatusCause{
							Type:    metav1.CauseTypeFieldValueInvalid,
							Message: "Invalid value: -1: should be a non-negative integer",
							Field:   ".spec.size",
						}).
						Validate(err)
				})

				It("rejects size above 256", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 257)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"cluster size cannot be more than 256").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive size tests here
			})
		})

		Context("spec.image", func() {
			Context("negative", func() {
				// Bug: For Empty image the error message is "CommunityEdition Cluster not supported".
				// This error messsage is not appropriate for an empty image.
				It("rejects empty image", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.Image = "" // Empty image

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"CommunityEdition Cluster not supported").
						Validate(err)
				})

				It("rejects invalid image format", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.Image = "aerospike/nosuchimage:latest@invalid-digest"

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"CommunityEdition Cluster not supported").
						Validate(err)
				})

				It("rejects image version below base (6.0.0.0)", func() {
					oldImage := testutil.GetEnterpriseImage("5.0.0.0")
					aeroCluster := testCluster.CreateAerospikeClusterPost640(clusterNamespacedName, 1, oldImage)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"image version 5.0.0.0 not supported. Base version 6.0.0.0").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive image tests here
			})
		})

		Context("spec.storage", func() {
			Context("negative", func() {
				It("rejects when volumes are missing", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.Storage.Volumes = nil // Remove volumes

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"namespace storage device related devicePath /test/dev/xvdf not found in Storage config",
							"<nil>", "deleteFiles deleteFiles false}",
							"{<nil> <nil>", "none dd false} 1 [] <nil> []}").
						Validate(err)
				})

				It("rejects when device is not in storage config", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})

					namespaceConfig := rawNs[0].(map[string]interface{})
					if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
						devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
						devList = append(
							devList, "andRandomDevice",
						)
						namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
						err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())

						// Webhook response validation
						envtests.NewStatusErrorMatcher().
							WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
								"namespace storage device related devicePath andRandomDevice not found in Storage config").
							Validate(err)
					}
				})
			})
			Context("positive", func() {
				// Add positive storage tests here
			})
		})

		Context("spec.aerospikeConfig (namespace)", func() {
			Context("negative", func() {
				It("rejects nil storage-engine", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace]

					namespaces, ok := rawNs.([]interface{})
					if !ok || len(namespaces) == 0 {
						Fail("Namespace configuration is missing or not a slice")
					}

					namespaceConfig := namespaces[0].(map[string]interface{})
					namespaceConfig["storage-engine"] = nil
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"storage-engine cannot be nil for namespace map[name:test replication-factor:2 storage-engine:<nil>",
							"strong-consistency:true]").
						Validate(err)
				})

				It("rejects nil storage-engine.device", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace]

					namespaces, ok := rawNs.([]interface{})
					if !ok || len(namespaces) == 0 {
						Fail("Namespace configuration is missing or not a slice")
					}

					namespaceConfig := namespaces[0].(map[string]interface{})
					if storageEngine, ok := namespaceConfig["storage-engine"].(map[string]interface{}); ok {
						// Force the invalid state
						storageEngine["devices"] = nil

						// Re-assign back up the chain to ensure the pointer/reference is updated
						namespaceConfig["storage-engine"] = storageEngine
					}

					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"aerospikeConfig not valid: generated config not valid for version",
							"config schema error",
							"{map[devices:<nil> type:device] number_one_of (root).",
							"namespaces.0.storage-engine Must validate one and only one schema (oneOf) namespaces.0.storage-engine}",
							"{<nil> invalid_type (root).namespaces.0.storage-engine.devices Invalid type.",
							"Expected: array, given: null namespaces.0.storage-engine.devices}").
						Validate(err)
				})

				It("rejects nil storage-engine.file", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)

					// Navigate the AerospikeConfig.Value structure
					config := aeroCluster.Spec.AerospikeConfig.Value
					if namespaces, ok := config[asdbv1.ConfKeyNamespace].([]interface{}); ok && len(namespaces) > 0 {
						ns := namespaces[0].(map[string]interface{})

						if storageEngine, ok := ns["storage-engine"].(map[string]interface{}); ok {
							// Force the invalid state
							storageEngine["files"] = nil

							// Re-assign back up the chain to ensure the pointer/reference is updated
							ns["storage-engine"] = storageEngine
							namespaces[0] = ns
							config[asdbv1.ConfKeyNamespace] = namespaces
						}
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							" aerospikeConfig not valid: generated config not valid for version",
							"config schema error [\t{map[devices:[/test/dev/xvdf] files:<nil> type:device]",
							"number_one_of (root).namespaces.0.storage-engine Must validate one and only one schema",
							"(oneOf) namespaces.0.storage-engine}\n \t{map[devices:[/test/dev/xvdf] files:<nil> type:device]",
							"number_one_of (root).namespaces.0.storage-engine Must validate one and only one schema",
							"(oneOf) namespaces.0.storage-engine}\n \t{<nil> invalid_type (root).namespaces.0.storage-engine.",
							"files Invalid type. Expected: array, given: null namespaces.0.storage-engine.files}\n]").
						Validate(err)
				})

				It("rejects 3 devices in single device string (shadow device config)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})

					namespaceConfig := rawNs[0].(map[string]interface{})
					if _, ok :=
						namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
						aeroCluster.Spec.Storage.Volumes = []asdbv1.VolumeSpec{
							{
								Name: "nsvol1",
								Source: asdbv1.VolumeSource{
									PersistentVolume: &asdbv1.PersistentVolumeSpec{
										Size:         resource.MustParse("1Gi"),
										StorageClass: testutil.StorageClass,
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
										StorageClass: testutil.StorageClass,
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
										StorageClass: testutil.StorageClass,
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
						err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())

						// Webhook response validation
						envtests.NewStatusErrorMatcher().
							WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
								"invalid device name /dev/xvdf1 /dev/xvdf2 /dev/xvdf3.",
								"Max 2 device can be mentioned in single line (Shadow device config)").
							Validate(err)
					}
				})

				It("rejects device used in multiple namespaces", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
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
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"device /test/dev/xvdf is already being referenced in multiple namespaces (test, ns1)").
						Validate(err)
				})

				// Bug: aerospikeConfig map should not be listed in Failure message.
				// It is making failure message unnecessarily long.
				It("rejects when namespace configuration is missing", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					// Remove namespaces from AerospikeConfigSpec
					delete(aeroCluster.Spec.AerospikeConfig.Value, "namespaces")

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"maerospikecluster.kb.io\"",
							"aerospikeConfig.namespaces not present.").
						Validate(err)
				})

				It("rejects when namespace configuration is empty", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)

					// Set Namespace to nil
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"maerospikecluster.kb.io\"",
							"aerospikeConfig.namespaces cannot be nil").
						Validate(err)
				})

				It("rejects when replication factor exceeds cluster size for SC", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1) // Size 1

					// 1. Get the namespaces slice from the Value map
					rawNamespaces := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})

					// 2. Modify the first namespace's replication factor
					if len(rawNamespaces) > 0 {
						nsMap := rawNamespaces[0].(map[string]interface{})
						nsMap["replication-factor"] = 3
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"strong-consistency namespace replication-factor 3 cannot be more than cluster size 1").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive aerospikeConfig (namespace) tests here
			})
		})

		Context("spec.aerospikeConfig", func() {
			Context("negative", func() {
				It("rejects empty aerospikeConfig", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" ",
							"\"maerospikecluster.kb.io\"",
							"spec.aerospikeConfig cannot be nil").
						Validate(err)
				})

				It("rejects invalid aerospikeConfig", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							asdbv1.ConfKeyNamespace: "invalidConf",
						},
					}
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" ",
							"\"maerospikecluster.kb.io\"",
							"aerospikeConfig.namespaces not valid namespace list invalidConf").
						Validate(err)
				})

				It("rejects using syslog param with file or console logging", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"can use facility only with `syslog` in aerospikeConfig.logging",
							"map[facility:local0 name:anyFileName path:/dev/log tag:asd]").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive aerospikeConfig tests here
			})
		})

		Context("spec.aerospikeConfig (service)", func() {
			Context("negative", func() {
				It("rejects setting advertise-ipv6", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["advertise-ipv6"] = true

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"advertise-ipv6 is not supported").
						Validate(err)
				})

				It("rejects setting node-id", func() {
					// Service conf: "node-id"
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a1"

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" \"maerospikecluster.kb.io\"",
							"failed to set default aerospikeConfig.service config:",
							"config node-id can not have non-default value (string a1).",
							"It will be set internally (string ENV_NODE_ID)").
						Validate(err)
				})

				It("rejects setting cluster-name", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					// 1. Extract the service configuration map
					serviceConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})

					// 2. Assign the value to the specific key
					serviceConf[testutil.ClusterNameConfig] = testutil.ClusterNameConfig

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" \"maerospikecluster.kb.io\"",
							"failed to set default aerospikeConfig.service config:",
							"config cluster-name can not have non-default value (string cluster-name).",
							fmt.Sprintf("It will be set internally (string %s)", clusterNamespacedName.Name)).
						Validate(err)
				})

				It("rejects feature-key-file path not in storage volumes", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService] = map[string]interface{}{
						"feature-key-file": "/randompath/features.conf",
					}
					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"feature-key-file paths or tls paths or default-password-file path are not mounted",
							"- create an entry for '/randompath/features.conf' in 'storage.volumes'").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive aerospikeConfig (service) tests here
			})
		})

		Context("spec.podSpec", func() {
			Context("negative", func() {
				It("rejects dnsPolicy Default", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					defaultDNS := v1.DNSDefault
					aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"dnsPolicy: Default is not supported").
						Validate(err)
				})

				It("rejects dnsPolicy None without dnsConfig", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					noneDNS := v1.DNSNone
					aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"dnsConfig is required field when dnsPolicy is set to None").
						Validate(err)
				})

				It("rejects hostNetwork and MultiPodPerHost both enabled", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.PodSpec.HostNetwork = true
					aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(true)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"host networking cannot be enabled with multi pod per host").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive podSpec tests here
			})
		})

		Context("spec.operatorClientCertSpec", func() {
			Context("negative", func() {
				It("rejects both SecretCertSource and CertPathInOperator set", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator = &asdbv1.AerospikeCertPathInOperatorSource{}

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"either `secretCertSource` or `certPathInOperator` must be set in `operatorClientCertSpec` but not both").
						Validate(err)
				})

				It("rejects missing ClientKeyFilename", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.ClientKeyFilename = ""

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"both `clientCertFilename` and `clientKeyFilename` should be either set or not set in `secretCertSource`").
						Validate(err)
				})

				It("rejects both CaCertsFilename and CaCertsSource set", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.CaCertsSource = &asdbv1.CaCertsSource{}

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"both `caCertsFilename` or `caCertsSource` cannot be set in `secretCertSource`").
						Validate(err)
				})

				It("rejects missing clientCertPath", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource = nil
					aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator =
						&asdbv1.AerospikeCertPathInOperatorSource{
							CaCertsPath:    "cacert.pem",
							ClientKeyPath:  "svc_key.pem",
							ClientCertPath: "",
						}

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"both `clientCertPath` and `clientKeyPath` should be either set or not set in `certPathInOperator`").
						Validate(err)
				})

				It("rejects when operator client cert is not configured", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
					)
					aeroCluster.Spec.OperatorClientCertSpec = nil

					// Deploy cluster
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"operator client cert is not specified").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive operatorClientCertSpec tests here
			})
		})

		Context("metadata", func() {
			Context("negative", func() {
				It("rejects empty cluster name", func() {
					cName := test.GetNamespacedName("", clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"\"\" is invalid:",
							"metadata.name: Required value: name or generateName is required").
						WithCauses(metav1.StatusCause{
							Type:    metav1.CauseTypeFieldValueRequired,
							Message: "Required value: name or generateName is required",
							Field:   "metadata.name",
						}).
						Validate(err)
				})

				It("rejects empty namespace name", func() {
					cName := test.GetNamespacedName(clusterNamespacedName.Name, "")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// err is not a Kubernetes StatusError,
					// so we need to check the error message
					Expect(err.Error()).To(Or(
						ContainSubstring("an empty namespace may not be set during creation"),
					))
				})

				It("rejects cluster name with spaces", func() {
					cName := test.GetNamespacedName("my cluster", clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com \"my cluster\" is invalid:",
							"metadata.name: Invalid value: \"my cluster\": a lowercase RFC 1123 subdomain ",
							"must consist of lower case alphanumeric characters,",
							"'-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com',",
							"regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')").
						Validate(err)
				})

				It("rejects cluster name with uppercase characters", func() {
					cName := test.GetNamespacedName("MyCluster", clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com \"MyCluster\" is invalid:",
							"metadata.name: Invalid value: \"MyCluster\": a lowercase RFC 1123 subdomain",
							"must consist of lower case alphanumeric characters,",
							"'-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com',",
							"regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')").
						Validate(err)
				})

				// cluster.Name is used as the headless-service name (and LB service base).
				// Kubernetes validates service names as DNS-1035 labels, which requires
				// the first character to be a lowercase letter — digits are not allowed.
				// cluster.Name is immutable in Kubernetes, so this is checked at CREATE only.
				It("rejects cluster name starting with a digit (invalid DNS-1035 service name)", func() {
					cName := test.GetNamespacedName("1mycluster", clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"cluster name \"1mycluster\" is not a valid Kubernetes service name (DNS-1035)",
							"start with an alphabetic character").
						Validate(err)
				})

				// The maximum-scale pod name overhead is 16 chars ("-1000000-aaa-255"),
				// so cluster.Name must be ≤ 47 chars. This subsumes both service name
				// checks (headless = cluster.Name, LB = cluster.Name+"-lb"), since
				// their overheads (0 and 3 chars) are smaller than 16.
				// Any cluster name > 47 chars is caught by the pod name check.
				It("rejects cluster name longer than 47 chars (projected pod name would exceed 63)", func() {
					// 48 chars: 48 + 16 = 64 → invalid DNS label.
					longName := strings.Repeat("a", 48)
					cName := test.GetNamespacedName(longName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"computed at max rack ID",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})

				It("rejects when cluster namespace is not found", func() {
					cName := test.GetNamespacedName(clusterNamespacedName.Name, "default ns")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("namespaces \"default ns\" not found").
						Validate(err)
				})
			})
			Context("positive", func() {
				It("accepts cluster name starting with a lowercase letter", func() {
					cName := test.GetNamespacedName("valid-cluster", clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := envtests.K8sClient.Create(ctx, aeroCluster)

					defer func() { _ = envtests.K8sClient.Delete(ctx, aeroCluster) }()

					Expect(err).ToNot(HaveOccurred())
				})

				// overhead = "-1000000-aaa-255" = 16 chars; cluster.Name ≤ 47 chars is safe.
				// cluster name exactly at the 47-char limit:
				// 47 + "-1000000-aaa-255" (16) = 63 → exactly at DNS label max.
				It("accepts cluster name of exactly 47 chars (projected pod name = 63 chars)", func() {
					exactName := strings.Repeat("a", 47)
					cName := test.GetNamespacedName(exactName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := envtests.K8sClient.Create(ctx, aeroCluster)

					defer func() { _ = envtests.K8sClient.Delete(ctx, aeroCluster) }()

					Expect(err).ToNot(HaveOccurred())
				})
			})
		})

		Context("spec.maxUnavailable", func() {
			Context("negative", func() {
				// Bug: Improve response message string to indicate acceptable value is 1 and not 2.
				// Current string "maxUnavailable 2 cannot be greater than or equal to 2" is not a clear message
				It("rejects maxUnavailable >= RF across all namespaces", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					maxUnav := intstr.FromInt32(2)
					aeroCluster.Spec.MaxUnavailable = &maxUnav
					// Ensure PDB is not disabled so MaxUnavailable is validated
					aeroCluster.Spec.DisablePDB = nil

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"maxUnavailable 2 cannot be greater than or equal to 2 ",
							"as it may result in data loss. Set it to a lower value").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive maxUnavailable tests here
			})
		})

		Context("spec.aerospikeAccessControl", func() {
			Context("negative", func() {
				It("rejects when security is disabled but access control is specified", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					// Remove security config so IsSecurityEnabled returns false
					delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)
					// AerospikeAccessControl is already set on the dummy cluster; keep it set

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"security is disabled but access control is specified").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive aerospikeAccessControl tests here
			})
		})

		Context("spec.rackConfig", func() {
			Context("negative", func() {
				It("rejects negative rack ID", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					// Add a rack with an ID that is too large or invalid structure
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: -1}, // Negative rack ID
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"aerospikeConfig not valid: generated config not valid for version",
							"config schema error",
							"{-1 number_gte (root).namespaces.0.rack-id Must be greater than or equal to 0 namespaces.0.rack-id}").
						Validate(err)
				})

				It("rejects negative rollingUpdateBatchSize", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					negVal := intstr.FromInt32(-1)
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &negVal

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"can not use negative spec.rackConfig.rollingUpdateBatchSize: -1").
						Validate(err)
				})

				It("rejects negative scaleDownBatchSize", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					negVal := intstr.FromInt32(-1)
					aeroCluster.Spec.RackConfig.ScaleDownBatchSize = &negVal

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"can not use negative spec.rackConfig.scaleDownBatchSize: -1").
						Validate(err)
				})

				It("rejects namespace name in rackConfig.Namespaces containing spaces", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test ns"},
						Racks:      []asdbv1.Rack{{ID: 1}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"namespace name `test ns` cannot have spaces",
							"Namespaces [test ns]").
						Validate(err)
				})
			})
		})
	})

	Context("Update validation", func() {
		const updateValidationClusterName = "update-validation-cluster"

		updateValidationClusterNamespacedName := uniqueNamespacedName(updateValidationClusterName)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateValidationClusterNamespacedName.Name,
					Namespace: updateValidationClusterNamespacedName.Namespace,
				},
			}
			Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		Context("spec.storage", func() {
			Context("negative", func() {
				It("rejects when storage config is updated", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(updateValidationClusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, updateValidationClusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change storage (e.g. StorageClass) to trigger "storage config cannot be updated"
					for i := range current.Spec.Storage.Volumes {
						if current.Spec.Storage.Volumes[i].Source.PersistentVolume != nil {
							current.Spec.Storage.Volumes[i].Source.PersistentVolume.StorageClass = "other-storage-class"
							break
						}
					}

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"denied the request:",
							"storage config cannot be updated",
							"cannot change volumes").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive update storage tests here
			})
		})

		Context("spec.podSpec", func() {
			Context("negative", func() {
				It("rejects when MultiPodPerHost is changed", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(updateValidationClusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, updateValidationClusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Toggle MultiPodPerHost to trigger "cannot update MultiPodPerHost setting"
					current.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"denied the request:",
							"cannot update MultiPodPerHost setting").
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive update podSpec tests here
			})
		})
	})
})
