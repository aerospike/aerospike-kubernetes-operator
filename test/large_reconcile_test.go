package test

import (
	goctx "context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	as "github.com/ashishshinde/aerospike-client-go/v6"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe(
	"LargeReconcile", func() {

		ctx := goctx.Background()

		Context(
			"When doing valid operations", func() {

				clusterName := "large-reconcile"
				clusterNamespacedName := getClusterNamespacedName(
					clusterName, namespace,
				)

				// Create a 5 node cluster
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 5,
				)
				networkPolicy := asdbv1beta1.AerospikeNetworkPolicy{
					AccessType:             asdbv1beta1.AerospikeNetworkType(*defaultNetworkType),
					AlternateAccessType:    asdbv1beta1.AerospikeNetworkType(*defaultNetworkType),
					TLSAccessType:          asdbv1beta1.AerospikeNetworkType(*defaultNetworkType),
					TLSAlternateAccessType: asdbv1beta1.AerospikeNetworkType(*defaultNetworkType),
				}
				aeroCluster.Spec.AerospikeNetworkPolicy = networkPolicy

				It(
					"Should try large reconcile operations", func() {

						By("Deploy and load data")

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = loadDataInCluster(k8sClient, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("ScaleDown")

						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change size to 2
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Size = 2
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Change size to 4 immediately
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.Size = 4
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

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

						// oldService := aeroCluster.Spec.AerospikeConfig.Value["service"]
						tempConf := 18000
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = tempConf
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Change config back to original value
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = defaultProtofdmax
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

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

						err = UpdateClusterImage(aeroCluster, prevImage)
						Expect(err).ToNot(HaveOccurred())
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Change build back to original
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						err = UpdateClusterImage(aeroCluster, latestImage)
						Expect(err).ToNot(HaveOccurred())
						err = k8sClient.Update(goctx.TODO(), aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Only 1 pod need upgrade
						err = waitForClusterUpgrade(
							k8sClient, aeroCluster, int(aeroCluster.Spec.Size),
							prevImage, retryInterval, getTimeout(4),
						)
						Expect(err).ToNot(HaveOccurred())

						// Test2
						// Create a 5 node cluster
						// Add some data to make migration time taking
						// Change build to build1
						// Change build to build2
						// Only a single pod may have build1 at max in whole upgrade. Ultimately all should reach build2

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

				Context(
					"WaitingForStableCluster", func() {
						It(
							"LargeMigration", func() {
								// Need to create large migration...is there any way to mimic or olny way is to load data
								// Tested manually
							},
						)
						It(
							"ColdStart", func() {
								// Not needed for this, isClusterStable call should fail and this will requeue request.
							},
						)
					},
				)
			},
		)

	},
)

func loadDataInCluster(
	k8sClient client.Client, aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	policy := getClientPolicy(aeroCluster, k8sClient)
	policy.FailIfNotConnected = false
	policy.Timeout = time.Minute * 2
	policy.UseServicesAlternate = true
	policy.ConnectionQueueSize = 100
	policy.LimitConnectionsToQueueSize = true

	hostList := make([]*as.Host, 0, len(aeroCluster.Status.Pods))

	for podIndex := range aeroCluster.Status.Pods {
		host, err := createHost(aeroCluster.Status.Pods[podIndex])
		if err != nil {
			return err
		}

		hostList = append(hostList, host)
	}

	clientP, err := as.NewClientWithPolicyAndHost(policy, hostList...)
	if clientP == nil {
		return fmt.Errorf(
			"failed to create aerospike cluster asClient: %v", err,
		)
	}

	asClient := *clientP
	defer func() {
		fmt.Println("Closing Aerospike client")
		asClient.Close()
	}()

	_, _ = asClient.WarmUp(-1)

	keyPrefix := "testkey"

	size := 100
	bufferSize := 10000
	token := make([]byte, bufferSize)

	_, readErr := rand.Read(token)
	if readErr != nil {
		fmt.Println("error:", readErr)
		return readErr
	}

	fmt.Printf("Loading record, isClusterConnected %v\n", clientP.IsConnected())
	fmt.Println(asClient.GetNodes())

	// The k8s services take time to come up so the timeouts are on the
	// higher side.
	wp := as.NewWritePolicy(0, 0)

	// loads size * bufferSize data
	for i := 0; i < size; i++ {
		key, err := as.NewKey("test", "testset", keyPrefix+strconv.Itoa(i))
		if err != nil {
			return err
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

		fmt.Print(strconv.Itoa(i) + ", ")
	}
	fmt.Println("added records")

	return nil
}

func waitForClusterScaleDown(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster, replicas int,
	retryInterval, timeout time.Duration,
) error {
	err := wait.Poll(
		retryInterval, timeout, func() (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1beta1.AerospikeCluster{}
			err = k8sClient.Get(
				goctx.TODO(), types.NamespacedName{
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
				err = fmt.Errorf("cluster size can not go below temp size, it should have only final value, as this is the new reconcile flow")
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

			return isClusterStateValid(aeroCluster, newCluster, replicas), nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func waitForClusterRollingRestart(
	k8sClient client.Client, aeroCluster *asdbv1beta1.AerospikeCluster,
	replicas int, tempConf int, retryInterval, timeout time.Duration,
) error {
	err := wait.Poll(
		retryInterval, timeout, func() (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1beta1.AerospikeCluster{}
			err = k8sClient.Get(
				goctx.TODO(), types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}

			protofdmax := newCluster.Status.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"].(float64)
			if int(protofdmax) == tempConf {
				err := fmt.Errorf(
					"cluster status can not be updated with intermediate conf value %d, it should have only final value, as this is the new reconcile flow",
					tempConf,
				)
				// t.Logf(err.Error())
				return false, err
			}
			// t.Logf("conf value to check proto-fd-max %d", protofdmax)

			return isClusterStateValid(aeroCluster, newCluster, replicas), nil
		},
	)
	if err != nil {
		return err
	}
	// t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	return nil
}

func waitForClusterUpgrade(
	k8sClient client.Client, aeroCluster *asdbv1beta1.AerospikeCluster,
	replicas int, tempImage string, retryInterval, timeout time.Duration,
) error {
	err := wait.Poll(
		retryInterval, timeout, func() (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1beta1.AerospikeCluster{}
			err = k8sClient.Get(
				goctx.TODO(), types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					// t.Logf("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
					return false, nil
				}
				return false, err
			}
			// t.Logf("Waiting for full availability of %s AerospikeCluster (%d/%d)\n", aeroCluster.Name, aeroCluster.Status.Size, replicas)

			if newCluster.Status.Image == tempImage {
				err := fmt.Errorf(
					"cluster status can not be updated with intermediate image value %s, it should have only final value, as this is the new reconcile flow",
					tempImage,
				)
				// t.Logf(err.Error())
				return false, err
			}

			return isClusterStateValid(aeroCluster, newCluster, replicas), nil
		},
	)
	if err != nil {
		return err
	}
	// t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	return nil
}
