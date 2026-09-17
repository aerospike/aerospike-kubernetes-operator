package cluster

import (
	goctx "context"
	"fmt"
	"time"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	operatorUtils "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
	"github.com/aerospike/aerospike-management-lib/deployment"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	ckptNsName         = "ckpt"
	ckptPath           = "/mnt/index-ckpt"
	testNsName         = "test"
	ckptIndexMountPath = "/test/dev/xvdf-index"
	ckptNumKeys        = 100
	ckptAlternatePath  = "/mnt/index-ckpt-alternate"
)

var _ = Describe(
	"IndexCheckpoint", func() {
		ctx := goctx.TODO()
		ckptClusterName := fmt.Sprintf("index-checkpoint-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(ckptClusterName, namespace)
		testLabels := map[string]string{"test-key": "test-value"}

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ckptClusterName,
					Namespace: namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, namespace, ckptClusterName)).ToNot(HaveOccurred())
		})

		It("Covers the checkpoint lifecycle across restart types", func() {
			By("Deploying the cluster with index-checkpoint enabled")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Writing records to the in-memory namespace")
			Expect(LoadBulkDataInCluster(aeroCluster, k8sClient, ckptNsName, ckptNumKeys)).ToNot(HaveOccurred())

			By("PHASE 1: warm (quick) restart — data and preview features survive in place")
			// The pod survives, so the shared-memory segments survive and no checkpoint
			// is needed. This confirm the --preview params survives a wam restart.
			svc := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
			svc["indent-allocations"] = true

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			expectRecords(ctx, clusterNamespacedName, ckptNsName,
				"record should survive a warm restart on the live shared-memory segments")

			By("PHASE 2: pod-restart rolling update — data survives via the checkpoint")
			// A pod restart destroys the pod sandbox and with it the shared-memory
			// segments, so the checkpoint is the only way this data comes back:
			// save -> park -> delete -> hydrate.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels
			// BOTH namespaces are checkpointed, for different reasons: the in-memory one
			// copies index + data stripes, the device-backed one copies its index only (its
			// data is already durable, and skipping the device scan is the point there).
			applyAndExpectCheckpoint(ctx, aeroCluster, []string{ckptNsName, testNsName})

			expectRecords(ctx, clusterNamespacedName, ckptNsName,
				"record should survive the pod restart via the index checkpoint")

			By("PHASE 3: data-size change — the checkpoint is skipped for that namespace only")
			// A checkpoint written under the old stripe geometry would be rejected by the
			// replacement pod, so the operator deliberately skips taking one.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			nsConf := getClusterNamespaceConfig(aeroCluster, ckptNsName)
			nsConf[asdbv1.ConfKeyStorageEngine].(map[string]interface{})[asdbv1.ConfKeyDataSize] = 536870912

			// Check only in-mem. Skip the device backed assertion again
			applyAndExpectCheckpoint(ctx, aeroCluster, []string{testNsName})

			By("PHASE 4: skip-checkpoint — the opted-out namespace is excluded from the save")
			// Opting out is a config-only change (a quick restart, which the live segments
			// survive — asserted below).
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(LoadBulkDataInCluster(aeroCluster, k8sClient, ckptNsName, ckptNumKeys)).ToNot(HaveOccurred())

			nsConf = getClusterNamespaceConfig(aeroCluster, ckptNsName)
			nsConf[asdbv1.ConfKeySkipCheckpoint] = true

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			expectRecords(ctx, clusterNamespacedName, ckptNsName,
				"record should survive the config-only restart that applies skip-checkpoint")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels

			applyAndExpectCheckpoint(ctx, aeroCluster, []string{testNsName})

			By("PHASE 5: index-checkpoint-path change — no checkpoint is taken at all")
			// The path is cluster-wide, so moving it invalidates EVERY namespace's
			// checkpoint at once.
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			svc = aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
			svc[asdbv1.ConfKeyServiceIndexCheckpointPath] = ckptAlternatePath

			// Plant a marker so the sweep has something to remove. In a normal hydrate flow, server deletes the
			// stored shmem files, there won't be anything to confirm AKO driven clean due to index checkpoint path change
			plantCheckpointMarkers(aeroCluster, ckptPath)

			applyAndExpectNoCheckpoint(ctx, aeroCluster,
				"a checkpoint-path change must not park any Pod: every checkpoint would land "+
					"at the abandoned path, so none is worth taking")

			// Only AKO can reclaim an abandoned path — the server only ever sweeps beneath its current one
			expectCheckpointMarkersSwept(aeroCluster, ckptPath)

			// Scale-down should not trigger checkpointing flow
			By("PHASE 6: scale-down — draining a node must not park it")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.Size--

			applyAndExpectNoCheckpoint(ctx, aeroCluster,
				"scale-down must not trigger an index checkpoint")
		})

		It("Should checkpoint during image upgrade", func() {
			if testutil.IndexCheckpointUpgradeImage == testutil.IndexCheckpointImage {
				Skip("set IndexCheckpointUpgradeImage to a second feature-carrying image to run this")
			}

			By("Deploying the cluster with index-checkpoint enabled")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			Expect(LoadBulkDataInCluster(aeroCluster, k8sClient, ckptNsName, ckptNumKeys)).ToNot(HaveOccurred())

			By("Upgrading the image — the checkpoint must carry the data across")

			aeroCluster.Spec.Image = testutil.IndexCheckpointUpgradeImage

			applyAndExpectCheckpoint(ctx, aeroCluster, []string{ckptNsName, testNsName})

			expectRecords(ctx, clusterNamespacedName, ckptNsName,
				"record should survive an image upgrade via the index checkpoint")
		})

		It("Should checkpoint during Rolling restart along with batch", func() {
			By("Deploying a 2-rack cluster with RollingUpdateBatchSize 2")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			aeroCluster.Spec.Size = 4
			aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
				Racks:                  []asdbv1.Rack{{ID: 1}, {ID: 2}},
				Namespaces:             []string{ckptNsName, testNsName},
				RollingUpdateBatchSize: percent("100%"),
			}

			getClusterNamespaceConfig(aeroCluster, ckptNsName)[asdbv1.ConfKeyReplicationFactor] = 2

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Forcing a pod restart — both pods of a rack must park together")

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels
			Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			Eventually(func() int {
				return len(findParkedPods(aeroCluster))
			}, 5*time.Minute, time.Second).Should(BeNumerically(">=", 2),
				"a batch of 2 must park together, not one pod at a time")

			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(aeroCluster.Spec.Size),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			Expect(findParkedPods(aeroCluster)).To(BeEmpty(),
				"every parked Pod in the batch must be reaped")
		})

		It("Should not checkpoint a failed Pod during recovery", func() {
			By("Deploying the cluster with index-checkpoint enabled")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Breaking one Pod so its server container cannot come up")

			podName := clusterNamespacedName.Name + "-0-0"
			Expect(markPodAsFailed(ctx, k8sClient, podName, clusterNamespacedName.Namespace)).ToNot(HaveOccurred())

			By("Recovering it — the trigger must be skipped, not attempted against a failed pod")

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels

			// The assertion is that recovery completes. A checkpoint-save against the failed pod fails at connect
			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		// Scenario:
		// When the changes that triggered the checkpoint-save are reverted even before the changes are reflect.
		// checkpoint-save is a point of no return: the node has left the cluster and shut its storage down, so a pod
		// has to complete the checkpoint cycle even though the changes are reverted.
		It("Should complete the checkpoint-save cycle even after the spec change that caused it is reverted", func() {
			By("Deploying the cluster with index-checkpoint enabled")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Forcing a pod restart and waiting until a Pod is parked")

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels
			Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			time.Sleep(1 * time.Second)

			By("Reverting the spec while the park is still held")

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = nil
			Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			Eventually(func() *corev1.Pod {
				return findParkedPod(aeroCluster)
			}, 5*time.Minute, time.Second).ShouldNot(BeNil(), "no Pod ever reported a park")

			By("The parked Pod must still be reaped and the cluster converge")
			Expect(waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(aeroCluster.Spec.Size),
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)).ToNot(HaveOccurred())

			Expect(findParkedPod(aeroCluster)).To(BeNil(),
				"a parked Pod outlived the rollout - the discharge phase did not reap it")
		})

		It("Should skip checkpoint for a namespace being removed", func() {
			By("Deploying the cluster with index-checkpoint enabled")

			aeroCluster := createIndexCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Dropping the in-memory namespace from the CR")

			nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]any)
			remaining := make([]any, 0, len(nsList))

			for _, nsIface := range nsList {
				if nsIface.(map[string]any)[asdbv1.ConfKeyName] != ckptNsName {
					remaining = append(remaining, nsIface)
				}
			}

			Expect(remaining).To(HaveLen(len(nsList)-1), "the in-memory namespace should have been dropped")
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = remaining

			applyAndExpectCheckpoint(ctx, aeroCluster, []string{testNsName})
		})

		// For a non-volatile like all-flash where everything on persistent storage, server smartly skips checkpointing
		// but parks. AKO should be able to handle such scenarios
		It("Should reap a node that parks with nothing to checkpoint", func() {
			By("Deploying an all-flash cluster with index-checkpoint enabled")

			aeroCluster := createAllFlashCheckpointCluster(clusterNamespacedName)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Forcing a pod restart - every node parks without writing a checkpoint")

			aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = testLabels

			// An EMPTY namespace list is the whole point: the node parks having copied
			// nothing, and checkpoint-status reports the node-global park state with no
			// per-namespace record at all.
			applyAndExpectCheckpoint(ctx, aeroCluster, nil)
		})
	},
)

