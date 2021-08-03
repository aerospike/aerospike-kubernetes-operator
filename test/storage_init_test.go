// +build !noac

// Tests storage initialization works as expected.
// If specifed devices should be initialized only on first use.

package test

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const (
	storageInitTestClusterSize            = 2
	storageInitTestWaitForVersionInterval = 1 * time.Second
	storageInitTestWaitForVersionTO       = 10 * time.Minute
	magicBytes                            = "aero"
)

var _ = Describe("StorageInit", func() {
	ctx := goctx.TODO()

	Context("When doing valid operations", func() {

		falseVar := false
		trueVar := true

		containerName := "tomcat"
		podSpec := asdbv1alpha1.AerospikePodSpec{
			Sidecars: []corev1.Container{
				{
					Name:  containerName,
					Image: "tomcat:8.0",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 7500,
						},
					},
				},
			},
		}
		// Create pods and strorge devices write data to the devices.
		// - deletes cluster without cascade delete of volumes.
		// - recreate and check if volumes are reinitialized correctly.
		fileDeleteInitMethod := asdbv1alpha1.AerospikeVolumeInitMethodDeleteFiles
		// TODO
		ddInitMethod := asdbv1alpha1.AerospikeVolumeInitMethodDD
		blkDiscardInitMethod := asdbv1alpha1.AerospikeVolumeInitMethodBlkdiscard

		storageConfig := asdbv1alpha1.AerospikeStorageSpec{
			BlockVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
				InputCascadeDelete: &falseVar,
			},
			FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
				InputCascadeDelete: &falseVar,
			},
			Volumes: []asdbv1alpha1.VolumeSpec{
				{
					Name: "file-noinit",
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeFilesystem,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/opt/aerospike/filesystem-noinit",
					},
				},
				{
					Name: "file-init",
					AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
						InputInitMethod: &fileDeleteInitMethod,
					},
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeFilesystem,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/opt/aerospike/filesystem-init",
					},
				},
				{
					Name: "device-noinit",
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeBlock,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/opt/aerospike/blockdevice-noinit",
					},
				},
				{
					Name: "device-dd",
					AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
						InputInitMethod: &ddInitMethod,
					},
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeBlock,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/opt/aerospike/blockdevice-init-dd",
					},
				},
				{
					Name: "device-blkdiscard",
					AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
						InputInitMethod: &blkDiscardInitMethod,
					},
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeBlock,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/opt/aerospike/blockdevice-init-blkdiscard",
					},
				},
				{
					Name: "file-noinit-1",
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeFilesystem,
						},
					},
					Sidecars: []asdbv1alpha1.VolumeAttachment{
						{
							ContainerName: containerName,
							Path:          "/opt/aerospike/filesystem-noinit",
						},
					},
				},
				// {
				// 	Name: "file-init-1",
				// 	AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
				// 		InputInitMethod: &fileDeleteInitMethod,
				// 	},
				// 	Source: asdbv1alpha1.VolumeSource{
				// 		PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
				// 			Size:         resource.MustParse("1Gi"),
				// 			StorageClass: storageClass,
				// 			VolumeMode:   corev1.PersistentVolumeFilesystem,
				// 		},
				// 	},
				// 	Sidecars: []asdbv1alpha1.VolumeAttachment{
				// 		{
				// 			ContainerName: containerName,
				// 			Path:          "/opt/aerospike/filesystem-init",
				// 		},
				// 	},
				// },
				// {
				// 	Name: "device-noinit-1",
				// 	Source: asdbv1alpha1.VolumeSource{
				// 		PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
				// 			Size:         resource.MustParse("1Gi"),
				// 			StorageClass: storageClass,
				// 			VolumeMode:   corev1.PersistentVolumeBlock,
				// 		},
				// 	},
				// 	Sidecars: []asdbv1alpha1.VolumeAttachment{
				// 		{
				// 			ContainerName: containerName,
				// 			Path:          "/opt/aerospike/blockdevice-noinit",
				// 		},
				// 	},
				// },
				{
					Name: "device-dd-1",
					AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
						InputInitMethod: &ddInitMethod,
					},
					Source: asdbv1alpha1.VolumeSource{
						PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   corev1.PersistentVolumeBlock,
						},
					},
					Sidecars: []asdbv1alpha1.VolumeAttachment{
						{
							ContainerName: containerName,
							Path:          "/opt/aerospike/blockdevice-init-dd",
						},
					},
				},
				// {
				// 	Name: "device-blkdiscard-1",
				// 	AerospikePersistentVolumePolicySpec: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
				// 		InputInitMethod: &blkDiscardInitMethod,
				// 	},
				// 	Source: asdbv1alpha1.VolumeSource{
				// 		PersistentVolume: &asdbv1alpha1.PersistentVolumeSpec{
				// 			Size:         resource.MustParse("1Gi"),
				// 			StorageClass: storageClass,
				// 			VolumeMode:   corev1.PersistentVolumeBlock,
				// 		},
				// 	},
				// 	Sidecars: []asdbv1alpha1.VolumeAttachment{
				// 		{
				// 			ContainerName: containerName,
				// 			Path:          "/opt/aerospike/blockdevice-init-blkdiscard",
				// 		},
				// 	},
				// },
				{
					Name: aerospikeConfigSecret,
					Source: asdbv1alpha1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: tlsSecretName,
						},
					},
					Aerospike: &asdbv1alpha1.AerospikeServerVolumeAttachment{
						Path: "/etc/aerospike/secret",
					},
				},
			},
		}

		var rackStorageConfig asdbv1alpha1.AerospikeStorageSpec
		rackStorageConfig.BlockVolumePolicy.InitMethod = asdbv1alpha1.AerospikeVolumeInitMethodBlkdiscard

		racks := []asdbv1alpha1.Rack{
			{
				ID:      1,
				Storage: rackStorageConfig,
			},
			{
				ID: 2,
			},
		}

		clusterName := "storage-init"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		aeroCluster := getStorageInitAerospikeCluster(clusterNamespacedName, storageConfig, racks, ctx)

		aeroCluster.Spec.PodSpec = podSpec

		It("Should validate all storage-init policies", func() {

			By("Cleaning up previous pvc")

			err := cleanupPVC(k8sClient, namespace)
			Expect(err).ToNot(HaveOccurred())

			By("Deploying the cluster")

			err = deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = checkData(aeroCluster, false, false)
			Expect(err).ToNot(HaveOccurred())

			By("Writing some data to the all volumes")
			err = writeDataToVolumes(aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = checkData(aeroCluster, true, true)
			Expect(err).ToNot(HaveOccurred())

			By("Forcing a rolling restart, volumes should still have data")
			aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:5.0.0.11"
			err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			err = checkData(aeroCluster, true, true)
			Expect(err).ToNot(HaveOccurred())

			By("Deleting the cluster but retaining the test volumes")
			deleteCluster(k8sClient, ctx, aeroCluster)

			By("Recreating. Older volumes will still be around and reused")
			aeroCluster = getStorageInitAerospikeCluster(clusterNamespacedName, storageConfig, racks, ctx)
			aeroCluster.Spec.PodSpec = podSpec
			err = aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())

			By("Checking volumes that need initialization, they should not have data")
			err = checkData(aeroCluster, false, true)
			Expect(err).ToNot(HaveOccurred())

			if aeroCluster != nil {

				By("Updating the volumes to cascade delete so that volumes are cleaned up")
				aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &trueVar
				aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &trueVar
				err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
				Expect(err).ToNot(HaveOccurred())

				deleteCluster(k8sClient, ctx, aeroCluster)

				pvcs, err := getAeroClusterPVCList(aeroCluster, k8sClient)
				Expect(err).ToNot(HaveOccurred())

				Expect(len(pvcs)).Should(Equal(0), "PVCs not deleted")
			}
		})
	})
})

