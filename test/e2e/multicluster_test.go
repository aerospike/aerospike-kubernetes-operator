package e2e

import (
	"sync"
	"testing"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

var multiClusterNs1 = "test1"
var multiClusterNs2 = "test2"

// DeployMultiClusterMultiNsTest tests multicluster in multi namespaces
func DeployMultiClusterMultiNsTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// 1st cluster
	clusterName1 := "aerocluster"
	clusterNamespacedName1 := getClusterNamespacedName(clusterName1, multiClusterNs1)

	// 2nd cluster
	clusterName2 := "aerocluster"
	clusterNamespacedName2 := getClusterNamespacedName(clusterName2, multiClusterNs2)

	t.Run("multiClusterLifeCycleTest", func(t *testing.T) {
		multiClusterLifeCycleTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterGenChangeTest", func(t *testing.T) {
		multiClusterGenChangeTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterPVCTest", func(t *testing.T) {
		multiClusterPVCTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterInParallel", func(t *testing.T) {
		multiClusterInParallel(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
}

// DeployMultiClusterSingleNsTest tests multicluster in single namespace
func DeployMultiClusterSingleNsTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// 1st cluster
	clusterName1 := "aerocluster1"
	clusterNamespacedName1 := getClusterNamespacedName(clusterName1, multiClusterNs1)

	// 2nd cluster
	clusterName2 := "aerocluster2"
	clusterNamespacedName2 := getClusterNamespacedName(clusterName2, multiClusterNs1)

	t.Run("multiClusterLifeCycleTest", func(t *testing.T) {
		multiClusterLifeCycleTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterGenChangeTest", func(t *testing.T) {
		multiClusterGenChangeTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterPVCTest", func(t *testing.T) {
		multiClusterPVCTest(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
	t.Run("multiClusterInParallel", func(t *testing.T) {
		multiClusterInParallel(t, f, ctx, clusterNamespacedName1, clusterNamespacedName2)
	})
}

func multiClusterInParallel(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Deploy 1st cluster
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
	go func() {
		defer wg.Done()
		t.Run("DeployCluster", func(t *testing.T) {
			if err := deployCluster(t, f, ctx, aeroCluster1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RollingRestart", func(t *testing.T) {
			if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName1); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, clusterNamespacedName1, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
		})
	}()

	aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
	go func() {
		defer wg.Done()
		// Deploy 2nd cluster
		t.Run("DeployCluster", func(t *testing.T) {
			if err := deployCluster(t, f, ctx, aeroCluster2); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RollingRestart", func(t *testing.T) {
			if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName2); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, clusterNamespacedName2, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
		})
	}()

	wg.Wait()

	deleteCluster(t, f, ctx, aeroCluster1)
	deleteCluster(t, f, ctx, aeroCluster2)
}

// multiClusterGenChangeTest tests if state of one cluster gets impacted by another
func multiClusterGenChangeTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	// Deploy 1st cluster
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
	if err := deployCluster(t, f, ctx, aeroCluster1); err != nil {
		t.Fatal(err)
	}
	aeroCluster1 = getCluster(t, f, ctx, clusterNamespacedName1)

	// Deploy 2nd cluster and run lifecycle ops
	aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
	if err := deployCluster(t, f, ctx, aeroCluster2); err != nil {
		t.Fatal(err)
	}
	t.Run("Validate_"+clusterNamespacedName2.String(), func(t *testing.T) {
		validateLifecycleOperation(t, f, ctx, clusterNamespacedName2)
	})
	deleteCluster(t, f, ctx, aeroCluster2)

	// Validate if there is any change in aeroCluster1
	newaeroCluster1 := getCluster(t, f, ctx, clusterNamespacedName1)
	if aeroCluster1.Generation != newaeroCluster1.Generation {
		t.Fatalf("Generation for cluster1 is changed affter deleting cluster2")
	}

	// Just try rolling restart to see if 1st cluster is fine
	t.Run("RollingRestart", func(t *testing.T) {
		if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName1); err != nil {
			t.Fatal(err)
		}
		validateRackEnabledCluster(t, f, ctx, clusterNamespacedName1)
	})

	deleteCluster(t, f, ctx, aeroCluster1)
}

// multiClusterPVCTest tests if pvc of one cluster gets impacted by another
func multiClusterPVCTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	cascadeDelete := true

	client := &framework.Global.Client.Client

	// Deploy 1st cluster
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
	aeroCluster1.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
	aeroCluster1.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
	if err := deployCluster(t, f, ctx, aeroCluster1); err != nil {
		t.Fatal(err)
	}
	aeroCluster1 = getCluster(t, f, ctx, clusterNamespacedName1)
	aeroClusterPVCList1, err := getAeroClusterPVCList(aeroCluster1, client)
	if err != nil {
		t.Fatal(err)
	}

	// Deploy 2nd cluster
	aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
	aeroCluster2.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
	aeroCluster2.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
	if err := deployCluster(t, f, ctx, aeroCluster2); err != nil {
		t.Fatal(err)
	}

	// Validate 1st cluster pvc before delete
	newaeroCluster1 := getCluster(t, f, ctx, clusterNamespacedName1)
	newaeroClusterPVCList1, err := getAeroClusterPVCList(newaeroCluster1, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := matchPVCList(aeroClusterPVCList1, newaeroClusterPVCList1); err != nil {
		t.Fatal(err)
	}

	// Delete 2nd cluster
	deleteCluster(t, f, ctx, aeroCluster2)

	// Validate 1st cluster pvc after delete
	newaeroCluster1 = getCluster(t, f, ctx, clusterNamespacedName1)
	newaeroClusterPVCList1, err = getAeroClusterPVCList(newaeroCluster1, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := matchPVCList(aeroClusterPVCList1, newaeroClusterPVCList1); err != nil {
		t.Fatal(err)
	}

	// Just try rolling restart to see if 1st cluster is fine
	t.Run("RollingRestart", func(t *testing.T) {
		if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName1); err != nil {
			t.Fatal(err)
		}
		validateRackEnabledCluster(t, f, ctx, clusterNamespacedName1)
	})
	// Delete 1st cluster
	deleteCluster(t, f, ctx, aeroCluster1)
}

// multiClusterLifeCycleTest tests multicluster
func multiClusterLifeCycleTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	// Deploy 1st cluster
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
	if err := deployCluster(t, f, ctx, aeroCluster1); err != nil {
		t.Fatal(err)
	}

	// Deploy 2nd cluster
	aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
	if err := deployCluster(t, f, ctx, aeroCluster2); err != nil {
		t.Fatal(err)
	}

	// Validate lifecycle for 1st cluster
	t.Run("Validate_"+clusterNamespacedName1.String(), func(t *testing.T) {
		validateLifecycleOperation(t, f, ctx, clusterNamespacedName1)
	})

	// Validate lifecycle for 2nd cluster
	t.Run("Validate_"+clusterNamespacedName2.String(), func(t *testing.T) {
		validateLifecycleOperation(t, f, ctx, clusterNamespacedName2)
	})

	deleteCluster(t, f, ctx, aeroCluster1)
	deleteCluster(t, f, ctx, aeroCluster2)
}

func validateLifecycleOperation(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) {
	t.Run("Positive", func(t *testing.T) {
		t.Run("ScaleUp", func(t *testing.T) {
			if err := scaleUpClusterTest(t, f, ctx, clusterNamespacedName, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("ScaleDown", func(t *testing.T) {
			if err := scaleDownClusterTest(t, f, ctx, clusterNamespacedName, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("RollingRestart", func(t *testing.T) {
			if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, clusterNamespacedName, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
	})
}
