package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	lib "github.com/aerospike/aerospike-management-lib"
)

var _ = Describe(
	"LocalPVCDelete", func() {
		ctx := goctx.TODO()
		clusterName := fmt.Sprintf("local-pvc-%d", GinkgoParallelProcess())
		migrateFillDelay := int64(300)
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		Context("When doing valid operations", func() {
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

			Context("When doing rolling restart", func() {
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set MFD dynamically",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Updating pod metadata to trigger rolling restart")
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
							Labels: map[string]string{
								"test-label": "test-value",
							},
						}

						updateAndValidateIntermediateMFD(ctx, k8sClient, aeroCluster, migrateFillDelay)

						By("Validating PVCs deletion")
						validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
					})

				It("Should delete the local PVCs of only 1 rack when deleteLocalStorageOnRestart is set "+
					"at the rack level and set MFD dynamically", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 2,
					)
					rackConfig := asdbv1.RackConfig{
						Racks: getDummyRackConf(1, 2),
					}
					storage := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
					rackConfig.Racks[0].InputStorage = storage
					rackConfig.Racks[0].InputStorage.DeleteLocalStorageOnRestart = ptr.To(true)
					rackConfig.Racks[0].InputStorage.LocalStorageClasses = []string{storageClass}
					aeroCluster.Spec.RackConfig = rackConfig
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Updating pod metadata to trigger rolling restart")
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{
							"test-label": "test-value",
						},
					}

					updateAndValidateIntermediateMFD(ctx, k8sClient, aeroCluster, migrateFillDelay)

					By("Validating PVCs deletion")
					validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, clusterName+"-2-0")
				})
			})

			Context("When doing upgrade", func() {
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set MFD dynamically",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Enable DeleteLocalStorageOnRestart and set localStorageClasses")
						aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							aeroCluster.Name + "-0-0": noRestart,
							aeroCluster.Name + "-0-1": noRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())

						By("Updating the image")
						Expect(UpdateClusterImage(aeroCluster, nextImage)).ToNot(HaveOccurred())

						updateAndValidateIntermediateMFD(ctx, k8sClient, aeroCluster, migrateFillDelay)

						By("Validating PVCs deletion")
						validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
					})

				It("Should delete the local PVCs of only 1 rack when deleteLocalStorageOnRestart is set "+
					"at the rack level and set MFD dynamically", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 2,
					)
					rackConfig := asdbv1.RackConfig{
						Racks: getDummyRackConf(1, 2),
					}
					storage := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
					rackConfig.Racks[0].InputStorage = storage
					rackConfig.Racks[0].InputStorage.DeleteLocalStorageOnRestart = ptr.To(true)
					rackConfig.Racks[0].InputStorage.LocalStorageClasses = []string{storageClass}
					aeroCluster.Spec.RackConfig = rackConfig
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Updating the image")
					Expect(UpdateClusterImage(aeroCluster, nextImage)).ToNot(HaveOccurred())

					updateAndValidateIntermediateMFD(ctx, k8sClient, aeroCluster, migrateFillDelay)

					By("Validating PVCs deletion")
					validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, clusterName+"-2-0")
				})
			})
		})

		Context("When doing invalid operations", func() {
			It("Should fail when deleteLocalStorageOnRestart is set but localStorageClasses is not set", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
			})
		})
	})

func validateClusterPVCDeletion(ctx goctx.Context, oldPvcInfoPerPod map[string]map[string]types.UID,
	podsToSkipFromPVCDeletion ...string) {
	for podName := range oldPvcInfoPerPod {
		shouldDeletePVC := true

		if utils.ContainsString(podsToSkipFromPVCDeletion, podName) {
			shouldDeletePVC = false
		}

		err := validatePVCDeletion(ctx, oldPvcInfoPerPod[podName], shouldDeletePVC)
		Expect(err).ToNot(HaveOccurred())
	}
}

func extractClusterPVC(ctx goctx.Context, k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
) (map[string]map[string]types.UID, error) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return nil, err
	}

	oldPvcInfoPerPod := make(map[string]map[string]types.UID)

	for idx := range podList.Items {
		pvcMap, err := extractPodPVC(&podList.Items[idx])
		if err != nil {
			return nil, err
		}

		oldPvcInfoPerPod[podList.Items[idx].Name] = pvcMap
	}

	return oldPvcInfoPerPod, nil
}

func updateAndValidateIntermediateMFD(ctx goctx.Context, k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
	expectedMigFillDelay int64) {
	aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["migrate-fill-delay"] =
		expectedMigFillDelay
	Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

	clusterNamespacedName := utils.GetNamespacedName(aeroCluster)

	err := waitForOperatorToStartPodRestart(ctx, k8sClient, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to given value before the restart")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to 0 after the restart (pod is running)")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, 0,
		&shortRetryInterval)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to given value before the restart of next pod")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval)
	Expect(err).ToNot(HaveOccurred())

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to given value after the operation is completed")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval)
	Expect(err).ToNot(HaveOccurred())
}
