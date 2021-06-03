package test

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// var _ = Describe("TestAerospikeCluster", func() {

// 	ctx := goctx.TODO()

// 	// Cluster lifecycle related
// 	Context("DeployClusterPost460", func() {
// 		DeployClusterForAllImagesPost460(ctx)
// 	})
// 	Context("DeployClusterDiffStorageMultiPodPerHost", func() {
// 		DeployClusterForDiffStorageTest(ctx, 2, true)
// 	})
// 	Context("DeployClusterDiffStorageSinglePodPerHost", func() {
// 		DeployClusterForDiffStorageTest(ctx, 2, false)
// 	})
// 	Context("CommonNegativeClusterValidationTest", func() {
// 		NegativeClusterValidationTest(ctx)
// 	})
// 	Context("UpdateCluster", func() {
// 		UpdateClusterTest(ctx)
// 	})
// })

var (
	ctx = goctx.TODO()
)

// Cluster lifecycle related
var _ = Describe("DeployClusterPost460", func() {
	DeployClusterForAllImagesPost460(ctx)
})
var _ = Describe("DeployClusterDiffStorageMultiPodPerHost", func() {
	DeployClusterForDiffStorageTest(ctx, 2, true)
})

// var _ = FDescribe("DeployClusterDiffStorageSinglePodPerHost", func() {
// 	DeployClusterForDiffStorageTest(ctx, 2, false)
// })
var _ = Describe("CommonNegativeClusterValidationTest", func() {
	NegativeClusterValidationTest(ctx)
})
var _ = Describe("UpdateCluster", func() {
	UpdateClusterTest(ctx)
})

