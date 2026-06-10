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
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set MFD dynamically",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)
						aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
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
					"at the rack level and set MFD dynamically", func() {
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
				It("Should delete the local PVCs when deleteLocalStorageOnRestart is set and set MFD dynamically",
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
					"at the rack level and set MFD dynamically", func() {
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

		Context("Scale-down local PVC policy", func() {
			DescribeTable("PVC outcome follows cascadeDelete, not deleteLocalStorageOnPodRecovery",
				func(cascadeDelete bool, expectPVCsDeleted bool) {
					aeroCluster := createNonSCDummyAerospikeCluster(clusterNamespacedName, 4)
					st := aeroCluster.Spec.Storage

					if cascadeDelete {
						st.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
						st.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
					} else {
						st.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteFalse
						st.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteFalse
					}

					st.DeleteLocalStorageOnPodRecovery = ptr.To(true)
					st.LocalStorageClasses = []string{storageClass}
					aeroCluster.Spec.Storage = st

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					failedPodName := clusterName + "-0-3"
					pvcBefore, err := pvcClaimUIDsForPod(ctx, k8sClient, failedPodName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(pvcBefore).ToNot(BeEmpty())

					Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					maxIgnorable := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorable
					aeroCluster.Spec.Size--

					Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					if expectPVCsDeleted {
						Expect(pvcClaimsEventuallyNotFound(ctx, k8sClient, pvcBefore, namespace)).To(Succeed())
					} else {
						Expect(pvcClaimsKeepStableUIDs(ctx, k8sClient, pvcBefore, namespace)).To(Succeed())
					}
				},
				Entry("deletes removed pod PVCs on scale-down when cascadeDelete=true even if "+
					"deleteLocalStorageOnPodRecovery=true",
					true, true),
				Entry(
					"preserves removed pod PVCs on scale-down when cascadeDelete=false even if "+
						"deleteLocalStorageOnPodRecovery=true",
					false, false),
			)
		})

		Context("When a pod is failed (failure recovery path)", func() {
			It("Should NOT delete local PVCs for a failed pod during rolling restart even if "+
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

				By("Triggering rolling restart via pod metadata update")

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
					Labels: map[string]string{"test-label": "failure-recovery"},
				}
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating failed pod PVC is preserved; active pods PVCs are deleted")
				// Failed pod PVC must survive (isFailureRecovery=true, no deleteLocalStorageOnPodRecovery).
				// All other pods are active restarts: their PVCs should be replaced.
				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, failedPodName)
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

				By("Triggering rolling restart via pod metadata update")

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
					Labels: map[string]string{"test-label": "failure-recovery-delete"},
				}
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating all pods PVCs are deleted (including failed pod)")
				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
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

			// deleteLocalPVCs tolerates NotFound when PVC was already removed manually.
			It("Should complete failed-pod rolling restart when a matching local PVC was deleted before recovery",
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
					aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					failedPodName := clusterName + "-0-0"
					oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					var oneClaim string

					for claim := range oldPvcInfoPerPod[failedPodName] {
						oneClaim = claim

						break
					}

					Expect(oneClaim).ToNot(BeEmpty())

					pvc := &corev1.PersistentVolumeClaim{}
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: oneClaim, Namespace: namespace}, pvc)).
						ToNot(HaveOccurred())
					Expect(k8sClient.Delete(ctx, pvc)).ToNot(HaveOccurred())

					Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{"test-label": "int-fph-033-notfound"},
					}
					Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				})

			// effective rack InputStorage deleteLocalStorageOnPodRecovery for failed pods.
			It("Should delete failed pod PVCs from a rack whose InputStorage sets deleteLocalStorageOnPodRecovery=true "+
				"while global deleteLocalStorageOnPodRecovery is false", func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnRestart = ptr.To(true)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(false)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}

				rackConfig := asdbv1.RackConfig{
					Racks:      getDummyRackConf(1, 2),
					Namespaces: []string{"test"},
				}
				rackSt := lib.DeepCopy(&aeroCluster.Spec.Storage).(*asdbv1.AerospikeStorageSpec)
				rackSt.DeleteLocalStorageOnPodRecovery = ptr.To(true)
				rackSt.LocalStorageClasses = []string{storageClass}
				rackConfig.Racks[0].InputStorage = rackSt
				aeroCluster.Spec.RackConfig = rackConfig

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				failedPodName := clusterName + "-1-0"
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
					Labels: map[string]string{"test-label": "int-fph-039-rack-override"},
				}
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
			})

			// per-rack mixed override — rack-1 deletes failed pod PVCs, rack-2 keeps them.
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
						Labels: map[string]string{"test-label": "int-fph-041-mixed-rack"},
					}
					Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					validateClusterPVCDeletion(ctx, oldPvcInfoPerPod, clusterName+"-2-0")
				})
		})

		Context("Data integrity after failed pod recovery", func() {
			// size 2 + RF=1 + maxIgnorablePods; Aerospike read may lag after rollout — Eventually.
			It("Should keep previously written Aerospike records when deleteLocalStorageOnPodRecovery is false", func() {
				aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 1)
				aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(false)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				var err error

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				Expect(WriteDataToCluster(aeroCluster, k8sClient, []string{"test"})).ToNot(HaveOccurred())

				failedPodName := clusterName + "-0-0"
				Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				maxIgnorable := intstr.FromInt32(1)
				aeroCluster.Spec.RackConfig.MaxIgnorablePods = &maxIgnorable

				aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
					Labels: map[string]string{"test-label": "int-fph-037-data"},
				}
				Expect(updateClusterWithTO(k8sClient, ctx, aeroCluster, 20*time.Minute)).ToNot(HaveOccurred())

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

			// size 3 + RF=2, no maxIgnorablePods (ignorable failed pods are skipped from rolling restart, so
			// deleteLocalStorageOnPodRecovery would not delete their PVCs). Assert failed pod PVCs go away.
			It("Should delete failed pod local PVCs when deleteLocalStorageOnPodRecovery is true during rolling restart",
				func() {
					aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 3, 2)
					aeroCluster.Spec.Storage.DeleteLocalStorageOnPodRecovery = ptr.To(true)
					aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

					var err error

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					Expect(WriteDataToCluster(aeroCluster, k8sClient, []string{"test"})).ToNot(HaveOccurred())

					failedPodName := clusterName + "-0-0"
					Expect(markPodAsFailed(ctx, k8sClient, failedPodName, namespace)).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{"test-label": "int-fph-038-pvc-delete"},
					}

					oldPVC, err := pvcClaimUIDsForPod(ctx, k8sClient, failedPodName, namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(oldPVC).ToNot(BeEmpty())

					Expect(updateClusterWithTO(k8sClient, ctx, aeroCluster, 20*time.Minute)).ToNot(HaveOccurred())

					Expect(validatePVCDeletion(ctx, oldPVC, true)).To(Succeed())
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
