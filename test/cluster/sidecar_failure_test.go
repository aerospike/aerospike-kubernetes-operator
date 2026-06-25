package cluster

import (
	"bytes"
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// crashingSidecar returns a sidecar container that immediately exits with a
// non-zero code, simulating a permanently failing sidecar. The Aerospike server
// container in the same pod continues to run normally.
func crashingSidecar() corev1.Container {
	return corev1.Container{
		Name:  "crashing-sidecar",
		Image: "busybox:1.28",
		Command: []string{
			"sh", "-c", "echo 'sidecar starting'; exit 1",
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
		},
	}
}

// waitForClusterPhase polls until the cluster reaches one of the given phases
// or the timeout expires.
func waitForClusterPhase(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	timeout time.Duration,
	phases ...asdbv1.AerospikeClusterPhase,
) error {
	phaseSet := make(map[asdbv1.AerospikeClusterPhase]struct{}, len(phases))
	for _, p := range phases {
		phaseSet[p] = struct{}{}
	}

	return wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true,
		func(ctx goctx.Context) (bool, error) {
			cluster := &asdbv1.AerospikeCluster{}
			if err := k8sClient.Get(ctx, clusterNamespacedName, cluster); err != nil {
				return false, client.IgnoreNotFound(err)
			}

			_, ok := phaseSet[cluster.Status.Phase]

			return ok, nil
		},
	)
}

// podHasCrashingSidecar returns true if any non-server container in the pod is
// in a crash-restart cycle. Kubernetes moves a failing container through several
// states before it reaches CrashLoopBackOff (Running → Terminated/Error →
// Waiting/Error → ... → Waiting/CrashLoopBackOff), so checking only for the
// CrashLoopBackOff reason would miss the earlier restart cycles. Instead, we
// use RestartCount > 0 combined with the container not currently being healthy
// (not Running and not Ready) as a more reliable signal.
func podHasCrashingSidecar(pod *corev1.Pod) bool {
	for idx := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[idx]
		if cs.Name == asdbv1.AerospikeServerContainerName {
			continue
		}

		if cs.RestartCount > 0 && !cs.Ready {
			return true
		}
	}

	return false
}

