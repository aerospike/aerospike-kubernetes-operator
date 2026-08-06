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
func waitForClusterPhase(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	phases ...asdbv1.AerospikeClusterPhase) error {
	phaseSet := make(map[asdbv1.AerospikeClusterPhase]struct{}, len(phases))
	for _, p := range phases {
		phaseSet[p] = struct{}{}
	}

	return wait.PollUntilContextTimeout(ctx, retryInterval, 2*time.Minute, true,
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

// signalControlledSidecar returns a sidecar that runs healthy until the file
// /signal/fail is created in its emptyDir volume. Once the file exists the
// sidecar exits, and because the emptyDir persists across container restarts
// the sidecar stays in permanent CrashLoopBackOff. Use with signalVolumeForSidecar
// and execInPodContainer to inject sidecar failures on demand.
func signalControlledSidecar() corev1.Container {
	return corev1.Container{
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
}

// signalVolumeForSidecar returns the emptyDir VolumeSpec for signalControlledSidecar.
// The volume persists across container restarts so the fail signal survives a
// container restart and keeps the sidecar in CrashLoopBackOff indefinitely.
func signalVolumeForSidecar() asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: "sidecar-signal",
		Source: asdbv1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
		Sidecars: []asdbv1.VolumeAttachment{
			{ContainerName: "signal-sidecar", Path: "/signal"},
		},
	}
}

// failPodSidecar writes the fail signal into the named pod's signal-sidecar
// container, triggering a permanent CrashLoopBackOff for that pod only.
func failPodSidecar(podNamespace, podName string) error {
	return execInPodContainer(podNamespace, podName, "signal-sidecar",
		[]string{"touch", "/signal/fail"})
}

var _ = Describe("SidecarFailure", func() {
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

		It("Should complete reconciliation and image upgrade when IgnoreSidecarFailure is true and"+
			" sidecar is crashing", func() {
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

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying sidecar is crashing and Aerospike server is reachable on every pod")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 2)

			By("Triggering an image upgrade to nextImage while the sidecar is still crashing")

			// The cluster is already Completed with IgnoreSidecarFailure=true, so
			// the upgrade proceeds through a rolling restart just like a healthy
			// cluster. Each pod gets the new image; the sidecar keeps crashing.
			aeroCluster.Spec.Image = nextImage
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying all pods are on the new image with servers reachable and sidecars still crashing")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 2)
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

			By("Verifying cluster transitions to Error — sidecar failure with IgnoreSidecarFailure=false triggers Error phase")
			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterError)).ToNot(HaveOccurred())

			By("Verifying the rolling restart is blocked at exactly one pod — sidecar failure has not propagated further")

			assertRollingRestartBlocked(k8sClient, ctx, clusterNamespacedName, int(aeroCluster.Spec.Size)-1)

			By("Enabling IgnoreSidecarFailure=true and verifying cluster reaches Completed")

			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying sidecar has propagated to all pods and server is reachable after rolling restart completes")

			// With IgnoreSidecarFailure=true the rolling restart is no longer
			// blocked, so it runs to completion. Every pod must now have been
			// restarted with the new spec and have its sidecar in CrashLoopBackOff.
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, int(aeroCluster.Spec.Size))
		})

		It("Should fix sidecar-failed pod via config-change-driven restart when IgnoreSidecarFailure is false", func() {
			// When IgnoreSidecarFailure=false and a sidecar is crashing, the
			// reconciler stalls waiting for full pod readiness. Updating the
			// sidecar spec to a healthy image constitutes a config change for
			// that pod. The sidecar-failed path in handleFailedPodsInRack detects
			// the hash mismatch and restarts the pod with the new spec (with full
			// migration/quiesce safety checks). The cluster should then complete
			// without the operator needing IgnoreSidecarFailure=true.
			By("Deploying a healthy 2-node cluster")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar with IgnoreSidecarFailure=false — rolling restart should stall")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(false)
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying cluster transitions to Error — sidecar failure with IgnoreSidecarFailure=false triggers Error phase")
			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterError)).ToNot(HaveOccurred())

			By("Waiting for rolling restart to stall — exactly 1 pod has crashing sidecar, 1 pod still fully ready")

			// A plain waitForClusterPhase(InProgress) is insufficient — the cluster
			// enters InProgress the moment the rolling restart begins, before any
			// sidecar has had a chance to crash. assertRollingRestartBlocked proves
			// the sidecar failure is the reason the restart stopped at pod-0.
			assertRollingRestartBlocked(k8sClient, ctx, clusterNamespacedName, int(aeroCluster.Spec.Size)-1)

			By("Fixing the sidecar by replacing the crashing command with a healthy one")

			// The sidecar spec change (new command → new config hash) triggers a
			// config-change-driven restart via handleFailedPodsInRack's sidecar
			// path. Safety checks (migration wait, quiesce) are applied because
			// the server container is still reachable.
			stalledCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			stalledCluster.Spec.PodSpec.Sidecars = []corev1.Container{
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
			Expect(updateCluster(k8sClient, ctx, stalledCluster)).ToNot(HaveOccurred())

			By("Verifying all pods are fully ready after the config-change-driven restart")

			podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			for idx := range podList.Items {
				pod := &podList.Items[idx]
				Expect(utils.IsPodRunningAndReady(pod)).To(BeTrue(),
					"expected pod %s to be fully ready after sidecar fix", pod.Name)
				Expect(podHasCrashingSidecar(pod)).To(BeFalse(),
					"expected no crashing sidecar on pod %s after config fix", pod.Name)
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

		It("Should include sidecar-failed pods in rolling restart config update — treated as active pods", func() {
			// getServerFailedAndActivePods classifies sidecar-failed pods as
			// "active" (server is running). They must therefore participate in
			// the rolling restart exactly like healthy pods: their config hash
			// is checked, they receive the new proto-fd-max value, and they are
			// included in all info calls and roster operations.
			By("Deploying a healthy 3-node cluster with a signal-controlled sidecar")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			vol := signalVolumeForSidecar()
			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{signalControlledSidecar()}
			aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, vol)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Failing the sidecar on pod-0 and pod-1 (2 of 3 pods become sidecar-failed)")

			for _, suffix := range []string{"-0-0", "-0-1"} {
				Expect(failPodSidecar(namespace, clusterName+suffix)).ToNot(HaveOccurred())
			}

			Eventually(func(g Gomega) {
				cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
				g.Expect(clusterErr).ToNot(HaveOccurred())

				pl, podListErr := getClusterPodList(k8sClient, ctx, cluster)
				g.Expect(podListErr).ToNot(HaveOccurred())

				crashingCount := 0

				for idx := range pl.Items {
					if podHasCrashingSidecar(&pl.Items[idx]) {
						crashingCount++
					}
				}

				g.Expect(crashingCount).To(Equal(2), "expected exactly 2 pods with crashing sidecars")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("Setting IgnoreSidecarFailure=true and triggering a proto-fd-max rolling restart")

			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = int64(20000)
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying proto-fd-max is updated on ALL 3 pods — including the 2 sidecar-failed ones")

			Expect(validateAerospikeConfigServiceClusterUpdate(
				logger, k8sClient, ctx, clusterNamespacedName, []string{"proto-fd-max"},
			)).ToNot(HaveOccurred())

			By("Verifying the SC roster includes all 3 nodes — sidecar-failed pods never excluded")

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
		})

		It("Should block rolling restart across all racks when IgnoreSidecarFailure is false, then"+
			" unblock when set to true", func() {
			// AKO reconciles racks sequentially. When handleFailedPodsInRack
			// returns a requeue for a sidecar-failed pod in any rack, the
			// reconcileRacks loop exits early — subsequent racks in the same
			// reconcile cycle are not processed. A sidecar failure in rack-1
			// therefore blocks rack-2's rolling restart even though rack-2 has
			// no sidecar failures. Setting IgnoreSidecarFailure=true removes the
			// early-exit so both racks proceed to completion in the same cycle.
			By("Deploying a healthy 4-node, 2-rack cluster (2 pods per rack)")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
			aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
				Racks: []asdbv1.Rack{{ID: 1}, {ID: 2}},
			}
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Adding a crashing sidecar to all pods with IgnoreSidecarFailure=false and a proto-fd-max change")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(false)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = int64(20000)
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying cluster transitions to Error — sidecar failure with IgnoreSidecarFailure=false" +
				" blocks rolling restart and sets Error phase")
			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterError)).ToNot(HaveOccurred())

			By("Verifying the entire cluster stalls — exactly 1 pod has crashing sidecar, 3 pods still fully ready")

			// Pod-0 in rack-1 gets the new spec first and enters CrashLoopBackOff.
			// With IgnoreSidecarFailure=false the reconciler blocks there; the
			// remaining 3 pods (including all of rack-2) are never restarted.
			assertRollingRestartBlocked(k8sClient, ctx, clusterNamespacedName, int(aeroCluster.Spec.Size)-1)

			By("Setting IgnoreSidecarFailure=true — both racks should complete their rolling restarts")

			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying proto-fd-max is updated on all 4 pods in both racks")

			Expect(validateAerospikeConfigServiceClusterUpdate(
				logger, k8sClient, ctx, clusterNamespacedName, []string{"proto-fd-max"},
			)).ToNot(HaveOccurred())

			By("Verifying all pods have running servers and crashing sidecars after the update")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 4)
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
			By("Deploying a 2-node SC cluster with all sidecars healthy")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{signalControlledSidecar()}
			aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, signalVolumeForSidecar())

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

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

			podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
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

			maxIgnorable := intstr.FromInt(1)
			aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorable
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(false)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["proto-fd-max"] = int64(20000)

			// Submit without waiting — the rolling restart should stall because
			// IgnoreSidecarFailure=false blocks when pod-0's sidecar is not ready.
			Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Verifying rolling restart is stuck in InProgress — sidecar failure blocks readiness wait")

			Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
				asdbv1.AerospikeClusterInProgress)).ToNot(HaveOccurred())

			By("Setting IgnoreSidecarFailure=true and verifying rolling restart completes")

			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

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

		// When a sidecar fails for an external reason (no AerospikeCluster spec
		// change), the operator runs the sidecar recovery path with podFailure !=
		// nil. Because no pod-level operation was triggered (podOpPerformed=false),
		// checkPodsFailedAfterRackOp is skipped and scale-up proceeds normally.
		// The signal-controlled sidecar is used so the failure is injected on
		// demand via exec — the cluster spec never changes after deployment.
		It("Should not block pure scale-up when a pod's sidecar fails from an external trigger",
			func() {
				By("Deploying a 2-node cluster with a signal-controlled sidecar (initially healthy)")

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				vol := signalVolumeForSidecar()
				aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{signalControlledSidecar()}
				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, vol)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Externally triggering sidecar failure on pod-0 — no AerospikeCluster spec change")
				Expect(failPodSidecar(namespace, clusterName+"-0-0")).ToNot(HaveOccurred())

				scaleUpPodName := clusterName + "-0-2"

				By("Applying a pure scale-up (size=3, no other spec changes)")

				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Size = 3
				Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Verifying the scale-up pod is created despite pod-0's externally-triggered sidecar failure")
				Eventually(func(g Gomega) {
					cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(clusterErr).ToNot(HaveOccurred())

					podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
					g.Expect(podListErr).ToNot(HaveOccurred())

					names := make([]string, 0, len(podList.Items))
					for idx := range podList.Items {
						names = append(names, podList.Items[idx].Name)
					}

					g.Expect(names).To(ContainElement(scaleUpPodName),
						"scale-up pod %s must be created even though pod-0 has an externally-triggered sidecar failure",
						scaleUpPodName)
				}, 5*time.Minute, 10*time.Second).Should(Succeed())
			})

		// When podFailure != nil (sidecar crashing) and a pod-level operation runs
		// (upgradeRack sets podOpPerformed=true), checkPodsFailedAfterRackOp fires
		// after the rolling restart and returns non-success — deferring scale-up
		// for one reconcile cycle. The signal-controlled sidecar is used here
		// because its failure is emptyDir-persisted: /signal/fail survives pod
		// restarts, keeping the sidecar in stable CrashLoopBackOff across the
		// entire rolling restart. exit 1 / exit 2 patterns would not be reliable
		// because State.Waiting toggles with Running between backoff intervals,
		// and the check might fire during a Running window where the sidecar
		// appears healthy.
		// Pod 2 (scale-up pod) gets a fresh emptyDir, so its sidecar is healthy —
		// once IgnoreSidecarFailure=true is applied the cluster reaches Completed.
		It("Should defer scale-up by one reconcile cycle when an image upgrade and sidecar failure coincide",
			func() {
				By("Deploying a 2-node cluster with a signal-controlled sidecar (initially healthy)")

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{{
					Name:    "signal-sidecar",
					Image:   "busybox:1.28",
					Command: []string{"sleep", "3600"},
				}}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				scaleUpPodName := clusterName + "-0-2"

				By("Applying sidecar failure and scale-up (size=3) simultaneously")

				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{
					{
						Name:    "signal-sidecar", // same container name
						Image:   "busybox:1.28",
						Command: []string{"sh", "-c", "exit 1"},
					},
				}

				aeroCluster.Spec.Size = 3
				Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

				// upgradeRack runs (podOpPerformed=true). After the rolling restart
				// completes, checkPodsFailedAfterRackOp detects the stable
				// CrashLoopBackOff on pods 0 and 1 and returns non-success — scale-up
				// is deferred. The next reconcile finds no pending upgrade, so
				// podOpPerformed=false and scaleUpRack runs.
				By("Confirming scale-up pod is absent while rolling restart is in progress")
				Consistently(func(g Gomega) {
					cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(clusterErr).ToNot(HaveOccurred())

					podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
					g.Expect(podListErr).ToNot(HaveOccurred())

					for idx := range podList.Items {
						g.Expect(podList.Items[idx].Name).NotTo(Equal(scaleUpPodName),
							"scale-up pod must not appear while the upgrade rolling restart is running")
					}
				}, 30*time.Second, 5*time.Second).Should(Succeed())

				By("Verifying cluster transitions to Error — crashing sidecar " +
					"(IgnoreSidecarFailure unset) triggers Error phase")
				Expect(waitForClusterPhase(k8sClient, ctx, clusterNamespacedName,
					asdbv1.AerospikeClusterError)).ToNot(HaveOccurred())

				By("Applying IgnoreSidecarFailure=true to let the cluster reach Completed")

				aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)
				Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Verifying the cluster reaches Completed with 3 pods on nextImage")
				Eventually(func(g Gomega) {
					cluster, clusterErr := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(clusterErr).ToNot(HaveOccurred())
					g.Expect(cluster.Status.Phase).To(Equal(asdbv1.AerospikeClusterCompleted),
						"cluster must reach Completed when IgnoreSidecarFailure bypasses the sidecar check")

					podList, podListErr := getClusterPodList(k8sClient, ctx, cluster)
					g.Expect(podListErr).ToNot(HaveOccurred())
					g.Expect(podList.Items).To(HaveLen(3),
						"expected 3 pods after scale-up completes and sidecar failure is ignored")
				}, getTimeout(3), 15*time.Second).Should(Succeed())
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

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
			aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(true)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			// Phase is not checked because perpetual CrashLoopBackOff events can
			// briefly push the cluster back to InProgress between reconcile cycles.
			By("Verifying initial 4-node state — all servers running, all sidecars crashing")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 4)

			// ── Scale-down ──────────────────────────────────────────────────
			By("Scaling down from 4 to 3 nodes")

			Expect(scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 1)).ToNot(HaveOccurred())

			By("Verifying 3-node state after scale-down — all servers running, all sidecars crashing")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 3)

			// ── Scale-up ────────────────────────────────────────────────────
			By("Scaling up from 3 to 5 nodes")

			Expect(scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)).ToNot(HaveOccurred())

			By("Verifying 5-node state after scale-up — all servers running, all sidecars crashing")
			verifyCrashingSidecarOnAllPods(k8sClient, ctx, clusterNamespacedName, 5)

			// ── Image upgrade ────────────────────────────────────────────────
			By("Triggering an image upgrade to nextImage")

			aeroCluster.Spec.Image = nextImage
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})
	})

	// A fresh cluster deployed with a permanently crashing sidecar must never
	// reach Completed regardless of the IgnoreSidecarFailure flag value. The STS
	// keeps recycling pods but the cluster cannot converge because the pods are
	// never fully ready.
	Context("Fresh cluster deploy with a crashing sidecar", func() {
		clusterName := fmt.Sprintf("sidecar-fresh-%d", GinkgoParallelProcess())
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

		for _, ignoreFlag := range []bool{true, false} {
			It(fmt.Sprintf("Should never reach Completed when IgnoreSidecarFailure=%v", ignoreFlag), func() {
				By(fmt.Sprintf("Creating a fresh cluster with a crashing sidecar and IgnoreSidecarFailure=%v", ignoreFlag))

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{crashingSidecar()}
				aeroCluster.Spec.IgnoreSidecarFailure = ptr.To(ignoreFlag)

				// Use Create directly — DeployCluster would block waiting for
				// Completed, which this cluster must never reach.
				Expect(k8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Verifying the cluster never reaches Completed — pods stay not-fully-ready due to crashing sidecar")

				Consistently(func(g Gomega) {
					cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.Phase).NotTo(Equal(asdbv1.AerospikeClusterCompleted),
						"cluster must not reach Completed while sidecar is permanently crashing")
				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			})
		}
	})
})

