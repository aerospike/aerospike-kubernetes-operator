package cluster

import (
	goctx "context"
	"fmt"
	"time"

	lib "github.com/aerospike/aerospike-management-lib"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"Revision", func() {
		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {
				Context(
					"When using rack revision for storage updates", func() {
						clusterName := fmt.Sprintf("rack-revision-%d", GinkgoParallelProcess())
						clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

						AfterEach(
							func() {
								aeroCluster := &asdbv1.AerospikeCluster{
									ObjectMeta: metav1.ObjectMeta{
										Name:      clusterNamespacedName.Name,
										Namespace: clusterNamespacedName.Namespace,
									},
								}
								Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
								Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
							},
						)

						It(
							"Should successfully migrate rack to new revision with storage update", func() {
								By("Creating cluster with initial storage configuration")
								aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 6)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								// Validate initial deployment
								err := validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Validate initial StatefulSets are created with v1 revision
								err = validateRackRevisionStatefulSets(k8sClient, ctx, clusterNamespacedName, versionV1, 2)
								Expect(err).ToNot(HaveOccurred())

								By("Updating storage configuration with new rack revision")
								updatedCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Update storage with new revision
								for idx := range updatedCluster.Spec.RackConfig.Racks {
									rack := &updatedCluster.Spec.RackConfig.Racks[idx]
									rack.Revision = versionV2

									// Add new storage volume to simulate storage update
									rack.Storage.Volumes = append(rack.Storage.Volumes, asdbv1.VolumeSpec{
										Name: "new-volume",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/data2",
										},
									})
								}

								Expect(updateClusterWithNoWait(k8sClient, ctx, updatedCluster)).ToNot(HaveOccurred())

								By("Validating gradual migration from v1 to v2")
								// Both v1 and v2 StatefulSets should exist during migration
								Eventually(func() bool {
									return checkBothRevisionsExist(k8sClient, ctx, clusterNamespacedName, versionV1, versionV2)
								}, 5*time.Minute, 10*time.Second).Should(BeTrue())

								By("Waiting for the migration to complete")
								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
									getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								// Validate final state
								err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Ensure v1 revision resources are cleaned up
								err = validateRackRevisionCleanup(k8sClient, ctx, aeroCluster, []int{1, 2}, versionV1)
								Expect(err).ToNot(HaveOccurred())
							},
						)

						It(
							"Should handle rack revision batching correctly", func() {
								By("Creating cluster with custom batch size")
								aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 6)
								aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &intstr.IntOrString{IntVal: 2}
								aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								err := validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								_ = changeRackRevision(k8sClient, ctx, clusterNamespacedName)

								By("Validating batch-wise migration from v1 to v2")
								Expect(isBatchRestart(ctx, aeroCluster)).To(BeTrue())

								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
									getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								// Validate final state
								err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Ensure v1 revision resources are cleaned up
								err = validateRackRevisionCleanup(k8sClient, ctx, aeroCluster, []int{1}, versionV1)
								Expect(err).ToNot(HaveOccurred())
							},
						)

						It(
							"Should handle external StatefulSet deletion gracefully", func() {
								By("Creating cluster and triggering migration")
								aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 6)
								aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
									getNonSCNamespaceConfig("test", "/test/dev/xvdf"),
								}
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								_ = changeRackRevision(k8sClient, ctx, clusterNamespacedName)

								Eventually(func() bool {
									return checkBothRevisionsExist(k8sClient, ctx, clusterNamespacedName, versionV1, versionV2)
								}, 5*time.Minute, 10*time.Second).Should(BeTrue())

								// Manually delete old StatefulSet
								By("Manually deleting old StatefulSet during migration")
								err := deleteOldStatefulSet(k8sClient, ctx, clusterNamespacedName, versionV1, 1)
								Expect(err).ToNot(HaveOccurred())

								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
									getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								// Validate final state
								err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())
							},
						)

						It(
							"Should handle cluster size reduction during migration", func() {
								By("Creating cluster and starting migration")
								aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 6)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								updatedCluster := changeRackRevision(k8sClient, ctx, clusterNamespacedName)

								Eventually(func() bool {
									return checkBothRevisionsExist(k8sClient, ctx, clusterNamespacedName, versionV1, versionV2)
								}, 5*time.Minute, 5*time.Second).Should(BeTrue())

								updatedCluster.Spec.Size = 2

								err := updateCluster(k8sClient, ctx, updatedCluster)
								Expect(err).ToNot(HaveOccurred())

								// Validate final state
								err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Ensure v1 revision resources are cleaned up
								err = validateRackRevisionCleanup(k8sClient, ctx, aeroCluster, []int{1}, versionV1)
								Expect(err).ToNot(HaveOccurred())
							},
						)

						It(
							"Should handle failed pods during rack revision migration", func() {
								By("Creating cluster and triggering migration")
								aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 6)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								podName := clusterName + "-1-v1-0"
								err := markPodAsFailed(ctx, k8sClient, podName, clusterNamespacedName.Namespace)
								Expect(err).ToNot(HaveOccurred())

								_ = changeRackRevision(k8sClient, ctx, clusterNamespacedName)

								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
									getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								// Validate final state
								err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								// Ensure v1 revision resources are cleaned up
								err = validateRackRevisionCleanup(k8sClient, ctx, aeroCluster, []int{1}, versionV1)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)
			},
		)
		Context(
			"When doing invalid operations", func() {
				clusterName := fmt.Sprintf("rack-revision-validation-%d", GinkgoParallelProcess())
				clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

				BeforeEach(
					func() {
						aeroCluster := createDummyClusterWithRackRevision(clusterNamespacedName, versionV1, 2)
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterNamespacedName.Name,
								Namespace: clusterNamespacedName.Namespace,
							},
						}
						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
					},
				)

				It(
					"Should reject more than 2 concurrent rack revisions", func() {
						By("Attempting to create more than 2 concurrent revisions for a rack")
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.RackConfig.Racks[0].Revision = versionV2

						Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						aeroCluster.Spec.RackConfig.Racks[0].Revision = "v3"

						Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
					},
				)

				It(
					"Should reject storage update validation bypass via revision bump and rollback", func() {
						By("Starting a revision change with storage update")
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						// Change revision and storage
						rack1 := aeroCluster.Spec.RackConfig.Racks[0]
						rack1.Revision = versionV2
						rack1.InputStorage = lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)

						rack1.InputStorage.Volumes = append(rack1.Storage.Volumes, asdbv1.VolumeSpec{
							Name: "bypass-volume",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("2Gi"),
									StorageClass: storageClass,
									VolumeMode:   corev1.PersistentVolumeFilesystem,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/opt/aerospike/bypass",
							},
						})

						aeroCluster.Spec.RackConfig.Racks[0] = rack1
						Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						By("Attempting to rollback revision while keeping new storage")

						aeroCluster.Spec.RackConfig.Racks[0].Revision = versionV1
						Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
					},
				)

				It(
					"Should reject in-place storage updates with same revision", func() {
						By("Attempting in-place storage update without revision change")
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						// Try to add storage without changing revision
						rack1 := aeroCluster.Spec.RackConfig.Racks[0]
						rack1.InputStorage = lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)

						rack1.InputStorage.Volumes = append(rack1.Storage.Volumes, asdbv1.VolumeSpec{
							Name: "inplace-volume",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("2Gi"),
									StorageClass: storageClass,
									VolumeMode:   corev1.PersistentVolumeFilesystem,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/opt/aerospike/inplace",
							},
						})

						aeroCluster.Spec.RackConfig.Racks[0] = rack1
						Expect(updateCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
					},
				)
			},
		)

	},
)

