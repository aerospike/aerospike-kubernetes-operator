package cluster

import (
	goctx "context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"LargeReconcile", func() {
		ctx := goctx.Background()

		Context(
			"When doing valid operations", func() {
				clusterName := "large-reconcile"
				clusterNamespacedName := test.GetNamespacedName(
					clusterName, namespace,
				)

				// Create a 5 node cluster
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 5,
				)
				networkPolicy := asdbv1.AerospikeNetworkPolicy{
					AccessType:             asdbv1.AerospikeNetworkType(*defaultNetworkType),
					AlternateAccessType:    asdbv1.AerospikeNetworkType(*defaultNetworkType),
					TLSAccessType:          asdbv1.AerospikeNetworkType(*defaultNetworkType),
					TLSAlternateAccessType: asdbv1.AerospikeNetworkType(*defaultNetworkType),
				}
				aeroCluster.Spec.AerospikeNetworkPolicy = networkPolicy

				AfterEach(
					func() {
						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
					},
				)

				It(
					"Should try large reconcile operations", func() {
						By("Deploy and load data")

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = loadDataInCluster(k8sClient, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("ScaleDown")

						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change size to 2
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Size = 2
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// This is put in eventually to retry Object Conflict error and change size to 4 immediately
						Eventually(func() error {
							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.Size = 4

							return k8sClient.Update(goctx.TODO(), aeroCluster)
						}, time.Minute, time.Second).ShouldNot(HaveOccurred())

						// Cluster size should never go below 4,
						// as only one node is removed at a time and before reducing 2nd node, we changed the size to 4
						err = waitForClusterScaleDown(
							k8sClient, ctx, aeroCluster,
							int(aeroCluster.Spec.Size), retryInterval,
							getTimeout(4),
						)
						Expect(err).ToNot(HaveOccurred())

						By("RollingRestart")

						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change config
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						tempConf := "cpu"
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["auto-pin"] = tempConf
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Change config back to original value
						Eventually(func() error {
							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["auto-pin"] = "none"

							return k8sClient.Update(goctx.TODO(), aeroCluster)
						}, time.Minute, time.Second).ShouldNot(HaveOccurred())

						// Cluster status should never get updated with old conf "tempConf"
						err = waitForClusterRollingRestart(
							k8sClient, aeroCluster, int(aeroCluster.Spec.Size),
							tempConf, retryInterval, getTimeout(4),
						)
						Expect(err).ToNot(HaveOccurred())

						By("Upgrade")

						// Test1
						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change build
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = UpdateClusterImage(aeroCluster, nextImage)
						Expect(err).ToNot(HaveOccurred())
						err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Change build back to original
						Eventually(func() error {
							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							err = UpdateClusterImage(aeroCluster, latestImage)
							Expect(err).ToNot(HaveOccurred())

							return k8sClient.Update(goctx.TODO(), aeroCluster)
						}, time.Minute, time.Second).ShouldNot(HaveOccurred())

						// Only 1 pod need upgrade
						err = waitForClusterUpgrade(
							k8sClient, aeroCluster, int(aeroCluster.Spec.Size),
							nextImage, retryInterval, getTimeout(4),
						)
						Expect(err).ToNot(HaveOccurred())

						// Test2
						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change build to build1
						// Change build to build2
						// Only a single pod may have build1 at max in whole upgrade. Ultimately all should reach build2
					},
				)
			},
		)
		Context(
			"WaitingForStableCluster", func() {
				It(
					"LargeMigration", func() {
						// Need to create large migration...is there any way to mimic or only way is to load data
						// Tested manually
					},
				)
				It(
					"Cold restart to Completed takes longer after more records are loaded (no secondary indexes)", func() {
						clusterName := fmt.Sprintf("lr-cold-restart-%d", GinkgoParallelProcess())
						clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
						aeroCluster.Spec.AerospikeNetworkPolicy = asdbv1.AerospikeNetworkPolicy{
							AccessType:             asdbv1.AerospikeNetworkType(*defaultNetworkType),
							AlternateAccessType:    asdbv1.AerospikeNetworkType(*defaultNetworkType),
							TLSAccessType:          asdbv1.AerospikeNetworkType(*defaultNetworkType),
							TLSAlternateAccessType: asdbv1.AerospikeNetworkType(*defaultNetworkType),
						}

						defer func() {
							cl := &asdbv1.AerospikeCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:      clusterName,
									Namespace: namespace,
								},
							}
							Expect(DeleteCluster(k8sClient, ctx, cl)).ToNot(HaveOccurred())
							Expect(CleanupPVC(k8sClient, namespace, clusterName)).ToNot(HaveOccurred())
						}()

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						cur, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						const (
							smallKeys      = 25
							largeExtraKeys = 350
							valueBytes     = 8192
						)

						By("Load a small record set and cold-restart; measure time to Completed")
						Expect(loadDataInClusterWithParams(k8sClient, cur, smallKeys, valueBytes, 0)).ToNot(HaveOccurred())

						cur, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						durationAfterSmallLoad, err := measureColdRestartToClusterCompleted(ctx, k8sClient, cur)
						Expect(err).ToNot(HaveOccurred())

						By("Load many more records and cold-restart; measure time to Completed")

						cur, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						Expect(loadDataInClusterWithParams(k8sClient, cur, largeExtraKeys, valueBytes, smallKeys)).ToNot(HaveOccurred())

						cur, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						durationAfterLargeLoad, err := measureColdRestartToClusterCompleted(ctx, k8sClient, cur)
						Expect(err).ToNot(HaveOccurred())

						By("Expect restart duration to grow with on-disk / cold-start record volume")
						Expect(durationAfterLargeLoad).To(
							BeNumerically(">", durationAfterSmallLoad),
							"larger dataset should tend to lengthen cold-start / readiness; if this flakes on CI, "+
								"increase key counts or valueBytes",
						)
					},
				)
			},
		)
	},
)

