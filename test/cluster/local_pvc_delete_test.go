package cluster

import (
	goctx "context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

		Context("When doing valid operations", func() {
			Context("When doing rolling restart", func() {
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set migrate-fill-delay dynamically",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(false)
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
					"at the rack level and set migrate-fill-delay dynamically", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 3,
					)
					rackConfig := asdbv1.RackConfig{
						Racks: getDummyRackConf(1, 2),
					}
					storage := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
					storage.DeleteLocalStorageOnRestart = ptr.To(true)
					storage.LocalStorageClasses = []string{storageClass}
					rackConfig.Racks[0].InputStorage = storage
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

				// active rolling restart with deleteLocalStorageOnRestart=false — PVCs must stay bound.
				It("Should preserve all local PVC UIDs when deleteLocalStorageOnRestart is false during rolling restart",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(false)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
							Labels: map[string]string{"test-label": "restart-no-pvc-delete"},
						}
						Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						for podName, pvcMap := range oldPvcInfoPerPod {
							Expect(validatePVCDeletion(ctx, pvcMap, false)).ToNot(HaveOccurred(),
								"PVCs for pod %s should be unchanged", podName)
						}
					})
			})

			Context("When doing upgrade", func() {
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set migrate-fill-delay dynamically",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
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
					"at the rack level and set migrate-fill-delay dynamically", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 3,
					)
					rackConfig := asdbv1.RackConfig{
						Racks: getDummyRackConf(1, 2),
					}
					storage := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
					storage.DeleteLocalStorageOnRestart = ptr.To(true)
					storage.LocalStorageClasses = []string{storageClass}
					rackConfig.Racks[0].InputStorage = storage
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

		Context("When a pod is failed (failure recovery path)", func() {
			// Test case: during failed-pod recovery, keep local PVCs and verify data survives.
			// Setup is size=2, RF=1 with deleteLocalStorageOnPodRecovery=false: the failed pod must retain PVCs.
			// We set maxIgnorablePods=1 so rollout can proceed while one failed pod is tolerated.
			// We intentionally do not enable deleteLocalStorageOnRestart here; with RF=1, deleting healthy pod PVCs
			// during rollout can remove the only replica and make the data-read assertion meaningless.
			// maxIgnorablePods and pod-label rollout are applied in separate updates for stable spec/status convergence.
			It("Should NOT delete local PVCs for a failed pod during rolling restart"+
				"when deleteLocalStorageOnPodRecovery is false and keep Aerospike data (RF=1)",
				func() {
					aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 3, 1)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(false)
					aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					Expect(WriteDataToCluster(aeroCluster, k8sClient, []string{"test"})).ToNot(HaveOccurred())

					failedPodName := clusterName + "-0-0"

					By("Marking pod as failed")
					Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

					By("Setting maxIgnorablePods so the failed pod is not skipped from rollout (separate update for spec/status sync)")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					maxIgnorable := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorable
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{"test-label": "failure-recovery"},
					}
					Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					By("Validating no local PVC UIDs change (failed pod: recovery path; healthy pod: no deleteLocalStorageOnRestart)")

					allPods := make([]string, 0, len(oldPvcInfoPerPod))
					for n := range oldPvcInfoPerPod {
						allPods = append(allPods, n)
					}

					validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, allPods...)

					By("Validating written bin remains readable after recovery")
					Eventually(func() bool {
						ac, gerr := getCluster(k8sClient, ctx, clusterNamespacedName)
						if gerr != nil {
							return false
						}

						data, derr := CheckDataInCluster(ac, k8sClient, []string{"test"})
						if derr != nil || data == nil {
							return false
						}

						return data["test"]
					}).WithTimeout(5*time.Minute).WithPolling(retryInterval).Should(BeTrue(),
						"written bin should remain readable after failed-pod recovery with deleteLocalStorageOnPodRecovery=false")
				})

			It("Should delete local PVCs for a failed pod during rolling restart when "+
				"deleteLocalStorageOnPodRecovery is set", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"

				By("Marking pod as failed")
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				By("Capturing failed-pod local PVC UIDs before rolling restart")

				oldPVC, err := getPVCClaimUIDsForPod(ctx, k8sClient, failedPodName, namespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(oldPVC).ToNot(BeEmpty())

				By("Triggering rolling restart via pod metadata update")

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
					Labels: map[string]string{"test-label": "failure-recovery-delete"},
				}
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating all pods PVCs are deleted (including failed pod)")
				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)

				By("Asserting failed pod PVCs were explicitly replaced")
				// Redundant with validateClusterPVCDeletion above,
				// but keeps failed-pod intent i.e validates failed pod PVC was explicitly replaced.
				Expect(validatePVCDeletion(ctx, oldPVC, true)).To(Succeed())
			})

			It("Should NOT delete local PVCs for a failed pod during image upgrade even if "+
				"deleteLocalStorageOnRestart is set", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"

				By("Marking pod as failed")
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				By("Triggering image upgrade")
				Expect(UpdateClusterImage(aeroCluster, nextImage)).ToNot(HaveOccurred())
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating failed pod PVC is preserved; active pods PVCs are deleted")
				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, failedPodName)
			})

			It("Should delete local PVCs for a failed pod during upgrade when "+
				"deleteLocalStorageOnPodRecovery is set", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"

				By("Marking pod as failed")
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				By("Triggering image upgrade")
				Expect(UpdateClusterImage(aeroCluster, nextImage)).ToNot(HaveOccurred())
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating all pods PVCs are deleted (including failed pod)")
				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
			})

			It("Should NOT delete local PVCs for a failed pod when its k8s node is blocked", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"

				By("Recording original pod UID and node name")

				originalPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: failedPodName, Namespace: namespace},
					originalPod)).ToNot(HaveOccurred())

				originalPodUID := string(originalPod.UID)
				blockedNode := originalPod.Spec.NodeName

				By("Marking pod as failed")
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				By("Adding the pod's node to k8sNodeBlockList (non-blocking)")

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.K8sNodeBlockList = []string{blockedNode}
				Expect(k8sClient.Update(ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Waiting for AKO to delete the failed pod (failure recovery — no PVC deletion)")
				// AKO treats the failed pod with isFailureRecovery=true → PVC preserved → pod deleted →
				// STS recreates pod with new anti-affinity, but local PVC pins it to blocked node → Pending.
				Eventually(func() bool {
					pod := &corev1.Pod{}
					err = k8sClient.Get(ctx, types.NamespacedName{Name: failedPodName, Namespace: namespace}, pod)

					return err == nil && string(pod.UID) != originalPodUID
				}, 3*time.Minute, retryInterval).Should(BeTrue(), "pod should have been recreated by AKO")

				By("Asserting failed pod PVC is preserved (bypassed k8sNodeBlockList PVC deletion)")
				Expect(validatePVCDeletion(ctx, oldPvcInfoPerPod[failedPodName], false)).ToNot(HaveOccurred())
			})

			It("Should delete local PVCs for a failed pod when its k8s node is blocked and "+
				"deleteLocalStorageOnPodRecovery is set from the start", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"

				By("Recording the pod's node name")

				pod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: failedPodName, Namespace: namespace}, pod)).
					ToNot(HaveOccurred())
				blockedNode := pod.Spec.NodeName

				By("Marking pod as failed")
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				By("Adding the pod's node to k8sNodeBlockList")

				aeroCluster.Spec.K8sNodeBlockList = []string{blockedNode}
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Asserting failed pod PVC was deleted (deleteLocalStorageOnPodRecovery=true)")
				Expect(validatePVCDeletion(ctx, oldPvcInfoPerPod[failedPodName], true)).ToNot(HaveOccurred())
			})

			// Cluster-level DeleteLocalStorageOnPodRecovery stays false. Rack-1 InputStorage enables it for that rack
			// and sets LocalStorageClasses to this suite's storageClass so only PVCs backed by that class are treated
			// as local and replaced after failure recovery. Rack-2 has no InputStorage override, so it keeps the global
			// false and failed-pod PVCs must survive (validateClusterPVCDeletion skips rack-2's pod).
			It("Should apply rack InputStorage deleteLocalStorageOnPodRecovery per rack when global is false",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(false)
					aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}

					rackConfig := asdbv1.RackConfig{
						Racks:      getDummyRackConf(1, 2),
						Namespaces: []string{"test"},
					}
					rack1St := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
					rack1St.DeleteLocalStorageOnPodRecovery = ptr.To(true)
					rack1St.LocalStorageClasses = []string{storageClass}
					rackConfig.Racks[0].InputStorage = rack1St
					aeroCluster.Spec.RackConfig = rackConfig

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					Expect(markPodAsFailed(ctx, k8sClient, clusterName+"-1-0", namespace)).ToNot(HaveOccurred())
					Expect(markPodAsFailed(ctx, k8sClient, clusterName+"-2-0", namespace)).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{"test-label": "rack-override-delete-local-pvc"},
					}
					Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					// Rack-1 InputStorage is a full DeepCopy of cluster storage (only recovery + local classes tweaked), so
					// deleteLocalStorageOnRestart stays true for rack-1: healthy pods use the planned-restart path and
					// still replace local PVCs. Only rack-2's failed pod is skipped below (failure recovery, no per-rack
					// deleteLocalStorageOnPodRecovery).
					validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, clusterName+"-2-0")

					By("Asserting healthy rack-1 pod still replaces PVCs on rollout")
					Expect(validatePVCDeletion(ctx, oldPvcInfoPerPod[clusterName+"-1-1"], true)).ToNot(HaveOccurred())
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
		pvcMap, err := extractPodPVC(ctx, k8sClient, &podList.Items[idx])
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

	By("Validating the migrate-fill-delay is set to given value before the restart")

	// Using last rack's pod for the confirmation as first rack pods are restarted first
	lastPodName := aeroCluster.Name + "-" +
		strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[len(aeroCluster.Spec.RackConfig.Racks)-1].ID) + "-0"

	err := validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval, lastPodName)
	Expect(err).ToNot(HaveOccurred())

	By("Wait for the operator to start the pod restart process")

	err = waitForOperatorToStartPodRestart(ctx, k8sClient, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to 0 after the restart (pod is running)")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, 0,
		&shortRetryInterval, lastPodName)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to given value before the restart of next pod")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval, lastPodName)
	Expect(err).ToNot(HaveOccurred())

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)
	Expect(err).ToNot(HaveOccurred())

	By("Validating the migrate-fill-delay is set to given value after the operation is completed")

	err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, expectedMigFillDelay,
		&shortRetryInterval, lastPodName)
	Expect(err).ToNot(HaveOccurred())
}
