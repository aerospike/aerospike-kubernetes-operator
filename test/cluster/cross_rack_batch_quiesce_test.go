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
// §3 Non-ready / never-joined pod handling
// §4 Rack operations (delete, replace)
// §5 Concurrent operations

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

// minCluster returns an AerospikeCluster stub for getPodList queries.
func minCluster(nsn types.NamespacedName) *asdbv1.AerospikeCluster {
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{Name: nsn.Name, Namespace: nsn.Namespace},
	}
}

// waitForQuiescedCount blocks until the cluster reports count
// quiesced nodes (polled via the first available pod).
func waitForQuiescedCount(
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	count int,
	timeout time.Duration,
) {
	GinkgoHelper()

	Eventually(func() bool {
		podList, err := getPodList(minCluster(clusterNamespacedName), k8sClient)
		if err != nil || len(podList.Items) == 0 {
			return false
		}

		return countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name) == count
	}, timeout, 2*time.Second).Should(
		BeTrue(),
		"Expected %d quiesced nodes within %s", count, timeout,
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

// assertCleanQuiesceState asserts zero quiesced nodes and no stale BatchQuiesceAnnotation.
func assertCleanQuiesceState(
	ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	clusterNamespacedName types.NamespacedName,
) {
	GinkgoHelper()
	assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
	assertNoBatchQuiesceAnnotations(aeroCluster)
}

// waitForPodsWithoutBatchAnnotation blocks until exactly count pods in the
// cluster do NOT carry BatchQuiesceAnnotation (e.g. use count==totalPods to
// wait for all annotations to be cleared after a mid-operation revert).
func waitForPodsWithoutBatchAnnotation(
	clusterNamespacedName types.NamespacedName,
	count int,
	timeout time.Duration,
) {
	GinkgoHelper()

	Eventually(func() int {
		podList, err := getPodList(minCluster(clusterNamespacedName), k8sClient)
		if err != nil {
			return -1
		}

		n := 0

		for i := range podList.Items {
			if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] != asdbv1.BatchQuiesceAnnotationValue {
				n++
			}
		}

		return n
	}, timeout, 2*time.Second).Should(
		Equal(count),
		"Expected exactly %d pod(s) without BatchQuiesceAnnotation within %s", count, timeout,
	)
}

// waitForCoQuiesceBeforeRemoval blocks until ≥minQuiesce pods are quiesced
// while duringPodCount pods still exist, then waits for the final pod count.
func waitForCoQuiesceBeforeRemoval(
	ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	clusterNamespacedName types.NamespacedName,
	duringPodCount int32,
	minQuiesce int,
	failMsg string,
) {
	GinkgoHelper()

	seen := false

	Eventually(func() bool {
		podList, lErr := getPodList(aeroCluster, k8sClient)
		Expect(lErr).ToNot(HaveOccurred())

		podCount := utils.Len32(podList.Items)

		if len(podList.Items) > 0 &&
			podCount == duringPodCount &&
			countQuiescedNodes(ctx, clusterNamespacedName, podList.Items[0].Name) >= minQuiesce {
			seen = true
		}

		return podCount == aeroCluster.Spec.Size && seen
	}, 15*time.Minute, 2*time.Second).Should(BeTrue(), failMsg)
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
		Racks:                              racks,
		Namespaces:                         []string{"test"},
		EnableParallelScaleDownAcrossRacks: ptr.To(true),
	}

	return aeroCluster
}

// ─── test suite ────────────────────────────────────────────────────────────

