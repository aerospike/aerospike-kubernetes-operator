package e2e

import (
	goctx "context"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ashishshinde/aerospike-client-go/pkg/ripemd160"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
)

// Test cluster cr updation
func ClusterStorageCleanUpTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	// Check defaults
	// Cleanup all volumes
	// Cleanup selected volumes
	// Update

	client := &framework.Global.Client.Client

	t.Run("Positive", func(t *testing.T) {
		// Deploy cluster with 6 racks to remove rack one by one and check for pvc
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
		racks := getDummyRackConf(1, 2, 3, 4)
		aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		// Check defaults
		t.Run("Defaults", func(t *testing.T) {
			oldPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}

			// removeRack should not remove any pvc
			removeLastRack(t, f, ctx, clusterNamespacedName)

			newPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}

			// Check for pvc, both list should be same
			if err := matchPVCList(oldPVCList, newPVCList); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("CleanupAllVolumes", func(t *testing.T) {

			// Set common FileSystemVolumePolicy, BlockVolumePolicy to true
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

			remove := true
			aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &remove
			aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &remove

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			// RackID to be used to check if pvc are removed
			racks := aeroCluster.Spec.RackConfig.Racks
			lastRackID := racks[len(racks)-1].ID
			stsName := aeroCluster.Name + "-" + strconv.Itoa(lastRackID)

			// remove Rack should remove all rack's pvc
			aeroCluster.Spec.RackConfig.Racks = racks[:len(racks)-1]
			aeroCluster.Spec.Size = aeroCluster.Spec.Size - 1

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			newPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}
			// Check for pvc
			for _, pvc := range newPVCList {
				if strings.Contains(pvc.Name, stsName) {
					t.Fatalf("PVC %s not removed for cluster sts %s", pvc.Name, stsName)
				}
			}
		})

		t.Run("CleanupSelectedVolumes", func(t *testing.T) {
			// Set common FileSystemVolumePolicy, BlockVolumePolicy to false and true for selected volumes
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			remove := true
			aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &remove
			aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &remove
			vRemove := false
			aeroCluster.Spec.Storage.Volumes[0].InputCascadeDelete = &vRemove

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			// RackID to be used to check if pvc are removed
			racks := aeroCluster.Spec.RackConfig.Racks
			lastRackID := racks[len(racks)-1].ID
			stsName := aeroCluster.Name + "-" + strconv.Itoa(lastRackID)
			// This should not be removed

			pvcName, err := getPVCName(aeroCluster.Spec.Storage.Volumes[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			pvcNamePrefix := pvcName + "-" + stsName

			// remove Rack should remove all rack's pvc
			aeroCluster.Spec.RackConfig.Racks = racks[:len(racks)-1]
			aeroCluster.Spec.Size = aeroCluster.Spec.Size - 1

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			newPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}
			// Check for pvc
			var found bool
			for _, pvc := range newPVCList {
				if strings.HasPrefix(pvc.Name, pvcNamePrefix) {
					found = true
					continue
				}
				if strings.Contains(pvc.Name, stsName) {
					t.Fatalf("PVC %s not removed for cluster sts %s", pvc.Name, stsName)
				}
			}
			if !found {
				t.Fatalf("PVC with prefix %s should not be removed for cluster sts %s", pvcNamePrefix, stsName)
			}
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})
}

// Test cluster cr updation
func RackUsingLocalStorageTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	client := &framework.Global.Client.Client

	// Positive
	// Global storage given, no local (already checked in normal cluster tests)
	// Global storage given, local also given
	// Local storage should be used for cascadeDelete, aerospikeConfig

	// Negative
	// Update rack storage should fail
	// (nil -> val), (val -> nil), (val1 -> val2)

	t.Run("Positive", func(t *testing.T) {
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		racks := getDummyRackConf(1)
		// AerospikeConfig is only patched
		devName := "/test/dev/rackstorage"
		racks[0].InputAerospikeConfig = &aerospikev1alpha1.Values{
			"namespaces": []interface{}{
				map[string]interface{}{
					"name": "test",
					"storage-engine": map[string]interface{}{
						"devices": []interface{}{devName},
					},
				},
			},
		}
		remove := true
		// Rack is completely replaced
		racks[0].InputStorage = &aerospikev1alpha1.AerospikeStorageSpec{
			BlockVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
				InputCascadeDelete: &remove,
			},
			FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
				InputCascadeDelete: &remove,
				InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
			},
			Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
				{
					Path:         devName,
					SizeInGB:     1,
					StorageClass: "ssd",
					VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
				},
				{
					Path:         "/opt/aerospike",
					SizeInGB:     1,
					StorageClass: "ssd",
					VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
				},
			},
		}

		aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("UseForAerospikeConfig", func(t *testing.T) {
			newPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}
			// There is only single rack
			pvcName, err := getPVCName(devName)
			if err != nil {
				t.Fatal(err)
			}
			stsName := aeroCluster.Name + "-" + strconv.Itoa(racks[0].ID)
			pvcNamePrefix := pvcName + "-" + stsName

			// If PVC is created and no error in deployment then it mean aerospikeConfig
			// has successfully used rack storage
			var found bool
			for _, pvc := range newPVCList {
				if strings.HasPrefix(pvc.Name, pvcNamePrefix) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("PVC with prefix %s not found in cluster pvcList %v", pvcNamePrefix, newPVCList)
			}
		})

		t.Run("UseForCascadeDelete", func(t *testing.T) {
			// There is only single rack
			podName := aeroCluster.Name + "-" + strconv.Itoa(racks[0].ID) + "-" + strconv.Itoa(int(aeroCluster.Spec.Size-1))

			if err := scaleDownClusterTest(t, f, ctx, clusterNamespacedName, 1); err != nil {
				t.Fatal(err)
			}

			newPVCList, err := getAeroClusterPVCList(aeroCluster, client)
			if err != nil {
				t.Fatal(err)
			}

			// Check for pvc
			for _, pvc := range newPVCList {
				if strings.Contains(pvc.Name, podName) {
					t.Fatalf("PVC %s not removed for cluster pod %s", pvc.Name, podName)
				}
			}
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		t.Run("BadAerospikeConfig", func(t *testing.T) {
			// Deploy cluster with 6 racks to remove rack one by one and check for pvc
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)
			// AerospikeConfig is only patched
			racks[0].InputAerospikeConfig = &aerospikev1alpha1.Values{
				"namespaces": []interface{}{
					map[string]interface{}{
						"name": "test",
						"storage-engine": map[string]interface{}{
							"devices": []interface{}{"random/device/name"},
						},
					},
				},
			}
			// Rack is completely replaced
			racks[0].InputStorage = &aerospikev1alpha1.AerospikeStorageSpec{
				FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &aerospikeVolumeInitMethodDeleteFiles,
				},
				Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			}

			aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for not having aerospikeConfig namespace Storage device in rack storage")

			deleteCluster(t, f, ctx, aeroCluster)
		})

		t.Run("Update", func(t *testing.T) {
			t.Run("NilToValue", func(t *testing.T) {
				// Deploy cluster with 6 racks to remove rack one by one and check for pvc
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
				racks := getDummyRackConf(1)
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}
				if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}

				// Update storage
				aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
				// Rack is completely replaced
				volumes := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike/new",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				}
				volumes = append(volumes, aeroCluster.Spec.Storage.Volumes...)
				racks[0].InputStorage = getStorage(volumes)
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}
				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating Storage. Cannot be updated")

				deleteCluster(t, f, ctx, aeroCluster)
			})
			t.Run("ValueToNil", func(t *testing.T) {
				// Deploy cluster with 6 racks to remove rack one by one and check for pvc
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
				racks := getDummyRackConf(1)
				// Rack is completely replaced
				volumes := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike/new",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				}
				volumes = append(volumes, aeroCluster.Spec.Storage.Volumes...)
				racks[0].InputStorage = getStorage(volumes)
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}
				if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}

				// Update storage
				aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
				// Rack is completely replaced
				racks[0].InputStorage = &aerospikev1alpha1.AerospikeStorageSpec{}
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}
				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating Storage. Cannot be updated")

				deleteCluster(t, f, ctx, aeroCluster)
			})

			t.Run("ValueToValue", func(t *testing.T) {
				// Deploy cluster with 6 racks to remove rack one by one and check for pvc
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
				racks := getDummyRackConf(1)
				// Rack is completely replaced
				volumes := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike/new",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				}
				volumes = append(volumes, aeroCluster.Spec.Storage.Volumes...)
				racks[0].InputStorage = getStorage(volumes)
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

				if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}

				// Update storage
				aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
				// Rack is completely replaced
				volumes = []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike/new2",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				}
				volumes = append(volumes, aeroCluster.Spec.Storage.Volumes...)
				racks[0].InputStorage = getStorage(volumes)
				aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

				err := f.Client.Update(goctx.TODO(), aeroCluster)
				validateError(t, err, "should fail for updating Storage. Cannot be updated")

				deleteCluster(t, f, ctx, aeroCluster)
			})
		})
		// Add test while rack using common aeroConfig but local storage, fail for mismatch
		t.Run("CommonConfigLocalStorage", func(t *testing.T) {
			// Deploy cluster with 6 racks to remove rack one by one and check for pvc
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
			racks := getDummyRackConf(1)
			// Rack is completely replaced
			volumes := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
				{
					Path:         "/opt/aerospike/new",
					SizeInGB:     1,
					StorageClass: "ssd",
					VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
				},
			}
			racks[0].InputStorage = getStorage(volumes)
			aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

			err := deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for deploying with wrong Storage. Storage doesn't have namespace related volumes")

			deleteCluster(t, f, ctx, aeroCluster)
		})
	})

}