func checkData(aeroCluster *asdbv1alpha1.AerospikeCluster, assertHasData bool, wasDataWritten bool) error {
	client := k8sClient

	podList, err := getPodList(aeroCluster, client)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for _, pod := range podList.Items {
		// t.Logf("Check data for pod %v", pod.Name)

		rackID, err := getRackID(&pod)
		if err != nil {
			return fmt.Errorf("failed to get rackID pods: %v", err)
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
			if volume.Source.PersistentVolume == nil {
				continue
			}
			// t.Logf("Check data for volume %v", volume.Path)
			var volumeHasData bool
			if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
				volumeHasData = hasDataBlock(&pod, volume)
			} else {
				volumeHasData = hasDataFilesystem(&pod, volume)
			}

			var expectedHasData = assertHasData && wasDataWritten
			if volume.InitMethod == asdbv1alpha1.AerospikeVolumeInitMethodNone {
				expectedHasData = wasDataWritten
			}

			if expectedHasData != volumeHasData {
				var expectedStr string
				if expectedHasData {
					expectedStr = " has data"
				} else {
					expectedStr = " is empty"
				}

				return fmt.Errorf("Expected volume %s %s but is not.", volume.Name, expectedStr)
			}
		}
	}
	return nil
}

func writeDataToVolumes(aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	client := k8sClient

	podList, err := getPodList(aeroCluster, client)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for _, pod := range podList.Items {
		// TODO: check rack as well.
		storage := aeroCluster.Spec.Storage
		err := writeDataToPodVolumes(storage, &pod)
		if err != nil {
			return err
		}
	}
	return nil
}

