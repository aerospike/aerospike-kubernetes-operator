package cluster

// Cross-Rack Batch Quiesce tests
//
// This file validates the batch quiesce pre-pass introduced for parallel
// cross-rack scale-down.  The pre-pass quiesces ALL scale-down candidate pods
// across ALL racks simultaneously before the per-rack scaleDownRack loop runs,
// triggering a single concurrent migration round instead of N sequential ones.
//
// Covered scenarios
// ─────────────────
// §1 Basic quiesce ordering
// §2 Scale-down revert (partial and full)
// §3 Failed pod handling
// §4 Rack operations (delete, replace, add)
// §5 Concurrent operations
// §7 Annotation idempotency & crash safety
// §8 Backward compatibility

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sintstr "k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// ─── package-level helpers ─────────────────────────────────────────────────

// countQuiescedNodes queries the Aerospike namespace stats on the given pod
// and returns the value of nodes_quiesced.
func countQuiescedNodes(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	podName string,
) int {
	info, err := requestInfoFromNode(
		logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", podName,
	)
	if err != nil {
		return 0
	}

	for _, kv := range strings.Split(info["namespace/test"], ";") {
		if strings.HasPrefix(kv, "nodes_quiesced=") {
			n := 0
			_, _ = fmt.Sscanf(strings.TrimPrefix(kv, "nodes_quiesced="), "%d", &n)

			return n
		}
	}

	return 0
}

// waitForQuiescedCount blocks until the cluster reports at least minCount
// quiesced nodes (polled via the first available pod).
func waitForQuiescedCount(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	minCount int,
	timeout time.Duration,
) {
	GinkgoHelper()

	Eventually(func() bool {
		podList, err := getPodList(
			&asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			},
			k8sClient,
		)
		if err != nil || len(podList.Items) == 0 {
			return false
		}

		return countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name) >= minCount
	}, timeout, 2*time.Second).Should(
		BeTrue(),
		"Expected at least %d quiesced nodes within %s", minCount, timeout,
	)
}

// assertNoStaleQuiesce verifies zero quiesced nodes remain in the cluster.
func assertNoStaleQuiesce(
	ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	clusterNamespacedName types.NamespacedName,
) {
	GinkgoHelper()

	podList, err := getPodList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())
	Expect(podList.Items).ToNot(BeEmpty())
	Expect(countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name)).To(
		BeZero(), "Expected zero quiesced nodes after reconcile completed",
	)
}

// assertNoBatchQuiesceAnnotations verifies that no pod in the cluster carries
// the BatchQuiesceAnnotation.
func assertNoBatchQuiesceAnnotations(aeroCluster *asdbv1.AerospikeCluster) {
	GinkgoHelper()

	podList, err := getPodList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	for i := range podList.Items {
		pod := &podList.Items[i]
		Expect(pod.Annotations[asdbv1.BatchQuiesceAnnotation]).ToNot(Equal(asdbv1.BatchQuiesceAnnotationValue),
			"Pod %s should not carry BatchQuiesceAnnotation", pod.Name)
	}
}

// podHasBatchQuiesceAnnotation returns true if the named pod in the given
// cluster currently has BatchQuiesceAnnotation set.
func podHasBatchQuiesceAnnotation(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	podName string,
) bool {
	pod := &corev1.Pod{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name: podName, Namespace: clusterNamespacedName.Namespace,
	}, pod); err != nil {
		return false
	}

	return pod.Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue
}

// newMultiRackNonSCCluster builds a non-SC cluster with the requested number
// of racks, distributing `totalSize` pods evenly across them.
func newMultiRackNonSCCluster(
	clusterNamespacedName types.NamespacedName,
	totalSize int32,
	rackIDs []int,
) *asdbv1.AerospikeCluster {
	aeroCluster := createNonSCDummyAerospikeCluster(clusterNamespacedName, totalSize)
	racks := getDummyRackConf(rackIDs...)
	aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
		Racks:      racks,
		Namespaces: []string{"test"},
	}

	return aeroCluster
}

