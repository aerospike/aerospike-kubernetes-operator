package test

// Tests storage initialization works as expected.
// If specified devices should be initialized only on first use.
import (
	goctx "context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const storageInitTestClusterSize = 2
const magicBytes = "aero"

var _ = Describe(
	"StorageInit", func() {
		ctx := goctx.TODO()
		Context(
			"When doing valid operations", func() {

				trueVar := true
				cleanupThreads := 3
				updatedCleanupThreads := 5

				containerName := "tomcat"
				podSpec := asdbv1beta1.AerospikePodSpec{
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
					AerospikeInitContainerSpec: &asdbv1beta1.
						AerospikeInitContainerSpec{},
				}

				clusterName := "storage-init"
				clusterNamespacedName := getClusterNamespacedName(
					clusterName, namespace,
				)

				It(
					"Should work for large device with long init and multithreading", func() {

						ddInitMethod := asdbv1beta1.AerospikeVolumeMethodDD

						racks := []asdbv1beta1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "10Gi", cloudProvider,
						)
						storageConfig.CleanupThreads = cleanupThreads

						storageConfig.Volumes = append(
							storageConfig.Volumes, asdbv1beta1.VolumeSpec{
								Name: "device-dd1",
								AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
									InputInitMethod: &ddInitMethod,
								},
								Source: asdbv1beta1.VolumeSource{
									PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
										Size:         resource.MustParse("10Gi"),
										StorageClass: storageClass,
										VolumeMode:   corev1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
									Path: "/opt/aerospike/blockdevice-init-dd1",
								},
							},
							asdbv1beta1.VolumeSpec{
								Name: "device-dd2",
								AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
									InputInitMethod: &ddInitMethod,
								},
								Source: asdbv1beta1.VolumeSource{
									PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
										Size:         resource.MustParse("10Gi"),
										StorageClass: storageClass,
										VolumeMode:   corev1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
									Path: "/opt/aerospike/blockdevice-init-dd2",
								},
							},
						)

						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks,
							latestImage,
						)

						aeroCluster.Spec.PodSpec = podSpec

						// It should be greater than given in cluster namespace
						resourceMem := resource.MustParse("2Gi")
						resourceCPU := resource.MustParse("500m")
						limitMem := resource.MustParse("2Gi")
						limitCPU := resource.MustParse("500m")

						resources := &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resourceCPU,
								corev1.ResourceMemory: resourceMem,
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    limitCPU,
								corev1.ResourceMemory: limitMem,
							},
						}

						aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = resources

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Storage.CleanupThreads = updatedCleanupThreads

						By("Updating the cluster")

						err = k8sClient.Update(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should validate all storage-init policies", func() {

						racks := []asdbv1beta1.Rack{
							{
								ID: 1,
							},
							{
								ID: 2,
							},
						}

						storageConfig := getAerospikeStorageConfig(
							containerName, false, "1Gi", cloudProvider,
						)
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks,
							latestImage,
						)

						aeroCluster.Spec.PodSpec = podSpec

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = checkData(
							aeroCluster, false, false, map[string]struct{}{},
						)
						Expect(err).ToNot(HaveOccurred())

						By("Writing some data to all volumes")
						err = writeDataToVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = checkData(
							aeroCluster, true, true, map[string]struct{}{},
						)
						Expect(err).ToNot(HaveOccurred())

						By("Forcing a rolling restart, volumes should still have data")
						err = UpdateClusterImage(aeroCluster, prevImage)
						Expect(err).ToNot(HaveOccurred())
						err = aerospikeClusterCreateUpdate(
							k8sClient, aeroCluster, ctx,
						)
						Expect(err).ToNot(HaveOccurred())

						err = checkData(
							aeroCluster, true, true, map[string]struct{}{},
						)
						Expect(err).ToNot(HaveOccurred())

						By("Deleting the cluster but retaining the test volumes")
						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Recreating. Older volumes will still be around and reused")
						aeroCluster = getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks,
							prevImage,
						)
						aeroCluster.Spec.PodSpec = podSpec
						err = aerospikeClusterCreateUpdate(
							k8sClient, aeroCluster, ctx,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Checking volumes that need initialization, they should not have data")
						err = checkData(
							aeroCluster, false, true, map[string]struct{}{},
						)
						Expect(err).ToNot(HaveOccurred())

						if aeroCluster != nil {

							By("Updating the volumes to cascade delete so that volumes are cleaned up")
							aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &trueVar
							aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &trueVar
							err := aerospikeClusterCreateUpdate(
								k8sClient, aeroCluster, ctx,
							)
							Expect(err).ToNot(HaveOccurred())

							err = deleteCluster(k8sClient, ctx, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							pvcs, err := getAeroClusterPVCList(
								aeroCluster, k8sClient,
							)
							Expect(err).ToNot(HaveOccurred())

							Expect(len(pvcs)).Should(
								Equal(0), "PVCs not deleted",
							)
						}
					},
				)

				It(
					"Should work for large device with long init", func() {

						racks := []asdbv1beta1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "50Gi", cloudProvider,
						)
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks,
							latestImage,
						)

						aeroCluster.Spec.PodSpec = podSpec

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {

				threeVar := 3

				clusterName := "storage-init"
				clusterNamespacedName := getClusterNamespacedName(
					clusterName, namespace,
				)

				It(
					"Should fail multithreading in init container if resource limit is not set", func() {

						racks := []asdbv1beta1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "50Gi", cloudProvider,
						)
						storageConfig.CleanupThreads = threeVar
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks,
							latestImage,
						)

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)
			},
		)
	},
)