// applyAndExpectCheckpoint applies an already-mutated spec without waiting, catches a pod
// while it is still holding its checkpoint park, asserts what the server says it saved, and
// only then waits for the rollout to finish.
func applyAndExpectCheckpoint(
	ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster, wantNamespaces []string,
) {
	GinkgoHelper()

	Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

	var (
		parked *corev1.Pod
		resp   deployment.CheckpointResponse
	)

	// Just check any one parked for verification
	Eventually(func() bool {
		parked = findParkedPod(aeroCluster)
		if parked == nil {
			return false
		}

		asConn, err := newAsConn(logger, aeroCluster, parked, k8sClient)
		if err != nil {
			return false
		}

		resp, err = asConn.CheckpointStatus(getClientPolicy(aeroCluster, k8sClient))

		return err == nil && resp.IsParked
	}, 5*time.Minute, 2*time.Second).Should(BeTrue(),
		"no Pod ever reported an index-checkpoint park that the server confirmed")

	got := sets.KeySet(resp.Namespaces).UnsortedList()
	Expect(got).To(ConsistOf(wantNamespaces),
		"checkpoint-status on parked Pod %s reported %v, expected exactly %v",
		parked.Name, got, wantNamespaces)

	Expect(waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
		[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)).ToNot(HaveOccurred())
}