func createDummyClusterWithRackRevision(
	clusterNamespacedName types.NamespacedName, revision string, size int32,
) *asdbv1.AerospikeCluster {
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, size)

	aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistryNamespace = ptr.To("abhishekdwivedi3060")
	aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageNameAndTag = "aerospike-kubernetes-init:2.3.0-2"

	racks := []asdbv1.Rack{
		{ID: 1, Revision: revision},
		{ID: 2, Revision: revision},
	}

	rackConf := asdbv1.RackConfig{
		Racks: racks,
	}

	aeroCluster.Spec.RackConfig = rackConf

	return aeroCluster
}

func validateRackRevisionStatefulSets(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	revision string, expectedCount int,
) error {
	stsList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(clusterNamespacedName.Name))
	listOps := &client.ListOptions{
		Namespace:     clusterNamespacedName.Namespace,
		LabelSelector: labelSelector,
	}

	if err := k8sClient.List(ctx, stsList, listOps); err != nil {
		return err
	}

	var revisionCount int

	for idx := range stsList.Items {
		if rackRevision, exists := stsList.Items[idx].Labels[asdbv1.AerospikeRackRevisionLabel]; exists &&
			rackRevision == revision {
			revisionCount++
		}
	}

	if revisionCount != expectedCount {
		return fmt.Errorf("expected %d StatefulSets with revision %s, found %d", expectedCount, revision, revisionCount)
	}

	return nil
}

func checkBothRevisionsExist(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, oldRevision,
	newRevision string,
) bool {
	stsList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(clusterNamespacedName.Name))
	listOps := &client.ListOptions{
		Namespace:     clusterNamespacedName.Namespace,
		LabelSelector: labelSelector,
	}

	if err := k8sClient.List(ctx, stsList, listOps); err != nil {
		return false
	}

	var oldExists, newExists bool

	for idx := range stsList.Items {
		if rackRevision, exists := stsList.Items[idx].Labels[asdbv1.AerospikeRackRevisionLabel]; exists {
			if rackRevision == oldRevision {
				oldExists = true
			}

			if rackRevision == newRevision && stsList.Items[idx].Status.ReadyReplicas > 1 {
				newExists = true
			}
		}
	}

	return oldExists && newExists
}

func validateRackRevisionCleanup(
	k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster,
	rackIDs []int, cleanedRevision string,
) error {
	for _, rackID := range rackIDs {
		oldResourceNsNm := GetNamespacedNameForSTS(aeroCluster, utils.GetRackIdentifier(rackID, cleanedRevision))

		oldSts := &appsv1.StatefulSet{}
		nsNm := types.NamespacedName{Namespace: oldResourceNsNm.Namespace, Name: oldResourceNsNm.Name}

		if err := k8sClient.Get(ctx, nsNm, oldSts); err == nil {
			return fmt.Errorf("StatefulSet with revision %s still exists after cleanup: %s", cleanedRevision, nsNm.Name)
		}

		cm := &corev1.ConfigMap{}

		if err := k8sClient.Get(ctx, nsNm, cm); err == nil {
			return fmt.Errorf("ConfigMap with revision %s still exists after cleanup: %s", cleanedRevision, nsNm.Name)
		}
	}

	return nil
}

func deleteOldStatefulSet(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	revision string, rackID int,
) error {
	stsName := fmt.Sprintf("%s-%d-%s", clusterNamespacedName.Name, rackID, revision)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: clusterNamespacedName.Namespace,
		},
	}

	return k8sClient.Delete(ctx, sts)
}

func changeRackRevision(k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName) *asdbv1.AerospikeCluster {
	// Start migration
	updatedCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	updatedCluster.Spec.RackConfig.Racks[0].Revision = versionV2

	err = updateClusterWithNoWait(k8sClient, ctx, updatedCluster)
	Expect(err).ToNot(HaveOccurred())

	return updatedCluster
}
