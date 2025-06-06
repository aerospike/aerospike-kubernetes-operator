package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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