// applyAndExpectNoCheckpoint applies an already-mutated spec and asserts that NO pod parks while the rollout runs.
func applyAndExpectNoCheckpoint(
	ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster, reason string,
) {
	GinkgoHelper()

	Expect(updateClusterWithNoWait(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

	Consistently(func() *corev1.Pod {
		return findParkedPod(aeroCluster)
	}, 2*time.Minute, time.Second).Should(BeNil(), reason)

	Expect(waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
		[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)).ToNot(HaveOccurred())
}

// ckptStaleMarker is a file planted in the checkpoint directory so the path-change sweep
// has something to remove. See PHASE 5 for why a real leftover cannot be relied on.
const ckptStaleMarker = "stale-marker"

// plantCheckpointMarkers creates the marker under dir on every pod, and verifies it
// landed — so a later "it is gone" assertion cannot pass because it was never there.
func plantCheckpointMarkers(aeroCluster *asdbv1.AerospikeCluster, dir string) {
	GinkgoHelper()

	podList, err := getPodList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())
	Expect(podList.Items).ToNot(BeEmpty(), "no Pods to plant a marker on")

	// Each Pod has its OWN checkpoint PVC (a persistentVolume source becomes a
	// volumeClaimTemplate), so the marker has to be planted on every one of them.
	for idx := range podList.Items {
		pod := &podList.Items[idx]
		marker := dir + "/" + ckptStaleMarker

		Expect(execInPodContainer(pod.Namespace, pod.Name, asdbv1.AerospikeServerContainerName,
			[]string{"sh", "-c", "touch " + marker + " && test -e " + marker})).ToNot(HaveOccurred(),
			"could not plant the marker on Pod %s", pod.Name)
	}
}

// expectCheckpointMarkersSwept asserts the marker is gone from dir on every pod.
// execInPodContainer returns an error on a non-zero exit, so the shell's own test does the asserting.
func expectCheckpointMarkersSwept(aeroCluster *asdbv1.AerospikeCluster, dir string) {
	GinkgoHelper()

	podList, err := getPodList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())
	Expect(podList.Items).ToNot(BeEmpty(), "no Pods to check the marker on")

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		Expect(execInPodContainer(pod.Namespace, pod.Name, asdbv1.AerospikeServerContainerName,
			[]string{"sh", "-c", "! test -e " + dir + "/" + ckptStaleMarker})).ToNot(HaveOccurred(),
			"the abandoned checkpoint path was not swept on Pod %s", pod.Name)
	}
}

// findParkedPods returns every pod currently holding a checkpoint park.
func findParkedPods(aeroCluster *asdbv1.AerospikeCluster) []*corev1.Pod {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil
	}

	parked := make([]*corev1.Pod, 0, len(podList.Items))

	for idx := range podList.Items {
		if operatorUtils.IsPodCheckpointing(&podList.Items[idx]) {
			parked = append(parked, &podList.Items[idx])
		}
	}

	return parked
}

