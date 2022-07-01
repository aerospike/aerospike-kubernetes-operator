package test

import (
	"strconv"
	"time"

	goctx "context"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe(
	"STSStorage", func() {
		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {

				BeforeEach(
					func() {
						clusterName := "sts-storage"
						clusterNamespacedName := getClusterNamespacedName(
							clusterName, namespace,
						)

						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						// Setup: Deploy cluster without rack
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)
				AfterEach(
					func() {
						clusterName := "sts-storage"
						clusterNamespacedName := getClusterNamespacedName(
							clusterName, namespace,
						)

						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should allow external volume mount on aerospike nodes", func() {
						clusterName := "sts-storage"
						clusterNamespacedName := getClusterNamespacedName(
							clusterName, namespace,
						)

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
						sts, err = getSTSFromRackID(aeroCluster, 0)
						Expect(err).ToNot(HaveOccurred())

						rackPodList, err := getRackPodList(k8sClient, ctx, sts)
						Expect(err).ToNot(HaveOccurred())

						// restart pod
						for _, pod := range rackPodList.Items {
							err := k8sClient.Delete(ctx, &pod)
							Expect(err).ToNot(HaveOccurred())
							started := waitForPod(&pod)
							Expect(started).To(
								BeTrue(), "pod was not able to come online in time",
							)
						}

						isPresent, err := validateExternalVolume(sts)
						Expect(err).ToNot(HaveOccurred())
						Expect(isPresent).To(
							BeTrue(), "Unable to find volume",
						)

						//>>>>>>>>>>  perform resize op.
						err = scaleUpClusterTest(
							k8sClient, ctx, clusterNamespacedName, 2,
						)
						Expect(err).ToNot(HaveOccurred())
						time.Sleep(5 * time.Second)

						isPresent, err = validateExternalVolume(sts)
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

func getSTSFromRackID(aeroCluster *asdbv1beta1.AerospikeCluster, rackID int) (
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

func validateExternalVolume(sts *appsv1.StatefulSet) (
	bool, error,
) {
	rackPodList, err := getRackPodList(k8sClient, goctx.TODO(), sts)
	if err != nil {
		return false, err
	}
	for _, pod := range rackPodList.Items {
		pkgLog.Info("Checking for pod", "podName", pod.Name)
		volumeMounts := pod.Spec.Containers[0].VolumeMounts
		for _, volumeMount := range volumeMounts {
			pkgLog.Info("Checking for pod", "volumeName", volumeMount.Name)
			if volumeMount.Name == "tzdata" {
				return true, nil
			}
		}
	}
	return false, nil
}

func getNamespacedNameForSTS(
	aeroCluster *asdbv1beta1.AerospikeCluster, rackID int,
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
