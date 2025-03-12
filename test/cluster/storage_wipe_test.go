package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var (
	namespaces = []string{"test", "test1"}
)
var _ = Describe(
	"StorageWipe", func() {
		ctx := goctx.Background()
		Context(
			"When doing valid operations", func() {

				containerName := "tomcat"
				podSpec := asdbv1.AerospikePodSpec{
					MultiPodPerHost: ptr.To(true),
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

				clusterName := "storage-wipe"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				aeroCluster := &asdbv1.AerospikeCluster{}

				AfterEach(
					func() {
						_ = deleteCluster(k8sClient, ctx, aeroCluster)
						_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
					},
				)

				It(
					"Should validate all storage-wipe policies", func() {

						storageConfig := getAerospikeWipeStorageConfig(
							containerName, false, cloudProvider,
						)
						rackStorageConfig := getAerospikeWipeRackStorageConfig(
							containerName, false, cloudProvider,
						)
						racks := []asdbv1.Rack{
							{
								ID: 1,
							},
							{
								ID:           2,
								InputStorage: rackStorageConfig,
							},
						}
						aeroCluster = getStorageWipeAerospikeCluster(
							clusterNamespacedName, storageConfig, racks,
							post6Image, getAerospikeClusterConfig(),
						)

						aeroCluster.Spec.PodSpec = podSpec

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace, aeroCluster.Name)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Writing some data to the all volumes")
						err = writeDataToVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Writing some data to the cluster")
						err = WriteDataToCluster(
							aeroCluster, k8sClient, namespaces,
						)
						Expect(err).ToNot(HaveOccurred())

						By(
							fmt.Sprintf(
								"Downgrading image from %s to %s - volumes should not be wiped",
								post6Image, version6,
							),
						)
						err = UpdateClusterImage(aeroCluster, version6Image)
						Expect(err).ToNot(HaveOccurred())
						err = aerospikeClusterCreateUpdate(
							k8sClient, aeroCluster, ctx,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Checking - unrelated volume attachments should not be wiped")
						err = checkData(
							aeroCluster, true, true, map[string]struct{}{
								"test-wipe-device-dd-1":         {},
								"test-wipe-device-blkdiscard-1": {},
								"test-wipe-device-dd-2":         {},
								"test-wipe-device-blkdiscard-2": {},
								"test-wipe-files-deletefiles-2": {},
							},
						)
						Expect(err).ToNot(HaveOccurred())

						By("Checking - cluster data should not be wiped")
						records, err := CheckDataInCluster(
							aeroCluster, k8sClient, namespaces,
						)
						Expect(err).ToNot(HaveOccurred())

						for namespace, recordExists := range records {
							Expect(recordExists).To(
								BeTrue(), fmt.Sprintf(
									"Namespace: %s - should have records",
									namespace,
								),
							)
						}
					},
				)
			},
		)
	},
)

func getAerospikeClusterConfig() *asdbv1.AerospikeConfigSpec {
	return &asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			"service": map[string]interface{}{
				"feature-key-file": "/etc/aerospike/secret/features.conf",
				"proto-fd-max":     defaultProtofdmax,
			},
			"network":  getNetworkConfig(),
			"security": map[string]interface{}{},
			"namespaces": []interface{}{
				map[string]interface{}{
					"name":               "test",
					"replication-factor": 1,
					"migrate-sleep":      0,
					"storage-engine": map[string]interface{}{
						"type": "device",
						"devices": []interface{}{
							"/test/wipe/dd/xvdf",
							"/test/wipe/blkdiscard/xvdf",
						},
					},
				},
				map[string]interface{}{
					"name":               "test1",
					"replication-factor": 1,
					"migrate-sleep":      0,
					"storage-engine": map[string]interface{}{
						"type": "device",
						"files": []interface{}{
							"/opt/aerospike/data/test.dat",
						},
						"filesize": 2000955200,
					},
				},
			},
		},
	}
}

