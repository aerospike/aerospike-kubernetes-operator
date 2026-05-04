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
	"k8s.io/apimachinery/pkg/types"
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

	// Another test cluster (dynamic object name for some cluster webhook test cases).
	var cName types.NamespacedName

	BeforeEach(func() {
		cName = types.NamespacedName{}
	})
	AfterEach(func() {
		// Delete the cluster after each test
		deleteCluster(ctx, clusterNamespacedName)

		if cName.Name != "" {
			deleteCluster(ctx, cName)
		}
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
				It("rejects empty image", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.Image = "" // Empty image

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"maerospikecluster.kb.io\"",
							"failed to get server image version: image version is mandatory for image: ").
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
							"\"maerospikecluster.kb.io\"",
							"invalid image version format: latest@invalid-digest").
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
					_, nsConf := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)

					if storageEngine, ok := nsConf["storage-engine"].(map[string]interface{}); ok {
						if _, ok := storageEngine["devices"]; ok {
							devList := storageEngine["devices"].([]interface{})
							storageEngine["devices"] = append(devList, "andRandomDevice")

							err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
							Expect(err).To(HaveOccurred())

							envtests.NewStatusErrorMatcher().
								WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
									"namespace storage device related devicePath andRandomDevice not found in Storage config").
								Validate(err)
						}
					}
				})
			})
			Context("positive", func() {
				// Add positive storage tests here
			})
		})

		// aerospikeConfigNSNegativeTests registers namespace validation It blocks that
		// are valid for BOTH config formats (legacy list and new YAML map).
		// Delete the legacy context below when pre-8.1.1 server support is dropped.
		aerospikeConfigNSNegativeTests := func(image string) {
			It("rejects nil storage-engine", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
				_, nsConf := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)
				Expect(nsConf).ToNot(BeNil(), "namespace configuration must be present")

				nsConf["storage-engine"] = nil
				setFirstNSConf(aeroCluster.Spec.AerospikeConfig.Value, nsConf)
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				// Check the format-neutral part of the error; the full map
				// representation differs between list and map formats.
				if image == "aerospike/aerospike-server-enterprise:8.1.0.0" {
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"storage-engine cannot be nil for namespace map[name:test replication-factor:2 storage-engine:<nil> strong-consistency:true]").
						Validate(err)
				} else {
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"{<nil> number_one_of (root).namespaces.test.storage-engine Must validate one and only one schema (oneOf) namespaces.test.storage-engine").
						Validate(err)
				}
			})

			It("rejects 3 devices in single device string (shadow device config)", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
				_, namespaceConfig := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)

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

					_, namespaceConfig = getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)
					namespaceConfig["storage-engine"].(map[string]interface{})["devices"] =
						[]string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"invalid device name /dev/xvdf1 /dev/xvdf2 /dev/xvdf3.",
							"Max 2 device can be mentioned in single line (Shadow device config)").
						Validate(err)
				}
			})

			It("rejects device used in multiple namespaces", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
				secondNs := map[string]interface{}{
					asdbv1.ConfKeyName:   "ns1",
					"replication-factor": 2,
					"storage-engine": map[string]interface{}{
						"type":    "device",
						"devices": []interface{}{"/test/dev/xvdf"},
					},
				}

				appendNSConf(aeroCluster.Spec.AerospikeConfig.Value, secondNs)
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
						"device /test/dev/xvdf is already being referenced in multiple namespaces (ns1, test)").
					Validate(err)
			})

			It("rejects when namespace configuration is missing", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
				delete(aeroCluster.Spec.AerospikeConfig.Value, "namespaces")

				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings(
						"\"maerospikecluster.kb.io\"",
						"aerospikeConfig.namespaces cannot be nil or not present").
					Validate(err)
			})

			It("rejects when namespace configuration is empty", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
				aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil

				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings(
						"\"maerospikecluster.kb.io\"",
						"aerospikeConfig.namespaces cannot be nil").
					Validate(err)
			})

			It("rejects when replication factor exceeds cluster size for SC", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
				setNSRFInConfig(aeroCluster.Spec.AerospikeConfig.Value, "test", 3)

				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
						"strong-consistency namespace replication-factor 3 cannot be more than cluster size 1").
					Validate(err)
			})
		}

		// aerospikeConfigNegativeTests registers aerospikeConfig (non-namespace)
		// validation It blocks valid for BOTH config formats.
		aerospikeConfigNegativeTests := func(image string) {
			It("rejects empty aerospikeConfig", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
				aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings(" ",
						"\"maerospikecluster.kb.io\"",
						"spec.aerospikeConfig cannot be nil").
					Validate(err)
			})

			It("rejects invalid aerospikeConfig", func() {
				aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
				aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
					Value: map[string]interface{}{
						asdbv1.ConfKeyNamespace: "invalidConf",
					},
				}
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings(" ",
						"\"maerospikecluster.kb.io\"",
						"aerospikeConfig.namespaces not a valid namespace list or map invalidConf").
					Validate(err)
			})
		}

		// ── Legacy list format (server < 8.1.1) ──────────────────────────────
		// Delete these two Context blocks when pre-8.1.1 server support is dropped.
		Context("spec.aerospikeConfig (namespace) [legacy list format]", func() {
			Context("negative", func() {
				aerospikeConfigNSNegativeTests(testutil.Pre811EnterpriseImage)

				// Schema error messages use legacy array-index paths (namespaces.0.*).
				// These are not valid for the new YAML format and live only here.
				It("rejects nil storage-engine.device", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, testutil.Pre811EnterpriseImage)
					_, nsConf := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)
					Expect(nsConf).ToNot(BeNil())

					if storageEngine, ok := nsConf["storage-engine"].(map[string]interface{}); ok {
						storageEngine["devices"] = nil
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"{<nil> invalid_type (root).namespaces.0.storage-engine.devices Invalid type. Expected: array, given: null namespaces.0.storage-engine.devices}").
						Validate(err)
				})

				It("rejects nil storage-engine.file", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, testutil.Pre811EnterpriseImage)
					_, nsConf := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)

					if nsConf != nil {
						if storageEngine, ok := nsConf["storage-engine"].(map[string]interface{}); ok {
							storageEngine["files"] = nil
						}
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"{<nil> invalid_type (root).namespaces.0.storage-engine.files Invalid type. Expected: array, given: null namespaces.0.storage-engine.files}").
						Validate(err)
				})

				// Duplicate names are impossible with map-keyed format; only testable
				// in the legacy list format where duplicates are structurally possible.
				It("rejects duplicate namespace names in aerospikeConfig.namespaces", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, testutil.Pre811EnterpriseImage)
					firstName, _ := getFirstNSMutableRef(aeroCluster.Spec.AerospikeConfig.Value)
					dupNs := map[string]interface{}{
						asdbv1.ConfKeyName:   firstName,
						"replication-factor": 2,
						"storage-engine": map[string]interface{}{
							"type": "memory",
						},
					}
					appendNSConf(aeroCluster.Spec.AerospikeConfig.Value, dupNs)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							`duplicate name "test" in namespaces list section`).
						Validate(err)
				})

				It("rejects duplicate namespace names in aerospikeConfig.xdr.dcs namespaces", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, testutil.Pre811EnterpriseImage)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyXdr] = map[string]interface{}{
						"dcs": []interface{}{
							map[string]interface{}{
								"name":               "dc1",
								"node-address-ports": []interface{}{"aeroclusterdst-0-0 3000"},
								"namespaces": []interface{}{
									map[string]interface{}{"name": "test"},
									map[string]interface{}{"name": "test"},
								},
							},
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							`xdr: duplicate name "test" in namespaces list section`).
						Validate(err)
				})

				It("rejects duplicate dc names in aerospikeConfig.xdr.dcs", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, testutil.Pre811EnterpriseImage)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyXdr] = map[string]interface{}{
						"dcs": []interface{}{
							map[string]interface{}{
								"name":               "dc1",
								"node-address-ports": []interface{}{"aeroclusterdst-0-0 3000"},
								"namespaces": []interface{}{
									map[string]interface{}{"name": "test"},
								},
							},
							map[string]interface{}{
								"name":               "dc1",
								"node-address-ports": []interface{}{"aeroclusterdst-0-0 3000"},
								"namespaces": []interface{}{
									map[string]interface{}{"name": "test"},
								},
							},
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							`xdr: duplicate name "dc1" in dcs list section`).
						Validate(err)
				})
			})
			Context("positive", func() {
				// Add positive aerospikeConfig (namespace) tests here
			})
		})

		Context("spec.aerospikeConfig [legacy list format]", func() {
			Context("negative", func() {
				aerospikeConfigNegativeTests(testutil.Pre811EnterpriseImage)

				// Logging uses "name" key in legacy format; syslog validation
				// with the old structure is only meaningful for legacy format.
				It("rejects using syslog param with file or console logging", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, testutil.Pre811EnterpriseImage)
					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

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

		// ── New YAML map format (server >= 8.1.1) ─────────────────────────────
		Context("spec.aerospikeConfig (namespace) [new YAML map format]", func() {
			Context("negative", func() {
				aerospikeConfigNSNegativeTests(testutil.LatestEnterpriseImage)
			})
			Context("positive", func() {
				// Add positive aerospikeConfig (namespace) tests here
			})
		})

		Context("spec.aerospikeConfig [new YAML map format]", func() {
			Context("negative", func() {
				aerospikeConfigNegativeTests(testutil.LatestEnterpriseImage)
			})
			Context("positive", func() {
				// Add positive aerospikeConfig tests here
			})
		})

		Context("spec.aerospikeConfig (config format / version compatibility)", func() {
			Context("negative", func() {
				It("rejects new YAML map-format namespaces with a server older than 8.1.1", func() {
					// Use a pre-8.1.1 image (8.0.0.0); map-keyed namespaces must be rejected.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.Image = testutil.Pre810EnterpriseImage
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = map[string]interface{}{
						"test": map[string]interface{}{
							"replication-factor": 2,
							"strong-consistency": true,
							asdbv1.ConfKeyStorageEngine: map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{testutil.DefaultDevicePath},
							},
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"maerospikecluster.kb.io\"", "8.1.1").
						Validate(err)
				})
			})
		})

		// Service-config tests exercise the same validation regardless of whether
		// namespaces are in list or map format.  Separate contexts per format make
		// it easy to delete legacy coverage when pre-8.1.1 support is dropped.
		Context("spec.aerospikeConfig (service)", func() {
			BeforeEach(func() {
				envtests.GlobalWarnings.Reset()
			})

			// serviceNegativeTests registers service-config negative It blocks for
			// the given server image.
			serviceNegativeTests := func(image string) {
				It("rejects setting advertise-ipv6", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["advertise-ipv6"] = true

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"advertise-ipv6 is not supported").
						Validate(err)
				})

				It("rejects setting node-id", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a1"

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" \"maerospikecluster.kb.io\"",
							"failed to set default aerospikeConfig.service config:",
							"config node-id can not have non-default value (string a1).",
							"It will be set internally (string ENV_NODE_ID)").
						Validate(err)
				})

				It("rejects setting cluster-name", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
					serviceConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
					serviceConf[testutil.ClusterNameConfig] = testutil.ClusterNameConfig

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(" \"maerospikecluster.kb.io\"",
							"failed to set default aerospikeConfig.service config:",
							"config cluster-name can not have non-default value (string cluster-name).",
							fmt.Sprintf("It will be set internally (string %s)", clusterNamespacedName.Name)).
						Validate(err)
				})
			}

			Context("negative", func() {
				// ── Legacy list format (server < 8.1.1) ──────────────────────
				// Delete this Context block when pre-8.1.1 server support is dropped.
				Context("[legacy list format]", func() {
					serviceNegativeTests(testutil.Pre811EnterpriseImage)
				})

				// ── New YAML map format (server >= 8.1.1) ─────────────────────
				Context("[new YAML map format]", func() {
					serviceNegativeTests(testutil.LatestEnterpriseImage)
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

				It("warns when cgroup-mem-tracking is absent (version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)

					Expect(envtests.WarningK8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).To(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})

				It("warns when cgroup-mem-tracking is false (version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface {
					})[asdbv1.ConfigKeyCgroupMemTracking] = false

					Expect(envtests.WarningK8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).To(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})
			})
			Context("positive", func() {
				It("does not warn when cgroup-mem-tracking is true "+
					"(version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface {
					})[asdbv1.ConfigKeyCgroupMemTracking] = true

					Expect(envtests.WarningK8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).NotTo(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})

				It("does not warn when cgroup-mem-tracking is absent "+
					"(version < "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						clusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.Pre810ServerVersion),
					)

					Expect(envtests.WarningK8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).NotTo(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})
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

				// Projected label rune count: cluster name + 10 (3 hyphens + 7 for MaxRackID)
				// + 3 (revision placeholder) + 10 (controller-revision hash upper bound) = 23
				// fixed runes; name can be at most 40 for 63 (40+23=63).
				It("rejects cluster name longer than 40 chars (projected Pod label value would exceed 63)", func() {
					// 48 chars: 48 + 10 + 3 + 10 = 71 > 63.
					longName := strings.Repeat("a", 48)
					cName := test.GetNamespacedName(longName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"Pod label value would exceed the",
							"63-character DNS label limit",
							"revision placeholder = 3",
							"total = 71",
							"reduce by 8",
						).Validate(err)
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
					Expect(err).ToNot(HaveOccurred())
				})

				// 40 (name) + 10 (Aerospike fixed) + 3 (min revision placeholder) + 10 (K8s) = 63.
				It("accepts cluster name of exactly 40 chars (at projected Pod label value limit)", func() {
					exactName := strings.Repeat("a", 40)
					cName := test.GetNamespacedName(exactName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)

					err := envtests.K8sClient.Create(ctx, aeroCluster)

					Expect(err).To(Succeed())
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
							"maxUnavailable 2 is invalid",
							"value must be less than the minimum replication factor").
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
							"aerospikeConfig schema validation failed: config not valid for version",
							"config schema error",
							"{-1 number_gte (root).namespaces.test.rack-id Must be greater than or equal to 0 namespaces.test.rack-id}").
						Validate(err)
				})

				It("rejects negative rollingUpdateBatchSize", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
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
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
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
			// Delete the cluster after each test
			deleteCluster(ctx, updateValidationClusterNamespacedName)
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

		Context("spec.aerospikeConfig (service)", func() {
			BeforeEach(func() {
				envtests.GlobalWarnings.Reset()
			})

			Context("negative", func() {
				It("warns when cgroup-mem-tracking is removed (version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						updateValidationClusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface {
					})[asdbv1.ConfigKeyCgroupMemTracking] = true
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, updateValidationClusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					delete(current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{}),
						asdbv1.ConfigKeyCgroupMemTracking)

					Expect(envtests.WarningK8sClient.Update(ctx, current)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).To(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})

				It("warns when cgroup-mem-tracking is set to false "+
					"(version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						updateValidationClusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface {
					})[asdbv1.ConfigKeyCgroupMemTracking] = true
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, updateValidationClusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.AerospikeConfig.
						Value[asdbv1.ConfKeyService].(map[string]interface{})[asdbv1.ConfigKeyCgroupMemTracking] = false

					Expect(envtests.WarningK8sClient.Update(ctx, current)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).To(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})
			})

			Context("positive", func() {
				It("does not warn when cgroup-mem-tracking remains true "+
					"(version >= "+testutil.CgroupMemTrackingServerVersion+")", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(
						updateValidationClusterNamespacedName, 1, testutil.GetEnterpriseImage(testutil.CgroupMemTrackingServerVersion),
					)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface {
					})[asdbv1.ConfigKeyCgroupMemTracking] = true
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, updateValidationClusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.Size = 1

					Expect(envtests.WarningK8sClient.Update(ctx, current)).ToNot(HaveOccurred())
					Expect(envtests.GlobalWarnings.Warnings).NotTo(ContainElement(ContainSubstring(asdbv1.ConfigKeyCgroupMemTracking)))
				})
			})
		})
	})
})