// assertRollingRestartBlocked polls until exactly 1 pod has a crashing sidecar
// (the first pod restarted) and expectedReadyCount pods are still fully ready
// (not yet restarted). It then holds that condition consistently to prove the
// rolling restart is genuinely blocked rather than transiently paused between
// pod restarts. The container transitions through ContainerCreating and
// Terminated/Error before Kubernetes applies exponential backoff and sets the
// reason to "CrashLoopBackOff", which is why an Eventually precedes the
// Consistently.
func assertRollingRestartBlocked(
	k8sClient client.Client,
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	expectedReadyCount int,
) {
	GinkgoHelper()

	check := func(g Gomega) {
		cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		g.Expect(err).ToNot(HaveOccurred())

		podList, err := getClusterPodList(k8sClient, ctx, cluster)
		g.Expect(err).ToNot(HaveOccurred())

		crashingCount, readyCount := 0, 0

		for idx := range podList.Items {
			pod := &podList.Items[idx]
			if podHasCrashingSidecar(pod) {
				crashingCount++
				continue
			}

			if utils.IsPodRunningAndReady(pod) {
				readyCount++
			}
		}

		g.Expect(crashingCount).To(Equal(1),
			"expected exactly 1 pod with a crashing sidecar; rolling restart should be blocked after the first pod")
		g.Expect(readyCount).To(Equal(expectedReadyCount),
			"expected %d pods to still be fully ready (not yet restarted)", expectedReadyCount)
	}

	Eventually(check, 5*time.Minute, 5*time.Second).Should(Succeed())
	Consistently(check, 30*time.Second, 5*time.Second).Should(Succeed())
}

// verifyCrashingSidecarOnAllPods polls until every pod in the cluster has its
// sidecar in CrashLoopBackOff and the Aerospike server is reachable via a live
// info call. expectedSize is asserted against the total pod count.
func verifyCrashingSidecarOnAllPods(
	k8sClient client.Client,
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	expectedSize int,
) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		cluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		g.Expect(err).ToNot(HaveOccurred())

		podList, err := getClusterPodList(k8sClient, ctx, cluster)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(podList.Items).To(HaveLen(expectedSize))

		for idx := range podList.Items {
			pod := &podList.Items[idx]
			g.Expect(podHasCrashingSidecar(pod)).To(BeTrue(),
				"expected sidecar to be crashing on pod %s", pod.Name)
			_, err := requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "node", pod.Name)
			g.Expect(err).ToNot(HaveOccurred(),
				"expected Aerospike server to be reachable on pod %s", pod.Name)
		}
	}, 3*time.Minute, 5*time.Second).Should(Succeed())
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