var _ = Describe("CrossRackBatchQuiesce", func() {
	ctx := goctx.TODO()

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

	// ── 1.1 3-rack cluster scale-down ───────────────────────────────────
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

	// ── 1.2 Single-rack scale-down ───────────────────────────────────────
	// reconcileBatchQuiesce runs regardless of rack count. Even for a
	// single-rack cluster, ALL target pods must be quiesced simultaneously in
	// the pre-pass before the StatefulSet replica count is reduced.
	Context("Single-rack scale-down", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should quiesce both target pods simultaneously upfront even for a single-rack cluster", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Scale down by 2: both targets must be annotated in the same pre-pass
			// before any pod is removed, distinguishing the parallel upfront quiesce
			// from the old sequential per-batch path.
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// 4 pods on rack 1 → scale-down targets are the two highest-indexed pods.
			target1 := clusterName + "-1-3"
			target2 := clusterName + "-1-2"

			isAnnotated := func(podName string) bool {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: podName, Namespace: clusterNamespacedName.Namespace,
				}, pod); err != nil {
					return false
				}

				return pod.Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue
			}

			// Both annotations must appear before either pod is deleted.
			Eventually(func() bool {
				return isAnnotated(target1) && isAnnotated(target2)
			}, 3*time.Minute, 2*time.Second).Should(BeTrue(),
				"Expected BatchQuiesceAnnotation on both scale-down targets (%s, %s)", target1, target2)

			// Let scale-down finish; annotations must be cleared.
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			assertCleanQuiesceState(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 1.3 No scale-down — batch quiesce pre-pass entirely skipped ──────
	Context("No scale-down — batch quiesce pre-pass should be skipped", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should not quiesce any pod when size is unchanged", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Trigger a reconcile via a dynamic config update (no restart, no
			// size change).
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = 18000
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Poll to Completed; latch annotationSeen if any pod carries the
			// annotation at any point (catches transient annotations).
			annotationSeen := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr == nil {
					for i := range podList.Items {
						if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
							annotationSeen = true
						}
					}
				}

				cur, cErr := getCluster(k8sClient, ctx, clusterNamespacedName)
				if cErr != nil {
					return false
				}

				return cur.Status.Phase == asdbv1.AerospikeClusterCompleted
			}, getTimeout(aeroCluster.Spec.Size), retryInterval).Should(BeTrue(),
				"cluster should reach Completed state after proto-fd-max update")

			Expect(annotationSeen).To(BeFalse(),
				"No pod should carry BatchQuiesceAnnotation during a no-scale-down reconcile")

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §2  Scale-Down Revert
	// ══════════════════════════════════════════════════════════════════════

	// ── 2.1 Partial revert ──────────────────────────────────────────────
	Context("Partial scale-down revert while migration is in progress", func() {
		BeforeEach(func() {
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 6, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should undo quiesce for the reverted pod and keep other targets quiesced", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size -= 4
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			waitForQuiescedCount(ctx, clusterNamespacedName, 4, 3*time.Minute)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size += 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())
			waitForPodsWithoutBatchAnnotation(clusterNamespacedName, 4, 3*time.Minute)

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterCompleted)).ToNot(HaveOccurred())
		})
	})

	// ── 2.2 Full revert ─────────────────────────────────────────────────
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

			waitForQuiescedCount(ctx, clusterNamespacedName, 2, 3*time.Minute)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size = originalSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			waitForPodsWithoutBatchAnnotation(clusterNamespacedName, 6, 3*time.Minute)

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterCompleted)).ToNot(HaveOccurred())
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §3  Non-Ready / Never-Joined Pod Handling
	// ══════════════════════════════════════════════════════════════════════

	// ── 3.1 Target pod never joined (no CR status) ───────────────────────
	// Scenario: cluster fills every schedulable worker node with one pod
	// (MultiPodPerHost=false).  Scaling up by 1 creates a pod that can never
	// be scheduled — it stays in Pending indefinitely with no CR status entry.
	// Scaling back down must complete without MaxIgnorablePods: the
	// Never-joined pods are handled upstream by getIgnorablePods (unschedulable
	// detection) and do not consume any MaxIgnorablePods budget.
	Context("Scale-down target never joined cluster (no CR status)", func() {
		var nodeCount int32

		BeforeEach(func() {
			nodeCount = validateMinNodeCountOrSkip(ctx, 2,
				"never-joined-pod test needs ≥2 nodes so MultiPodPerHost=false cluster can start")

			// Fill all nodes; the +1 pod will be permanently unschedulable.
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, nodeCount, []int{1, 2})
			aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should add the never-joined pod to ignorablePodNames and complete scale-down", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			originalSize := aeroCluster.Spec.Size // == nodeCount
			newSize := originalSize + 1

			// Scale up by 1; the new pod stays Pending (all nodes occupied).
			aeroCluster.Spec.Size = newSize
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait for the Pending pod to appear with no CR status entry.
			Eventually(func() bool {
				podList, listErr := getPodList(aeroCluster, k8sClient)
				if listErr != nil || len(podList.Items) < int(newSize) {
					return false
				}

				cl, getErr := getCluster(k8sClient, ctx, clusterNamespacedName)
				if getErr != nil {
					return false
				}

				for i := range podList.Items {
					pod := &podList.Items[i]
					if pod.Status.Phase != corev1.PodPending {
						continue
					}

					if _, hasStatus := cl.Status.Pods[pod.Name]; !hasStatus {
						return true // found a never-joined Pending pod
					}
				}

				return false
			}, 2*time.Minute, 2*time.Second).Should(BeTrue(),
				"expected a never-joined Pending pod (unschedulable) to appear")

			// Scale back down; the unschedulable pod is in ignorablePodNames
			// (via getIgnorablePods) without consuming any MaxIgnorablePods budget.
			aeroCluster.Spec.Size = originalSize
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// No quiesced nodes or stale annotations must remain.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			assertCleanQuiesceState(ctx, aeroCluster, clusterNamespacedName)
		})
	})

	// ── 3.2 New rack addition combined with scale-down ──────────────────
	// New rack pods must be server-ready before quiesce fires; they are not scale-down targets.
	Context("Rack addition: new rack pods ready and not quiesced", func() {
		BeforeEach(func() {
			// Start with 2 racks, 2 pods each = 4 total.
			aeroCluster := newMultiRackNonSCCluster(clusterNamespacedName, 4, []int{1, 2})
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		It("Should wait for new rack pods to be ready and not quiesce them", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Add rack 3.
			// Net: +1 from rack 3, -1 from rack 2
			newRacks := append(aeroCluster.Spec.RackConfig.Racks, getDummyRackConf(3)[0])
			aeroCluster.Spec.RackConfig.Racks = newRacks
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			rack3Pod0Name := clusterName + "-3-0"
			// rack 2 scales 2→1 pods; the highest-indexed pod is the target.
			rack2TargetName := clusterName + "-2-1"

			// Phase 1: wait for rack 3's first pod to become server-ready.
			// During this window NO pod should carry the annotation — Step 3
			// (reconcileBatchQuiesce) must not run before Step 2 finishes.
			annotationBeforeReady := false

			Eventually(func() bool {
				podList, lErr := getPodList(aeroCluster, k8sClient)
				if lErr == nil {
					for i := range podList.Items {
						if podList.Items[i].Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue {
							annotationBeforeReady = true
						}
					}
				}

				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: rack3Pod0Name, Namespace: clusterNamespacedName.Namespace,
				}, pod); err != nil {
					return false
				}

				return utils.IsAerospikeServerReady(pod)
			}, 5*time.Minute, 2*time.Second).Should(BeTrue(),
				"rack 3 pod 0 should become server-ready")

			Expect(annotationBeforeReady).To(BeFalse(),
				"no pod should carry BatchQuiesceAnnotation before rack 3's first pod is server-ready")

			// Phase 2: once rack 3 is up, the batch quiesce pre-pass must annotate
			// rack 2's scale-down target.
			Eventually(func() bool {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: rack2TargetName, Namespace: clusterNamespacedName.Namespace,
				}, pod); err != nil {
					return false
				}

				return pod.Annotations[asdbv1.BatchQuiesceAnnotation] == asdbv1.BatchQuiesceAnnotationValue
			}, 3*time.Minute, 2*time.Second).Should(BeTrue(),
				"rack 2's scale-down target %s should be annotated after rack 3 is ready", rack2TargetName)

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterCompleted)).ToNot(HaveOccurred())
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §4  Rack Operations
	// ══════════════════════════════════════════════════════════════════════

	// ── 4.1 Rack delete ─────────────────────────────────────────────────
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

			// Quiesce window: 6 total pods (rack1×2 + rack2×2 + rack3×2), ≥3 targets
			// (1 rack2 scale-down + 2 rack3 delete targets) must be quiesced together.
			waitForCoQuiesceBeforeRemoval(ctx, aeroCluster, clusterNamespacedName, 6, 3,
				"Pods of deleted rack should be co-quiesced before removal")

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterCompleted)).ToNot(HaveOccurred())
		})
	})

	// ── 4.2 Rack replace ────────────────────────────────────────────────
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

			// Quiesce window: 8 total pods (rack1×3 + rack3×2 new + rack2×3 old),
			// ≥3 targets (rack 2 delete targets) must be quiesced together.
			waitForCoQuiesceBeforeRemoval(ctx, aeroCluster, clusterNamespacedName, 8, 3,
				"Old rack pods should be quiesced before removal")

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterCompleted)).ToNot(HaveOccurred())
		})
	})

	// ══════════════════════════════════════════════════════════════════════
	// §5  Concurrent Operations
	// ══════════════════════════════════════════════════════════════════════

	// ── 5.1 Scale-down + pause/unpause ───────────────────────────────────
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
			aeroCluster.Spec.Size -= 2
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			// Wait a moment and verify no quiesce happened.
			time.Sleep(30 * time.Second)
			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)

			// Unpause — scale-down should now complete.
			aeroCluster.Spec.Paused = nil
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			assertNoStaleQuiesce(ctx, aeroCluster, clusterNamespacedName)
		})
	})
})
