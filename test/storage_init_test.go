package test

// Tests storage initialization works as expected.
// If specified devices should be initialized only on first use.
import (
	goctx "context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const (
	storageInitTestClusterSize = 2
	magicBytes                 = "aero"
	sidecarContainerName       = "tomcat"
	clusterName                = "storage-init"
)

var _ = Describe(
	"StorageInit", func() {
		ctx := goctx.TODO()
		Context(
			"When doing valid operations", func() {

				trueVar := true
				cleanupThreads := 3
				updatedCleanupThreads := 5

				podSpec := asdbv1.AerospikePodSpec{
					Sidecars: []corev1.Container{
						{
							Name:  sidecarContainerName,
							Image: "tomcat:8.0",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 7500,
								},
							},
						},
					},
					AerospikeInitContainerSpec: &asdbv1.
						AerospikeInitContainerSpec{},
				}

				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				It(
					"Should work for large device with long init and multithreading", func() {

						ddInitMethod := asdbv1.AerospikeVolumeMethodDD

						racks := []asdbv1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "10Gi",
						)
						storageConfig.CleanupThreads = cleanupThreads

						storageConfig.Volumes = append(
							storageConfig.Volumes, asdbv1.VolumeSpec{
								Name: "device-dd1",
								AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
									InputInitMethod: &ddInitMethod,
								},
								Source: asdbv1.VolumeSource{
									PersistentVolume: &asdbv1.PersistentVolumeSpec{
										Size:         resource.MustParse("10Gi"),
										StorageClass: storageClass,
										VolumeMode:   corev1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
									Path: "/opt/aerospike/blockdevice-init-dd1",
								},
							},
							asdbv1.VolumeSpec{
								Name: "device-dd2",
								AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
									InputInitMethod: &ddInitMethod,
								},
								Source: asdbv1.VolumeSource{
									PersistentVolume: &asdbv1.PersistentVolumeSpec{
										Size:         resource.MustParse("10Gi"),
										StorageClass: storageClass,
										VolumeMode:   corev1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
									Path: "/opt/aerospike/blockdevice-init-dd2",
								},
							},
						)

						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, storageConfig, racks,
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

						racks := []asdbv1.Rack{
							{
								ID: 1,
							},
							{
								ID: 2,
							},
						}

						storageConfig := getAerospikeStorageConfig(
							sidecarContainerName, false, "1Gi", cloudProvider,
						)
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, storageConfig, racks,
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
							clusterNamespacedName, storageConfig, racks,
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

						By("Updating the volumes to cascade delete so that volumes are cleaned up")
						aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &trueVar
						aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &trueVar
						err = aerospikeClusterCreateUpdate(
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
					},
				)

				It(
					"Should work for large device with long init", func() {

						racks := []asdbv1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "50Gi",
						)
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, storageConfig, racks,
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
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				It(
					"Should fail multithreading in init container if resource limit is not set", func() {

						racks := []asdbv1.Rack{
							{
								ID: 1,
							},
						}

						storageConfig := getLongInitStorageConfig(
							false, "50Gi",
						)
						storageConfig.CleanupThreads = threeVar
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, storageConfig, racks,
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

		Context(
			"When doing PVC change", func() {

				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)
				initVolName := "ns"

				BeforeEach(
					func() {

						racks := []asdbv1.Rack{
							{ID: 1},
							{ID: 2},
						}

						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						rackConf := asdbv1.RackConfig{
							Racks: racks,
						}
						aeroCluster.Spec.RackConfig = rackConf
						aeroCluster.Labels = make(map[string]string)
						aeroCluster.Labels["checkLabel"] = "true"

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should change the pvcUID in initialisedVolumes", func() {

						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						// validate initialisedVolumes format
						err = validateInitVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						unscheduledPod := clusterName + "-1-0"
						deletedPVC := initVolName + "-" + unscheduledPod

						oldInitVol := aeroCluster.Status.Pods[unscheduledPod].InitializedVolumes

						By("Unschedule aerospike pods")

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).Should(HaveOccurred())

						By("Cleaning up previous pvc")

						pvcNamespacedName := getNamespacedName(
							deletedPVC, namespace,
						)

						err = deletePVC(k8sClient, pvcNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						By("Setting resources to schedule the pods")

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						newInitVol := aeroCluster.Status.Pods[unscheduledPod].InitializedVolumes

						By("Validating initialisedVolumes list")

						err = compareInitialisedVolumes(oldInitVol, newInitVol, initVolName)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should remove old formatted initialisedVolumes names", func() {

						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						// validate initialisedVolumes format
						err = validateInitVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						unscheduledPod := clusterName + "-1-0"

						oldInitVol := aeroCluster.Status.Pods[unscheduledPod].InitializedVolumes

						By("Patching old volume name format")

						patch1 := jsonpatch.PatchOperation{
							Operation: "replace",
							Path:      "/status/pods/" + unscheduledPod + "/initializedVolumes",
							Value:     append(oldInitVol, initVolName),
						}

						var patches []jsonpatch.PatchOperation
						patches = append(patches, patch1)

						jsonPatchJSON, err := json.Marshal(patches)
						Expect(err).ToNot(HaveOccurred())

						constantPatch := crClient.RawPatch(types.JSONPatchType, jsonPatchJSON)

						err = k8sClient.Status().Patch(
							ctx, aeroCluster, constantPatch, crClient.FieldOwner("pod"),
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						checkLabel := aeroCluster.Labels["checkLabel"]
						Expect(checkLabel).To(Equal("true"))

						apiLabel := aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel]
						Expect(apiLabel).To(Equal(asdbv1.AerospikeAPIVersion))

						err = validateInitVolumes(aeroCluster)
						Expect(err).Should(HaveOccurred())

						aeroCluster.Labels = nil

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						_, ok := aeroCluster.Labels["checkLabel"]
						Expect(ok).To(Equal(false))

						apiLabel = aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel]
						Expect(apiLabel).To(Equal(asdbv1.AerospikeAPIVersion))

						By("Unschedule aerospike pods")

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).Should(HaveOccurred())

						By("Setting resources to schedule the pods")

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						By("Validating initialisedVolumes list")

						err = validateInitVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)
			},
		)
	},
)

func compareInitialisedVolumes(oldInitVol, newInitVol []string, initVolName string) error {
	newInitVolMap := make(map[string]string, 2)

	for idx := range newInitVol {
		initVol := strings.Split(newInitVol[idx], "@")
		if _, ok := newInitVolMap[initVol[0]]; ok {
			return fmt.Errorf("found more than one occurrences of same volume in initialisedVolume list")
		}

		newInitVolMap[initVol[0]] = initVol[1]
	}

	for idx := range oldInitVol {
		initVol := strings.Split(oldInitVol[idx], "@")
		if initVol[0] == initVolName {
			if initVol[1] == newInitVolMap[initVol[0]] {
				return fmt.Errorf("invalid pvc uid, it should be changed after deleting pvc")
			}

			return nil
		}
	}

	return fmt.Errorf("volume not found in initialisedVolumes list %s", initVolName)
}

func validateInitVolumes(aeroCluster *asdbv1.AerospikeCluster) error {
	for podName := range aeroCluster.Status.Pods {
		for idx := range aeroCluster.Status.Pods[podName].InitializedVolumes {
			if !strings.Contains(aeroCluster.Status.Pods[podName].InitializedVolumes[idx], "@") {
				return fmt.Errorf("InitializedVolume is not formatted as expected, current volName=%s",
					aeroCluster.Status.Pods[podName].InitializedVolumes[idx])
			}
		}
	}

	return nil
}

func checkData(
	aeroCluster *asdbv1.AerospikeCluster, assertHasData bool,
	wasDataWritten bool, skip map[string]struct{},
) error {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for podIndex := range podList.Items {
		rackID, err := getRackID(&podList.Items[podIndex])
		if err != nil {
			return fmt.Errorf("failed to get rackID pods: %v", err)
		}

		storage := aeroCluster.Spec.Storage

		if rackID != 0 {
			for rackIndex := range aeroCluster.Spec.RackConfig.Racks {
				if aeroCluster.Spec.RackConfig.Racks[rackIndex].ID == rackID {
					storage = aeroCluster.Spec.RackConfig.Racks[rackIndex].Storage
				}
			}
		}

		for volumeIndex := range storage.Volumes {
			if storage.Volumes[volumeIndex].Source.PersistentVolume == nil {
				continue
			}

			if _, ok := skip[storage.Volumes[volumeIndex].Name]; ok {
				continue
			}

			var volumeHasData bool

			if storage.Volumes[volumeIndex].Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
				volumeHasData = hasDataBlock(&podList.Items[podIndex], &storage.Volumes[volumeIndex])
			} else {
				volumeHasData = hasDataFilesystem(&podList.Items[podIndex], &storage.Volumes[volumeIndex])
			}

			var expectedHasData = assertHasData && wasDataWritten
			if storage.Volumes[volumeIndex].InitMethod == asdbv1.AerospikeVolumeMethodNone {
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
					"expected volume %s %s but is not", storage.Volumes[volumeIndex].Name,
					expectedStr,
				)
			}
		}
	}

	return nil
}