var _ = FDescribe("SidecarFailure", func() {
	ctx := goctx.TODO()

	Context("IgnoreSidecarFailure flag", func() {
		clusterName := fmt.Sprintf("sidecar-failure-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		It("Should complete reconciliation when IgnoreSidecarFailure is true and sidecar is crashing", func() {
			// Deploy a healthy cluster first so the initial reconcile completes
			// with all pods fully ready. Adding the crashing sidecar as a
			// subsequent rolling-restart update guarantees that at least one pod
			// with the old (healthy) spec is always present while the rolling
			// restart is in progress. That means hasClusterFailed always finds a
			// PodHealthy pod and never triggers the grace-period requeue loop,
			// so the update reliably reaches Completed.
			By("Deploying a healthy 2-node cluster")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar with IgnoreSidecarFailure=true via a rolling restart")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying Aerospike server is running and reachable in every pod (nodeIDs populated)")

			cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			for podName, podStatus := range cluster.Status.Pods {
				Expect(podStatus.Aerospike.NodeID).ToNot(BeEmpty(),
					"expected nodeID to be set for pod %s", podName)
			}

			By("Verifying that every pod has its sidecar in CrashLoopBackOff (server still running)")

			Eventually(func(g Gomega) {
				podList, err := getClusterPodList(k8sClient, ctx, cluster)
				g.Expect(err).ToNot(HaveOccurred())

				for idx := range podList.Items {
					g.Expect(podHasCrashingSidecar(&podList.Items[idx])).To(BeTrue(),
						"expected sidecar to be crashing on pod %s", podList.Items[idx].Name)
					g.Expect(utils.IsAerospikeServerRunning(&podList.Items[idx])).To(BeTrue(),
						"expected server container to be running on pod %s", podList.Items[idx].Name)
				}
			}, 3*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("Should block reconciliation when IgnoreSidecarFailure is false and sidecar starts crashing on an"+
			"existing cluster, then unblock when flag is enabled", func() {
			By("Deploying a healthy cluster without a sidecar")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashlooping sidecar with IgnoreSidecarFailure=false")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(false)

			// Apply the update without waiting for Completed.
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying the rolling restart is blocked at exactly one pod — sidecar failure has not propagated further")

			cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// checkBlockedState asserts the expected steady-blocked condition:
			//   • exactly 1 pod with a CrashLoopBackOff sidecar (the restarted one)
			//   • the remaining pods fully ready (not yet restarted, no sidecar)
			checkBlockedState := func(g Gomega) {
				podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
				g.Expect(podListErr).ToNot(HaveOccurred())

				crashingCount := 0
				readyCount := 0

				for idx := range podList.Items {
					pod := &podList.Items[idx]
					if podHasCrashingSidecar(pod) {
						crashingCount++
					}

					if utils.IsPodRunningAndReady(pod) {
						readyCount++
					}
				}

				g.Expect(crashingCount).To(Equal(1),
					"expected exactly 1 pod with a crashing sidecar; rolling restart should be blocked after the first pod")
				g.Expect(readyCount).To(Equal(int(cluster.Spec.Size)-1),
					"expected the remaining %d pods to still be fully ready (not yet restarted)", cluster.Spec.Size-1)
			}

			// Wait for the blocked state to be reached. The container transitions
			// through transient states (ContainerCreating, Terminated/Error) before
			// Kubernetes applies the exponential backoff and sets the reason to
			// "CrashLoopBackOff".
			Eventually(checkBlockedState, 5*time.Minute, 5*time.Second).Should(Succeed())

			// Prove the rolling restart is truly blocked, not just transiently at 1
			// crashing pod. An Eventually alone could pass during a fleeting window
			// between pod restarts. Consistently asserts the state holds for a
			// sustained period, confirming no further pods are being restarted.
			Consistently(checkBlockedState, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Enabling IgnoreSidecarFailure=true and verifying cluster reaches Completed")

			cluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			Expect(updateCluster(k8sClient, ctx, cluster)).ToNot(HaveOccurred())

			updatedCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Phase).To(Equal(asdbv1.AerospikeClusterCompleted))

			By("Verifying the crashing sidecar has propagated to all pods after rolling restart completes")

			// With IgnoreSidecarFailure=true the rolling restart is no longer
			// blocked, so it runs to completion. Every pod must now have been
			// restarted with the new spec and have its sidecar in CrashLoopBackOff.
			finalPodList, err := getClusterPodList(k8sClient, ctx, updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(finalPodList.Items).To(HaveLen(int(updatedCluster.Spec.Size)))

			for idx := range finalPodList.Items {
				pod := &finalPodList.Items[idx]
				Expect(podHasCrashingSidecar(pod)).To(BeTrue(),
					"expected crashing sidecar on pod %s after full rolling restart", pod.Name)
				Expect(utils.IsAerospikeServerRunning(pod)).To(BeTrue(),
					"expected Aerospike server to be running on pod %s", pod.Name)
			}
		})
	})

	Context("Rolling restart with crashing sidecar", func() {
		clusterName := fmt.Sprintf("sidecar-rolling-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		It("Should complete rolling restart when IgnoreSidecarFailure is true and sidecar is crashing", func() {
			// Deploy a healthy cluster first, then introduce the crashing
			// sidecar and the proto-fd-max config change in a single update.
			// Combining them into one rolling restart ensures that pods not yet
			// restarted still carry the old (healthy) spec, so hasClusterFailed
			// always finds at least one PodHealthy pod and never triggers the
			// grace-period requeue loop.
			By("Deploying a healthy 2-node cluster")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar with IgnoreSidecarFailure=true and triggering a rolling restart " +
				"via proto-fd-max change")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = int64(20000)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying proto-fd-max is actually updated on every server node")

			Expect(validateAerospikeConfigServiceClusterUpdate(
				logger, k8sClient, ctx, clusterNamespacedName, []string{"proto-fd-max"},
			)).ToNot(HaveOccurred())

			By("Verifying all pods still have the crashing sidecar and running server")

			Eventually(func(g Gomega) {
				cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
				g.Expect(clusterErr).ToNot(HaveOccurred())

				podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
				g.Expect(podListErr).ToNot(HaveOccurred())

				for idx := range podList.Items {
					g.Expect(podHasCrashingSidecar(&podList.Items[idx])).To(BeTrue(),
						"expected sidecar to be crashing on pod %s after rolling restart", podList.Items[idx].Name)
					g.Expect(utils.IsAerospikeServerRunning(&podList.Items[idx])).To(BeTrue(),
						"expected server to be running on pod %s after rolling restart", podList.Items[idx].Name)
				}
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("Curing the sidecar — replacing it with a healthy one and verifying full recovery")

			// Replace the crashlooping sidecar with a long-running healthy one.
			// The cluster should complete another rolling restart and all pods
			// should become fully ready (all containers including the sidecar).
			curedCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			curedCluster.Spec.PodSpec.Sidecars = []corev1.Container{
				{
					Name:    "crashing-sidecar",
					Image:   "busybox:1.28",
					Command: []string{"sh", "-c", "echo 'sidecar healthy'; sleep 3600"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("16Mi"),
						},
					},
				},
			}

			Expect(updateCluster(k8sClient, ctx, curedCluster)).ToNot(HaveOccurred())

			recoveredCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(recoveredCluster.Status.Phase).To(Equal(asdbv1.AerospikeClusterCompleted))

			// After the sidecar is fixed every pod must be fully ready —
			// both the Aerospike server and the sidecar container are running.
			recoveredPodList, err := getClusterPodList(k8sClient, ctx, recoveredCluster)
			Expect(err).ToNot(HaveOccurred())

			for idx := range recoveredPodList.Items {
				pod := &recoveredPodList.Items[idx]
				Expect(utils.IsPodRunningAndReady(pod)).To(BeTrue(),
					"expected pod %s to be fully ready after sidecar is cured", pod.Name)
				Expect(podHasCrashingSidecar(pod)).To(BeFalse(),
					"expected sidecar to no longer be crashing on pod %s", pod.Name)
			}
		})
	})

	Context("Image upgrade with crashing sidecar", func() {
		clusterName := fmt.Sprintf("sidecar-upgrade-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		It("Should complete image upgrade when IgnoreSidecarFailure is true and sidecar is crashing", func() {
			// Deploy a healthy cluster first, then add the crashing sidecar and
			// the image upgrade in a single update. Combining them into one
			// rolling restart ensures pods not yet restarted still carry the old
			// (healthy) spec, so hasClusterFailed always finds a PodHealthy pod
			// and never blocks on the grace-period requeue loop.
			By("Deploying a healthy 2-node cluster")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar with IgnoreSidecarFailure=true and triggering an image upgrade in one update")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			aeroCluster.Spec.Image = nextImage

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying cluster returns to Completed with the new image")

			// updateCluster already verified Completed; fetch cluster here only
			// to read pod statuses. The phase snapshot may briefly show InProgress
			// again (sidecar crash event) so we do not re-check it.
			cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			for podName, podStatus := range cluster.Status.Pods {
				Expect(utils.IsImageEqual(podStatus.Image, nextImage)).To(BeTrue(),
					"expected pod %s to be on image %s after upgrade, got %s",
					podName, nextImage, podStatus.Image)
			}
		})
	})

	Context("MaxIgnorablePods budget with crashing sidecar", func() {
		clusterName := fmt.Sprintf("sidecar-budget-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		It("Should not count sidecar-failed pods against the MaxIgnorablePods budget and roster should include the"+
			" sidecar-failed pod", func() {
			// The sidecar polls /signal/fail every 5 s and exits when the file
			// appears. An emptyDir volume persists the file across container
			// restarts, so touching the file once is enough to put the sidecar
			// into permanent CrashLoopBackOff. Initially the file does not exist,
			// so all pods start fully ready.
			signalSidecar := corev1.Container{
				Name:  "signal-sidecar",
				Image: "busybox:1.28",
				Command: []string{
					"sh", "-c",
					`while true; do
  if [ -f /signal/fail ]; then
    echo "signal-sidecar: fail signal received, crashing"
    exit 1
  fi
  sleep 5
done`,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("16Mi"),
					},
				},
			}

			// emptyDir volume scoped to the sidecar; persists across container
			// restarts within the pod so the fail signal survives a restart.
			signalVolume := asdbv1.VolumeSpec{
				Name: "sidecar-signal",
				Source: asdbv1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
				Sidecars: []asdbv1.VolumeAttachment{
					{ContainerName: "signal-sidecar", Path: "/signal"},
				},
			}

			By("Deploying a 2-node SC cluster with all sidecars healthy")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{signalSidecar}
			aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, signalVolume)

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying all pods are fully ready — sidecar is healthy before the fail signal is sent")

			podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(podList.Items).To(HaveLen(2))

			for idx := range podList.Items {
				Expect(utils.IsPodRunningAndReady(&podList.Items[idx])).To(BeTrue(),
					"expected pod %s to be fully ready before fail signal", podList.Items[idx].Name)
			}

			By("Explicitly failing pod-0's sidecar by writing the fail signal into its emptyDir")

			// Pod naming with default rack (ID 0): <clusterName>-0-0 and <clusterName>-0-1.
			// We signal only the first pod so exactly 1 sidecar ends up crashing.
			pod0Name := clusterName + "-0-0"
			Expect(execInPodContainer(namespace, pod0Name, "signal-sidecar",
				[]string{"touch", "/signal/fail"}),
			).ToNot(HaveOccurred())

			By("Waiting for pod-0's sidecar to enter CrashLoopBackOff")

			pod0NamespacedName := types.NamespacedName{Name: pod0Name, Namespace: namespace}

			Eventually(func() bool {
				pod := &corev1.Pod{}
				if getErr := k8sClient.Get(ctx, pod0NamespacedName, pod); getErr != nil {
					return false
				}

				return podHasCrashingSidecar(pod)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue(),
				"pod-0 sidecar should enter CrashLoopBackOff after the fail signal")

			By("Verifying exactly 1 pod has a crashing sidecar and 1 pod remains fully ready")

			podList, err = getClusterPodList(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			crashingCount := 0

			for idx := range podList.Items {
				if podHasCrashingSidecar(&podList.Items[idx]) {
					crashingCount++
				}
			}

			Expect(crashingCount).To(Equal(1),
				"expected exactly 1 pod to have a crashing sidecar")

			By("Setting MaxIgnorablePods=1, IgnoreSidecarFailure=false and triggering a rolling restart via proto-fd-max change")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			maxIgnorable := intOrStr(1)
			aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorable
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(false)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = int64(20000)

			// Submit without waiting — the rolling restart should stall because
			// IgnoreSidecarFailure=false blocks when pod-0's sidecar is not ready.
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying rolling restart is stuck in InProgress — sidecar failure blocks readiness wait")

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				2*time.Minute, asdbv1.AerospikeClusterInProgress),
			).ToNot(HaveOccurred())

			stuckCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(stuckCluster.Status.Phase).ToNot(Equal(asdbv1.AerospikeClusterCompleted),
				"cluster should not have completed: sidecar failure must block the rolling restart")

			By("Setting IgnoreSidecarFailure=true and verifying rolling restart completes")

			stuckCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			Expect(updateCluster(k8sClient, ctx, stuckCluster)).ToNot(HaveOccurred())

			completedCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(completedCluster.Status.Phase).To(Equal(asdbv1.AerospikeClusterCompleted))

			By("Verifying proto-fd-max is updated on all server nodes including the sidecar-failed pod")

			Expect(validateAerospikeConfigServiceClusterUpdate(
				logger, k8sClient, ctx, clusterNamespacedName, []string{"proto-fd-max"},
			)).ToNot(HaveOccurred())

			By("Verifying the SC roster includes the sidecar-failed pod — its Aerospike server was never affected")

			// The sidecar-failed pod's Aerospike server ran throughout; the pod
			// must remain in the roster. If the sidecar failure were incorrectly
			// counted against MaxIgnorablePods the pod would be skipped and
			// could drop out of the roster.
			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
		})
	})

	Context("Lifecycle operations with crashing sidecar", func() {
		clusterName := fmt.Sprintf("sidecar-lifecycle-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		It("Should complete scale-down, scale-up and image upgrade when IgnoreSidecarFailure is true and sidecar "+
			"is crashing", func() {
			// Deploy a healthy cluster first, then add the crashing sidecar via
			// a rolling-restart update. During that rolling restart at least one
			// pod still carries the old healthy spec so hasClusterFailed never
			// triggers the grace-period requeue loop and the update reliably
			// completes. Subsequent scale and upgrade operations may incur a
			// short grace-period delay (≤60 s) on their first reconcile after
			// all pods have a crashing sidecar, but they always complete within
			// the standard per-operation timeout.
			By("Deploying a healthy 4-node cluster")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar with IgnoreSidecarFailure=true via a rolling restart")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// verifyClusterState is a local helper that asserts all servers are
			// running, all sidecars are crashing, and the pod count matches
			// expectedSize. Phase is not checked here because the perpetual
			// CrashLoopBackOff events can briefly push the cluster back to
			// InProgress between reconcile cycles; the important invariant is
			// that every Aerospike server is up and every sidecar is crashing.
			verifyClusterState := func(expectedSize int) {
				GinkgoHelper()

				Eventually(func(g Gomega) {
					cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(clusterErr).ToNot(HaveOccurred())

					podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
					g.Expect(podListErr).ToNot(HaveOccurred())
					g.Expect(podList.Items).To(HaveLen(expectedSize),
						"expected %d pods after operation", expectedSize)

					for idx := range podList.Items {
						pod := &podList.Items[idx]
						g.Expect(utils.IsAerospikeServerRunning(pod)).To(BeTrue(),
							"expected Aerospike server to be running on pod %s", pod.Name)
						g.Expect(podHasCrashingSidecar(pod)).To(BeTrue(),
							"expected sidecar to still be crashing on pod %s", pod.Name)
					}
				}, 3*time.Minute, 10*time.Second).Should(Succeed())
			}

			By("Verifying initial 4-node state — all servers running, all sidecars crashing")
			verifyClusterState(4)

			// ── Scale-down ──────────────────────────────────────────────────
			By("Scaling down from 4 to 3 nodes")

			Expect(scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 1)).ToNot(HaveOccurred())

			By("Verifying 3-node state after scale-down — all servers running, all sidecars crashing")
			verifyClusterState(3)

			// ── Scale-up ────────────────────────────────────────────────────
			By("Scaling up from 3 to 5 nodes")

			Expect(scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)).ToNot(HaveOccurred())

			By("Verifying 5-node state after scale-up — all servers running, all sidecars crashing")
			verifyClusterState(5)

			// ── Image upgrade ────────────────────────────────────────────────
			By("Triggering an image upgrade to nextImage")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Image = nextImage
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying 5-node state after image upgrade — all pods on new image, all servers running, all sidecars crashing")

			Eventually(func(g Gomega) {
				cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				g.Expect(err).ToNot(HaveOccurred())

				podList, err := getClusterPodList(k8sClient, ctx, cluster)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(5))

				for idx := range podList.Items {
					pod := &podList.Items[idx]
					podStatus := cluster.Status.Pods[pod.Name]
					g.Expect(utils.IsImageEqual(podStatus.Image, nextImage)).To(BeTrue(),
						"expected pod %s to be on image %s after upgrade, got %s",
						pod.Name, nextImage, podStatus.Image)
					g.Expect(utils.IsAerospikeServerRunning(pod)).To(BeTrue(),
						"expected Aerospike server to be running on pod %s after upgrade", pod.Name)
					g.Expect(podHasCrashingSidecar(pod)).To(BeTrue(),
						"expected sidecar to still be crashing on pod %s after upgrade", pod.Name)
				}
			}, 3*time.Minute, 10*time.Second).Should(Succeed())
		})
	})
})

// intOrStr is a convenience wrapper so test code can build an IntOrString inline.
func intOrStr(v int) intstr.IntOrString {
	return intstr.FromInt(v)
}

// execInPodContainer runs command inside containerName of the named pod and
// returns an error if the exec request itself fails. It is used by tests to
// inject state into a running container (e.g. touching a signal file).
func execInPodContainer(podNamespace, podName, containerName string, command []string) error {
	req := k8sClientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerName,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, clientgoscheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create SPDY executor for pod %s/%s: %w", podNamespace, podName, err)
	}

	var stdout, stderr bytes.Buffer

	if err = executor.StreamWithContext(goctx.TODO(), remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return fmt.Errorf("exec failed in pod %s/%s container %s (stdout=%q stderr=%q): %w",
			podNamespace, podName, containerName, stdout.String(), stderr.String(), err)
	}

	return nil
}
