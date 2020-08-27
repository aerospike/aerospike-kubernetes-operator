package e2e

import (
	goctx "context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

var (
	retryInterval = time.Second * 5
	timeout       = time.Second * 120
)

const latestClusterBuild = "aerospike/aerospike-server-enterprise:4.8.0.6"
const buildToUpgrade = "aerospike/aerospike-server-enterprise:4.8.0.1"

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

	// t.Run("DeployClusterPost460", func(t *testing.T) {
	// 	DeployClusterForAllBuildsPost460(t, f, ctx)
	// })

	// t.Run("DeployClusterDiffStorageMultiPodPerHost", func(t *testing.T) {
	// 	DeployClusterForDiffStorageTest(t, f, ctx, 2, true)
	// })

	// t.Run("DeployClusterDiffStorageSinglePodPerHost", func(t *testing.T) {
	// 	DeployClusterForDiffStorageTest(t, f, ctx, 2, false)
	// })

	// t.Run("CommonNegativeClusterValidationTest", func(t *testing.T) {
	// 	CommonNegativeClusterValidationTest(t, f, ctx)
	// })

	// t.Run("UpdateCluster", func(t *testing.T) {
	// 	UpdateClusterTest(t, f, ctx)
	// })

	// t.Run("ClusterResources", func(t *testing.T) {
	// 	ClusterResourceTest(t, f, ctx)
	// })
	t.Run("RackManagement", func(t *testing.T) {
		RackManagementTest(t, f, ctx)
	})

	// t.Run("RackEnabledCluster", func(t *testing.T) {
	// 	RackEnabledClusterTest(t, f, ctx)
	// })
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

func CreateBasicCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"
	aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
	t.Run("Positive", func(t *testing.T) {
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}
	})
}

func ClusterResourceTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"
	aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)

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
			aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
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
	})
}

func updateCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func validateResource(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster) {
	found := &appsv1.StatefulSet{}
	if err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, found); err != nil {
		t.Fatal(err)
	}
	mem := aeroCluster.Spec.Resources.Requests.Memory()
	stMem := found.Spec.Template.Spec.Containers[0].Resources.Requests.Memory()
	if !mem.Equal(*stMem) {
		t.Fatal(fmt.Errorf("resource memory not matching. want %v, got %v", mem.String(), stMem.String()))
	}
	limitMem := found.Spec.Template.Spec.Containers[0].Resources.Limits.Memory()
	if !mem.Equal(*limitMem) {
		t.Fatal(fmt.Errorf("limit memory not matching. want %v, got %v", mem.String(), limitMem.String()))
	}

	cpu := aeroCluster.Spec.Resources.Requests.Cpu()
	stCPU := found.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu()
	if !cpu.Equal(*stCPU) {
		t.Fatal(fmt.Errorf("resource cpu not matching. want %v, got %v", cpu.String(), stCPU.String()))
	}
	limitCPU := found.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu()
	if !cpu.Equal(*limitCPU) {
		t.Fatal(fmt.Errorf("resource cpu not matching. want %v, got %v", cpu.String(), limitCPU.String()))
	}
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
		build := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", v)
		aeroCluster := createAerospikeClusterPost460(clusterName, namespace, 2, build)

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
			aeroCluster := createSSDStorageCluster("ssdstoragecluster", namespace, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})

		// HDD Storage Engine with Data in Memory
		t.Run("HDDAndDataInMemStorageCluster", func(t *testing.T) {
			aeroCluster := createHDDAndDataInMemStorageCluster("inmemstoragecluster", namespace, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// HDD Storage Engine with Data in Index Engine
		t.Run("HDDAndDataInIndexStorageCluster", func(t *testing.T) {
			aeroCluster := createHDDAndDataInIndexStorageCluster("datainindexcluster", namespace, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// Data in Memory Without Persistence
		t.Run("DataInMemWithoutPersistentStorageCluster", func(t *testing.T) {
			aeroCluster := createDataInMemWithoutPersistentStorageCluster("nopersistentcluster", namespace, clusterSz, repFact, multiPodPerHost)

			err := deployCluster(t, f, ctx, aeroCluster)
			deleteCluster(t, f, ctx, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
		})
		// Shadow Device
		// t.Run("ShadowDeviceStorageCluster", func(t *testing.T) {
		// 	aeroCluster := createShadowDeviceStorageCluster(clusterName, namespace, clusterSz, repFact, multiPodPerHost)
		// 	if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
		// 		t.Fatal(err)
		// 	}
		// 	// make info call

		// 	deleteCluster(t, f, ctx, aeroCluster)
		// })

		// Persistent Memory (pmem) Storage Engine

	})
}

// Test cluster validation Common for deployment and update both
func CommonNegativeClusterValidationTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aeroclusterdiffstorage"

	validateCluster(t, f, ctx, clusterName, namespace)
}

// Test cluster cr updation
func UpdateClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 3)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("ScaleUp", func(t *testing.T) {
			if err := scaleUpClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, 1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("ScaleDown", func(t *testing.T) {
			// TODO:
			// How to check if it is checking cluster stability before killing node
			// Check if tip-clear, alumni-reset is done or not
			if err := scaleDownClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, 1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RollingRestart", func(t *testing.T) {
			// TODO: How to check if it is checking cluster stability before killing node
			if err := rollingRestartClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// TODO: How to check if it is checking cluster stability before killing node
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 3)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("ValidateUpdate", func(t *testing.T) {
			aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
			err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
			// TODO: No jump version yet but will be used
			// t.Run("Build", func(t *testing.T) {
			// 	old := aeroCluster.Spec.Build
			// 	aeroCluster.Spec.Build = "aerospike/aerospike-server-enterprise:4.0.0.5"
			// 	err = f.Client.Update(goctx.TODO(), aeroCluster)
			// 	validateError(t, err, "should fail for upgrading to jump version")
			// 	aeroCluster.Spec.Build = old
			// })
			t.Run("MultiPodPerHost", func(t *testing.T) {
				old := aeroCluster.Spec.MultiPodPerHost
				aeroCluster.Spec.MultiPodPerHost = !aeroCluster.Spec.MultiPodPerHost
				err = f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating MultiPodPerHost. Cannot be updated")
				aeroCluster.Spec.MultiPodPerHost = old
			})

			t.Run("StorageValidation", func(t *testing.T) {
				old := aeroCluster.Spec.Storage.Volumes
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
				err = f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating Storage. Cannot be updated")
				aeroCluster.Spec.Storage.Volumes = old
			})

			t.Run("AerospikeConfig", func(t *testing.T) {
				t.Run("Namespace", func(t *testing.T) {
					t.Run("UpdateNamespaceList", func(t *testing.T) {
						old := aeroCluster.Spec.AerospikeConfig["namespace"]
						var new []interface{}
						new = append(new, old, map[string]interface{}{
							"name":           "bar",
							"memory-size":    2000955200,
							"storage-engine": "memory",
						})
						aeroCluster.Spec.AerospikeConfig["namespace"] = new
						err = f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace list. Cannot be updated")
						aeroCluster.Spec.AerospikeConfig["namespace"] = old
					})
					t.Run("UpdateStorageEngine", func(t *testing.T) {
						old := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"]
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = "memory"
						err = f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace storage-engine. Cannot be updated")
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = old
					})
					t.Run("UpdateReplicationFactor", func(t *testing.T) {
						old := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"]
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 5
						err = f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating namespace replication-factor. Cannot be updated")
						aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = old
					})
				})
			})
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})
}

func validateCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string) {
	t.Run("Validation", func(t *testing.T) {
		t.Run("EmptyClusterName", func(t *testing.T) {
			c := createDummyAerospikeCluster("", namespace, 1)
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for EmptyClusterName")
		})
		t.Run("EmptyNamespaceName", func(t *testing.T) {
			c := createDummyAerospikeCluster("validClusterName", "", 1)
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for EmptyNamespaceName")
		})
		t.Run("InvalidClusterName", func(t *testing.T) {
			// Name cannot have `-`
			c := createDummyAerospikeCluster("invalid-cluster-name", namespace, 1)
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "invalid-cluster-name")
		})
		t.Run("InvalidBuild", func(t *testing.T) {
			c := createDummyAerospikeCluster(clusterName, namespace, 1)
			c.Spec.Build = "InvalidBuild"
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for InvalidBuild")

			c.Spec.Build = "aerospike/aerospike-server-enterprise:3.0.0.4"
			err = deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for build lower than base")
		})
		t.Run("InvalidSize", func(t *testing.T) {
			c := createDummyAerospikeCluster(clusterName, namespace, 0)
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for zero size")

			// c = createDummyAerospikeCluster(clusterName, namespace, 9)
			// err = deployCluster(t, f, ctx, c)
			// validateError(t, err, "should fail for community eidition having more than 8 nodes")
		})
		t.Run("InvalidAerospikeConfig", func(t *testing.T) {
			c := createDummyAerospikeCluster(clusterName, namespace, 1)
			c.Spec.AerospikeConfig = aerospikev1alpha1.Values{}
			err := deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for empty aerospikeConfig")

			c = createDummyAerospikeCluster(clusterName, namespace, 1)
			c.Spec.AerospikeConfig = aerospikev1alpha1.Values{"namespace": "invalidConf"}
			err = deployCluster(t, f, ctx, c)
			validateError(t, err, "should fail for invalid aerospikeConfig")

			t.Run("InvalidNamespace", func(t *testing.T) {
				t.Run("NilAerospikeNamespace", func(t *testing.T) {
					c = createDummyAerospikeCluster(clusterName, namespace, 1)
					c.Spec.AerospikeConfig["namespace"] = nil
					err = deployCluster(t, f, ctx, c)
					validateError(t, err, "should fail for nil aerospikeConfig.namespace")
				})

				t.Run("InvalidReplicationFactor", func(t *testing.T) {
					c = createDummyAerospikeCluster(clusterName, namespace, 1)
					c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["replication-factor"] = 3
					err = deployCluster(t, f, ctx, c)
					validateError(t, err, "should fail for replication-factor greater than node sz")
				})

				// Should we test for overridden fields
				t.Run("InvalidStorage", func(t *testing.T) {
					t.Run("NilStorageEngine", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"] = nil
						err = deployCluster(t, f, ctx, c)
						validateError(t, err, "should fail for nil storage-engine")
					})

					t.Run("NilStorageEngineDevice", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						if _, ok := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = nil
							err = deployCluster(t, f, ctx, c)
							validateError(t, err, "should fail for nil storage-engine.device")
						}
					})

					t.Run("InvalidStorageEngineDevice", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						if _, ok := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							c.Spec.Storage.Volumes = []aerospikev1alpha1.AerospikePersistentVolumeSpec{
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

							c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = []string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
							err = deployCluster(t, f, ctx, c)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot have 3 devices in single device string")
						}
					})

					t.Run("NilStorageEngineFile", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						if _, ok := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"]; ok {
							c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["file"] = nil
							err = deployCluster(t, f, ctx, c)
							validateError(t, err, "should fail for nil storage-engine.file")
						}
					})

					t.Run("ExtraStorageEngineDevice", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						if _, ok := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							devList := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"].([]interface{})
							devList = append(devList, "andRandomDevice")
							c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"] = devList
							err = deployCluster(t, f, ctx, c)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot a device which doesn't exist in storage")
						}
					})

					t.Run("InvalidxdrConfig", func(t *testing.T) {
						c = createDummyAerospikeCluster(clusterName, namespace, 1)
						if _, ok := c.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							c.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{}
							c.Spec.AerospikeConfig["xdr"] = map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
							}
							err = deployCluster(t, f, ctx, c)
							validateError(t, err, "should fail for invalid xdr config. mountPath for digestlog not present in storage")
						}
					})
				})
			})
		})

		t.Run("InvalidAerospikeConfigSecret", func(t *testing.T) {
			t.Run("WhenFeatureKeyExist", func(t *testing.T) {
				c := createAerospikeClusterPost460(clusterName, namespace, 1, latestClusterBuild)
				c.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				c.Spec.AerospikeConfig["service"] = map[string]interface{}{
					"feature-key-file": "/opt/aerospike/features.conf",
				}
				err := deployCluster(t, f, ctx, c)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when feature-key-file exist")
			})

			t.Run("WhenTLSExist", func(t *testing.T) {
				c := createAerospikeClusterPost460(clusterName, namespace, 1, latestClusterBuild)
				c.Spec.AerospikeConfigSecret = aerospikev1alpha1.AerospikeConfigSecretSpec{}
				c.Spec.AerospikeConfig["network"] = map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"name": "bob-cluster-b",
						},
					},
				}
				err := deployCluster(t, f, ctx, c)
				validateError(t, err, "should fail for empty aerospikeConfigSecret when tls exist")
			})
		})
	})
}

func deployCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	return deployClusterWithTO(t, f, ctx, aeroCluster, retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func deployClusterWithTO(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster, retryInterval, timeout time.Duration) error {
	// Use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := f.Client.Create(goctx.TODO(), aeroCluster, cleanupOption(ctx))
	if err != nil {
		return err
	}
	// Wait for aerocluster to reach desired cluster size.
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, timeout)
}

func deleteCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	if err := f.Client.Delete(goctx.TODO(), aeroCluster); err != nil {
		return err
	}
	// wait for all pod to get deleted
	time.Sleep(time.Second * 12)
	// TODO: do we need to do anything more
	return nil
}

func scaleUpClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string, increaseBy int32) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size = aeroCluster.Spec.Size + increaseBy
	err = f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	// Wait for aerocluster to reach 2 replicas
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(increaseBy))
}

func scaleDownClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string, decreaseBy int32) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size = aeroCluster.Spec.Size - decreaseBy
	err = f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	// How much time to wait in scaleDown
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(decreaseBy))
}

func rollingRestartClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
	if err != nil {
		return err
	}

	// Change config
	if _, ok := aeroCluster.Spec.AerospikeConfig["service"]; !ok {
		aeroCluster.Spec.AerospikeConfig["service"] = map[string]interface{}{}
	}
	aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["proto-fd-max"] = 1100

	err = f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func upgradeClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string, build string) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
	if err != nil {
		return err
	}

	// Change config
	aeroCluster.Spec.Build = build
	err = f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}
