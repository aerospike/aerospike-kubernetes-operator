package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"OnDemandOperations", func() {

		ctx := goctx.Background()
		var clusterNamespacedName = getNamespacedName(
			"operations", namespace,
		)

		aeroCluster := &asdbv1.AerospikeCluster{}

		BeforeEach(
			func() {
				// Create a 2 node cluster
				aeroCluster = createDummyRackAwareAerospikeCluster(
					clusterNamespacedName, 2,
				)

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		AfterEach(
			func() {
				_ = deleteCluster(k8sClient, ctx, aeroCluster)
			},
		)

		Context(
			"When doing valid operations", func() {

				It(
					"Should execute quickRestart operations on all pods", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationWarmRestart,
								ID:   "1",
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationWarmRestart,
							"operations-1-1": asdbv1.OperationWarmRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should execute podRestart operation on all pods", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationPodRestart,
								ID:   "1",
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationPodRestart,
							"operations-1-1": asdbv1.OperationPodRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should be able to replace/remove the running operations", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationWarmRestart,
								ID:   "1",
							},
						}

						aeroCluster.Spec.Operations = operations

						err = k8sClient.Update(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						Eventually(func() error {
							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.Operations[0].Kind = asdbv1.OperationPodRestart
							aeroCluster.Spec.Operations[0].ID = "2"

							return updateCluster(k8sClient, ctx, aeroCluster)
						}, 1*time.Minute).ShouldNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationPodRestart,
							"operations-1-1": asdbv1.OperationPodRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())

						// Remove operations
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Operations = nil

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should execute operations on selected pods with dynamic config change", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationPodRestart,
								ID:      "1",
								PodList: []string{"operations-1-0"},
							},
						}

						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 18000
						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationPodRestart,
							"operations-1-1": "noRestart",
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should execute on-demand podRestart operations on all pods along with scale down", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Size = 4

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationPodRestart,
								ID:   "1",
							},
						}

						aeroCluster.Spec.Operations = operations
						aeroCluster.Spec.Size = 2

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationPodRestart,
							"operations-1-1": asdbv1.OperationPodRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should execute podRestart if podSpec is changed with on-demand warm restart", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPodIDs, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")
						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationWarmRestart,
								ID:   "1",
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operationTypeMap := map[string]asdbv1.OperationKind{
							"operations-1-0": asdbv1.OperationPodRestart,
							"operations-1-1": asdbv1.OperationPodRestart,
						}

						err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
						Expect(err).ToNot(HaveOccurred())
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {
				It(
					"Should fail if there are more than 1 operations", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind: asdbv1.OperationWarmRestart,
								ID:   "1",
							},
							{
								Kind: asdbv1.OperationPodRestart,
								ID:   "2",
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"should fail if invalid pod name is mentioned in the pod list", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationWarmRestart,
								ID:      "1",
								PodList: []string{"operations-1-0", "invalid-pod"},
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"should fail if operationType is modified", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationWarmRestart,
								ID:      "1",
								PodList: []string{"operations-1-0"},
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Modify operationType
						operations[0].Kind = asdbv1.OperationPodRestart
						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"should fail if podList is modified", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationWarmRestart,
								ID:      "1",
								PodList: []string{"operations-1-0"},
							},
						}

						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Modify podList
						operations[0].PodList = []string{"operations-1-1"}
						aeroCluster.Spec.Operations = operations

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"should fail any operation along with cluster scale-up", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationWarmRestart,
								ID:      "1",
								PodList: []string{"operations-1-0"},
							},
						}

						aeroCluster.Spec.Operations = operations
						aeroCluster.Spec.Size++

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"should fail any operation along with cluster upgrade", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						operations := []asdbv1.OperationSpec{
							{
								Kind:    asdbv1.OperationWarmRestart,
								ID:      "1",
								PodList: []string{"operations-1-0"},
							},
						}

						aeroCluster.Spec.Operations = operations
						aeroCluster.Spec.Image = nextImage

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)
			},
		)
	},
)

func validateOperationTypes(ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster, pid map[string]podID,
	operationTypeMap map[string]asdbv1.OperationKind) error {
	newPodPidMap, err := getPodIDs(ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	for podName, opType := range operationTypeMap {
		switch opType {
		case asdbv1.OperationWarmRestart:
			if newPodPidMap[podName].podUID != pid[podName].podUID || newPodPidMap[podName].asdPID == pid[podName].asdPID {
				return fmt.Errorf("failed to quick restart pod %s", podName)
			}
		case asdbv1.OperationPodRestart:
			if newPodPidMap[podName].podUID == pid[podName].podUID {
				return fmt.Errorf("failed to restart pod %s", podName)
			}
		case "noRestart":
			if newPodPidMap[podName].podUID != pid[podName].podUID || newPodPidMap[podName].asdPID != pid[podName].asdPID {
				return fmt.Errorf("unexpected restart pod %s", podName)
			}
		}
	}

	return nil
}
