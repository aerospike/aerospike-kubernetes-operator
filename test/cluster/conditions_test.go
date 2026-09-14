package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe("ClusterConditions", func() {
	ctx := goctx.Background()

	clusterNamespacedName := test.GetNamespacedName(
		fmt.Sprintf("conditions-%d", GinkgoParallelProcess()), namespace,
	)

	BeforeEach(func() {
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
		Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
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

	DescribeTable("Scale condition lifecycle",
		func(targetSize int32,
			condType asdbv1.AerospikeClusterConditionType, reason string,
		) {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = targetSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			validateCondition(ctx, clusterNamespacedName, condType, reason, "rack")

			By("Wait for cluster to reach Completed phase")

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(targetSize), retryInterval, getTimeout(targetSize),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())
		},

		Entry("sets ScalingUp while adding pods, then clears it", int32(4),
			asdbv1.AerospikeClusterConditionScalingUp, asdbv1.AerospikeClusterReasonScalingUp),
		Entry("sets ScalingDown while removing pods, then clears it", int32(2),
			asdbv1.AerospikeClusterConditionScalingDown, asdbv1.AerospikeClusterReasonScalingDown),
	)

	// This test-case also checks LastTransitionTime update time
	It("Should set Upgrading=True during image upgrade, then False with Ready=True after", func() {
		By("Trigger image upgrade without waiting")

		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		initialReady := apimeta.FindStatusCondition(
			aeroCluster.Status.Conditions, string(asdbv1.AerospikeClusterConditionReady),
		)
		Expect(initialReady).ToNot(BeNil())
		initialLTT := initialReady.LastTransitionTime

		Expect(UpdateClusterImage(aeroCluster, nextImage)).ToNot(HaveOccurred())
		Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

		By("Upgrading condition must become True during the operation")

		validateCondition(ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionUpgrading,
			asdbv1.AerospikeClusterReasonUpgrading, "rack")

		By("Wait for upgrade to complete")

		err = waitForAerospikeCluster(
			k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size),
			[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
		)
		Expect(err).ToNot(HaveOccurred())

		By("LastTransitionTime must be strictly after the initial value (status changed False→True)")

		finalReady, err := getCondition(ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionReady)
		Expect(err).ToNot(HaveOccurred())
		Expect(finalReady).ToNot(BeNil())
		Expect(finalReady.LastTransitionTime.After(initialLTT.Time)).To(BeTrue(),
			"LastTransitionTime should be later after a True→False→True transition")
	})

	It("Should set RollingRestart=True during config-change restart, then False with Ready=True after", func() {
		By("Trigger a rolling restart via non-dynamic config change (indent-allocations) without waiting")

		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["indent-allocations"] = true
		Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

		By("RollingRestart condition must become True during the operation")

		validateCondition(ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionRollingRestart,
			asdbv1.AerospikeClusterReasonRollingRestart, "rack")

		By("Wait for rolling restart to complete")

		err = waitForAerospikeCluster(
			k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size),
			[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
		)
		Expect(err).ToNot(HaveOccurred())
	})

	// pause preserves the interrupted operation
	It("Should keep the in-flight operation condition as is and report Paused=True when paused", func() {
		By("Start a rolling restart without waiting for it to finish")

		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["indent-allocations"] = true

		Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

		By("Wait for RollingRestart to become True")

		Expect(waitForCondition(
			ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionRollingRestart,
			metav1.ConditionTrue, 3*time.Minute,
		)).ToNot(HaveOccurred(), "RollingRestart condition never became True")

		By("Pause reconciliation mid-operation")

		Expect(setPauseFlag(ctx, clusterNamespacedName, ptr.To(true))).ToNot(HaveOccurred())

		By("Paused must be True while the interrupted operation is preserved")

		Expect(waitForCondition(
			ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionPaused,
			metav1.ConditionTrue, 3*time.Minute,
		)).ToNot(HaveOccurred(), "Paused condition never became True")

		validateCondition(ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionPaused,
			asdbv1.AerospikeClusterReasonPausedByUser, "paused")

		validateCondition(ctx, clusterNamespacedName, asdbv1.AerospikeClusterConditionRollingRestart,
			asdbv1.AerospikeClusterReasonRollingRestart, "rack")

		By("Unpausing must let the operation finish and settle every condition")

		Expect(setPauseFlag(ctx, clusterNamespacedName, nil)).ToNot(HaveOccurred())

		Expect(waitForAerospikeCluster(
			k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size),
			[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
		)).ToNot(HaveOccurred())
	})

	// TODO: This test-case will pass when the persistent error fix is in place.
	// An interrupted operation keeps reporting.
	// The only place the operation conditions are observable in a settled, non-Completed state.
	// isClusterStateValid asserts the terminal condition set for every lifecycle test in the
	// suite, but only for phase=Completed — a cluster stuck in Error never reaches that check.
	It("Should report the failing stage on Ready, keep Upgrading=True, and not claim RollingRestart "+
		"during recovery, then recover", func() {
		By("Trigger an upgrade to a non-existent image")

		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster.Spec.Image = unavailableImage
		Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

		By("The interrupted operation must stay True alongside phase=Error")

		// Every assertion evaluates the same object. us.
		Eventually(func(g Gomega) {
			cluster, gErr := getCluster(k8sClient, ctx, clusterNamespacedName)
			g.Expect(gErr).ToNot(HaveOccurred())
			g.Expect(cluster.Status.Phase).To(Equal(asdbv1.AerospikeClusterError))

			upgrading := apimeta.FindStatusCondition(
				cluster.Status.Conditions, string(asdbv1.AerospikeClusterConditionUpgrading),
			)
			g.Expect(upgrading).ToNot(BeNil())
			g.Expect(upgrading.Status).To(Equal(metav1.ConditionTrue))

			// Ready carries the stage that failed rather than a generic ReconcileFailed. Asserted
			// in the same snapshot as the phase because a requeueing reconcile rewrites this
			// reason twice per pass — Reconciling at the start, RackReconcileFailed at the end —
			// so a separate fetch could land on either.
			ready := apimeta.FindStatusCondition(
				cluster.Status.Conditions, string(asdbv1.AerospikeClusterConditionReady),
			)
			g.Expect(ready).ToNot(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(asdbv1.AerospikeClusterReasonRackReconcileFailed))
			g.Expect(ready.Message).ToNot(BeEmpty())

			// handleFailedPodsInRack force-restarts the stuck pod, which goes through
			// rollingRestartRack. That is recovery machinery serving the upgrade, not an operation
			// the user asked for, so it must not claim RollingRestart.
			restart := apimeta.FindStatusCondition(
				cluster.Status.Conditions, string(asdbv1.AerospikeClusterConditionRollingRestart),
			)
			g.Expect(restart).ToNot(BeNil())
			g.Expect(restart.Status).To(Equal(metav1.ConditionFalse))
		}, getTimeout(2), retryInterval).Should(Succeed())

		By("Reverting the image must clear the operation and restore Ready")

		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster.Spec.Image = latestImage

		Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
	})
})

// waitForCondition polls until the named condition on the cluster reaches
// expectedStatus, or the timeout elapses.
func waitForCondition(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	condType asdbv1.AerospikeClusterConditionType,
	expectedStatus metav1.ConditionStatus,
	timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, timeout, true,
		func(ctx goctx.Context) (bool, error) {
			cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			if err != nil {
				return false, nil
			}

			cond := apimeta.FindStatusCondition(cluster.Status.Conditions, string(condType))
			if cond == nil {
				return false, nil
			}

			return cond.Status == expectedStatus, nil
		},
	)
}

// getCondition is a convenience helper for assertion blocks.
func getCondition(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	condType asdbv1.AerospikeClusterConditionType,
) (*metav1.Condition, error) {
	cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	return apimeta.FindStatusCondition(cluster.Status.Conditions, string(condType)), nil
}

func validateCondition(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	condType asdbv1.AerospikeClusterConditionType,
	reason, msg string,
) {
	By(fmt.Sprintf("%s condition must become True during the operation", condType))

	Expect(
		waitForCondition(ctx, clusterNamespacedName, condType,
			metav1.ConditionTrue, 3*time.Minute),
	).ToNot(HaveOccurred(), fmt.Sprintf("%s condition never became True", condType))

	cond, err := getCondition(ctx, clusterNamespacedName, condType)
	Expect(err).ToNot(HaveOccurred())
	Expect(cond).ToNot(BeNil())
	Expect(cond.Reason).To(Equal(reason))
	Expect(cond.Message).To(ContainSubstring(msg))
}
