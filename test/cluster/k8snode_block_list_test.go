package cluster

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

const (
	wrongImage = "wrong-image"
)

var _ = Describe(
	"K8sNodeBlockList", func() {
		ctx := context.TODO()
		Context(
			"Migrate pods from K8s blocked nodes", func() {
				var (
					podName    string
					oldK8sNode string
					oldPvcInfo map[string]types.UID
				)

				clusterName := fmt.Sprintf("k8s-node-block-cluster-%d", GinkgoParallelProcess())
				clusterNamespacedName := getNamespacedName(clusterName, namespace)
				aeroCluster := &asdbv1.AerospikeCluster{}

				var (
					err   error
					zones []string
				)

				BeforeEach(
					func() {
						aeroCluster = createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)

						// Zones are set to distribute the pods across different zone nodes.
						zones, err = getZones(ctx, k8sClient)
						Expect(err).ToNot(HaveOccurred())

						zone1 := zones[0]
						zone2 := zones[0]
						if len(zones) > 1 {
							zone2 = zones[1]
						}

						batchSize := intstr.FromString("100%")
						rackConf := asdbv1.RackConfig{
							Racks: []asdbv1.Rack{
								{ID: 1, Zone: zone1},
								{ID: 2, Zone: zone2},
							},
							RollingUpdateBatchSize: &batchSize,
							Namespaces:             []string{"test"},
						}

						aeroCluster.Spec.RackConfig = rackConf

						aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
						aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface {
						})["service"].(map[string]interface{})["port"] = serviceNonTLSPort + GinkgoParallelProcess()*10
						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						podName = clusterName + "-2-0"
						pod := &corev1.Pod{}
						err = k8sClient.Get(ctx, getNamespacedName(podName, namespace), pod)
						Expect(err).ToNot(HaveOccurred())
						oldK8sNode = pod.Spec.NodeName
						oldPvcInfo, err = extractPodPVC(pod)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						_ = cleanupPVC(k8sClient, namespace, aeroCluster.Name)
					},
				)

				It(
					"Should migrate the pods from blocked nodes to other nodes", func() {
						By("Blocking the k8s node")
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.K8sNodeBlockList = []string{oldK8sNode}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying if the pod is migrated to other nodes and pod pvcs are not deleted")
						validatePodAndPVCMigration(ctx, podName, oldK8sNode, oldPvcInfo, true)
					},
				)

				It(
					"Should migrate the pods from blocked nodes to other nodes along with rolling "+
						"restart", func() {
						By("Blocking the k8s node and updating aerospike config")
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.K8sNodeBlockList = []string{oldK8sNode}
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] =
							defaultProtofdmax

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying if the pod is migrated to other nodes and pod pvcs are not deleted")
						validatePodAndPVCMigration(ctx, podName, oldK8sNode, oldPvcInfo, true)
					},
				)

				It(
					"Should migrate the pods from blocked nodes to other nodes along with upgrade", func() {
						By("Blocking the k8s node and updating aerospike image")
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.K8sNodeBlockList = []string{oldK8sNode}
						aeroCluster.Spec.Image = availableImage1

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying if the pod is migrated to other nodes and pod pvcs are not deleted")
						validatePodAndPVCMigration(ctx, podName, oldK8sNode, oldPvcInfo, true)
					},
				)

				It(
					"Should migrate the pods from blocked nodes to other nodes and delete corresponding"+
						"local PVCs", func() {
						By("Blocking the k8s node")
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())
						aeroCluster.Spec.K8sNodeBlockList = []string{oldK8sNode}
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying if the pod is migrated to other nodes and pod local pvcs are deleted")
						validatePodAndPVCMigration(ctx, podName, oldK8sNode, oldPvcInfo, false)
					},
				)

				It(
					"Should migrate the failed pods from blocked nodes to other nodes with maxIgnorablePod", func() {
						By(fmt.Sprintf("Fail %s aerospike pod", podName))
						pod := &corev1.Pod{}
						err := k8sClient.Get(ctx, types.NamespacedName{Name: podName,
							Namespace: clusterNamespacedName.Namespace}, pod)
						Expect(err).ToNot(HaveOccurred())

						pod.Spec.Containers[0].Image = wrongImage
						err = k8sClient.Update(ctx, pod)
						Expect(err).ToNot(HaveOccurred())

						By("Blocking the k8s node and setting maxIgnorablePod to 1")
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())
						maxIgnorablePods := intstr.FromInt32(1)
						aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorablePods
						aeroCluster.Spec.K8sNodeBlockList = []string{oldK8sNode}
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verifying if the failed pod is migrated to other nodes and pod pvcs are not deleted")
						validatePodAndPVCMigration(ctx, podName, oldK8sNode, oldPvcInfo, true)
					},
				)
			},
		)
	},
)

func extractPodPVC(pod *corev1.Pod) (map[string]types.UID, error) {
	pvcUIDMap := make(map[string]types.UID)

	for idx := range pod.Spec.Volumes {
		if pod.Spec.Volumes[idx].PersistentVolumeClaim != nil {
			pvcUIDMap[pod.Spec.Volumes[idx].PersistentVolumeClaim.ClaimName] = ""
		}
	}

	for p := range pvcUIDMap {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := k8sClient.Get(context.TODO(), getNamespacedName(p, pod.Namespace), pvc); err != nil {
			return nil, err
		}

		pvcUIDMap[p] = pvc.UID
	}

	return pvcUIDMap, nil
}

func validatePVCDeletion(ctx context.Context, pvcUIDMap map[string]types.UID, shouldDelete bool) error {
	pvc := &corev1.PersistentVolumeClaim{}

	for pvcName, pvcUID := range pvcUIDMap {
		pvcNamespacesName := getNamespacedName(
			pvcName, namespace,
		)

		if err := k8sClient.Get(ctx, pvcNamespacesName, pvc); err != nil {
			return err
		}

		if shouldDelete && pvc.UID != pvcUID {
			return fmt.Errorf("PVC %s is unintentionally deleted", pvcName)
		}

		if !shouldDelete && pvc.UID == pvcUID {
			return fmt.Errorf("PVC %s is not deleted", pvcName)
		}
	}

	return nil
}

func validatePodAndPVCMigration(ctx context.Context, podName, oldK8sNode string,
	oldPvcInfo map[string]types.UID, shouldDelete bool) {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, getNamespacedName(podName, namespace), pod)
	Expect(err).ToNot(HaveOccurred())
	Expect(pod.Spec.NodeName).ToNot(Equal(oldK8sNode))

	err = validatePVCDeletion(ctx, oldPvcInfo, shouldDelete)
	Expect(err).ToNot(HaveOccurred())
}
