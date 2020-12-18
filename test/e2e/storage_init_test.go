// +build !noac

// Tests storage initialization works as expected.
// If specifed devices should be initialized only on first use.

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
)

const (
	storageInitTestClusterSize            = 2
	storageInitTestWaitForVersionInterval = 1 * time.Second
	storageInitTestWaitForVersionTO       = 10 * time.Minute
	magicBytes                            = "aero"
)

func TestStorageInit(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Errorf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	falseVar := false
	trueVar := true

	var aeroCluster *aerospikev1alpha1.AerospikeCluster = nil
	// Create pods and strorge devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteInitMethod := aerospikev1alpha1.AerospikeVolumeInitMethodDeleteFiles
	// TODO
	ddInitMethod := aerospikev1alpha1.AerospikeVolumeInitMethodDD
	blkDiscardInitMethod := aerospikev1alpha1.AerospikeVolumeInitMethodBlkdiscard

	storageConfig := aerospikev1alpha1.AerospikeStorageSpec{
		BlockVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &falseVar,
		},
		FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &falseVar,
		},
		Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/opt/aerospike/filesystem-noinit",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
			},
			{
				Path:         "/opt/aerospike/filesystem-init",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
				AerospikePersistentVolumePolicySpec: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteInitMethod,
				},
			},
			{
				Path:         "/opt/aerospike/blockdevice-noinit",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
			},
			{
				Path:         "/opt/aerospike/blockdevice-init-dd",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
				AerospikePersistentVolumePolicySpec: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
			},
			{
				Path:         "/opt/aerospike/blockdevice-init-blkdiscard",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
				AerospikePersistentVolumePolicySpec: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardInitMethod,
				},
			},
		},
	}

	var rackStorageConfig aerospikev1alpha1.AerospikeStorageSpec
	rackStorageConfig.BlockVolumePolicy.InitMethod = aerospikev1alpha1.AerospikeVolumeInitMethodBlkdiscard

	racks := []aerospikev1alpha1.Rack{
		aerospikev1alpha1.Rack{
			ID:      1,
			Storage: rackStorageConfig,
		},
		{
			ID: 2,
		},
	}

	aeroCluster = getStorageInitAerospikeCluster(storageConfig, racks, ctx)
	err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
	if err != nil {
		t.Error(err)
	}

	// Ensure volumes are empty.
	checkData(aeroCluster, false, false, t)

	// Write some data to the all volumes.
	writeDataToVolumes(aeroCluster, t)

	// Ensure volumes have data.
	checkData(aeroCluster, true, true, t)

	// Force a rolling restart, volumes should still have data.
	aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:5.0.0.11"
	err = aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
	if err != nil {
		t.Error(err)
	}
	checkData(aeroCluster, true, true, t)

	// Delete the cluster but retain the test volumes.
	deleteCluster(t, f, ctx, aeroCluster)

	// Recreate. Older volumes will still be around and reused.
	aeroCluster = getStorageInitAerospikeCluster(storageConfig, racks, ctx)
	err = aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
	if err != nil {
		t.Error(err)
	}

	// Volumes that need initialization should not have data.
	checkData(aeroCluster, false, true, t)

	if aeroCluster != nil {

		// Update the volumes to cascade delete so that volumes are cleaned up.
		aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &trueVar
		aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &trueVar
		err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}

		deleteCluster(t, f, ctx, aeroCluster)

		pvcs, err := getAeroClusterPVCList(aeroCluster, &framework.Global.Client.Client)
		if err != nil {
			t.Error(err)
		}

		if len(pvcs) != 0 {
			t.Errorf("PVCs not deleted: %v", pvcs)
		}
	}
}

func checkData(aeroCluster *aerospikev1alpha1.AerospikeCluster, assertHasData bool, wasDataWritten bool, t *testing.T) {
	client := framework.Global.Client.Client

	podList, err := getPodList(aeroCluster, &client)
	if err != nil {
		t.Errorf("Failed to list pods: %v", err)
		return
	}

	for _, pod := range podList.Items {
		rackID, err := getRackID(&pod)
		if err != nil {
			t.Errorf("Failed to get rackID pods: %v", err)
			return
		}

		storage := aeroCluster.Spec.Storage
		if rackID != 0 {
			for _, rack := range aeroCluster.Spec.RackConfig.Racks {
				if rack.ID == rackID {
					storage = rack.Storage
				}
			}
		}
		for _, volume := range storage.Volumes {
			var volumeHasData bool
			if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeBlock {
				volumeHasData = hasDataBlock(&pod, volume, t)
			} else {
				volumeHasData = hasDataFilesystem(&pod, volume, t)
			}

			var expectedHasData = assertHasData && wasDataWritten
			if volume.InitMethod == aerospikev1alpha1.AerospikeVolumeInitMethodNone {
				expectedHasData = wasDataWritten
			}

			if expectedHasData != volumeHasData {
				var expectedStr string
				if expectedHasData {
					expectedStr = " has data"
				} else {
					expectedStr = " is empty"
				}

				t.Errorf("Expected volume %s %s but is not.", volume.Path, expectedStr)
			}
		}
	}
}

