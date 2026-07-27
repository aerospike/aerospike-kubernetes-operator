package cluster

import (
	goctx "context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"RestartMigrateFillDelay", func() {
		ctx := goctx.TODO()

		Context(
			"RestartMigrateFillDelay", func() {
				RestartMigrateFillDelayTest(ctx)
			},
		)
	},
)

func RestartMigrateFillDelayTest(ctx goctx.Context) {
	Context(
		"When RestartMigrateFillDelay is configured", func() {
			clusterNamespacedName := test.GetNamespacedName(
				fmt.Sprintf("restart-mfd-cluster-%d", GinkgoParallelProcess()), namespace,
			)
			restartMigrateFillDelay := int64(120)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RestartMigrateFillDelay = &restartMigrateFillDelay
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
				"Should set RestartMigrateFillDelay before pod restart and reset to 0 after", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Query a pod that won't be the first to restart.
					// Rolling restart processes pods in order; the last pod stays running
					// longest and is the safest to query for the in-flight MFD value.
					rackID := aeroCluster.Spec.RackConfig.Racks[0].ID
					lastPodName := aeroCluster.Name + "-" + strconv.Itoa(rackID) + "-" +
						strconv.Itoa(int(aeroCluster.Spec.Size)-1)
					firstPodName := aeroCluster.Name + "-" + strconv.Itoa(rackID) + "-0"

					// Trigger pod restart
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationPodRestart, ID: "mfd-restart-1"},
					}

					err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// Wait for the operator to start restarting pods, then verify MFD is set
					// to RestartMigrateFillDelay. MFD is applied cluster-wide before the first
					// pod is taken down, so the last pod (still running) can serve the check.
					err = waitForOperatorToStartPodRestart(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						restartMigrateFillDelay, &shortRetryInterval, lastPodName)
					Expect(err).ToNot(HaveOccurred())

					// Wait for all restarts to complete
					err = waitForAerospikeCluster(
						k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
						getTimeout(aeroCluster.Spec.Size), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
					)
					Expect(err).ToNot(HaveOccurred())

					// After all pods have rejoined, MFD should be reset to 0 so the
					// cluster can rebalance immediately.
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						0, nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"Should restore aerospike config migrate-fill-delay after pod restart", func() {
					configMFD := int64(60)

					// Set migrate-fill-delay in the aerospike service config
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["migrate-fill-delay"] =
						configMFD

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// Fetch the updated cluster and trigger a pod restart
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					firstPodName := aeroCluster.Name + "-" +
						strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[0].ID) + "-0"

					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationPodRestart, ID: "mfd-restart-2"},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// After all restarts, the reconciler revert should restore the
					// aerospike-config-level migrate-fill-delay value.
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						configMFD, nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"Should not set RestartMigrateFillDelay during warm restart", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					firstPodName := aeroCluster.Name + "-" +
						strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[0].ID) + "-0"

					// Trigger a warm restart (ASD-only restart, not pod restart).
					// shouldSetMigrateFillDelay only fires for podRestart type, so
					// MFD should remain at 0 throughout.
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationWarmRestart, ID: "mfd-warm-1"},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// MFD must still be 0 — RestartMigrateFillDelay must not have fired
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						0, nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)

	Context(
		"When RestartMigrateFillDelay is not configured", func() {
			clusterNamespacedName := test.GetNamespacedName(
				fmt.Sprintf("no-restart-mfd-cluster-%d", GinkgoParallelProcess()), namespace,
			)
			configMFD := int64(60)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					// Set migrate-fill-delay in aerospike config but leave
					// RestartMigrateFillDelay unset to verify no interference.
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["migrate-fill-delay"] =
						configMFD
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
				"Should leave migrate-fill-delay unchanged during pod restart", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					firstPodName := aeroCluster.Name + "-" +
						strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[0].ID) + "-0"

					// Trigger pod restart without RestartMigrateFillDelay set.
					// AKO must not touch migrate-fill-delay at all — it should remain
					// at the configured aerospike config value.
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationPodRestart, ID: "no-mfd-restart-1"},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// migrate-fill-delay must remain at the user-configured value
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						configMFD, nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}
