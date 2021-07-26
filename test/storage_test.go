package test

import (
	goctx "context"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

var _ = Describe("Using storage volumes", func() {
	ctx := goctx.Background()

	clusterName := "storage"
	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	Context("When adding cluster", func() {
		Context("When using volume", func() {
			It("Should not allow same name volumes", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, aeroCluster.Spec.Storage.Volumes[0])

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
		})
		Context("When using volume source", func() {
			It("Should not specify multiple source", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.Storage.Volumes[0].Source.EmptyDir = &v1.EmptyDirVolumeSource{}
				aeroCluster.Spec.Storage.Volumes[0].Source.Secret = &v1.SecretVolumeSource{
					SecretName: "secret",
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should specify one source", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.Storage.Volumes[0].Source = v1alpha1.VolumeSource{}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should specify valid volumeMode for PV source", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				volumeMode := v1.PersistentVolumeMode("invalid")
				aeroCluster.Spec.Storage.Volumes[0].Source.PersistentVolume.VolumeMode = volumeMode

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should specify valid accessMode for PV source", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				accessMode := v1.PersistentVolumeAccessMode("invalid")
				aeroCluster.Spec.Storage.Volumes[0].Source.PersistentVolume.AccessModes = []v1.PersistentVolumeAccessMode{accessMode}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

		})
		Context("When using volume attachment", func() {

			It("Should not use invalid container name", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{
					{
						ContainerName: "invalid",
						Path:          "/opt/aerospike",
					},
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should give at least one of the following -> sidecar, initcontainer, aerospike", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{}
				aeroCluster.Spec.Storage.Volumes[0].InitContainers = []v1alpha1.VolumeAttachment{}
				aeroCluster.Spec.Storage.Volumes[0].Aerospike = nil

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should not allow mounting same volume at multiple path in a container", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				containerName := "container"
				aeroCluster.Spec.PodSpec = v1alpha1.AerospikePodSpec{
					Sidecars: []v1.Container{
						{
							Name: containerName,
						},
					},
				}
				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike",
					},
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/data",
					},
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should not allow mounting multiple volumes at same path in a container", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				containerName := "container"
				aeroCluster.Spec.PodSpec = v1alpha1.AerospikePodSpec{
					Sidecars: []v1.Container{
						{
							Name: containerName,
						},
					},
				}
				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike",
					},
				}
				aeroCluster.Spec.Storage.Volumes[1].Sidecars = []v1alpha1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike",
					},
				}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should not use aerospike-server container name in sidecars", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{
					{
						ContainerName: v1alpha1.AerospikeServerContainerName,
						Path:          "/opt/aerospike/newpath",
					},
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should not use aerospike-init container name in initcontainers", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.Storage.Volumes[0].InitContainers = []v1alpha1.VolumeAttachment{
					{
						ContainerName: v1alpha1.AerospikeServerInitContainerName,
						Path:          "/opt/aerospike/newpath",
					},
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

		})

	})

	Context("When cluster is already deployed", func() {
		BeforeEach(func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("When using volume", func() {
			It("Should not allow adding PV volume", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				volume := v1alpha1.VolumeSpec{
					Name: "newvolume",
					Source: v1alpha1.VolumeSource{
						PersistentVolume: &v1alpha1.PersistentVolumeSpec{
							Size:         resource.MustParse("1Gi"),
							StorageClass: storageClass,
							VolumeMode:   v1.PersistentVolumeFilesystem,
						},
					},
					Aerospike: &v1alpha1.AerospikeServerVolumeAttachment{
						Path: "/newvolume",
					},
				}
				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, volume)

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
			It("Should not allow deleting PV volume", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Storage.Volumes = []v1alpha1.VolumeSpec{}

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
			It("Should allow adding/deleting Non PV volume", func() {
				// Add
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				volume := v1alpha1.VolumeSpec{
					Name: "newvolume",
					Source: v1alpha1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
					Aerospike: &v1alpha1.AerospikeServerVolumeAttachment{
						Path: "/newvolume",
					},
				}

				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, volume)
				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Delete
				newAeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec = newAeroCluster.Spec

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("When using volume source", func() {
			It("Should not allow update of PV source", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Storage.Volumes[0].Source = v1alpha1.VolumeSource{
					EmptyDir: &v1.EmptyDirVolumeSource{},
				}

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("Should allow update of Non-PV source", func() {
				// Add volume
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				volume := v1alpha1.VolumeSpec{
					Name: "newvolume",
					Source: v1alpha1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
					Aerospike: &v1alpha1.AerospikeServerVolumeAttachment{
						Path: "/newvolume",
					},
				}

				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, volume)
				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Update
				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				volumes := aeroCluster.Spec.Storage.Volumes
				aeroCluster.Spec.Storage.Volumes[len(volumes)-1].Source = v1alpha1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: authSecretName,
					},
				}

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			})
		})
		Context("When using volume attachment", func() {
			It("Should allow adding/deleting volume attachments", func() {
				// Add
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				containerName := "tomcat"
				aeroCluster.Spec.PodSpec = v1alpha1.AerospikePodSpec{
					Sidecars: []v1.Container{
						{
							Name:  containerName,
							Image: "tomcat:8.0",
							Ports: []v1.ContainerPort{
								{
									ContainerPort: 7500,
								},
							},
						},
					},
				}

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				va := v1alpha1.VolumeAttachment{
					ContainerName: containerName,
					Path:          "/newpath",
				}
				aeroCluster.Spec.Storage.Volumes[0].Sidecars = append(aeroCluster.Spec.Storage.Volumes[0].Sidecars, va)
				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Update
				aeroCluster.Spec.Storage.Volumes[0].Sidecars[0].Path = "/newpath2"
				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Delete
				aeroCluster.Spec.Storage.Volumes[0].Sidecars = []v1alpha1.VolumeAttachment{}
				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

})