// Test cluster deployment with all image post 4.6.0
func DeployClusterForAllImagesPost460(ctx goctx.Context) {
	// get namespace
	// namespace, err := ctx.GetNamespace()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// post 4.6.0, need feature-key file
	versions := []string{"5.5.0.3", "5.4.0.5", "5.3.0.10", "5.2.0.17", "5.1.0.25", "5.0.0.21", "4.9.0.11"}
	// versions := []string{"5.2.0.17", "5.1.0.25", "5.0.0.21", "4.9.0.11", "4.8.0.6", "4.8.0.1", "4.7.0.11", "4.7.0.2", "4.6.0.13", "4.6.0.2"}

	for _, v := range versions {

		It(fmt.Sprintf("Deploy-%s", v), func() {
			clusterName := "deploy-cluster"
			clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

			image := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", v)
			aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, image)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			// time.Sleep(time.Minute * 20)

			deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	}
}

// Test cluster deployment with different namespace storage
func DeployClusterForDiffStorageTest(ctx goctx.Context, nHosts int32, multiPodPerHost bool) {
	clusterSz := nHosts
	if multiPodPerHost {
		clusterSz++
	}
	repFact := nHosts
	// get namespace
	// namespace, err := ctx.GetNamespace()
	// if err != nil {
	// 	t.Fatal(err)
	// }

	Context("Positive", func() {
		// Cluster with n nodes, enterprise can be more than 8
		// Cluster with resources
		// Verify: Connect with cluster

		// Namespace storage configs
		//
		// SSD Storage Engine
		It("SSDStorageCluster", func() {
			clusterNamespacedName := getClusterNamespacedName("ssdstoragecluster", namespace)
			aeroCluster := createSSDStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		// HDD Storage Engine with Data in Memory
		It("HDDAndDataInMemStorageCluster", func() {
			clusterNamespacedName := getClusterNamespacedName("inmemstoragecluster", namespace)

			aeroCluster := createHDDAndDataInMemStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
		// HDD Storage Engine with Data in Index Engine
		It("HDDAndDataInIndexStorageCluster", func() {
			clusterNamespacedName := getClusterNamespacedName("datainindexcluster", namespace)

			aeroCluster := createHDDAndDataInIndexStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
		// Data in Memory Without Persistence
		It("DataInMemWithoutPersistentStorageCluster", func() {
			clusterNamespacedName := getClusterNamespacedName("nopersistentcluster", namespace)

			aeroCluster := createDataInMemWithoutPersistentStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
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

	})
}

// Test cluster cr updation
func UpdateClusterTest(ctx goctx.Context) {
	// get namespace
	// namespace, err := ctx.GetNamespace()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	clusterName := "update-cluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	Context("Positive", func() {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		It("Deploy", func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
		It("ScaleUp", func() {
			err := scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 1)
			Expect(err).ToNot(HaveOccurred())
		})
		It("ScaleDown", func() {
			// TODO:
			// How to check if it is checking cluster stability before killing node
			// Check if tip-clear, alumni-reset is done or not
			err := scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 1)
			Expect(err).ToNot(HaveOccurred())

		})
		It("RollingRestart", func() {
			// TODO: How to check if it is checking cluster stability before killing node
			err := rollingRestartClusterTest(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

		})
		It("Upgrade/Downgrade", func() {
			// TODO: How to check if it is checking cluster stability before killing node
			// dont change image, it upgrade, check old version
			err := upgradeClusterTest(k8sClient, ctx, clusterNamespacedName, imageToUpgrade)
			Expect(err).ToNot(HaveOccurred())

		})

		It("It should remove cluster", func() {
			deleteCluster(k8sClient, ctx, aeroCluster)
		})
	})

	Context("Negative", func() {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		It("Deploy", func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
		Context("ValidateUpdate", func() {
			// TODO: No jump version yet but will be used
			// It("Image", func() {
			// 	old := aeroCluster.Spec.Image
			// 	aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:4.0.0.5"
			// 	err = k8sClient.Update(goctx.TODO(), aeroCluster)
			// 	validateError(err, "should fail for upgrading to jump version")
			// 	aeroCluster.Spec.Image = old
			// })
			It("MultiPodPerHost: should fail for updating MultiPodPerHost. Cannot be updated", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.MultiPodPerHost = !aeroCluster.Spec.MultiPodPerHost

				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("StorageValidation: should fail for updating Storage. Cannot be updated", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				new := []asdbv1alpha1.AerospikePersistentVolumeSpec{
					asdbv1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/dev/xvdf2",
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
						SizeInGB:     1,
					},
					asdbv1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aeropsike/ns1",
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
						SizeInGB:     1,
					},
				}
				aeroCluster.Spec.Storage.Volumes = new

				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			Context("AerospikeConfig", func() {
				Context("Namespace", func() {
					It("UpdateNamespaceList: should fail for updating namespace list. Cannot be updated", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
						nsList = append(nsList, map[string]interface{}{
							"name":        "bar",
							"memory-size": 2000955200,
							"storage-engine": map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{"/test/dev/xvdf"},
							},
						})
						aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).Should(HaveOccurred())

					})
					It("UpdateStorageEngine: should fail for updating namespace storage-engine. Cannot be updated", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["type"] = "memory"

						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).Should(HaveOccurred())
					})
					It("UpdateReplicationFactor: should fail for updating namespace replication-factor. Cannot be updated", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 5

						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).Should(HaveOccurred())
					})
				})
			})
		})

		It("It should remove cluster", func() {
			deleteCluster(k8sClient, ctx, aeroCluster)
		})
	})
}

// Test cluster validation Common for deployment and update both
func NegativeClusterValidationTest(ctx goctx.Context) {
	// get namespace
	// namespace, err := ctx.GetNamespace()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	clusterName := "cluster-diffstorage"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	Context("NegativeDeployClusterValidationTest", func() {
		negativeDeployClusterValidationTest(ctx, clusterNamespacedName)
	})

	Context("NegativeUpdateClusterValidationTest", func() {
		negativeUpdateClusterValidationTest(ctx, clusterNamespacedName)
	})
}