func writeDataToVolumes(aeroCluster *asdbv1.AerospikeCluster) error {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for podIndex := range podList.Items {
		rackID, err := getRackID(&podList.Items[podIndex])
		if err != nil {
			return fmt.Errorf("failed to get rackID pods: %v", err)
		}

		storage := aeroCluster.Spec.Storage

		if rackID != 0 {
			for rackIndex := range aeroCluster.Spec.RackConfig.Racks {
				if aeroCluster.Spec.RackConfig.Racks[rackIndex].ID == rackID {
					storage = aeroCluster.Spec.RackConfig.Racks[rackIndex].Storage
				}
			}
		}

		err = writeDataToPodVolumes(&storage, &podList.Items[podIndex])
		if err != nil {
			return err
		}
	}

	return nil
}

func writeDataToPodVolumes(
	storage *asdbv1.AerospikeStorageSpec, pod *corev1.Pod,
) error {
	for volumeIndex := range storage.Volumes {
		if storage.Volumes[volumeIndex].Source.PersistentVolume == nil {
			continue
		}

		if storage.Volumes[volumeIndex].Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
			err := writeDataToVolumeBlock(pod, &storage.Volumes[volumeIndex])
			if err != nil {
				return err
			}
		} else {
			err := writeDataToVolumeFileSystem(pod, &storage.Volumes[volumeIndex])
			if err != nil {
				return err
			}
		}
	}

	return nil
}
func getContainerNameAndPath(volume *asdbv1.VolumeSpec) (containerName, path string) {
	if volume.Aerospike != nil {
		return asdbv1.AerospikeServerContainerName, volume.Aerospike.Path
	}

	return volume.Sidecars[0].ContainerName, volume.Sidecars[0].Path
}

