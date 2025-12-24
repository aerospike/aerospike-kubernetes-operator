package cluster

import (
	goctx "context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// * Test
//     * Storage Volume
//         * source
//             * can not specify multiple source
//             * one source has to be specified
//             * Non pv
//                 * can be updated — rolling restart…
//             * pv
//                 * can  not be updated
//                 * can not give wrong volumeMode
//                 * can not give wrong accessMode
//         * Attachments
//             * container name should be valid
//             * sidecar, initcontainer, aerospike all 3 can not be empty
//             * all attachments can be added, removed, updated — rolling restart
//             * container can not mount multiple volume in same path
//         * Volume can be added removed, if it is not PV — rolling restart
//     * PodSpec
//         * Affinity can be updated — rolling restart
//         * Sidecar/initcontainer can be added removed — rolling restart

var _ = Describe(
	"StorageVolumes", func() {
		ctx := goctx.Background()
		var (
			clusterNamespacedName types.NamespacedName
			clusterName           string
		)

		Context(
			"When adding cluster", func() {
				BeforeEach(func() {
					clusterName = fmt.Sprintf("storage-%d", GinkgoParallelProcess())
					clusterNamespacedName = test.GetNamespacedName(
						clusterName, namespace,
					)
				})

				AfterEach(func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				})

				Context(
					"When using volume", func() {
						It(
							"Should not allow same name volumes", func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.Storage.Volumes = append(
									aeroCluster.Spec.Storage.Volumes,
									aeroCluster.Spec.Storage.Volumes[0],
								)

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)
					},
				)
				Context(
					"When using volume source", func() {
						It(
							"Should not specify multiple source", func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.Storage.Volumes[0].Source.EmptyDir = &v1.EmptyDirVolumeSource{}
								aeroCluster.Spec.Storage.Volumes[0].Source.Secret = &v1.SecretVolumeSource{
									SecretName: "secret",
								}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should specify one source", func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.Storage.Volumes[0].Source = asdbv1.VolumeSource{}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should specify valid volumeMode for PV source",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								volumeMode := v1.PersistentVolumeMode("invalid")
								aeroCluster.Spec.Storage.Volumes[0].Source.PersistentVolume.VolumeMode = volumeMode

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should specify valid accessMode for PV source",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								accessMode := v1.PersistentVolumeAccessMode("invalid")
								aeroCluster.Spec.Storage.Volumes[0].Source.PersistentVolume.AccessModes =
									[]v1.PersistentVolumeAccessMode{accessMode}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should allow setting labels and annotation in PVC",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								labels := map[string]string{
									"pvc": "labels",
								}
								annotations := map[string]string{
									"pvc": "annotations",
								}
								for i, volume := range aeroCluster.Spec.Storage.Volumes {
									if volume.Source.PersistentVolume != nil {
										aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume.Labels = labels
										aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume.Annotations = annotations
									}
								}
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())

								pvcs, err := getAeroClusterPVCList(
									aeroCluster, k8sClient,
								)
								Expect(err).ShouldNot(HaveOccurred())
								Expect(pvcs).ShouldNot(BeEmpty())

								for _, pvc := range pvcs {
									// Match annotations
									annot, ok := pvc.Annotations["pvc"]
									Expect(ok).To(BeTrue())
									Expect(annot).To(Equal("annotations"))

									// Match label
									lab, ok := pvc.Labels["pvc"]
									Expect(ok).To(BeTrue())
									Expect(lab).To(Equal("labels"))
								}
								Expect(err).ShouldNot(HaveOccurred())
							},
						)
					},
				)
				Context(
					"When using volume attachment", func() {

						It(
							"Should not use invalid container name", func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{
									{
										ContainerName: "invalid",
										Path:          "/opt/aerospike",
									},
								}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should give at least one of the following -> sidecar, initcontainer, aerospike",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{}
								aeroCluster.Spec.Storage.Volumes[0].InitContainers = []asdbv1.VolumeAttachment{}
								aeroCluster.Spec.Storage.Volumes[0].Aerospike = nil

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should not allow mounting same volume at multiple path in a container",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								containerName := "container"
								aeroCluster.Spec.PodSpec = asdbv1.AerospikePodSpec{
									Sidecars: []v1.Container{
										{
											Name: containerName,
										},
									},
								}
								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{
									{
										ContainerName: containerName,
										Path:          "/opt/aerospike",
									},
									{
										ContainerName: containerName,
										Path:          "/opt/aerospike/data",
									},
								}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should not allow mounting multiple volumes at same path in a container",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								containerName := "container"
								aeroCluster.Spec.PodSpec = asdbv1.AerospikePodSpec{
									Sidecars: []v1.Container{
										{
											Name: containerName,
										},
									},
								}
								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{
									{
										ContainerName: containerName,
										Path:          "/opt/aerospike",
									},
								}
								aeroCluster.Spec.Storage.Volumes[1].Sidecars = []asdbv1.VolumeAttachment{
									{
										ContainerName: containerName,
										Path:          "/opt/aerospike",
									},
								}
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should not use aerospike-server container name in sidecars",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{
									{
										ContainerName: asdbv1.AerospikeServerContainerName,
										Path:          "/opt/aerospike/newpath",
									},
								}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)

						It(
							"Should not use aerospike-init container name in initcontainers",
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								aeroCluster.Spec.Storage.Volumes[0].InitContainers = []asdbv1.VolumeAttachment{
									{
										ContainerName: asdbv1.AerospikeInitContainerName,
										Path:          "/opt/aerospike/newpath",
									},
								}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)
					},
				)
				Context(
					"When configuring Non PV workdir", func() {
						It(
							"Should allow emptydir volume for default workdir", func() {
								aeroCluster := createDummyAerospikeClusterWithNonPVWorkdir(
									clusterNamespacedName, "/opt/aerospike", "",
								)

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())
							},
						)
						It(
							"Should allow emptydir volume for any workdir", func() {
								aeroCluster := createDummyAerospikeClusterWithNonPVWorkdir(
									clusterNamespacedName, "/opt/aerospike/data", "/opt/aerospike/data",
								)

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())
							},
						)
						It(
							"Should allow default workdir without any configured volume", func() {
								aeroCluster := createDummyAerospikeClusterWithNonPVWorkdir(
									clusterNamespacedName, "", "",
								)

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ShouldNot(HaveOccurred())
							},
						)
						It(
							"Should fail if workdir configured but volume is not", func() {
								aeroCluster := createDummyAerospikeClusterWithNonPVWorkdir(
									clusterNamespacedName, "", "/opt/aerospike",
								)

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
							},
						)
					},
				)

				Context("When testing mount options for hostPath volumes", func() {
					var (
						aeroCluster               *asdbv1.AerospikeCluster
						mountOptionsForContainers []asdbv1.MountOptions
					)

					BeforeEach(func() {
						aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)

						// Setup common mount options for all container types
						mountOptionsForContainers = []asdbv1.MountOptions{
							{
								ReadOnly:         ptr.To(true),
								SubPath:          "subdir",
								MountPropagation: &[]v1.MountPropagationMode{v1.MountPropagationHostToContainer}[0],
							},
							{
								ReadOnly:         ptr.To(true),
								SubPath:          "custom-init-subdir",
								MountPropagation: &[]v1.MountPropagationMode{v1.MountPropagationHostToContainer}[0],
							},
							{
								ReadOnly:         ptr.To(true),
								SubPath:          "sidecar-subdir",
								MountPropagation: &[]v1.MountPropagationMode{v1.MountPropagationHostToContainer}[0],
							},
						}

						// Add sidecar and init containers
						setupContainersForMountOptionsTest(aeroCluster)
					})

					It("Should validate all mount options in all containers for hostPath volume", func() {
						By("Creating hostPath volume with mount options")
						hostPathVolume := createHostPathVolumeWithMountOptions(mountOptionsForContainers)
						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, hostPathVolume)

						By("Deploying the cluster")
						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Getting the deployed pod")
						pod := getPodForMountOptionsTest(aeroCluster)

						By("Validating Aerospike server container mount options")
						validateContainerMountOptions(pod, asdbv1.AerospikeServerContainerName,
							"hostpath-mount-options-test", &mountOptionsForContainers[0])

						By("Validating init container mount options")
						validateContainerMountOptions(pod, asdbv1.AerospikeInitContainerName,
							"hostpath-mount-options-test", &mountOptionsForContainers[0])

						By("Validating custom init container mount options")
						validateContainerMountOptions(pod, "custom-init-container",
							"hostpath-mount-options-test", &mountOptionsForContainers[1])

						By("Validating sidecar container mount options")
						validateContainerMountOptions(pod, "sidecar-container",
							"hostpath-mount-options-test", &mountOptionsForContainers[2])

						By("Verifying that changing ReadOnly to false is rejected for hostPath volumes")
						volumeIndex := len(aeroCluster.Spec.Storage.Volumes) - 1

						// Try to set read-write for sidecar (should fail)
						aeroCluster.Spec.Storage.Volumes[volumeIndex].Sidecars[0].ReadOnly = ptr.To(false)
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())

						// Try to set read-write for init container (should fail)
						aeroCluster.Spec.Storage.Volumes[volumeIndex].InitContainers[0].ReadOnly = ptr.To(false)
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())

						// Try to set read-write for Aerospike server (should fail)
						aeroCluster.Spec.Storage.Volumes[volumeIndex].Aerospike.ReadOnly = ptr.To(false)
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					})

					It("Should not reflect mount options in aerospike containers for non-hostPath volumes", func() {
						By("Creating emptyDir volume with mount options")
						emptyDirVolume := createEmptyDirVolumeWithMountOptions(mountOptionsForContainers)
						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, emptyDirVolume)

						By("Deploying the cluster")
						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Getting the deployed pod")
						pod := getPodForMountOptionsTest(aeroCluster)

						By("Validating that Aerospike server container ignores mount options for emptyDir")
						validateContainerMountOptions(pod, asdbv1.AerospikeServerContainerName,
							"emptydir-mount-options-test", &asdbv1.MountOptions{})

						By("Validating that init container ignores mount options for emptyDir")
						validateContainerMountOptions(pod, asdbv1.AerospikeInitContainerName,
							"emptydir-mount-options-test", &asdbv1.MountOptions{})

						By("Updating mount options for non-hostPath volume in sidecrs (should succeed)")
						volumeIndex := len(aeroCluster.Spec.Storage.Volumes) - 1

						aeroCluster.Spec.Storage.Volumes[volumeIndex].Sidecars[0].ReadOnly = ptr.To(false)
						aeroCluster.Spec.Storage.Volumes[volumeIndex].InitContainers[0].ReadOnly = ptr.To(false)
						aeroCluster.Spec.Storage.Volumes[volumeIndex].Aerospike.ReadOnly = ptr.To(true)

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying mount options after update")
						err = k8sClient.Get(context.TODO(), test.GetNamespacedName(aeroCluster.Name+"-0-1", aeroCluster.Namespace), pod)
						Expect(err).ToNot(HaveOccurred())

						// Aerospike server should still ignore mount options
						validateContainerMountOptions(pod, asdbv1.AerospikeServerContainerName,
							"emptydir-mount-options-test", &asdbv1.MountOptions{})

						// Custom init container should respect ReadOnly change
						expectedInitMountOptions := mountOptionsForContainers[1]
						expectedInitMountOptions.ReadOnly = ptr.To(false)
						validateContainerMountOptions(pod, "custom-init-container",
							"emptydir-mount-options-test", &expectedInitMountOptions)

						// Sidecar should respect ReadOnly change
						expectedSidecarMountOptions := mountOptionsForContainers[2]
						expectedSidecarMountOptions.ReadOnly = ptr.To(false)
						validateContainerMountOptions(pod, "sidecar-container",
							"emptydir-mount-options-test", &expectedSidecarMountOptions)
					})
				})
			},
		)

		Context(
			"When cluster is already deployed", func() {
				clusterName = fmt.Sprintf("storage-%d", GinkgoParallelProcess())
				clusterNamespacedName = test.GetNamespacedName(
					clusterName, namespace,
				)

				BeforeEach(
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterName,
								Namespace: namespace,
							},
						}

						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
					},
				)

				Context(
					"When using volume", func() {
						It(
							"Should not allow adding PV volume", func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								volume := asdbv1.VolumeSpec{
									Name: "newvolume",
									Source: asdbv1.VolumeSource{
										PersistentVolume: &asdbv1.PersistentVolumeSpec{
											Size:         resource.MustParse("1Gi"),
											StorageClass: storageClass,
											VolumeMode:   v1.PersistentVolumeFilesystem,
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/newvolume",
									},
								}
								aeroCluster.Spec.Storage.Volumes = append(
									aeroCluster.Spec.Storage.Volumes, volume,
								)

								err = k8sClient.Update(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)
						It(
							"Should not allow deleting PV volume", func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.Storage.Volumes = []asdbv1.VolumeSpec{}

								err = k8sClient.Update(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)
						It(
							"Should allow adding/deleting Non PV volume",
							func() {
								// Add
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								volume := asdbv1.VolumeSpec{
									Name: "newvolume",
									Source: asdbv1.VolumeSource{
										EmptyDir: &v1.EmptyDirVolumeSource{},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/newvolume",
									},
								}

								aeroCluster.Spec.Storage.Volumes = append(
									aeroCluster.Spec.Storage.Volumes, volume,
								)
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								// Delete

								newAeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								aeroCluster.Spec = newAeroCluster.Spec

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)
				Context(
					"When using volume source", func() {
						It(
							"Should not allow update of PV source", func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.Storage.Volumes[0].Source = asdbv1.VolumeSource{
									EmptyDir: &v1.EmptyDirVolumeSource{},
								}

								err = k8sClient.Update(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)
						It(
							"Should not allow updating PVC", func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.Storage.Volumes[0].Source.PersistentVolume.
									Labels = map[string]string{"pvc": "labels"}

								err = k8sClient.Update(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)

						It(
							"Should allow update of Non-PV source", func() {
								// Add volume
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								volume := asdbv1.VolumeSpec{
									Name: "newvolume",
									Source: asdbv1.VolumeSource{
										EmptyDir: &v1.EmptyDirVolumeSource{},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/newvolume",
									},
								}

								aeroCluster.Spec.Storage.Volumes = append(
									aeroCluster.Spec.Storage.Volumes, volume,
								)
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								// Update

								volumes := aeroCluster.Spec.Storage.Volumes
								aeroCluster.Spec.Storage.Volumes[len(volumes)-1].Source = asdbv1.VolumeSource{
									Secret: &v1.SecretVolumeSource{
										SecretName: test.AuthSecretName,
									},
								}

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)
				Context(
					"When using volume attachment", func() {
						It(
							"Should allow adding/deleting volume attachments",
							func() {
								// Add
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								containerName := "tomcat"
								aeroCluster.Spec.PodSpec.Sidecars = []v1.Container{
									{
										Name:  containerName,
										Image: "tomcat:8.0",
										Ports: []v1.ContainerPort{
											{
												ContainerPort: 7500,
											},
										},
									},
								}

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								va := asdbv1.VolumeAttachment{
									ContainerName: containerName,
									Path:          "/newpath",
								}
								aeroCluster.Spec.Storage.Volumes[0].Sidecars = append(
									aeroCluster.Spec.Storage.Volumes[0].Sidecars,
									va,
								)

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								// Update

								aeroCluster.Spec.Storage.Volumes[0].Sidecars[0].Path = "/newpath2"
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								// Delete

								aeroCluster.Spec.Storage.Volumes[0].Sidecars = []asdbv1.VolumeAttachment{}
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)
			},
		)
	},
)

// getVolumeMountOptions retrieves mount options for a specific volume in a container
//

func getVolumeMountOptions(pod *v1.Pod, containerName,
	volumeName string) (*asdbv1.MountOptions, error) {
	// Check containers first (includes Aerospike server and sidecars)
	for idx := range pod.Spec.Containers {
		if pod.Spec.Containers[idx].Name == containerName {
			for _, vm := range pod.Spec.Containers[idx].VolumeMounts {
				if vm.Name == volumeName {
					return &asdbv1.MountOptions{
						ReadOnly:         ptr.To(vm.ReadOnly),
						SubPath:          vm.SubPath,
						SubPathExpr:      vm.SubPathExpr,
						MountPropagation: vm.MountPropagation,
					}, nil
				}
			}

			return nil, fmt.Errorf("volume %s not found in container %s mounts", volumeName, containerName)
		}
	}

	// Check init containers if not found in containers
	for idx := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[idx].Name == containerName {
			for _, vm := range pod.Spec.InitContainers[idx].VolumeMounts {
				if vm.Name == volumeName {
					return &asdbv1.MountOptions{
						ReadOnly:         ptr.To(vm.ReadOnly),
						SubPath:          vm.SubPath,
						SubPathExpr:      vm.SubPathExpr,
						MountPropagation: vm.MountPropagation,
					}, nil
				}
			}

			return nil, fmt.Errorf("volume %s not found in init container %s mounts", volumeName, containerName)
		}
	}

	return nil, fmt.Errorf("container %s not found in pod", containerName)
}

// Taking workdirVolumePath and workdirConfigPath separately as
// workdirConfigPath can be empty in case of default workdir
func createDummyAerospikeClusterWithNonPVWorkdir(
	clusterNamespacedName types.NamespacedName, workdirVolumePath, workdirConfigPath string) *asdbv1.AerospikeCluster {
	aeroCluster := createDummyAerospikeCluster(
		clusterNamespacedName, 2,
	)

	aeroCluster.Spec.ValidationPolicy = &asdbv1.ValidationPolicySpec{
		SkipWorkDirValidate: true,
	}

	aeroCluster.Spec.PodSpec = getNonRootPodSpec()

	aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{
		Volumes: []asdbv1.VolumeSpec{
			getStorageVolumeForSecret(),
		},
	}

	if workdirVolumePath != "" {
		workdirVolume := asdbv1.VolumeSpec{
			Name: "workdir",
			Source: asdbv1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
				Path: workdirVolumePath,
			},
		}

		aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, workdirVolume)
	}

	if workdirConfigPath != "" {
		aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{
			"feature-key-file": "/etc/aerospike/secret/features.conf",
			"work-directory":   workdirConfigPath,
		}
	}

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		getNonSCInMemoryNamespaceConfig("mem"),
	}

	return aeroCluster
}

func validateMountOptions(currentMountOptions, expectedMountOptions *asdbv1.MountOptions) {
	Expect(asdbv1.GetBool(currentMountOptions.ReadOnly)).To(Equal(asdbv1.GetBool(expectedMountOptions.ReadOnly)))
	Expect(currentMountOptions.SubPath).To(Equal(expectedMountOptions.SubPath))
	Expect(currentMountOptions.SubPathExpr).To(BeEmpty())
	Expect(reflect.DeepEqual(expectedMountOptions.MountPropagation, currentMountOptions.MountPropagation)).To(BeTrue())
}

func setupContainersForMountOptionsTest(aeroCluster *asdbv1.AerospikeCluster) {
	// Add sidecar container
	aeroCluster.Spec.PodSpec.Sidecars = []v1.Container{
		{
			Name:  "sidecar-container",
			Image: "busybox:latest",
			Command: []string{
				"/bin/sh",
				"-c",
				"sleep 1000",
			},
		},
	}

	// Add init container
	aeroCluster.Spec.PodSpec.InitContainers = []v1.Container{
		{
			Name:  "custom-init-container",
			Image: "busybox:latest",
			Command: []string{
				"/bin/sh",
				"-c",
				"sleep 10",
			},
		},
	}
}

func createHostPathVolumeWithMountOptions(mountOptions []asdbv1.MountOptions) asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: "hostpath-mount-options-test",
		Source: asdbv1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: "/tmp/aerospike-test",
				Type: &[]v1.HostPathType{v1.HostPathDirectoryOrCreate}[0],
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: "/mnt/hostpath-data",
			AttachmentOptions: asdbv1.AttachmentOptions{
				MountOptions: mountOptions[0],
			},
		},
		InitContainers: []asdbv1.VolumeAttachment{
			{
				ContainerName: "custom-init-container",
				Path:          "/mnt/hostpath-custom-init",
				AttachmentOptions: asdbv1.AttachmentOptions{
					MountOptions: mountOptions[1],
				},
			},
		},
		Sidecars: []asdbv1.VolumeAttachment{
			{
				ContainerName: "sidecar-container",
				Path:          "/mnt/hostpath-sidecar",
				AttachmentOptions: asdbv1.AttachmentOptions{
					MountOptions: mountOptions[2],
				},
			},
		},
	}
}

func createEmptyDirVolumeWithMountOptions(mountOptions []asdbv1.MountOptions) asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: "emptydir-mount-options-test",
		Source: asdbv1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: "/mnt/hostpath-data",
			AttachmentOptions: asdbv1.AttachmentOptions{
				MountOptions: mountOptions[0],
			},
		},
		InitContainers: []asdbv1.VolumeAttachment{
			{
				ContainerName: "custom-init-container",
				Path:          "/mnt/hostpath-custom-init",
				AttachmentOptions: asdbv1.AttachmentOptions{
					MountOptions: mountOptions[1],
				},
			},
		},
		Sidecars: []asdbv1.VolumeAttachment{
			{
				ContainerName: "sidecar-container",
				Path:          "/mnt/hostpath-sidecar",
				AttachmentOptions: asdbv1.AttachmentOptions{
					MountOptions: mountOptions[2],
				},
			},
		},
	}
}

func getPodForMountOptionsTest(aeroCluster *asdbv1.AerospikeCluster) *v1.Pod {
	podNamespacedName := test.GetNamespacedName(aeroCluster.Name+"-0-1", aeroCluster.Namespace)
	pod := &v1.Pod{}
	err := k8sClient.Get(context.TODO(), podNamespacedName, pod)
	Expect(err).ToNot(HaveOccurred())

	return pod
}

func validateContainerMountOptions(
	pod *v1.Pod,
	containerName string,
	volumeName string,
	expectedMountOptions *asdbv1.MountOptions,
) {
	mountOptions, err := getVolumeMountOptions(pod, containerName, volumeName)
	Expect(err).ToNot(HaveOccurred(),
		"Failed to get mount options for container %s and volume %s", containerName, volumeName)
	validateMountOptions(mountOptions, expectedMountOptions)
}