func negativeDeployClusterValidationTest(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	Context("Validation", func() {
		It("EmptyClusterName: should fail for EmptyClusterName", func() {
			cName := getClusterNamespacedName("", clusterNamespacedName.Namespace)

			aeroCluster := createDummyAerospikeCluster(cName, 1)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())
		})
		It("EmptyNamespaceName: should fail for EmptyNamespaceName", func() {
			cName := getClusterNamespacedName("validclustername", "")

			aeroCluster := createDummyAerospikeCluster(cName, 1)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())
		})
		It("InvalidImage: should fail for InvalidImage", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.Image = "InvalidImage"
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())

			aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:3.0.0.4"
			err = deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())
		})
		It("InvalidSize: should fail for zero size", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 0)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())

			// aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 9)
			// err = deployCluster(k8sClient, ctx, aeroCluster)
			// validateError(err, "should fail for community eidition having more than 8 nodes")
		})
		Context("InvalidAerospikeConfig: should fail for empty/invalid aerospikeConfig", func() {
			It("should fail for empty/invalid aerospikeConfig", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
				aeroCluster.Spec.AerospikeConfig = &asdbv1alpha1.AerospikeConfigSpec{}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())

				aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
				aeroCluster.Spec.AerospikeConfig = &asdbv1alpha1.AerospikeConfigSpec{
					Value: map[string]interface{}{
						"namespaces": "invalidConf",
					},
				}
				err = deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())

			})

			Context("InvalidNamespace", func() {
				It("NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace", func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nil
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				It("InvalidReplicationFactor: should fail for replication-factor greater than node sz", func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 3
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				// Should we test for overridden fields
				Context("InvalidStorage", func() {
					It("NilStorageEngine: should fail for nil storage-engine", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"] = nil
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					})

					It("NilStorageEngineDevice: should fail for nil storage-engine.device", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"] = nil
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("InvalidStorageEngineDevice: should fail for invalid storage-engine.device, cannot have 3 devices in single device string", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
								asdbv1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf1",
									SizeInGB:     1,
									StorageClass: storageClass,
									VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
								},
								asdbv1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf2",
									SizeInGB:     1,
									StorageClass: storageClass,
									VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
								},
								asdbv1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf3",
									SizeInGB:     1,
									StorageClass: storageClass,
									VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
								},
							}

							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"] = []string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("NilStorageEngineFile: should fail for nil storage-engine.file", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["files"]; ok {
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["files"] = nil
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("ExtraStorageEngineDevice: should fail for invalid storage-engine.device, cannot a device which doesn't exist in storage", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							devList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"].([]interface{})
							devList = append(devList, "andRandomDevice")
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"] = devList
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("InvalidxdrConfig: should fail for invalid xdr config. mountPath for digestlog not present in storage", func() {
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.Storage = asdbv1alpha1.AerospikeStorageSpec{}
							aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})
				})
			})

			Context("ChangeDefaultConfig", func() {
				It("NsConf", func() {
					// Ns conf
					// Rack-id
					// aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					// aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["rack-id"] = 1
					// aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					// err := deployCluster(k8sClient, ctx, aeroCluster)
					// validateError(err, "should fail for setting rack-id")
				})

				It("ServiceConf: should fail for setting node-id/cluster-name", func() {
					// Service conf
					// 	"node-id"
					// 	"cluster-name"
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["node-id"] = "a1"
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
					err = deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				It("NetworkConf: should fail for setting network conf/tls network conf", func() {
					// Network conf
					// "port"
					// "access-port"
					// "access-addresses"
					// "alternate-access-port"
					// "alternate-access-addresses"
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					networkConf := map[string]interface{}{
						"service": map[string]interface{}{
							"port":             3000,
							"access-addresses": []string{"<access_addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-addresses"
					// "tls-alternate-access-port"
					// "tls-alternate-access-addresses"
					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					networkConf = map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":             "aerospike-a-0.test-runner",
							"tls-port":             3001,
							"tls-access-addresses": []string{"<tls-access-addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
					err = deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				// Logging conf
				// XDR conf
			})
		})

		Context("InvalidAerospikeConfigSecret", func() {
			It("WhenFeatureKeyExist: should fail for empty aerospikeConfigSecret when feature-key-file exist", func() {
				aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 1, latestClusterImage)
				aeroCluster.Spec.AerospikeConfigSecret = asdbv1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{
					"feature-key-file": "/opt/aerospike/features.conf",
				}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("WhenTLSExist: should fail for empty aerospikeConfigSecret when tls exist", func() {
				aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 1, latestClusterImage)
				aeroCluster.Spec.AerospikeConfigSecret = asdbv1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"name": "aerospike-a-0.test-runner",
						},
					},
				}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
		})
	})
}

func negativeUpdateClusterValidationTest(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	// Will be used in Update
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)

	It("Deploy", func() {
		err := deployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Validation", func() {

		It("InvalidImage: should fail for InvalidImage, should fail for image lower than base", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Image = "InvalidImage"
			err = k8sClient.Update(goctx.TODO(), aeroCluster)
			Expect(err).Should(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:3.0.0.4"
			err = k8sClient.Update(goctx.TODO(), aeroCluster)
			Expect(err).Should(HaveOccurred())
		})
		It("InvalidSize: should fail for zero size", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = 0
			err = k8sClient.Update(goctx.TODO(), aeroCluster)
			Expect(err).Should(HaveOccurred())

			// aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 9)
			// err = deployCluster(k8sClient, ctx, aeroCluster)
			// validateError(err, "should fail for community eidition having more than 8 nodes")
		})
		Context("InvalidAerospikeConfig: should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig", func() {
			It("should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.AerospikeConfig = &asdbv1alpha1.AerospikeConfigSpec{}
				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.AerospikeConfig = &asdbv1alpha1.AerospikeConfigSpec{
					Value: map[string]interface{}{
						"namespaces": "invalidConf",
					},
				}
				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())

			})

			Context("InvalidNamespace", func() {
				It("NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nil
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				It("InvalidReplicationFactor: should fail for replication-factor greater than node sz", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 30
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				// Should we test for overridden fields
				Context("InvalidStorage", func() {
					It("NilStorageEngine: should fail for nil storage-engine", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"] = nil
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).Should(HaveOccurred())
					})

					It("NilStorageEngineDevice: should fail for nil storage-engine.device", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"] = nil
							err = k8sClient.Update(goctx.TODO(), aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("NilStorageEngineFile: should fail for nil storage-engine.file", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["files"]; ok {
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["files"] = nil
							err = k8sClient.Update(goctx.TODO(), aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("ExtraStorageEngineDevice: should fail for invalid storage-engine.device, cannot add a device which doesn't exist in BlockStorage", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							devList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"].([]interface{})
							devList = append(devList, "andRandomDevice")
							aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"] = devList
							err = k8sClient.Update(goctx.TODO(), aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})

					It("InvalidxdrConfig: should fail for invalid xdr config. mountPath for digestlog not present in fileStorage", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						if _, ok := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "randomPath 100G",
							}
							err = k8sClient.Update(goctx.TODO(), aeroCluster)
							Expect(err).Should(HaveOccurred())
						}
					})
				})
			})

			Context("ChangeDefaultConfig", func() {
				It("NsConf", func() {
					// Ns conf
					// Rack-id
					// aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					// aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					// // Rack-id is checked only for rack enabled namespaces.
					// aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["rack-id"] = 1
					// err = k8sClient.Update(goctx.TODO(), aeroCluster)
					// validateError(err, "should fail for setting rack-id")
				})

				It("ServiceConf: should fail for setting node-id, should fail for setting cluster-name", func() {
					// Service conf
					// 	"node-id"
					// 	"cluster-name"
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["node-id"] = "a10"
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				It("NetworkConf: should fail for setting network conf, should fail for setting tls network conf", func() {
					// Network conf
					// "port"
					// "access-port"
					// "access-addresses"
					// "alternate-access-port"
					// "alternate-access-addresses"
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					networkConf := map[string]interface{}{
						"service": map[string]interface{}{
							"port":             3000,
							"access-addresses": []string{"<access_addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-addresses"
					// "tls-alternate-access-port"
					// "tls-alternate-access-addresses"
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					networkConf = map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":             "aerospike-a-0.test-runner",
							"tls-port":             3001,
							"tls-access-addresses": []string{"<tls-access-addresses>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
					err = k8sClient.Update(goctx.TODO(), aeroCluster)
					Expect(err).Should(HaveOccurred())
				})

				// Logging conf
				// XDR conf
			})
		})

		Context("InvalidAerospikeConfigSecret", func() {
			It("Deploy", func() {
				err := deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Will be used in Update
				aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, latestClusterImage)
				err = deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

			})

			It("WhenFeatureKeyExist: should fail for empty aerospikeConfigSecret when feature-key-file exist", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.AerospikeConfigSecret = asdbv1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{
					"feature-key-file": "/opt/aerospike/features.conf",
				}
				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("WhenTLSExist: should fail for empty aerospikeConfigSecret when tls exist", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.AerospikeConfigSecret = asdbv1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"name": "aerospike-a-0.test-runner",
						},
					},
				}
				err = k8sClient.Update(goctx.TODO(), aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
		})
	})

	It("It should remove cluster", func() {
		deleteCluster(k8sClient, ctx, aeroCluster)
	})
}
