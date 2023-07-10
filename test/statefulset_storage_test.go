package test

import (
	goctx "context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

var _ = Describe(
	"STSStorage", func() {
		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {
				clusterName := "sts-storage"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				BeforeEach(
					func() {
						aeroCluster := createNonSCDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						// Setup: Deploy cluster without rack
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)
				AfterEach(
					func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should allow external volume mount on aerospike nodes", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						sts, err := getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						mountedVolumes := sts.Spec.Template.Spec.Containers[0].VolumeMounts
						mountedVolumes = append(mountedVolumes, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						volumes := sts.Spec.Template.Spec.Volumes
						volumes = append(volumes, v1.Volume{
							Name: "tzdata",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/etc/localtime",
								},
							},
						})

						sts.Spec.Template.Spec.Containers[0].VolumeMounts = mountedVolumes
						sts.Spec.Template.Spec.Volumes = volumes

						err = updateSTS(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						sts, err = getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						rackPodList, err := getRackPodList(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						// restart pod
						for podIndex := range rackPodList.Items {
							err = k8sClient.Delete(ctx, &rackPodList.Items[podIndex])
							Expect(err).ToNot(HaveOccurred())
							started := waitForPod(&rackPodList.Items[podIndex])
							Expect(started).To(
								BeTrue(), "pod was not able to come online in time",
							)
						}

						isPresent, err := validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						// >>>>>>>>>>  perform resize op.
						err = scaleUpClusterTest(
							k8sClient, ctx, clusterNamespacedName, 2,
						)
						Expect(err).ToNot(HaveOccurred())
						time.Sleep(5 * time.Second)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)
					},
				)

				It(
					"Should not affect external volume mount when adding or deleting aerospike volumes", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						sts, err := getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						mountedVolumes := sts.Spec.Template.Spec.Containers[0].VolumeMounts
						mountedVolumes = append(mountedVolumes, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						initcontainerMountedVolumes := sts.Spec.Template.Spec.InitContainers[0].VolumeMounts
						initcontainerMountedVolumes = append(initcontainerMountedVolumes, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						volumes := sts.Spec.Template.Spec.Volumes
						volumes = append(volumes, v1.Volume{
							Name: "tzdata",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/etc/localtime",
								},
							},
						})

						sts.Spec.Template.Spec.Containers[0].VolumeMounts = mountedVolumes
						sts.Spec.Template.Spec.InitContainers[0].VolumeMounts = initcontainerMountedVolumes
						sts.Spec.Template.Spec.Volumes = volumes

						err = updateSTS(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						sts, err = getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						rackPodList, err := getRackPodList(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						// restart pod
						for podIndex := range rackPodList.Items {
							err = k8sClient.Delete(ctx, &rackPodList.Items[podIndex])
							Expect(err).ToNot(HaveOccurred())
							started := waitForPod(&rackPodList.Items[podIndex])
							Expect(started).To(
								BeTrue(), "pod was not able to come online in time",
							)
						}

						isPresent, err := validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, true)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						aeroCluster, err = getCluster(
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

						isPresent, err = validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, true)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						// Delete
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						newAeroCluster := createNonSCDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec = newAeroCluster.Spec

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						isPresent, err = validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, true)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)
					},
				)

				It(
					"Should not affect external volume mount in sidecars when adding aerospike volumes", func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						sidecar1 := v1.Container{
							Name:  "nginx1",
							Image: "nginx:1.14.2",
							Ports: []v1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						}

						aeroCluster.Spec.PodSpec.Sidecars = append(
							aeroCluster.Spec.PodSpec.Sidecars, sidecar1,
						)

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						sts, err := getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						mountedVolumes := sts.Spec.Template.Spec.Containers[0].VolumeMounts
						mountedVolumes = append(mountedVolumes, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						mountedVolumesSidecar := sts.Spec.Template.Spec.Containers[1].VolumeMounts
						mountedVolumesSidecar = append(mountedVolumesSidecar, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						initcontainerMountedVolumes := sts.Spec.Template.Spec.InitContainers[0].VolumeMounts
						initcontainerMountedVolumes = append(initcontainerMountedVolumes, v1.VolumeMount{
							Name: "tzdata", MountPath: "/etc/localtime",
						})

						volumes := sts.Spec.Template.Spec.Volumes
						volumes = append(volumes, v1.Volume{
							Name: "tzdata",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/etc/localtime",
								},
							},
						})

						sts.Spec.Template.Spec.Containers[0].VolumeMounts = mountedVolumes
						sts.Spec.Template.Spec.Containers[1].VolumeMounts = mountedVolumesSidecar
						sts.Spec.Template.Spec.InitContainers[0].VolumeMounts = initcontainerMountedVolumes
						sts.Spec.Template.Spec.Volumes = volumes

						err = updateSTS(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						sts, err = getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						rackPodList, err := getRackPodList(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						// restart pod
						for podIndex := range rackPodList.Items {
							err = k8sClient.Delete(ctx, &rackPodList.Items[podIndex])
							Expect(err).ToNot(HaveOccurred())
							started := waitForPod(&rackPodList.Items[podIndex])
							Expect(started).To(
								BeTrue(), "pod was not able to come online in time",
							)
						}

						isPresent, err := validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 1, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, true)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)
						aeroCluster, err = getCluster(
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

						isPresent, err = validateExternalVolumeInContainer(sts, 0, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 1, false)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						isPresent, err = validateExternalVolumeInContainer(sts, 0, true)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

					},
				)
			},
		)
	},
)

//nolint:unparam // generic function
func getSTSFromRackID(aeroCluster *asdbv1.AerospikeCluster, rackID int) (
	*appsv1.StatefulSet, error,
) {
	found := &appsv1.StatefulSet{}
	err := k8sClient.Get(
		goctx.TODO(),
		getNamespacedNameForSTS(aeroCluster, rackID),
		found,
	)

	if err != nil {
		return nil, err
	}

	return found, nil
}

func validateExternalVolumeInContainer(sts *appsv1.StatefulSet, index int, isInit bool) (
	bool, error,
) {
	rackPodList, err := getRackPodList(k8sClient, goctx.TODO(), sts)
	if err != nil {
		return false, err
	}

	for podIndex := range rackPodList.Items {
		var container v1.Container
		if isInit {
			container = rackPodList.Items[podIndex].Spec.InitContainers[index]
		} else {
			container = rackPodList.Items[podIndex].Spec.Containers[index]
		}

		volumeMounts := container.VolumeMounts
		for _, volumeMount := range volumeMounts {
			pkgLog.Info("Checking for pod", "volumeName", volumeMount.Name, "in container", container.Name)

			if volumeMount.Name == "tzdata" {
				return true, nil
			}
		}
	}

	return false, nil
}

func getNamespacedNameForSTS(
	aeroCluster *asdbv1.AerospikeCluster, rackID int,
) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func waitForPod(pod *v1.Pod) bool {
	var started bool

	for i := 0; i < 20; i++ {
		updatedPod := &v1.Pod{}

		err := k8sClient.Get(
			goctx.TODO(),
			types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace},
			updatedPod,
		)
		if err != nil {
			time.Sleep(time.Second * 5)
			continue
		}

		if !utils.IsPodRunningAndReady(updatedPod) {
			time.Sleep(time.Second * 5)
			continue
		}

		started = true

		break
	}

	return started
}