func getStorage(volumes []aerospikev1alpha1.AerospikePersistentVolumeSpec) *aerospikev1alpha1.AerospikeStorageSpec {
	cascadeDelete := true
	storage := aerospikev1alpha1.AerospikeStorageSpec{
		BlockVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cascadeDelete,
		},
		FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cascadeDelete,
			InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
		},
		Volumes: volumes,
	}
	return &storage
}

func truncateString(str string, num int) string {
	if len(str) > num {
		return str[0:num]
	}
	return str
}

func getPVCName(path string) (string, error) {
	path = strings.Trim(path, "/")

	hashPath, err := getHash(path)
	if err != nil {
		return "", err
	}

	reg, err := regexp.Compile("[^-a-z0-9]+")
	if err != nil {
		return "", err
	}
	newPath := reg.ReplaceAllString(path, "-")
	return truncateString(hashPath, 30) + "-" + truncateString(newPath, 20), nil
}

func getHash(str string) (string, error) {
	var digest []byte
	hash := ripemd160.New()
	hash.Reset()
	if _, err := hash.Write([]byte(str)); err != nil {
		return "", err
	}
	res := hash.Sum(digest)
	return hex.EncodeToString(res), nil
}

func matchPVCList(oldPVCList, newPVCList []corev1.PersistentVolumeClaim) error {
	for _, oldPVC := range oldPVCList {
		var found bool
		for _, newPVC := range newPVCList {
			if oldPVC.Name == newPVC.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("PVC %s not found in new PVCList %v", oldPVC.Name, newPVCList)
		}
	}
	return nil
}