// findParkedPod returns a pod currently holding a checkpoint park, or nil.
// It reuses the operator's own predicate rather than re-deriving the annotation comparison, so the two cannot drift.
func findParkedPod(aeroCluster *asdbv1.AerospikeCluster) *corev1.Pod {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil
	}

	for idx := range podList.Items {
		if operatorUtils.IsPodCheckpointing(&podList.Items[idx]) {
			return &podList.Items[idx]
		}
	}

	return nil
}

// expectRecords asserts that the bulk data set either survived intact or is entirely gone.
//
//nolint:unparam // for future use
func expectRecords(ctx goctx.Context, clusterNamespacedName types.NamespacedName, nsName, explain string) {
	GinkgoHelper()

	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	found, err := CheckBulkDataInCluster(aeroCluster, k8sClient, nsName, ckptNumKeys)
	Expect(err).ToNot(HaveOccurred())

	Expect(found).To(Equal(ckptNumKeys), "%s (found %d of %d records in %q)",
		explain, found, ckptNumKeys, nsName)
}

// createIndexCheckpointCluster builds a 2-node cluster with the index-checkpoint
// feature enabled and a pure in-memory namespace at RF=1 alongside the device backed namespace.
func createIndexCheckpointCluster(clusterNamespacedName types.NamespacedName) *asdbv1.AerospikeCluster {
	aeroCluster := CreateAerospikeClusterPost640(
		clusterNamespacedName, 2, testutil.IndexCheckpointImage,
	)

	nsConf := getNonSCInMemoryNamespaceConfig(ckptNsName)
	nsConf[asdbv1.ConfKeyReplicationFactor] = 1

	nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
	aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = append(nsList, nsConf)

	svc := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
	svc[asdbv1.ConfKeyServiceIndexCheckpointPath] = ckptPath

	aeroCluster.Spec.PreviewFeatures = []string{asdbv1.PreviewFeatureIndexCheckpoint}

	aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes,
		checkpointVolume("index-ckpt", ckptPath),
		// Mounted but unused until the path-change phase moves the config onto it.
		checkpointVolume("index-ckpt-alt", ckptAlternatePath),
	)

	setCheckpointImagePullSecret(aeroCluster) // TODO(index-checkpoint): remove, see above

	return aeroCluster
}

// createAllFlashCheckpointCluster builds a 2-node cluster with PI, SI and data on flash
func createAllFlashCheckpointCluster(clusterNamespacedName types.NamespacedName) *asdbv1.AerospikeCluster {
	aeroCluster := createAllFlashCluster(clusterNamespacedName, 2)
	aeroCluster.Spec.Image = testutil.IndexCheckpointImage

	svc := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]any)
	svc[asdbv1.ConfKeyServiceIndexCheckpointPath] = ckptPath

	aeroCluster.Spec.PreviewFeatures = []string{asdbv1.PreviewFeatureIndexCheckpoint}

	aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes,
		checkpointVolume("index-ckpt", ckptPath),
		// Filesystem, not Block: an index mount is a directory the server writes into.
		asdbv1.VolumeSpec{
			Name: "index-mount",
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					// Must comfortably exceed the PI + SI budgets combined (4 GiB + 1 GiB),
					// since both index mounts point here.
					Size:         resource.MustParse("4Gi"),
					StorageClass: storageClass,
					VolumeMode:   corev1.PersistentVolumeFilesystem,
				},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{Path: ckptIndexMountPath},
		},
	)

	setCheckpointImagePullSecret(aeroCluster) // TODO(index-checkpoint): remove, see above

	return aeroCluster
}

// TODO(index-checkpoint): REMOVE once the index-checkpoint server image is published
// publicly. The feature is still on an unmerged server branch, so its image lives in a
// private dev registry and every Pod needs a pull secret to start. When the image ships
// publicly, delete ckptImagePullSecret, this function, and its two call sites in the
// cluster builders — nothing else in the suite needs a pull secret.
const ckptImagePullSecret = "regcred"

func setCheckpointImagePullSecret(aeroCluster *asdbv1.AerospikeCluster) {
	aeroCluster.Spec.PodSpec.ImagePullSecrets = []corev1.LocalObjectReference{
		{Name: ckptImagePullSecret},
	}
}

// checkpointVolume is the durable per-pod volume backing index-checkpoint-path.
func checkpointVolume(name, path string) asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: name,
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("2Gi"),
				StorageClass: storageClass,
				VolumeMode:   corev1.PersistentVolumeFilesystem,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{Path: path},
	}
}