func writeDataToVolumes(aeroCluster *aerospikev1alpha1.AerospikeCluster, t *testing.T) {
	client := framework.Global.Client.Client

	podList, err := getPodList(aeroCluster, &client)
	if err != nil {
		t.Errorf("Failed to list pods: %v", err)
		return
	}

	for _, pod := range podList.Items {
		// TODO: check rack as well.
		storage := aeroCluster.Spec.Storage
		writeDataToPodVolumes(storage, &pod, t)
	}
}

func writeDataToPodVolumes(storage aerospikev1alpha1.AerospikeStorageSpec, pod *corev1.Pod, t *testing.T) {
	for _, volume := range storage.Volumes {
		if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeBlock {
			writeDataToVolumeBlock(pod, volume, t)
		} else {
			writeDataToVolumeFileSystem(pod, volume, t)
		}
	}
}

func writeDataToVolumeBlock(pod *corev1.Pod, volume aerospikev1alpha1.AerospikePersistentVolumeSpec, t *testing.T) {
	_, _, err := ExecuteCommandOnPod(pod, utils.AerospikeServerContainerName, "bash", "-c", fmt.Sprintf("echo %s > /tmp/magic.txt && dd if=/tmp/magic.txt of=%s", magicBytes, volume.Path))

	if err != nil {
		t.Errorf("Error creating file %v", err)
	}
}

func writeDataToVolumeFileSystem(pod *corev1.Pod, volume aerospikev1alpha1.AerospikePersistentVolumeSpec, t *testing.T) {
	_, _, err := ExecuteCommandOnPod(pod, utils.AerospikeServerContainerName, "bash", "-c", fmt.Sprintf("echo %s > %s/magic.txt", magicBytes, volume.Path))

	if err != nil {
		t.Errorf("Error creating file %v", err)
	}
}

func hasDataBlock(pod *corev1.Pod, volume aerospikev1alpha1.AerospikePersistentVolumeSpec, t *testing.T) bool {
	stdout, _, _ := ExecuteCommandOnPod(pod, utils.AerospikeServerContainerName, "bash", "-c", fmt.Sprintf("dd if=%s count=1 status=none", volume.Path))
	return strings.HasPrefix(stdout, magicBytes)
}

func hasDataFilesystem(pod *corev1.Pod, volume aerospikev1alpha1.AerospikePersistentVolumeSpec, t *testing.T) bool {
	stdout, _, _ := ExecuteCommandOnPod(pod, utils.AerospikeServerContainerName, "bash", "-c", fmt.Sprintf("cat %s/magic.txt", volume.Path))
	return strings.HasPrefix(stdout, magicBytes)
}

// getStorageInitAerospikeCluster returns a spec with in memory namespaces and input storage. None of the  storage volumes are used by Aerospike and are free to be used for testing.
func getStorageInitAerospikeCluster(storageConfig aerospikev1alpha1.AerospikeStorageSpec, racks []aerospikev1alpha1.Rack, ctx *framework.TestCtx) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	kubeNs, _ := ctx.GetNamespace()

	// create Aerospike custom resource
	return &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aeroclustertest",
			Namespace: kubeNs,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:    storageInitTestClusterSize,
			Image:   "aerospike/aerospike-server-enterprise:5.0.0.4",
			Storage: storageConfig,
			RackConfig: aerospikev1alpha1.RackConfig{
				Namespaces: []string{"test"},
				Racks:      racks,
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			AerospikeAccessControl: &aerospikev1alpha1.AerospikeAccessControlSpec{
				Users: []aerospikev1alpha1.AerospikeUserSpec{
					aerospikev1alpha1.AerospikeUserSpec{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			ValidationPolicy: &aerospikev1alpha1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			MultiPodPerHost: true,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: map[string]interface{}{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
					"migrate-threads":  4,
				},
				"security": map[string]interface{}{"enable-security": true},
				"namespaces": []interface{}{
					map[string]interface{}{
						"name":               "test",
						"replication-factor": networkTestPolicyClusterSize,
						"memory-size":        3000000000,
						"migrate-sleep":      0,
						"storage-engine":     "memory",
					},
				},
			},
		},
	}
}