func getAerospikeWipeStorageConfig(
	containerName string, inputCascadeDelete bool, cloudProvider CloudProvider,
) *asdbv1.AerospikeStorageSpec {
	// Create pods and storage devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteMethod := asdbv1.AerospikeVolumeMethodDeleteFiles
	ddMethod := asdbv1.AerospikeVolumeMethodDD
	blkDiscardMethod := asdbv1.AerospikeVolumeMethodBlkdiscard

	if cloudProvider == CloudProviderAWS {
		// Blkdiscard methood is not supported in AWS, so it is initialized as DD Method
		blkDiscardMethod = asdbv1.AerospikeVolumeMethodDD
	}

	return &asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddMethod,
					InputWipeMethod: &ddMethod,
				},
				Name: "test-wipe-device-dd-1",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/test/wipe/dd/xvdf",
				},
			},
			{
				Name: "test-wipe-device-blkdiscard-1",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardMethod,
					InputWipeMethod: &blkDiscardMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/test/wipe/blkdiscard/xvdf",
				},
			},
			{
				Name: "test-wipe-files-deletefiles-1",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteMethod,
					InputWipeMethod: &fileDeleteMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/data",
				},
			},
			{
				Name: "file-noinit",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-noinit",
				},
			},
			{
				Name: "file-init",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-init",
				},
			},
			{
				Name: "device-noinit",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-noinit",
				},
			},
			{
				Name: "device-dd",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-dd",
				},
			},
			{
				Name: "device-blkdiscard",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-blkdiscard",
				},
			},
			{
				Name: "file-noinit-1",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Sidecars: []asdbv1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/filesystem-noinit",
					},
				},
			},
			{
				Name: "sidecar-dd-1",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddMethod,
					InputWipeMethod: &ddMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Sidecars: []asdbv1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/blockdevice-init-dd",
					},
				},
			},
			// {
			//	Name: "init-container-volume-1",
			//	AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
			//		InputInitMethod: &ddMethod,
			//		InputWipeMethod: &ddMethod,
			//	},
			//	Source: asdbv1.VolumeSource{
			//		PersistentVolume: &asdbv1.PersistentVolumeSpec{
			//			Size:         resource.MustParse("1Gi"),
			//			StorageClass: storageClass,
			//			VolumeMode:   corev1.PersistentVolumeBlock,
			//		},
			//	},
			//	InitContainers: []asdbv1.VolumeAttachment{
			//		{
			//			ContainerName: initContainerName,
			//			Path:          "/opt/aerospike/newpath",
			//		},
			//	},
			// },
			getStorageVolumeForSecret(),
		},
	}
}

func getAerospikeWipeRackStorageConfig(
	containerName string, inputCascadeDelete bool, cloudProvider CloudProvider,
) *asdbv1.AerospikeStorageSpec {
	aerospikeStorageSpec := getAerospikeWipeStorageConfig(
		containerName, inputCascadeDelete, cloudProvider,
	)
	aerospikeStorageSpec.Volumes[0].Name = "test-wipe-device-dd-2"
	aerospikeStorageSpec.Volumes[1].Name = "test-wipe-device-blkdiscard-2"
	aerospikeStorageSpec.Volumes[2].Name = "test-wipe-files-deletefiles-2"

	return aerospikeStorageSpec
}

func getStorageWipeAerospikeCluster(
	clusterNamespacedName types.NamespacedName,
	storageConfig *asdbv1.AerospikeStorageSpec,
	racks []asdbv1.Rack,
	image string,
	aerospikeConfigSpec *asdbv1.AerospikeConfigSpec,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:    storageInitTestClusterSize,
			Image:   image,
			Storage: *storageConfig,
			RackConfig: asdbv1.RackConfig{
				Namespaces: namespaces,
				Racks:      racks,
			},
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: test.AuthSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
							"read-write",
						},
					},
				},
			},
			AerospikeNetworkPolicy: asdbv1.AerospikeNetworkPolicy{
				AccessType:             asdbv1.AerospikeNetworkType(*defaultNetworkType),
				AlternateAccessType:    asdbv1.AerospikeNetworkType(*defaultNetworkType),
				TLSAccessType:          asdbv1.AerospikeNetworkType(*defaultNetworkType),
				TLSAlternateAccessType: asdbv1.AerospikeNetworkType(*defaultNetworkType),
			},
			ValidationPolicy: &asdbv1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: ptr.To(true),
			},
			AerospikeConfig: aerospikeConfigSpec,
		},
	}
}