func checkData(
	aeroCluster *asdbv1beta1.AerospikeCluster, assertHasData bool,
	wasDataWritten bool, skip map[string]struct{},
) error {
	podList, err := getPodList(aeroCluster, k8sClient)
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
			if _, ok := skip[volume.Name]; ok {
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
			if volume.InitMethod == asdbv1beta1.AerospikeVolumeMethodNone {
				expectedHasData = wasDataWritten
			}

			if expectedHasData != volumeHasData {
				var expectedStr string
				if expectedHasData {
					expectedStr = " has data"
				} else {
					expectedStr = " is empty"
				}

				return fmt.Errorf(
					"expected volume %s %s but is not", volume.Name,
					expectedStr,
				)
			}
		}
	}
	return nil
}

func writeDataToVolumes(aeroCluster *asdbv1beta1.AerospikeCluster) error {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for _, pod := range podList.Items {

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

		err = writeDataToPodVolumes(storage, &pod)
		if err != nil {
			return err
		}

	}
	return nil
}

func writeDataToPodVolumes(
	storage asdbv1beta1.AerospikeStorageSpec, pod *corev1.Pod,
) error {
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
func getContainerNameAndPath(volume asdbv1beta1.VolumeSpec) (string, string) {
	if volume.Aerospike != nil {
		return asdbv1beta1.AerospikeServerContainerName, volume.Aerospike.Path
	}

	return volume.Sidecars[0].ContainerName, volume.Sidecars[0].Path
}

func writeDataToVolumeBlock(
	pod *corev1.Pod, volume asdbv1beta1.VolumeSpec,
) error {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{
		"bash", "-c", fmt.Sprintf(
			"echo %s > /tmp/magic.txt && dd if=/tmp/magic.txt of=%s",
			magicBytes, path,
		),
	}
	_, _, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	if err != nil {
		return fmt.Errorf("error creating file %v", err)
	}
	return nil
}

func writeDataToVolumeFileSystem(
	pod *corev1.Pod, volume asdbv1beta1.VolumeSpec,
) error {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{
		"bash", "-c", fmt.Sprintf("echo %s > %s/magic.txt", magicBytes, path),
	}
	_, _, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	if err != nil {
		return fmt.Errorf("error creating file %v", err)
	}
	return nil
}

func hasDataBlock(pod *corev1.Pod, volume asdbv1beta1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{
		"bash", "-c", fmt.Sprintf("dd if=%s count=1 status=none", path),
	}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

func hasDataFilesystem(pod *corev1.Pod, volume asdbv1beta1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("cat %s/magic.txt", path)}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

// getStorageInitAerospikeCluster returns a spec with in memory namespaces and input storage. None of the  storage volumes are used by Aerospike and are free to be used for testing.
func getStorageInitAerospikeCluster(
	clusterNamespacedName types.NamespacedName,
	storageConfig asdbv1beta1.AerospikeStorageSpec, racks []asdbv1beta1.Rack,
	image string,
) *asdbv1beta1.AerospikeCluster {
	// create Aerospike custom resource
	return &asdbv1beta1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:    storageInitTestClusterSize,
			Image:   image,
			Storage: storageConfig,
			RackConfig: asdbv1beta1.RackConfig{
				Namespaces: []string{"test"},
				Racks:      racks,
			},
			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
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
			ValidationPolicy: &asdbv1beta1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			PodSpec: asdbv1beta1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"migrate-threads":  4,
					},
					"network":  getNetworkConfig(),
					"security": map[string]interface{}{},
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

func getLongInitStorageConfig(
	inputCascadeDelete bool, storageSize string, cloudProvider CloudProvider,
) *asdbv1beta1.AerospikeStorageSpec {

	// Create pods and storage devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteInitMethod := asdbv1beta1.AerospikeVolumeMethodDeleteFiles
	ddInitMethod := asdbv1beta1.AerospikeVolumeMethodDD
	if cloudProvider == CloudProviderAWS {
		// Blkdiscard method is not supported in AWS, so it is initialized as DD Method

	}

	return &asdbv1beta1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		Volumes: []asdbv1beta1.VolumeSpec{
			{
				Name: "file-init",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-init",
				},
			},
			{
				Name: "device-dd",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-dd",
				},
			},
			{
				Name: aerospikeConfigSecret,
				Source: asdbv1beta1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: tlsSecretName,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}
