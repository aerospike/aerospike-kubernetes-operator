package test

import (
	goctx "context"
	"fmt"
	"strconv"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

type UnwipedVolumesError struct {
	err string
}

func (e UnwipedVolumesError) Error() string {
	return fmt.Sprintf("unwiped volumes: %s\n", e.err)
}

var _ = Describe(
	"StorageWipe", func() {
		ctx := goctx.TODO()
		Context(
			"When doing valid operations", func() {

				trueVar := true

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
				}

				clusterName := "storage-wipe"
				clusterNamespacedName := getClusterNamespacedName(
					clusterName, namespace,
				)

				It(

					"Should validate all storage-wipe policies", func() {

						var rackStorageConfig asdbv1beta1.AerospikeStorageSpec
						rackStorageConfig.BlockVolumePolicy.WipeMethod = asdbv1beta1.AerospikeVolumeWipeMethodBlkdiscard
						rackStorageConfig.BlockVolumePolicy.InitMethod = asdbv1beta1.AerospikeVolumeInitMethodDD
						if cloudProvider == CloudProviderAWS {
							rackStorageConfig.BlockVolumePolicy.InitMethod = asdbv1beta1.AerospikeVolumeInitMethodDD
							rackStorageConfig.BlockVolumePolicy.WipeMethod = asdbv1beta1.AerospikeVolumeWipeMethodBlkdiscard
						}
						racks := []asdbv1beta1.Rack{
							{
								ID:      1,
								Storage: rackStorageConfig,
							},
							{
								ID: 2,
							},
						}

						storageConfig := getAerospikeStorageConfig(
							containerName, false, cloudProvider)
						aeroCluster := getStorageInitAerospikeCluster(
							clusterNamespacedName, *storageConfig, racks, latestImage)

						aeroCluster.Spec.PodSpec = podSpec

						By("Cleaning up previous pvc")

						err := cleanupPVC(k8sClient, namespace)
						Expect(err).ToNot(HaveOccurred())

						By("Deploying the cluster")

						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Writing some data to the all volumes")
						err = writeDataToVolumes(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = checkIfVolumesWiped(aeroCluster)
						if err != nil {
							switch err.(type) {
							case UnwipedVolumesError:
								Expect(err).To(HaveOccurred(), "volumes are not wiped")
							default:
								Expect(err).ToNot(HaveOccurred())

							}
						} else {
							Expect(nil).ToNot(HaveOccurred(), "volumes should not be zeroed")
						}

						By(fmt.Sprintf("Downgrading image from %s to %s - volumes should not be wiped",
							latestImage, version6))
						err = UpdateClusterImage(aeroCluster, version6Image)
						Expect(err).ToNot(HaveOccurred())
						err = aerospikeClusterCreateUpdate(
							k8sClient, aeroCluster, ctx,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Checking if volumes are wiped")
						err = checkIfVolumesWiped(aeroCluster)
						if err != nil {
							switch err.(type) {
							case UnwipedVolumesError:
								Expect(err).To(HaveOccurred(), "volumes are not wiped")
							default:
								Expect(err).ToNot(HaveOccurred())

							}
						} else {
							Expect(nil).ToNot(HaveOccurred(), "volumes should not be wiped")
						}

						By(fmt.Sprintf("Downgrading image from %s to %s - volumes should be wiped",
							version6, prevServerVersion))
						err = UpdateClusterImage(aeroCluster, prevImage)
						Expect(err).ToNot(HaveOccurred())
						err = aerospikeClusterCreateUpdate(
							k8sClient, aeroCluster, ctx,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Checking if volumes are wiped")
						err = checkIfVolumesWiped(aeroCluster)
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
			},
		)
	},
)

func checkIfVolumesWiped(aeroCluster *asdbv1beta1.AerospikeCluster) error {
	unzeroedVolumes := ""
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
		for _, volume := range storage.Volumes {
			if volume.Source.PersistentVolume == nil {
				continue
			}
			volumeWasWiped := false
			if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
				volumeWasWiped, err = checkIfVolumeBlockZeroed(&pod, volume)
				if err != nil {
					return err
				}
			} else if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeFilesystem {
				volumeWasWiped, err = checkIfVolumeFileSystemZeored(&pod, volume)
				if err != nil {
					return err
				}

			} else {
				return fmt.Errorf("pod: %s volume: %s mood: %s - invalid volume mode",
					pod.Name, volume.Name, volume.Source.PersistentVolume.VolumeMode)
			}
			if !volumeWasWiped {
				unzeroedVolumes += fmt.Sprintf("pod: %s, volume: %s wipe-method: %s \n",
					pod.Name, volume.Name, volume.WipeMethod)
			}
		}
	}
	if len(unzeroedVolumes) > 0 {
		return UnwipedVolumesError{err: unzeroedVolumes}
	}
	return nil
}

func checkIfVolumeBlockZeroed(pod *corev1.Pod, volume asdbv1beta1.VolumeSpec) (bool, error) {
	cName, path := getContainerNameAndPath(volume)
	cmd := []string{
		"bash", "-c", fmt.Sprintf("dd if=%s bs=1M status=none "+
			"| od -An "+
			"| head "+
			"| grep -v '*' "+
			"| awk '{for(i=1;i<=NF;i++)$i=(sum[i]+=$i)}END{print}' "+
			"| awk '{sum=0; for(i=1; i<=NF; i++) sum += $i; print sum}'", path)}

	stdout, stderr, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)
	if err != nil {
		return false, err
	}
	if stderr != "" {
		return false, fmt.Errorf("%s", stderr)
	}
	retval, err := strconv.Atoi(strings.TrimRight(stdout, "\r\n"))
	if err != nil {
		return false, err
	}
	if retval == 0 {
		return true, nil
	}
	return false, nil
}

func checkIfVolumeFileSystemZeored(pod *corev1.Pod, volume asdbv1beta1.VolumeSpec) (bool, error) {
	cName, path := getContainerNameAndPath(volume)

	cmd := []string{"bash", "-c", fmt.Sprintf("find %s -type f | wc -l", path)}
	stdout, stderr, err := utils.Exec(pod, cName, cmd, k8sClientset, cfg)
	if err != nil {
		return false, err
	}
	if stderr != "" {
		return false, fmt.Errorf("%s", stderr)
	}
	retval, err := strconv.Atoi(strings.TrimRight(stdout, "\r\n"))
	if err != nil {
		return false, err
	}
	if retval == 0 {
		return true, nil
	}
	return false, nil
}
