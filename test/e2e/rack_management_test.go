package e2e

import (
	goctx "context"
	"testing"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

// Test cluster cr updation
func RackManagementTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	t.Run("Positive", func(t *testing.T) {
		// Deploy cluster without rack
		aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}
		clusterNamespacedName := getClusterNamespacedName(aeroCluster)

		// Add new rack
		t.Run("AddRack", func(t *testing.T) {
			t.Run("Add1stRackOfCluster", func(t *testing.T) {
				aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
				err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
				if err != nil {
					t.Fatal(err)
				}
				rackConf := aerospikev1alpha1.RackConfig{
					RackPolicy: []aerospikev1alpha1.RackPolicy{},
					Racks:      []aerospikev1alpha1.Rack{{ID: 1}},
				}
				aeroCluster.Spec.RackConfig = rackConf
				// Also increase the size
				aeroCluster.Spec.Size = 3

				if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, clusterNamespacedName)
			})
			t.Run("AddRackInExistingRacks", func(t *testing.T) {
				if err := addRack(t, f, ctx, clusterNamespacedName, aerospikev1alpha1.Rack{ID: 2}); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, clusterNamespacedName)
			})
		})
		// Remove rack
		t.Run("RemoveRack", func(t *testing.T) {
			t.Run("RemoveSingleRack", func(t *testing.T) {
				if err := removeRack(t, f, ctx, clusterNamespacedName); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, clusterNamespacedName)
			})
			t.Run("RemoveAllRacks", func(t *testing.T) {
				aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
				err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
				if err != nil {
					t.Fatal(err)
				}
				rackConf := aerospikev1alpha1.RackConfig{}
				aeroCluster.Spec.RackConfig = rackConf

				// This will also indirectl check if older rack is removed or not.
				// If older node is not deleted then cluster sz will not be as expected
				if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, clusterNamespacedName)
			})

		})
		// Update not allowed

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		// // Will be used in Update also
		// aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
		// rackConf := aerospikev1alpha1.RackConfig{
		// 	RackPolicy: []aerospikev1alpha1.RackPolicy{},
		// 	Racks:      []aerospikev1alpha1.Rack{{ID: 1}, {ID: 2}},
		// }
		// aeroCluster.Spec.RackConfig = rackConf
		// if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
		// 	t.Fatal(err)
		// }
		// clusterNamespacedName := getClusterNamespacedName(aeroCluster)

		// // Update not allowed
		// t.Run("Update", func(t *testing.T) {

		// })

		// deleteCluster(t, f, ctx, aeroCluster)
	})
}

func addRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, rack)
	// Size shouldn't make any difference in working. Still put different size to check if it create any issue.
	aeroCluster.Spec.Size = aeroCluster.Spec.Size + 1
	return updateAndWait(t, f, ctx, aeroCluster)
}

func removeRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
	if err != nil {
		return err
	}
	racks := aeroCluster.Spec.RackConfig.Racks
	if len(racks) > 0 {
		racks = racks[:len(racks)-1]
	}

	aeroCluster.Spec.RackConfig.Racks = racks
	aeroCluster.Spec.Size = aeroCluster.Spec.Size - 1
	// This will also indirectl check if older rack is removed or not.
	// If older node is not deleted then cluster sz will not be as expected
	return updateAndWait(t, f, ctx, aeroCluster)
}

func updateRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
	if err != nil {
		return err
	}
	racks := aeroCluster.Spec.RackConfig.Racks
	if len(racks) > 0 {
		racks[0].Region = "randomValue"
	} else {
		t.Fatalf("No rack available to update")
	}

	aeroCluster.Spec.RackConfig.Racks = racks

	return f.Client.Update(goctx.TODO(), aeroCluster)
}

func updateAndWait(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	err := f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}
