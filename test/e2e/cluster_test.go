package e2e

import (
	goctx "context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

func TestAerospikeCluster(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Fatalf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	// Cluster lifecycle related
	t.Run("DeployClusterPost460", func(t *testing.T) {
		DeployClusterForAllBuildsPost460(t, f, ctx)
	})
	t.Run("DeployClusterDiffStorageMultiPodPerHost", func(t *testing.T) {
		DeployClusterForDiffStorageTest(t, f, ctx, 2, true)
	})
	t.Run("DeployClusterDiffStorageSinglePodPerHost", func(t *testing.T) {
		DeployClusterForDiffStorageTest(t, f, ctx, 2, false)
	})
	t.Run("CommonNegativeClusterValidationTest", func(t *testing.T) {
		NegativeClusterValidationTest(t, f, ctx)
	})
	t.Run("UpdateCluster", func(t *testing.T) {
		UpdateClusterTest(t, f, ctx)
	})

	// CPU, Mem resource related
	t.Run("ClusterResources", func(t *testing.T) {
		ClusterResourceTest(t, f, ctx)
	})

	// Rack related
	t.Run("RackEnabledCluster", func(t *testing.T) {
		RackEnabledClusterTest(t, f, ctx)
	})
	t.Run("RackManagement", func(t *testing.T) {
		RackManagementTest(t, f, ctx)
	})
	t.Run("RackAerospikeConfigUpdateTest", func(t *testing.T) {
		RackAerospikeConfigUpdateTest(t, f, ctx)
	})
	t.Run("ClusterStorageCleanUpTest", func(t *testing.T) {
		ClusterStorageCleanUpTest(t, f, ctx)
	})
	t.Run("RackUsingLocalStorageTest", func(t *testing.T) {
		RackUsingLocalStorageTest(t, f, ctx)
	})

	// Multicluster related
	t.Run("DeployMultiClusterMultiNsTest", func(t *testing.T) {
		DeployMultiClusterMultiNsTest(t, f, ctx)
	})
	t.Run("DeployMultiClusterSingleNsTest", func(t *testing.T) {
		DeployMultiClusterSingleNsTest(t, f, ctx)
	})
}

func initializeOperator(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	err := ctx.InitializeClusterResources(cleanupOption(ctx))
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}

	t.Log("Initialized cluster resource")
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Initializing configmap")
	if err := setupByUser(f, ctx); err != nil {
		t.Fatal(err)
	}

	t.Log("Initializing operator")
	// wait for aerospike-kubernetes-operator to be ready
	err = waitForOperatorDeployment(t, f.KubeClient, namespace, "aerospike-kubernetes-operator-test", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}
}

// func DeployClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
// 	t.Run("Positive", func(t *testing.T) {
// 		t.Run("DeployClusterForAllBuildsPost460", func(t *testing.T) {
// 			DeployClusterForAllBuildsPost460(t, f, ctx)
// 		})
// 		t.Run("DeployClusterForDiffStorageTest", func(t *testing.T) {
// 			DeployClusterForDiffStorageTest(t, f, ctx)
// 		})
// 	})
// 	t.Run("Negative", func(t *testing.T) {
// 		t.Run("ValidateDeployCluster", func(t *testing.T) {
// 			// get namespace
// 			namespace, err := ctx.GetNamespace()
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			clusterName := "deploycluster"

// 			negativeDeployClusterValidationTest(t, f, ctx, clusterNamespacedName)
// 		})
// 	})
// }

func ClusterResourceTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"
	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

	t.Run("Positive", func(t *testing.T) {
		t.Run("DeployClusterWithResource", func(t *testing.T) {
			// It should be greater than given in cluster namespace
			mem := resource.MustParse("6Gi")
			cpu := resource.MustParse("2000m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			}
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			validateResource(t, f, ctx, aeroCluster)
		})

		t.Run("UpdateClusterWithResource", func(t *testing.T) {
			aeroCluster = &aerospikev1alpha1.AerospikeCluster{}
			err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
			// It should be greater than given in cluster namespace
			mem := resource.MustParse("8Gi")
			cpu := resource.MustParse("2500m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			}
			if err := updateCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			validateResource(t, f, ctx, aeroCluster)
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})
	t.Run("Negative", func(t *testing.T) {
		t.Run("NoResourceRequest", func(t *testing.T) {
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{}
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for nil resource.request")
		})
		t.Run("DeployClusterWithResource", func(t *testing.T) {
			// It should be greater than given in cluster namespace
			resourceMem := resource.MustParse("8Gi")
			resourceCPU := resource.MustParse("2500m")
			limitMem := resource.MustParse("6Gi")
			limitCPU := resource.MustParse("2000m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resourceCPU,
					corev1.ResourceMemory: resourceMem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    limitCPU,
					corev1.ResourceMemory: limitMem,
				},
			}
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for request exceeding limit")
		})

		t.Run("UpdateClusterWithResource", func(t *testing.T) {
			// setup cluster to update resource
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			// It should be greater than given in cluster namespace
			resourceMem := resource.MustParse("8Gi")
			resourceCPU := resource.MustParse("2500m")
			limitMem := resource.MustParse("6Gi")
			limitCPU := resource.MustParse("2000m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resourceCPU,
					corev1.ResourceMemory: resourceMem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    limitCPU,
					corev1.ResourceMemory: limitMem,
				},
			}
			err := updateCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for request exceeding limit")
		})
		deleteCluster(t, f, ctx, aeroCluster)
	})

}

// Test cluster deployment with all build post 4.6.0
func DeployClusterForAllBuildsPost460(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// post 4.6.0, need feature-key file
	versions := []string{"4.8.0.6", "4.8.0.1", "4.7.0.11", "4.7.0.2", "4.6.0.13", "4.6.0.2"}

	for _, v := range versions {
		clusterName := "aerocluster"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		build := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", v)
		aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, build)

		t.Run(fmt.Sprintf("Deploy-%s", v), func(t *testing.T) {
			err := deployCluster(t, f, ctx, aeroCluster)
			// time.Sleep(time.Minute * 20)

			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Test cluster deployment with different namespace storage
func DeployClusterForDiffStorageTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, nHosts int32, multiPodPerHost bool) {
	clusterSz := nHosts
	if multiPodPerHost {
		clusterSz++
	}
	repFact := nHosts
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Positive", func(t *testing.T) {
		// Cluster with n nodes, enterprise can be more than 8
		// Cluster with resources
		// Verify: Connect with cluster

		// Namespace storage configs
		//
		// SSD Storage Engine
		t.Run("SSDStorageCluster", func(t *testing.T) {
			clusterNamespacedName := getClusterNamespacedName("ssdstoragecluster", namespace)
			aeroCluster := createSSDStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})

		// HDD Storage Engine with Data in Memory
		t.Run("HDDAndDataInMemStorageCluster", func(t *testing.T) {
			clusterNamespacedName := getClusterNamespacedName("inmemstoragecluster", namespace)

			aeroCluster := createHDDAndDataInMemStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// HDD Storage Engine with Data in Index Engine
		t.Run("HDDAndDataInIndexStorageCluster", func(t *testing.T) {
			clusterNamespacedName := getClusterNamespacedName("datainindexcluster", namespace)

			aeroCluster := createHDDAndDataInIndexStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// Data in Memory Without Persistence
		t.Run("DataInMemWithoutPersistentStorageCluster", func(t *testing.T) {
			clusterNamespacedName := getClusterNamespacedName("nopersistentcluster", namespace)

			aeroCluster := createDataInMemWithoutPersistentStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// Shadow Device
		// t.Run("ShadowDeviceStorageCluster", func(t *testing.T) {
		// 	aeroCluster := createShadowDeviceStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)
		// 	if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
		// 		t.Fatal(err)
		// 	}
		// 	// make info call

		// 	deleteCluster(t, f, ctx, aeroCluster)
		// })

		// Persistent Memory (pmem) Storage Engine

	})
}

// Test cluster cr updation
func UpdateClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("ScaleUp", func(t *testing.T) {
			if err := scaleUpClusterTest(t, f, ctx, clusterNamespacedName, 1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("ScaleDown", func(t *testing.T) {
			// TODO:
			// How to check if it is checking cluster stability before killing node
			// Check if tip-clear, alumni-reset is done or not
			if err := scaleDownClusterTest(t, f, ctx, clusterNamespacedName, 1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RollingRestart", func(t *testing.T) {
			// TODO: How to check if it is checking cluster stability before killing node
			if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// TODO: How to check if it is checking cluster stability before killing node
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, clusterNamespacedName, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("ValidateUpdate", func(t *testing.T) {
			// TODO: No jump version yet but will be used
			// t.Run("Build", func(t *testing.T) {
			// 	old := aeroCluster.Spec.Build
			// 	aeroCluster.Spec.Build = "aerospike/aerospike-server-enterprise:4.0.0.5"
			// 	err := f.Client.Update(goctx.TODO(), aeroCluster)
			// 	validateError(t, err, "should fail for upgrading to jump version")
			// 	aeroCluster.Spec.Build = old
			// })
			t.Run("MultiPodPerHost", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.MultiPodPerHost = !aeroCluster.Spec.MultiPodPerHost

				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating MultiPodPerHost. Cannot be updated")
			})

			t.Run("StorageValidation", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				new := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/dev/xvdf2",
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
						SizeInGB:     1,
					},
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aeropsike/ns1",
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
						SizeInGB:     1,
					},
				}
				aeroCluster.Spec.Storage.Volumes = new

				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating Storage. Cannot be updated")
			})

			t.Run("AerospikeConfig", func(t *testing.T) {
				t.Run("Namespace", func(t *testing.T) {
					t.Run("UpdateNamespaceList", func(t *testing.T) {
						aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

						nsList := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})
						nsList = append(nsList, map[string]interface{}{
							"name":        "bar",
							"memory-size": 2000955200,
							"storage-engine": map[string]interface{}{
								"device": []interface{}{"/test/dev/xvdf"},
							},
						})
						aeroCluster.Spec.AerospikeConfig["namespace"] = nsList
						err := f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace list. Cannot be updated")
					})
					t.Run("UpdateStorageEngine", func(t *testing.T) {
						aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = "memory"

						err := f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace storage-engine. Cannot be updated")
					})
					t.Run("UpdateReplicationFactor", func(t *testing.T) {
						aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 5

						err := f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace replication-factor. Cannot be updated")
					})
				})
			})
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})
}

// Test cluster validation Common for deployment and update both
func NegativeClusterValidationTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aeroclusterdiffstorage"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	t.Run("NegativeDeployClusterValidationTest", func(t *testing.T) {
		negativeDeployClusterValidationTest(t, f, ctx, clusterNamespacedName)
	})

	t.Run("NegativeUpdateClusterValidationTest", func(t *testing.T) {
		negativeUpdateClusterValidationTest(t, f, ctx, clusterNamespacedName)
	})
}

func negativeDeployClusterValidationTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) {
	t.Run("Validation", func(t *testing.T) {
		t.Run("EmptyClusterName", func(t *testing.T) {
			cName := getClusterNamespacedName("", clusterNamespacedName.Namespace)

			aeroCluster := createDummyAerospikeCluster(cName, 1)
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for EmptyClusterName")
		})
		t.Run("EmptyNamespaceName", func(t *testing.T) {
			cName := getClusterNamespacedName("validclustername", "")

			aeroCluster := createDummyAerospikeCluster(cName, 1)
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for EmptyNamespaceName")
		})
		t.Run("InvalidClusterName", func(t *testing.T) {
			// Name cannot have `-`
			cName := getClusterNamespacedName("invalid-cluster-name", clusterNamespacedName.Namespace)

			aeroCluster := createDummyAerospikeCluster(cName, 1)
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "invalid-cluster-name")
		})
		t.Run("InvalidBuild", func(t *testing.T) {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.Build = "InvalidBuild"
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for InvalidBuild")

			aeroCluster.Spec.Build = "aerospike/aerospike-server-enterprise:3.0.0.4"
			err = deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for build lower than base")
		})
		t.Run("InvalidSize", func(t *testing.T) {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 0)
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for zero size")

			// aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 9)
			// err = deployCluster(t, f, ctx, aeroCluster)
			// validateError(t, err, "should fail for community eidition having more than 8 nodes")
		})
		t.Run("InvalidAerospikeConfig", func(t *testing.T) {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.AerospikeConfig = aerospikev1alpha1.Values{}
			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for empty aerospikeConfig")

			aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.AerospikeConfig = aerospikev1alpha1.Values{"namespace": "invalidConf"}
			err = deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for invalid aerospikeConfig")

			t.Run("InvalidNamespace", func(t *testing.T) {
				t.Run("NilAerospikeNamespace", func(t *testing.T) {
					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig["namespace"] = nil
					err = deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for nil aerospikeConfig.namespace")
				})

				t.Run("InvalidReplicationFactor", func(t *testing.T) {
					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 3
					err = deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for replication-factor greater than node sz")
				})

				// Should we test for overridden fields
				t.Run("InvalidStorage", func(t *testing.T) {
					t.Run("NilStorageEngine", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = nil
						err = deployCluster(t, f, ctx, aeroCluster)
						validateError(t, err, "should fail for nil storage-engine")
					})

					t.Run("NilStorageEngineDevice", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = nil
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for nil storage-engine.device")
						}
					})

					t.Run("InvalidStorageEngineDevice", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroCluster.Spec.Storage.Volumes = []aerospikev1alpha1.AerospikePersistentVolumeSpec{
								aerospikev1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf1",
									SizeInGB:     1,
									StorageClass: "ssd",
									VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
								},
								aerospikev1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf2",
									SizeInGB:     1,
									StorageClass: "ssd",
									VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
								},
								aerospikev1alpha1.AerospikePersistentVolumeSpec{
									Path:         "/dev/xvdf3",
									SizeInGB:     1,
									StorageClass: "ssd",
									VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
								},
							}

							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = []string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot have 3 devices in single device string")
						}
					})

					t.Run("NilStorageEngineFile", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"]; ok {
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"] = nil
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for nil storage-engine.file")
						}
					})

					t.Run("ExtraStorageEngineDevice", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							devList := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"].([]interface{})
							devList = append(devList, "andRandomDevice")
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = devList
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot a device which doesn't exist in storage")
						}
					})

					t.Run("InvalidxdrConfig", func(t *testing.T) {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroCluster.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{}
							aeroCluster.Spec.AerospikeConfig["xdr"] = map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
							}
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid xdr config. mountPath for digestlog not present in storage")
						}
					})
				})
			})

			t.Run("ChangeDefaultConfig", func(t *testing.T) {
				t.Run("NsConf", func(t *testing.T) {
					// Ns conf
					// Rack-id
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["rack-id"] = 1
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for setting rack-id")
				})

				t.Run("ServiceConf", func(t *testing.T) {
					// Service conf
					// 	"node-id"
					// 	"cluster-name"
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["node-id"] = "a1"
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for setting node-id")

					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
					err = deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for setting cluster-name")
				})

				t.Run("NetworkConf", func(t *testing.T) {
					// Network conf
					// "port"
					// "access-port"
					// "access-address"
					// "alternate-access-port"
					// "alternate-access-address"
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
					networkConf := map[string]interface{}{
						"service": map[string]interface{}{
							"port":           3000,
							"access-address": []string{"<access_address>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig["network"] = networkConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for setting network conf")

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-address"
					// "tls-alternate-access-port"
					// "tls-alternate-access-address"
					aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 1)
					networkConf = map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":           "bob-cluster-a",
							"tls-port":           3001,
							"tls-access-address": []string{"<tls-access-address>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig["network"] = networkConf
					err = deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for setting tls network conf")
				})

				// Logging conf
				// XDR conf
			})
		})

		t.Run("InvalidAerospikeConfigSecret", func(t *testing.T) {
			t.Run("WhenFeatureKeyExist", func(t *testing.T) {
				aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 1, latestClusterBuild)
				aeroCluster.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig["service"] = map[string]interface{}{
					"feature-key-file": "/opt/aerospike/features.conf",
				}
				err := deployCluster(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when feature-key-file exist")
			})

			t.Run("WhenTLSExist", func(t *testing.T) {
				aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 1, latestClusterBuild)
				aeroCluster.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig["network"] = map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"name": "bob-cluster-b",
						},
					},
				}
				err := deployCluster(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when tls exist")
			})
		})
	})
}

func negativeUpdateClusterValidationTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) {
	// Will be used in Update
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
	if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
		t.Fatal(err)
	}
	t.Run("Validation", func(t *testing.T) {

		t.Run("InvalidBuild", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Build = "InvalidBuild"
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for InvalidBuild")

			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Build = "aerospike/aerospike-server-enterprise:3.0.0.4"
			err = f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for build lower than base")
		})
		t.Run("InvalidSize", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Size = 0
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for zero size")

			// aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 9)
			// err = deployCluster(t, f, ctx, aeroCluster)
			// validateError(t, err, "should fail for community eidition having more than 8 nodes")
		})
		t.Run("InvalidAerospikeConfig", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.AerospikeConfig = aerospikev1alpha1.Values{}
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for empty aerospikeConfig")

			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.AerospikeConfig = aerospikev1alpha1.Values{"namespace": "invalidConf"}
			err = f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for invalid aerospikeConfig")

			t.Run("InvalidNamespace", func(t *testing.T) {
				t.Run("NilAerospikeNamespace", func(t *testing.T) {
					aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
					aeroCluster.Spec.AerospikeConfig["namespace"] = nil
					err := f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for nil aerospikeConfig.namespace")
				})

				t.Run("InvalidReplicationFactor", func(t *testing.T) {
					aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
					aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 30
					err := f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for replication-factor greater than node sz")
				})

				// Should we test for overridden fields
				t.Run("InvalidStorage", func(t *testing.T) {
					t.Run("NilStorageEngine", func(t *testing.T) {
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = nil
						err := f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for nil storage-engine")
					})

					t.Run("NilStorageEngineDevice", func(t *testing.T) {
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = nil
							err := f.Client.Update(goctx.TODO(), aeroCluster)
							validateError(t, err, "should fail for nil storage-engine.device")
						}
					})

					t.Run("NilStorageEngineFile", func(t *testing.T) {
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"]; ok {
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"] = nil
							err := f.Client.Update(goctx.TODO(), aeroCluster)
							validateError(t, err, "should fail for nil storage-engine.file")
						}
					})

					t.Run("ExtraStorageEngineDevice", func(t *testing.T) {
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							devList := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"].([]interface{})
							devList = append(devList, "andRandomDevice")
							aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = devList
							err := f.Client.Update(goctx.TODO(), aeroCluster)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot add a device which doesn't exist in BlockStorage")
						}
					})

					t.Run("InvalidxdrConfig", func(t *testing.T) {
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroCluster.Spec.AerospikeConfig["xdr"] = map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "randomPath 100G",
							}
							err := f.Client.Update(goctx.TODO(), aeroCluster)
							validateError(t, err, "should fail for invalid xdr config. mountPath for digestlog not present in fileStorage")
						}
					})
				})
			})

			t.Run("ChangeDefaultConfig", func(t *testing.T) {
				t.Run("NsConf", func(t *testing.T) {
					// Ns conf
					// Rack-id
					aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
					aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["rack-id"] = 1
					err := f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for setting rack-id")
				})

				t.Run("ServiceConf", func(t *testing.T) {
					// Service conf
					// 	"node-id"
					// 	"cluster-name"
					aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
					aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["node-id"] = "a10"
					err := f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for setting node-id")

					aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
					aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
					err = f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for setting cluster-name")
				})

				t.Run("NetworkConf", func(t *testing.T) {
					// Network conf
					// "port"
					// "access-port"
					// "access-address"
					// "alternate-access-port"
					// "alternate-access-address"
					aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
					networkConf := map[string]interface{}{
						"service": map[string]interface{}{
							"port":           3000,
							"access-address": []string{"<access_address>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig["network"] = networkConf
					err := f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for setting network conf")

					// if "tls-name" in conf
					// "tls-port"
					// "tls-access-port"
					// "tls-access-address"
					// "tls-alternate-access-port"
					// "tls-alternate-access-address"
					aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
					networkConf = map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":           "bob-cluster-a",
							"tls-port":           3001,
							"tls-access-address": []string{"<tls-access-address>"},
						},
					}
					aeroCluster.Spec.AerospikeConfig["network"] = networkConf
					err = f.Client.Update(goctx.TODO(), aeroCluster)
					validateError(t, err, "should fail for setting tls network conf")
				})

				// Logging conf
				// XDR conf
			})
		})

		t.Run("InvalidAerospikeConfigSecret", func(t *testing.T) {
			// Will be used in Update
			aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, latestClusterBuild)
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			t.Run("WhenFeatureKeyExist", func(t *testing.T) {
				aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig["service"] = map[string]interface{}{
					"feature-key-file": "/opt/aerospike/features.conf",
				}
				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when feature-key-file exist")
			})

			t.Run("WhenTLSExist", func(t *testing.T) {
				aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				aeroCluster.Spec.AerospikeConfig["network"] = map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"name": "bob-cluster-b",
						},
					},
				}
				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when tls exist")
			})
		})
	})
}

func scaleUpClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, increaseBy int32) error {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	aeroCluster.Spec.Size = aeroCluster.Spec.Size + increaseBy
	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	// Wait for aerocluster to reach 2 replicas
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(increaseBy))
}

func scaleDownClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, decreaseBy int32) error {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	aeroCluster.Spec.Size = aeroCluster.Spec.Size - decreaseBy
	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	// How much time to wait in scaleDown
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(decreaseBy))
}

func rollingRestartClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) error {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	// Change config
	if _, ok := aeroCluster.Spec.AerospikeConfig["service"]; !ok {
		aeroCluster.Spec.AerospikeConfig["service"] = map[string]interface{}{}
	}
	aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["proto-fd-max"] = 1100

	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func upgradeClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, build string) error {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	// Change config
	aeroCluster.Spec.Build = build
	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}