func writeDataToPodVolumes(storage asdbv1alpha1.AerospikeStorageSpec, pod *corev1.Pod) error {
	for _, volume := range storage.Volumes {
		if volume.Source.PersistentVolume == nil {
			continue
		}
		if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
			err := writeDataToVolumeBlock(pod, volume)
			if err != nil {
				return err
			}
		} else {
			err := writeDataToVolumeFileSystem(pod, volume)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func getContainerNameAndPath(volume asdbv1alpha1.VolumeSpec) (string, string) {
	if volume.Aerospike != nil {
		return asdbv1alpha1.AerospikeServerContainerName, volume.Aerospike.Path
	}

	return volume.Sidecars[0].ContainerName, volume.Sidecars[0].Path
}

func writeDataToVolumeBlock(pod *corev1.Pod, volume asdbv1alpha1.VolumeSpec) error {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("echo %s > /tmp/magic.txt && dd if=/tmp/magic.txt of=%s", magicBytes, path)}
	_, _, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	if err != nil {
		return fmt.Errorf("error creating file %v", err)
	}
	return nil
}

func writeDataToVolumeFileSystem(pod *corev1.Pod, volume asdbv1alpha1.VolumeSpec) error {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("echo %s > %s/magic.txt", magicBytes, path)}
	_, _, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	if err != nil {
		return fmt.Errorf("error creating file %v", err)
	}
	return nil
}

func hasDataBlock(pod *corev1.Pod, volume asdbv1alpha1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("dd if=%s count=1 status=none", path)}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

func hasDataFilesystem(pod *corev1.Pod, volume asdbv1alpha1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("cat %s/magic.txt", path)}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

// getStorageInitAerospikeCluster returns a spec with in memory namespaces and input storage. None of the  storage volumes are used by Aerospike and are free to be used for testing.
func getStorageInitAerospikeCluster(clusterNamespacedName types.NamespacedName, storageConfig asdbv1alpha1.AerospikeStorageSpec, racks []asdbv1alpha1.Rack, ctx goctx.Context) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	// create Aerospike custom resource
	return &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:    storageInitTestClusterSize,
			Image:   "aerospike/aerospike-server-enterprise:5.0.0.4",
			Storage: storageConfig,
			RackConfig: asdbv1alpha1.RackConfig{
				Namespaces: []string{"test"},
				Racks:      racks,
			},
			AerospikeAccessControl: &asdbv1alpha1.AerospikeAccessControlSpec{
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			ValidationPolicy: &asdbv1alpha1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			PodSpec: asdbv1alpha1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{
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
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			},
		},
	}
}

func cleanupPVC(k8sClient client.Client, ns string) error {
	// t.Log("Cleanup old pvc")

	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	clLabels := map[string]string{"app": "aerospike-cluster"}
	labelSelector := labels.SelectorFromSet(clLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}

	if err := k8sClient.List(goctx.TODO(), pvcList, listOps); err != nil {
		return err
	}

	for _, pvc := range pvcList.Items {
		// t.Logf("Found pvc %s, deleting it", pvc.Name)

		if utils.IsPVCTerminating(&pvc) {
			continue
		}

		if err := k8sClient.Delete(goctx.TODO(), &pvc); err != nil {
			return fmt.Errorf("could not delete pvc %s: %v", pvc.Name, err)
		}
	}
	return nil
}