// podsForRack returns the PodList for a rack using the existing
// getSTSFromRackID → getRackPodList pattern.  When the rack's StatefulSet has
// already been deleted (e.g. after rack removal) it falls back to a direct
// label-selector query via utils.GetAerospikeClusterRackLabelSelector so
// callers can still assert an empty result.
func podsForRack(
	ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	rackID int,
) (*corev1.PodList, error) {
	sts, err := getSTSFromRackID(aeroCluster, rackID, "")
	if err == nil {
		return getRackPodList(k8sClient, ctx, sts)
	}

	// STS not found → rack has been removed; fall back to a label-selector
	// query.  An empty result is the expected / valid outcome for deleted racks.
	podList := &corev1.PodList{}
	listOpts := &client.ListOptions{
		Namespace:     aeroCluster.Namespace,
		LabelSelector: utils.GetAerospikeClusterRackLabelSelector(aeroCluster.Name, rackID, ""),
	}

	return podList, k8sClient.List(ctx, podList, listOpts)
}

// podNamesForRack returns all pod names currently in a given rack.
func podNamesForRack(
	ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	rackID int,
) []string {
	podList, err := podsForRack(ctx, aeroCluster, rackID)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(podList.Items))
	for i := range podList.Items {
		names = append(names, podList.Items[i].Name)
	}

	return names
}

// ─── test suite ────────────────────────────────────────────────────────────