func writeDataToVolumeBlock(
	pod *corev1.Pod, volume *asdbv1.VolumeSpec,
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
	pod *corev1.Pod, volume *asdbv1.VolumeSpec,
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

func hasDataBlock(pod *corev1.Pod, volume *asdbv1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{
		"bash", "-c", fmt.Sprintf("dd if=%s count=1 status=none", path),
	}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

func hasDataFilesystem(pod *corev1.Pod, volume *asdbv1.VolumeSpec) bool {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("cat %s/magic.txt", path)}
	stdout, _, _ := utils.Exec(pod, cName, cmd, k8sClientset, cfg)

	return strings.HasPrefix(stdout, magicBytes)
}

// getStorageInitAerospikeCluster returns a spec with in memory namespaces and input storage.
// None of the storage volumes are used by Aerospike and are free to be used for testing.
func getStorageInitAerospikeCluster(
	clusterNamespacedName types.NamespacedName,
	storageConfig *asdbv1.AerospikeStorageSpec, racks []asdbv1.Rack,
	image string,
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
				Namespaces: []string{"test"},
				Racks:      racks,
			},
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
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
			ValidationPolicy: &asdbv1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
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
							"replication-factor": storageInitTestClusterSize,
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
	inputCascadeDelete bool, storageSize string,
) *asdbv1.AerospikeStorageSpec {
	// Create pods and storage devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteInitMethod := asdbv1.AerospikeVolumeMethodDeleteFiles
	ddInitMethod := asdbv1.AerospikeVolumeMethodDD

	// Note: Blkdiscard method is not supported in AWS, so it is initialized as DD Method

	return &asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "file-init",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-init",
				},
			},
			{
				Name: "device-dd",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-dd",
				},
			},
			{
				Name: aerospikeConfigSecret,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: tlsSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}