func loadDataInCluster(
	k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
) error {
	return loadDataInClusterWithParams(k8sClient, aeroCluster, 100, 10000, 0)
}

// loadDataInClusterWithParams writes keyCount records into namespace/set "test"/"testset" used by
// createDummyAerospikeCluster. Keys are testkey<keyOffset>, ... testkey<keyOffset+keyCount-1>.
func loadDataInClusterWithParams(
	k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
	keyCount, bufferSize, keyOffset int,
) error {
	asClient, err := getAerospikeClient(aeroCluster, k8sClient)
	if err != nil {
		return err
	}

	defer func() {
		fmt.Println("Closing Aerospike client")
		asClient.Close()
	}()

	keyPrefix := "testkey"

	token := make([]byte, bufferSize)

	_, readErr := rand.Read(token)
	if readErr != nil {
		return readErr
	}

	pkgLog.Info(
		"Loading records", "nodes", asClient.GetNodeNames(),
		"keyCount", keyCount, "bufferSize", bufferSize, "keyOffset", keyOffset,
	)

	wp := as.NewWritePolicy(0, 0)

	for i := 0; i < keyCount; i++ {
		idx := keyOffset + i

		key, keyErr := as.NewKey("test", "testset", keyPrefix+strconv.Itoa(idx))
		if keyErr != nil {
			return keyErr
		}

		binMap := map[string]interface{}{
			"testbin": token,
		}

		for j := 0; j < 1000; j++ {
			err = asClient.Put(wp, key, binMap)
			if err == nil {
				break
			}

			time.Sleep(time.Second * 1)
		}

		if err != nil {
			return err
		}

		fmt.Print(strconv.Itoa(idx) + ", ")
	}

	fmt.Println("added records")

	return nil
}

// measureColdRestartToClusterCompleted deletes all Aerospike pods (cold restart), waits for new pods to be
// Ready, then waits for AerospikeCluster phase Completed. Returns elapsed wall time from first delete.
func measureColdRestartToClusterCompleted(
	ctx goctx.Context, k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
) (time.Duration, error) {
	start := time.Now()

	current, err := getCluster(k8sClient, ctx, utils.GetNamespacedName(aeroCluster))
	if err != nil {
		return 0, err
	}

	podList, err := getClusterPodList(k8sClient, ctx, current)
	if err != nil {
		return 0, err
	}

	if len(podList.Items) == 0 {
		return 0, fmt.Errorf("no pods to delete for cold restart")
	}

	oldUIDByName := make(map[string]string, len(podList.Items))

	for idx := range podList.Items {
		pod := &podList.Items[idx]
		oldUIDByName[pod.Name] = string(pod.UID)

		if delErr := k8sClient.Delete(ctx, pod); delErr != nil {
			return 0, delErr
		}
	}

	for podName, oldUID := range oldUIDByName {
		if restartErr := waitForPodRestart(ctx, podName, current.Namespace, oldUID); restartErr != nil {
			return 0, restartErr
		}
	}

	current, err = getCluster(k8sClient, ctx, utils.GetNamespacedName(aeroCluster))
	if err != nil {
		return 0, err
	}

	err = waitForAerospikeCluster(
		k8sClient, ctx, current, int(current.Spec.Size), retryInterval,
		getTimeout(current.Spec.Size), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)

	return time.Since(start), err
}

func waitForClusterScaleDown(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster, replicas int,
	retryInterval, timeout time.Duration,
) error {
	err := wait.PollUntilContextTimeout(ctx,
		retryInterval, timeout, true, func(ctx goctx.Context) (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1.AerospikeCluster{}

			err = k8sClient.Get(
				ctx, types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			if int(newCluster.Status.Size) < replicas {
				err = fmt.Errorf("cluster size can not go below temp size," +
					" it should have only final value, as this is the new reconcile flow")

				return false, err
			}

			podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
			if err != nil {
				return false, err
			}

			if len(podList.Items) < replicas {
				err := fmt.Errorf("cluster pods number can not go below replica size")
				return false, err
			}

			return isClusterStateValid(aeroCluster, newCluster, replicas,
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted}), nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func waitForClusterRollingRestart(
	k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
	replicas int, tempConf string, retryInterval, timeout time.Duration,
) error {
	err := wait.PollUntilContextTimeout(goctx.TODO(),
		retryInterval, timeout, true, func(ctx goctx.Context) (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1.AerospikeCluster{}

			err = k8sClient.Get(
				ctx, types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			autoPin := newCluster.Status.AerospikeConfig.Value["service"].(map[string]interface{})["auto-pin"].(string)
			if autoPin == tempConf {
				err := fmt.Errorf(
					"cluster status can not be updated with intermediate conf value %v,"+
						" it should have only final value, as this is the new reconcile flow",
					tempConf,
				)

				return false, err
			}

			return isClusterStateValid(aeroCluster, newCluster, replicas,
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted}), nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func waitForClusterUpgrade(
	k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
	replicas int, tempImage string, retryInterval, timeout time.Duration,
) error {
	err := wait.PollUntilContextTimeout(goctx.TODO(),
		retryInterval, timeout, true, func(ctx goctx.Context) (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1.AerospikeCluster{}

			err = k8sClient.Get(
				ctx, types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			if newCluster.Status.Image == tempImage {
				err := fmt.Errorf(
					"cluster status can not be updated with intermediate image value %s,"+
						" it should have only final value, as this is the new reconcile flow",
					tempImage,
				)

				return false, err
			}

			return isClusterStateValid(aeroCluster, newCluster, replicas,
				[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted}), nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}