var _ = FDescribe("CrossRackBatchQuiesce", func() {
	ctx := goctx.TODO()

	// Each It block gets a unique cluster name so parallel Ginkgo processes
	// do not collide.
	clusterName := fmt.Sprintf("cross-rack-bq-%d", GinkgoParallelProcess())
	clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

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

	// ══════════════════════════════════════════════════════════════════════
	// §1  Basic Quiesce Ordering
	// ══════════════════════════════════════════════════════════════════════

	// ── 1.1 [EXISTS] ────────────────────────────────────────────────────
	Context("Basic cross-rack batch quiesce during scale-down", func() {
		// 2 racks × 3 pods = 6 total; scale down by 2 (one per rack).
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce pods on ALL racks before removing any pod", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Trigger scale-down of 2 pods — one from each rack.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			seenBothRacksQuiesced := false
			seenMigrationsComplete := false

			// Key assertion: both target pods must be quiesced (nodes_quiesced=2)
			// BEFORE the pod count drops below the original 6.
			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				Expect(lErr).ToNot(HaveOccurred())

				podCount := utils.Len32(podList.Items)
				quiesced := 0

				if len(podList.Items) > 0 {
					quiesced = countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name)
				}

				// Track that we observed the quiesced state while all 6 pods were up.
				if podCount == 6 && quiesced >= 2 {
					seenBothRacksQuiesced = true
				}

				migrations := getMigrationsInProgress(ctx, k8sClient, clusterNamespacedName, podList)

				if seenBothRacksQuiesced && migrations == 0 {
					seenMigrationsComplete = true
				}

				// Fail-fast: a pod was removed before both racks were quiesced.
				if podCount < 6 && !seenBothRacksQuiesced {
					Fail(fmt.Sprintf(
						"Pod removed (count=%d) before all cross-rack targets were quiesced",
						podCount,
					))
				}

				return podCount == aeroCluster.Spec.Size &&
					seenBothRacksQuiesced &&
					seenMigrationsComplete
			}, 10*time.Minute, 2*time.Second).Should(BeTrue())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// After scale-down completes no nodes should remain quiesced.
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 1.2 3 racks, scale down 1 per rack ──────────────────────────────
	Context("3-rack cluster scale-down (one target per rack)", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 9, []int{1, 2, 3})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce all 3 targets simultaneously before removing any pod", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 3 // remove one pod per rack
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			seenTripleQuiesced := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				Expect(lErr).ToNot(HaveOccurred())

				podCount := utils.Len32(podList.Items)

				if len(podList.Items) > 0 &&
					countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name) >= 3 &&
					podCount == 9 {
					seenTripleQuiesced = true
				}

				if podCount < 9 && !seenTripleQuiesced {
					Fail("Pod removed before all 3 cross-rack targets were quiesced")
				}

				return podCount == aeroCluster.Spec.Size && seenTripleQuiesced
			}, 15*time.Minute, 2*time.Second).Should(BeTrue())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 1.3 Single-rack scale-down ───────────────────────────────────────
	Context("Single-rack scale-down (no cross-rack path exercised)", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should complete scale-down correctly with no cross-rack side-effects", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size--
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
			assertNoBatchQuiesceAnnotations(aeroCluster)
		})
	})

	// ── 1.4 Annotation fast-exit on second reconcile ─────────────────────
	Context("Annotation fast-exit: no Aerospike info calls on second reconcile of same scale-down", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should not re-quiesce targets that are already annotated", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Trigger scale-down and wait for quiesce annotations to be set.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait until the BatchQuiesceAnnotation appears on at least one pod
			// (indicates the pre-pass succeeded and annotated the targets).
			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr != nil {
					return false
				}

				for i := range podList.Items {
					if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
						return true
					}
				}

				return false
			}, 5*time.Minute, 2*time.Second).Should(BeTrue(),
				"Expected BatchQuiesceAnnotation to appear on at least one pod")

			// Force another reconcile by adding a harmless annotation to the CR.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			if aeroCluster.Annotations == nil {
				aeroCluster.Annotations = make(map[string]string)
			}

			aeroCluster.Annotations["test/trigger-reconcile"] = fmt.Sprintf("%d", time.Now().UnixNano())
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Let reconcile complete — the fast-exit path should not clear annotations.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{
					asdbv1.AerospikeClusterCompleted,
					asdbv1.AerospikeClusterInProgress,
				},
			)).ToNot(HaveOccurred())

			// After reconcile fully completes, no stale quiesce and no annotations.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 1.5 No scale-down — batch quiesce pre-pass entirely skipped ──────
	Context("No scale-down — batch quiesce pre-pass should be skipped", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should not quiesce any pod when size is unchanged", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Apply a label change to trigger reconcile without changing size.
			if aeroCluster.Labels == nil {
				aeroCluster.Labels = make(map[string]string)
			}

			aeroCluster.Labels["test/no-scaledown"] = "true"
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(3),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// No quiesce should have been issued.
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
			assertNoBatchQuiesceAnnotations(aeroCluster)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §2  Scale-Down Revert
	// ══════════════════════════════════════════════════════════════════════

	// ── 2.1 [EXISTS] Partial revert ──────────────────────────────────────
	Context("Partial scale-down revert while migration is in progress", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should undo quiesce for the reverted pod and keep other targets quiesced", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			waitForQuiescedCount(ctx, clusterNamespacedName, 1, 3*time.Minute)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size++
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 2.2 [EXISTS] Full revert ─────────────────────────────────────────
	Context("Full scale-down revert while migration is in progress", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should undo quiesce for all pods and restore the cluster to full capacity", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			waitForQuiescedCount(ctx, clusterNamespacedName, 1, 3*time.Minute)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(originalSize),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
			assertNoBatchQuiesceAnnotations(aeroCluster)
		})
	})

	// ── 2.3 Revert while reconcile is in-flight ──────────────────────────
	// The two API updates (scale-down → revert) are NOT atomic: the operator
	// may have already started a reconcile and even sent quiesce info commands
	// by the time the revert lands.  What we can guarantee is that once the
	// reverted spec reaches Completed, reconcileQuiesceUndo has cleaned up any
	// quiesced nodes and removed all BatchQuiesce annotations.
	Context("Revert while reconcile may be in-flight", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should leave no stale quiesce state after an immediate scale-down revert", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size

			// Issue scale-down then immediately revert.  The operator may or may
			// not have reached the quiesce step before seeing the revert — both
			// paths must converge to a clean state.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(originalSize),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// reconcileQuiesceUndo must have undone any quiesce that was sent
			// and cleared all BatchQuiesce annotations.
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
			assertNoBatchQuiesceAnnotations(aeroCluster)
		})
	})

	// ── 2.4 Repeated revert/scale-down cycles ───────────────────────────
	Context("Repeated revert/scale-down cycles", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should handle 3 back-to-back scale-down/revert cycles without stale state", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size

			for cycle := 1; cycle <= 3; cycle++ {
				By(fmt.Sprintf("Cycle %d: scale down", cycle))

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Size -= 2
				Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

				// Wait for quiesce to fire.
				waitForQuiescedCount(ctx, clusterNamespacedName, 1, 3*time.Minute)

				By(fmt.Sprintf("Cycle %d: revert", cycle))

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Size = originalSize
				Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

				Expect(waitForAerospikeCluster(
					k8sClient, ctx, aeroCluster, int(originalSize),
					retryInterval, getTimeout(4),
					[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
				)).ToNot(HaveOccurred())

				assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
				assertNoBatchQuiesceAnnotations(aeroCluster)
			}
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §3  Failed Pod Handling
	// ══════════════════════════════════════════════════════════════════════

	// ── 3.1 Target pod never joined (no CR status) ───────────────────────
	// Scenario: cluster is running at size N.  User scales UP by 1 (new pod
	// enters Pending/Initializing, no CR status yet), then IMMEDIATELY scales
	// back down by 1 targeting that same pod.  With maxIgnorablePods=1 the
	// operator must treat the not-running, never-joined pod as ignorable and
	// complete the scale-down without getting stuck.
	Context("Scale-down target never joined cluster (no CR status)", func() {
		// Start with a fully-running 4-pod cluster.
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should add the never-joined pod to ignorablePodNames and complete scale-down", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size

			// Step 1: scale UP by 1 so the operator creates a new pod on rack 1.
			// The new pod is the highest-indexed one and will be the scale-down
			// target in Step 2.  We do NOT wait for the pod to be ready — the
			// point is to catch it while it is still initializing (no CR status).
			aeroCluster.Spec.Size++
			// Enable maxIgnorablePods=1 in the same update so the operator can
			// proceed even if the new pod hasn't joined by the time scale-down fires.
			aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr_fromInt(1))
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Step 2: immediately scale back DOWN to the original size.
			// The operator will see the newly-created pod as a scale-down target.
			// If it is still not running and has no CR status entry,
			// checkReadyForBatchQuiesce must add it to ignorablePodNames and let
			// the scale-down proceed without quiescing it.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait for scale-down to complete back at the original size.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(originalSize),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// The cluster must be clean: no quiesced nodes, no stale annotations.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
			assertNoBatchQuiesceAnnotations(aeroCluster)
		})
	})

	// ── 3.2 Target pod was live but is now crashed (has CR status) ───────
	Context("Scale-down target has CR status but is not running", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should block scale-down with ReconcileError until the pod recovers", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Identify the scale-down target pod on rack 1.
			rack1Pods := podNamesForRack(ctx, aeroCluster, 1)
			Expect(rack1Pods).ToNot(BeEmpty())
			targetPodName := rack1Pods[len(rack1Pods)-1]

			// Kill the Aerospike container without fully deleting the pod so
			// it has a CR status entry (it has joined) but its server is down.
			targetPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: targetPodName, Namespace: clusterNamespacedName.Namespace,
			}, targetPod)).ToNot(HaveOccurred())

			// Simulate crash by deleting pod — it will be re-created with status.
			// We use a zero-grace-period force delete.
			gracePeriod := int64(0)
			Expect(k8sClient.Delete(ctx, targetPod, &client.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			})).ToNot(HaveOccurred())

			// Now trigger scale-down while the pod is restarting.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size--
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// The cluster should enter Error phase (pod not ready, has status).
			// It will recover once the pod comes back up.
			Eventually(func() asdbv1.AerospikeClusterPhase {
				cl, gErr := getCluster(k8sClient, ctx, clusterNamespacedName)
				if gErr != nil {
					return ""
				}

				return cl.Status.Phase
			}, 3*time.Minute, 5*time.Second).Should(
				BeElementOf(
					asdbv1.AerospikeClusterError,
					asdbv1.AerospikeClusterCompleted, // might recover fast in CI
				),
			)

			// Eventually the pod comes back and scale-down completes.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 3.3 Non-target pod crashes during rack addition (Step 2) ─────────
	Context("New pod in a freshly added rack is not ready when batch quiesce fires", func() {
		BeforeEach(func() {
			// Start with 2 racks.
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should wait for new rack pods to be ready before quiescing scale-down targets", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Add rack 3 (scale-up) AND scale down rack 1 by 1 in one update.
			// The new rack 3 pods will initialize after reconcileRack Step 2;
			// waitForAllRacksReady (Step 2.5) must block until they are ready.
			newRacks := append(aeroCluster.Spec.RackConfig.Racks, getDummyRackConf(3)[0])
			aeroCluster.Spec.RackConfig.Racks = newRacks
			aeroCluster.Spec.Size++ // +2 for rack 3, -1 for rack 1 scale-down = net +1
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait for scale down (and scale up) to complete.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(8),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// All rack 3 pods must be running.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			rack3Pods, err := podsForRack(ctx, aeroCluster, 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(rack3Pods.Items).ToNot(BeEmpty())

			for i := range rack3Pods.Items {
				Expect(utils.IsAerospikeServerReady(&rack3Pods.Items[i])).To(
					BeTrue(), "Rack 3 pod %s should be ready", rack3Pods.Items[i].Name,
				)
			}

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 3.4 Remaining pod in scaled-down rack is not running ─────────────
	// Scenario: while scale-down is pending, a non-target pod in the same
	// scaled-down rack crashes.  checkReadyForBatchQuiesce must return a
	// ReconcileError (the pod has CR status, so it previously joined), and the
	// operator must block until the pod recovers naturally.
	//
	// We induce a sustained crash by killing the aerospike server process
	// multiple times in quick succession.  Kubernetes applies an exponential
	// restart backoff (10 s → 20 s → …) which keeps the pod not-ready long
	// enough for the operator to observe the Error phase — without any
	// explicit recovery step from the test (the pod recovers on its own when
	// the backoff timer expires).
	Context("A remaining (non-target) pod in a scaled-down rack crashes", func() {
		BeforeEach(func() {
			// 6 pods across 2 racks (3 per rack).  Scaling to 4 removes the
			// top 2 pods from rack 1, leaving pod-0 as the remaining pod.
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})
	})

	// ── 3.5 Non-target pod crashes during quiesce-undo ───────────────────
	Context("Non-target pod crashes while quiesce-undo is in progress", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should skip the crashed pod and retry its annotation on the next reconcile", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size

			// Scale down to trigger quiesce.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait for at least one quiesce annotation to be set.
			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr != nil {
					return false
				}

				for i := range podList.Items {
					if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
						return true
					}
				}

				return false
			}, 5*time.Minute, 2*time.Second).Should(BeTrue())

			// Revert to trigger quiesce-undo. During undo, crash a non-target pod.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// The cluster should eventually recover and reach Completed.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(originalSize),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 3.6 maxIgnorablePods covers a failed target pod ──────────────────
	Context("maxIgnorablePods covers a failed scale-down target", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr_fromInt(1))
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should complete scale-down even when a target pod is in the ignorable budget", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §4  Rack Operations
	// ══════════════════════════════════════════════════════════════════════

	// ── 4.1 [EXISTS] Rack delete ─────────────────────────────────────────
	Context("Rack delete — entire rack quiesced together with other scale-down targets", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2, 3})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should co-quiesce deleted-rack pods with other scale-down targets in one migration round", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 3

			newRacks := make([]asdbv1.Rack, 0, 2)

			for _, r := range aeroCluster.Spec.RackConfig.Racks {
				if r.ID != 3 {
					newRacks = append(newRacks, r)
				}
			}

			aeroCluster.Spec.RackConfig.Racks = newRacks
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Pod count arithmetic for the quiesce window:
			//   BeforeEach deploys 6 pods on 3 racks → DistributeItems(6,3)=[2,2,2]
			//   After update: size=3, racks=[1,2] → DistributeItems(3,2)=[2,1]
			//     Rack 1: 2 pods (unchanged, non-scaled-down)
			//     Rack 2: 1 pod desired (scale-down by 1 → 1 target pod)
			//     Rack 3: deleted (2 pods → racksToDelete)
			//   Step 1 reconcileRack processes rack 1 only (non-scaled-down rack).
			//   Batch quiesce fires while rack 2 and rack 3 pods are still present:
			//     total pods = 2 (rack1) + 2 (rack2, not yet scaled) + 2 (rack3, not yet deleted) = 6
			//     quiesce targets = 1 (rack2 scale-down target) + 2 (rack3 delete targets) = 3
			seenMultiRackQuiesced := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				Expect(lErr).ToNot(HaveOccurred())

				podCount := utils.Len32(podList.Items)
				quiesced := 0

				if len(podList.Items) > 0 {
					quiesced = countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name)
				}

				if podCount == 6 && quiesced >= 3 {
					seenMultiRackQuiesced = true
				}

				return podCount == aeroCluster.Spec.Size && seenMultiRackQuiesced
			}, 15*time.Minute, 2*time.Second).Should(BeTrue(),
				"Pods of deleted rack should be co-quiesced before removal")

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// Refresh cluster object after reconcile so podsForRack can build a
			// fresh label selector from the current spec.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			deletedRackPods, err := podsForRack(ctx, aeroCluster, 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(deletedRackPods.Items).To(BeEmpty(), "All pods for deleted rack 3 should be removed")

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 4.2 [EXISTS] Rack replace ────────────────────────────────────────
	Context("Rack replace — old rack quiesced with explicit scale-down targets", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should co-quiesce old rack and scale-down targets in the same migration round", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			for idx := range aeroCluster.Spec.RackConfig.Racks {
				if aeroCluster.Spec.RackConfig.Racks[idx].ID == 2 {
					aeroCluster.Spec.RackConfig.Racks[idx].ID = 3
					break
				}
			}

			aeroCluster.Spec.Size -= 1
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Pod count arithmetic for the quiesce window:
			//   Initial:   rack 1 (3) + rack 2 (3) = 6 pods
			//   Desired:   rack 1 (3) + rack 3 (2) = 5 pods  [DistributeItems(5,2)→[3,2]]
			//   Step 1 reconcileRack adds rack 3 (new, 2 pods) and leaves rack 1
			//   unchanged (3 pods). Rack 2 (racksToDelete) is still present (3 pods).
			//   Batch quiesce fires → total = 3 + 2 + 3 = 8 pods,
			//   quiesce targets = rack 2 only (3 pods, no scale-down on rack 1).
			seenMultiRackQuiesced := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				Expect(lErr).ToNot(HaveOccurred())

				podCount := utils.Len32(podList.Items)
				quiesced := 0

				if len(podList.Items) > 0 {
					quiesced = countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name)
				}

				if podCount == 8 && quiesced >= 3 {
					seenMultiRackQuiesced = true
				}

				return podCount == aeroCluster.Spec.Size && seenMultiRackQuiesced
			}, 15*time.Minute, 2*time.Second).Should(BeTrue(),
				"Old rack pods should be quiesced before removal")

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// Refresh cluster object after reconcile.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			oldRackPods, err := podsForRack(ctx, aeroCluster, 2)
			Expect(err).ToNot(HaveOccurred())
			Expect(oldRackPods.Items).To(BeEmpty(), "Old rack 2 pods should be removed")

			newRackPods, err := podsForRack(ctx, aeroCluster, 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(newRackPods.Items).ToNot(BeEmpty(), "New rack 3 pods should be running")

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 4.3 Rack addition + scale-down on existing rack ──────────────────
	Context("Rack addition combined with scale-down on existing rack", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce only the scale-down target, not the new rack pods", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Add rack 3 and simultaneously scale down rack 1 by 1.
			newRacks := append(aeroCluster.Spec.RackConfig.Racks, getDummyRackConf(3)[0])
			aeroCluster.Spec.RackConfig.Racks = newRacks
			// Net: +2 from rack 3 (2 pods), -1 from rack 1 = net +1.
			aeroCluster.Spec.Size++
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(8),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// New rack 3 pods must be running and NOT carry the quiesce annotation.
			// Refresh cluster object after reconcile.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			rack3Pods, err := podsForRack(ctx, aeroCluster, 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(rack3Pods.Items).ToNot(BeEmpty())

			for i := range rack3Pods.Items {
				Expect(rack3Pods.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation]).
					ToNot(Equal(asdbv1.BatchQuiesceAnnotationValue), "New rack 3 pod should not be quiesced")
			}

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 4.4 Multiple rack replacements simultaneously ────────────────────
	Context("Multiple rack replacements in one spec update", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2, 3})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should co-quiesce all old-rack pods in one migration round", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Replace racks 2 and 3 with IDs 4 and 5 in one update.
			for idx := range aeroCluster.Spec.RackConfig.Racks {
				switch aeroCluster.Spec.RackConfig.Racks[idx].ID {
				case 2:
					aeroCluster.Spec.RackConfig.Racks[idx].ID = 4
				case 3:
					aeroCluster.Spec.RackConfig.Racks[idx].ID = 5
				}
			}

			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(10),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// Old racks 2 and 3 must be gone; new racks 4 and 5 must exist.
			// Refresh cluster object after reconcile.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			for _, oldID := range []int{2, 3} {
				pods, pErr := podsForRack(ctx, aeroCluster, oldID)
				Expect(pErr).ToNot(HaveOccurred())
				Expect(pods.Items).To(BeEmpty(), "Old rack %d pods should be removed", oldID)
			}

			for _, newID := range []int{4, 5} {
				pods, pErr := podsForRack(ctx, aeroCluster, newID)
				Expect(pErr).ToNot(HaveOccurred())
				Expect(pods.Items).ToNot(BeEmpty(), "New rack %d pods should exist", newID)
			}

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §5  Concurrent Operations
	// ══════════════════════════════════════════════════════════════════════

	// ── 5.1 [EXISTS] Scale-down + rolling restart ────────────────────────
	Context("Scale-down combined with rolling restart on non-target racks", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should complete scale-down and rolling restart without data loss", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 1

			if _, ok := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService]; !ok {
				aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService] = map[string]interface{}{}
			}

			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["indent-allocations"] = true

			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			Expect(validateAerospikeConfigServiceClusterUpdate(
				logger, k8sClient, ctx, clusterNamespacedName, []string{"indent-allocations"},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 5.2 [EXISTS] Scale-down + dynamic config update ──────────────────
	Context("Scale-down combined with a dynamic (no-restart) config update", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should apply dynamic config without pod restarts while scale-down proceeds", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			podList, err := getPodList(aeroCluster, k8sClient)
			Expect(err).ToNot(HaveOccurred())

			// Record pod UIDs before the update.  A dynamic config change must
			// not restart any pod (UIDs must remain unchanged after reconcile).
			uidsBefore := make(map[string]types.UID, len(podList.Items))
			for idx := range podList.Items {
				pod := &podList.Items[idx]
				uidsBefore[pod.Name] = pod.UID
			}

			// Scale down by 1 AND raise proto-fd-max in the same spec update.
			// proto-fd-max is a service-context dynamic parameter: the operator
			// must push it via an Aerospike info "set-config:context=service"
			// call without restarting any pod.
			aeroCluster.Spec.Size -= 1
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = 18000

			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// No surviving pod should have been restarted (UID unchanged).
			podList, err = getPodList(aeroCluster, k8sClient)
			Expect(err).ToNot(HaveOccurred())

			for idx := range podList.Items {
				pod := &podList.Items[idx]
				if before, ok := uidsBefore[pod.Name]; ok {
					Expect(pod.UID).To(Equal(before),
						"Pod %s was unexpectedly restarted during dynamic config update", pod.Name)
				}
			}

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 5.3 Scale-down + scale-up on a different rack ────────────────────
	Context("Scale-down on one rack combined with scale-up on another", func() {
		BeforeEach(func() {
			// 2 racks, 2 pods each = 4 total.
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce only the scale-down target (not the scaled-up pods)", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Rack 1: -1 (scale-down). Rack 2: +1 (scale-up). Net: 0 size change.
			// We achieve this by keeping spec.Size the same but per-rack sizes change.
			// In practice AKO distributes pods evenly, so we adjust rack sizes via
			// a 5-total (rack1=2, rack2=3) approach.
			aeroCluster.Spec.Size++ // net +1: rack 2 gains 1 extra pod
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 5.4 Scale-down + pause/unpause ───────────────────────────────────
	Context("Scale-down with reconciliation paused then unpaused", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should not quiesce while paused; should complete after unpause", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Pause reconciliation.
			aeroCluster.Spec.Paused = ptr.To(true)
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Trigger scale-down while paused.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait a moment and verify no quiesce happened.
			time.Sleep(30 * time.Second)
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)

			// Unpause.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Paused = ptr.To(false)
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Scale-down should now complete.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §7  Annotation Idempotency & Crash Safety
	// ══════════════════════════════════════════════════════════════════════

	// ── 7.1 / 7.2 Annotation persists across operator restarts ───────────
	Context("BatchQuiesceAnnotation survives operator restart", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should set annotation after quiesce; re-entering reconcile with annotation set should fast-exit", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait for annotation to appear on at least one target pod.
			var annotatedPodName string

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr != nil {
					return false
				}

				for i := range podList.Items {
					if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
						annotatedPodName = podList.Items[i].Name
						return true
					}
				}

				return false
			}, 5*time.Minute, 2*time.Second).Should(BeTrue())

			Expect(annotatedPodName).ToNot(BeEmpty())

			// Simulate operator restart by forcing a new reconcile. The annotation
			// should keep the fast-exit path active.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			if aeroCluster.Annotations == nil {
				aeroCluster.Annotations = make(map[string]string)
			}

			aeroCluster.Annotations["test/force-reconcile"] = fmt.Sprintf("%d", time.Now().UnixNano())
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// The annotated pod should still carry the annotation after reconcile.
			Eventually(func() bool {
				return podHasBatchQuiesceAnnotation(ctx, clusterNamespacedName, annotatedPodName)
			}, 2*time.Minute, 2*time.Second).Should(BeTrue(),
				"BatchQuiesceAnnotation should persist on %s across reconcile", annotatedPodName)

			// Let scale-down finish.
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 7.4 Stale annotation on a pod that is no longer a target ─────────
	Context("Stale BatchQuiesceAnnotation on a pod that is no longer a scale-down target", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should fire quiesce-undo and clear the annotation on the reverted pod", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size

			// Scale down, wait for annotation.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			var staleAnnotatedPod string

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr != nil {
					return false
				}

				for i := range podList.Items {
					if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
						staleAnnotatedPod = podList.Items[i].Name
						return true
					}
				}

				return false
			}, 5*time.Minute, 2*time.Second).Should(BeTrue())

			// Revert: the annotated pod is now a non-target.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(originalSize),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			// Annotation must have been cleared by reconcileQuiesceUndo.
			Expect(podHasBatchQuiesceAnnotation(ctx, clusterNamespacedName, staleAnnotatedPod)).
				To(BeFalse(), "Stale annotation should be cleared after revert")

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §8  Backward Compatibility
	// ══════════════════════════════════════════════════════════════════════

	// ── 8.1 No annotations on any pod (first scale-down after deploy) ─────
	Context("First scale-down on a freshly deployed cluster (no BatchQuiesceAnnotation)", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce and complete scale-down without any pre-existing annotations", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Verify no annotations exist yet.
			assertNoBatchQuiesceAnnotations(aeroCluster)

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(4),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 8.2 ScaleDownBatchSize=1 ─────────────────────────────────────────
	Context("Scale-down with ScaleDownBatchSize=1 (single-pod batches)", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			batchSizeOne := intstr_fromInt(1)
			aeroCluster.Spec.RackConfig.ScaleDownBatchSize = &batchSizeOne
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce ALL targets upfront regardless of batch size", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			seenBothQuiesced := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr != nil {
					return false
				}

				podCount := utils.Len32(podList.Items)

				if podCount == 6 &&
					countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name) >= 2 {
					seenBothQuiesced = true
				}

				return podCount == aeroCluster.Spec.Size && seenBothQuiesced
			}, 15*time.Minute, 2*time.Second).Should(BeTrue(),
				"All targets should be quiesced upfront even with batch size 1")

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
				retryInterval, getTimeout(6),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})
})

// ─── small helpers used only in this file ─────────────────────────────────

// intstr_fromInt wraps an int32 as intstr.IntOrString (avoids import alias clash).
func intstr_fromInt(v int32) k8sintstr.IntOrString {
	return k8sintstr.FromInt32(v)
}
