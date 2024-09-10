package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var _ = Describe(
	"AerospikeCluster", func() {

		ctx := goctx.TODO()

		// Cluster lifecycle related
		Context(
			"DeployClusterPost490", func() {
				DeployClusterForAllImagesPost490(ctx)
			},
		)
		Context(
			"DeployClusterDiffStorageMultiPodPerHost", func() {
				DeployClusterForDiffStorageTest(ctx, 2, true)
			},
		)
		Context(
			"DeployClusterDiffStorageSinglePodPerHost", func() {
				DeployClusterForDiffStorageTest(ctx, 2, false)
			},
		)
		Context(
			"DeployClusterWithDNSConfiguration", func() {
				DeployClusterWithDNSConfiguration(ctx)
			},
		)
		// Need to setup some syslog related things for this
		// Context(
		// 	"DeployClusterWithSyslog", func() {
		// 		DeployClusterWithSyslog(ctx)
		// 	},
		// )
		Context(
			"DeployClusterWithMaxIgnorablePod", func() {
				clusterWithMaxIgnorablePod(ctx)
			},
		)
		Context(
			"CommonNegativeClusterValidationTest", func() {
				NegativeClusterValidationTest(ctx)
			},
		)
		Context(
			"UpdateTLSCluster", func() {
				UpdateTLSClusterTest(ctx)
			},
		)
		Context(
			"UpdateCluster", func() {
				UpdateClusterTest(ctx)
			},
		)
		Context(
			"RunScaleDownWithMigrateFillDelay", func() {
				ScaleDownWithMigrateFillDelay(ctx)
			},
		)
		Context(
			"UpdateClusterPre600", func() {
				UpdateClusterPre600(ctx)
			},
		)
		Context(
			"PauseReconcile", func() {
				PauseReconcileTest(ctx)
			},
		)
	},
)

func PauseReconcileTest(ctx goctx.Context) {
	clusterNamespacedName := getNamespacedName(
		"pause-reconcile", namespace,
	)

	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster, err := getCluster(
				k8sClient, ctx, clusterNamespacedName,
			)
			Expect(err).ToNot(HaveOccurred())

			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

	It(
		"Should pause reconcile", func() {
			// Testing over upgrade as it is a long-running operation
			By("1. Start upgrade and pause at partial upgrade")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = UpdateClusterImage(aeroCluster, nextImage)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Update(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			Eventually(
				func() bool {
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Check if at least one pod is upgraded
					podUpgraded := false
					for podName := range aeroCluster.Status.Pods {
						podStatus := aeroCluster.Status.Pods[podName]
						if podStatus.Image == nextImage {
							pkgLog.Info("One Pod upgraded", "pod", podName, "image", podStatus.Image)
							podUpgraded = true
							break
						}
					}

					return podUpgraded
				}, 2*time.Minute, 1*time.Second,
			).Should(BeTrue())

			By("Pause reconcile")

			err = setPauseFlag(ctx, clusterNamespacedName, ptr.To(true))
			Expect(err).ToNot(HaveOccurred())

			By("2. Upgrade should fail")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(1), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)
			Expect(err).To(HaveOccurred())

			// Resume reconcile and Wait for all pods to be upgraded
			By("3. Resume reconcile and upgrade should succeed")

			err = setPauseFlag(ctx, clusterNamespacedName, nil)
			Expect(err).ToNot(HaveOccurred())

			By("Upgrade should succeed")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = waitForAerospikeCluster(
				k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
			)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func setPauseFlag(ctx goctx.Context, clusterNamespacedName types.NamespacedName, pause *bool) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Paused = pause

	return k8sClient.Update(ctx, aeroCluster)
}

func UpdateClusterPre600(ctx goctx.Context) {
	Context(
		"UpdateClusterPre600", func() {
			clusterNamespacedName := getNamespacedName(
				"deploy-cluster-pre6", namespace,
			)

			BeforeEach(
				func() {
					image := fmt.Sprintf(
						"aerospike/aerospike-server-enterprise:%s", pre6Version,
					)
					aeroCluster, err := getAeroClusterConfig(
						clusterNamespacedName, image,
					)
					Expect(err).ToNot(HaveOccurred())

					err = deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It(
				"UpdateReplicationFactor: should fail for updating namespace replication-factor on server"+
					"before 6.0.0. Cannot be updated", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					namespaceConfig :=
						aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
					namespaceConfig["replication-factor"] = 5
					aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig

					err = k8sClient.Update(
						ctx, aeroCluster,
					)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

func ScaleDownWithMigrateFillDelay(ctx goctx.Context) {
	Context(
		"ScaleDownWithMigrateFillDelay", func() {
			clusterNamespacedName := getNamespacedName(
				"migrate-fill-delay-cluster", namespace,
			)
			migrateFillDelay := int64(120)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["migrate-fill-delay"] =
						migrateFillDelay
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It(
				"Should ignore migrate-fill-delay while scaling down", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size -= 2
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// verify that migrate-fill-delay is set to 0 while scaling down
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, 0)
					Expect(err).ToNot(HaveOccurred())

					err = waitForAerospikeCluster(
						k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
						getTimeout(2), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
					)
					Expect(err).ToNot(HaveOccurred())

					// verify that migrate-fill-delay is reverted to original value after scaling down
					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName, migrateFillDelay)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

func clusterWithMaxIgnorablePod(ctx goctx.Context) {
	var (
		aeroCluster    *asdbv1.AerospikeCluster
		err            error
		nodeList       = &v1.NodeList{}
		podList        = &v1.PodList{}
		expectedPhases = []asdbv1.AerospikeClusterPhase{
			asdbv1.AerospikeClusterInProgress, asdbv1.AerospikeClusterCompleted,
		}
	)

	clusterNamespacedName := getNamespacedName(
		"ignore-pod-cluster", namespace,
	)

	AfterEach(
		func() {
			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	Context(
		"UpdateClusterWithMaxIgnorablePodAndPendingPod", func() {
			BeforeEach(
				func() {
					nodeList, err = getNodeList(ctx, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					size := len(nodeList.Items)

					deployClusterForMaxIgnorablePods(ctx, clusterNamespacedName, size)

					By("Scale up 1 pod to make that pod pending due to lack of k8s nodes")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size++
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			It(
				"Should allow cluster operations with pending pod", func() {
					By("Set MaxIgnorablePod and Rolling restart cluster")

					// As pod is in pending state, CR object will be updated continuously
					// This is put in eventually to retry Object Conflict error
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())
							val := intstr.FromInt32(1)
							aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
							aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["enable-quotas"] = true

							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, 1*time.Minute,
					).ShouldNot(HaveOccurred())

					By("Upgrade version")
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())
							aeroCluster.Spec.Image = nextImage
							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, 1*time.Minute,
					).ShouldNot(HaveOccurred())

					By("Verify pending pod")

					podList, err = getPodList(aeroCluster, k8sClient)

					var counter int

					for idx := range podList.Items {
						if podList.Items[idx].Status.Phase == v1.PodPending {
							counter++
						}
					}
					// There should be only one pending pod
					Expect(counter).To(Equal(1))

					By("Executing on-demand operation")
					Eventually(
						func() error {
							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							operations := []asdbv1.OperationSpec{
								{
									Kind: asdbv1.OperationWarmRestart,
									ID:   "1",
								},
							}
							aeroCluster.Spec.Operations = operations
							// As pod is in pending state, CR object won't reach the final phase.
							// So expectedPhases can be InProgress or Completed
							return updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
						}, 1*time.Minute,
					).ShouldNot(HaveOccurred())

					By("Verify pending pod")

					podList, err = getPodList(aeroCluster, k8sClient)

					counter = 0

					for idx := range podList.Items {
						if podList.Items[idx].Status.Phase == v1.PodPending {
							counter++
						}
					}
					// There should be only one pending pod
					Expect(counter).To(Equal(1))

					By("Scale down 1 pod")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size--
					// As pod is in pending state, CR object won't reach the final phase.
					// So expectedPhases can be InProgress or Completed
					err = updateClusterWithExpectedPhases(k8sClient, ctx, aeroCluster, expectedPhases)
					Expect(err).ToNot(HaveOccurred())

					By("Verify if all pods are running")

					podList, err = getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					for idx := range podList.Items {
						Expect(utils.IsPodRunningAndReady(&podList.Items[idx])).To(BeTrue())
					}
				},
			)
		},
	)

	Context(
		"UpdateClusterWithMaxIgnorablePodAndFailedPod", func() {
			clusterNamespacedName := getNamespacedName(
				"ignore-pod-cluster", namespace,
			)

			BeforeEach(
				func() {
					deployClusterForMaxIgnorablePods(ctx, clusterNamespacedName, 4)
				},
			)

			It(
				"Should allow rack deletion with failed pods in different rack", func() {
					By("Fail 1-1 aerospike pod")

					ignorePodName := clusterNamespacedName.Name + "-1-1"
					pod := &v1.Pod{}

					err := k8sClient.Get(
						ctx, types.NamespacedName{
							Name:      ignorePodName,
							Namespace: clusterNamespacedName.Namespace,
						}, pod,
					)
					Expect(err).ToNot(HaveOccurred())

					pod.Spec.Containers[0].Image = wrongImage
					err = k8sClient.Update(ctx, pod)
					Expect(err).ToNot(HaveOccurred())

					// Underlying kubernetes cluster should have atleast 6 nodes to run this test successfully.
					By("Delete rack with id 2")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					val := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
					aeroCluster.Spec.RackConfig.Racks = getDummyRackConf(1)
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By(fmt.Sprintf("Verify if failed pod %s is automatically recovered", ignorePodName))
					Eventually(
						func() bool {
							err = k8sClient.Get(
								ctx, types.NamespacedName{
									Name:      ignorePodName,
									Namespace: clusterNamespacedName.Namespace,
								}, pod,
							)

							return len(pod.Status.ContainerStatuses) != 0 && *pod.Status.ContainerStatuses[0].Started &&
								pod.Status.ContainerStatuses[0].Ready
						}, 1*time.Minute,
					).Should(BeTrue())

					Eventually(
						func() error {
							return InterceptGomegaFailure(
								func() {
									validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
								},
							)
						}, 4*time.Minute,
					).Should(BeNil())
				},
			)

			It(
				"Should allow namespace addition and removal with failed pod", func() {
					By("Fail 1-1 aerospike pod")

					ignorePodName := clusterNamespacedName.Name + "-1-1"
					pod := &v1.Pod{}

					err := k8sClient.Get(
						ctx, types.NamespacedName{
							Name:      ignorePodName,
							Namespace: clusterNamespacedName.Namespace,
						}, pod,
					)
					Expect(err).ToNot(HaveOccurred())

					pod.Spec.Containers[0].Image = wrongImage
					err = k8sClient.Update(ctx, pod)
					Expect(err).ToNot(HaveOccurred())

					By("Set MaxIgnorablePod and Rolling restart by removing namespace")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					val := intstr.FromInt32(1)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = &val
					nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
					nsList = nsList[:len(nsList)-1]
					aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					err = validateDirtyVolumes(ctx, k8sClient, clusterNamespacedName, []string{"bar"})
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart by re-using previously removed namespace storage")

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList = aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
					nsList = append(nsList, getNonSCNamespaceConfig("barnew", "/test/dev/xvdf1"))
					aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}

func deployClusterForMaxIgnorablePods(ctx goctx.Context, clusterNamespacedName types.NamespacedName, size int) {
	By("Deploying cluster")

	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, int32(size))

	// Add a nonsc namespace. This will be used to test dirty volumes
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = append(nsList, getNonSCNamespaceConfig("bar", "/test/dev/xvdf1"))
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes,
		asdbv1.VolumeSpec{
			Name: "bar",
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					Size:         resource.MustParse("1Gi"),
					StorageClass: storageClass,
					VolumeMode:   v1.PersistentVolumeBlock,
				},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
				Path: "/test/dev/xvdf1",
			},
		},
	)
	racks := getDummyRackConf(1, 2)
	aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
		Namespaces: []string{scNamespace}, Racks: racks,
	}
	aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
	err := deployCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())
}

// Test cluster deployment with all image post 4.9.0
func DeployClusterForAllImagesPost490(ctx goctx.Context) {
	// post 4.9.0, need feature-key file
	versions := []string{
		"6.4.0.7", "6.3.0.13", "6.2.0.9", "6.1.0.14", "6.0.0.16", "5.7.0.8", "5.6.0.7", "5.5.0.3", "5.4.0.5",
	}

	for _, v := range versions {
		It(
			fmt.Sprintf("Deploy-%s", v), func() {
				clusterName := "deploy-cluster"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				image := fmt.Sprintf(
					"aerospike/aerospike-server-enterprise:%s", v,
				)
				aeroCluster, err := getAeroClusterConfig(
					clusterNamespacedName, image,
				)
				Expect(err).ToNot(HaveOccurred())

				err = deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				By("Validating Readiness probe")

				err = validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceTLSPort)
				Expect(err).ToNot(HaveOccurred())

				_ = deleteCluster(k8sClient, ctx, aeroCluster)
			},
		)
	}
}

// Test cluster deployment with different namespace storage
func DeployClusterForDiffStorageTest(
	ctx goctx.Context, nHosts int32, multiPodPerHost bool,
) {
	clusterSz := nHosts
	if multiPodPerHost {
		clusterSz++
	}

	repFact := nHosts

	Context(
		"Positive", func() {
			// Cluster with n nodes, enterprise can be more than 8
			// Cluster with resources
			// Verify: Connect with cluster

			// Namespace storage configs
			//
			// SSD Storage Engine
			It(
				"SSDStorageCluster", func() {
					clusterNamespacedName := getNamespacedName(
						"ssdstoragecluster", namespace,
					)
					aeroCluster := createSSDStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			// HDD Storage Engine with Data in Memory
			It(
				"HDDAndDataInMemStorageCluster", func() {
					clusterNamespacedName := getNamespacedName(
						"inmemstoragecluster", namespace,
					)

					aeroCluster := createHDDAndDataInMemStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
					err = deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
			// Data in Memory Without Persistence
			It(
				"DataInMemWithoutPersistentStorageCluster", func() {
					clusterNamespacedName := getNamespacedName(
						"nopersistentcluster", namespace,
					)

					aeroCluster := createDataInMemWithoutPersistentStorageCluster(
						clusterNamespacedName, clusterSz, repFact,
						multiPodPerHost,
					)

					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
					err = deleteCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
			// Shadow Device
			// It("ShadowDeviceStorageCluster", func() {
			// 	aeroCluster := createShadowDeviceStorageCluster(clusterNamespacedName, clusterSz, repFact, multiPodPerHost)
			// 	if err := deployCluster(k8sClient, ctx, aeroCluster); err != nil {
			// 		t.Fatal(err)
			// 	}
			// 	// make info call

			// 	deleteCluster(k8sClient, ctx, aeroCluster)
			// })

			// Persistent Memory (pmem) Storage Engine
		},
	)
}

func DeployClusterWithDNSConfiguration(ctx goctx.Context) {
	var aeroCluster *asdbv1.AerospikeCluster

	It(
		"deploy with dnsPolicy 'None' and dnsConfig given",
		func() {
			clusterNamespacedName := getNamespacedName(
				"dns-config-cluster", namespace,
			)
			aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)
			noneDNS := v1.DNSNone
			aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS

			var kubeDNSSvc v1.Service

			// fetch kube-dns service to get the DNS server IP for DNS lookup
			// This service name is same for both kube-dns and coreDNS DNS servers
			Expect(
				k8sClient.Get(
					ctx, types.NamespacedName{
						Namespace: "kube-system",
						Name:      "kube-dns",
					}, &kubeDNSSvc,
				),
			).ShouldNot(HaveOccurred())

			dnsConfig := &v1.PodDNSConfig{
				Nameservers: kubeDNSSvc.Spec.ClusterIPs,
				Searches:    []string{"svc.cluster.local", "cluster.local"},
				Options: []v1.PodDNSConfigOption{
					{
						Name:  "ndots",
						Value: func(input string) *string { return &input }("5"),
					},
				},
			}
			aeroCluster.Spec.PodSpec.DNSConfig = dnsConfig

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ShouldNot(HaveOccurred())

			sts, err := getSTSFromRackID(aeroCluster, 0)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(sts.Spec.Template.Spec.DNSConfig).To(Equal(dnsConfig))
		},
	)

	AfterEach(
		func() {
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func DeployClusterWithSyslog(ctx goctx.Context) {
	It(
		"deploy with syslog logging config", func() {
			clusterNamespacedName := getNamespacedName(
				"logging-config-cluster", namespace,
			)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			loggingConf := []interface{}{
				map[string]interface{}{
					"name":     "syslog",
					"any":      "INFO",
					"path":     "/dev/log",
					"tag":      "asd",
					"facility": "local0",
				},
			}

			aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ShouldNot(HaveOccurred())

			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func UpdateTLSClusterTest(ctx goctx.Context) {
	clusterName := "update-tls-cluster"
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	BeforeEach(
		func() {
			aeroCluster := createBasicTLSCluster(clusterNamespacedName, 3)
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
				getSCNamespaceConfig("test", "/test/dev/xvdf"),
			}
			aeroCluster.Spec.Storage = getBasicStorageSpecObject()

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster, err := getCluster(
				k8sClient, ctx, clusterNamespacedName,
			)
			Expect(err).ToNot(HaveOccurred())

			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

	Context(
		"When doing valid operations", func() {
			It(
				"Try update operations", func() {
					By("Adding new TLS configuration")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network := aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList := network["tls"].([]interface{})
					tlsList = append(
						tlsList, map[string]interface{}{
							"name":      "aerospike-a-0.test-runner1",
							"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/svc_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
					)
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Modifying unused TLS configuration")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					unusedTLS := tlsList[1].(map[string]interface{})
					unusedTLS["name"] = "aerospike-a-0.test-runner2"
					unusedTLS["ca-file"] = "/etc/aerospike/secret/fb_cert.pem"
					tlsList[1] = unusedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Removing unused TLS configuration")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					network["tls"] = tlsList[:1]
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Changing ca-file to ca-path in TLS configuration")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS := tlsList[0].(map[string]interface{})
					usedTLS["ca-path"] = "/etc/aerospike/secret/cacerts"
					delete(usedTLS, "ca-file")
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					secretVolume := asdbv1.VolumeSpec{
						Name: test.TLSCacertSecretName,
						Source: asdbv1.VolumeSource{
							Secret: &v1.SecretVolumeSource{
								SecretName: test.TLSCacertSecretName,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret/cacerts",
						},
					}
					aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, secretVolume)
					operatorClientCertSpec := getOperatorCert()
					operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.CaCertsFilename = ""
					cacertPath := &asdbv1.CaCertsSource{
						SecretName: test.TLSCacertSecretName,
					}
					operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.CaCertsSource = cacertPath
					aeroCluster.Spec.OperatorClientCertSpec = operatorClientCertSpec
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)

	Context(
		"When doing invalid operations", func() {
			It(
				"Try update operations", func() {
					By("Modifying name of used TLS configuration")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network := aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList := network["tls"].([]interface{})
					usedTLS := tlsList[0].(map[string]interface{})
					usedTLS["name"] = "aerospike-a-0.test-runner2"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Modifying ca-file of used TLS configuration")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS = tlsList[0].(map[string]interface{})
					usedTLS["ca-file"] = "/etc/aerospike/secret/fb_cert.pem"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating both ca-file and ca-path in TLS configuration")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					tlsList = network["tls"].([]interface{})
					usedTLS = tlsList[0].(map[string]interface{})
					usedTLS["ca-path"] = "/etc/aerospike/secret/cacerts"
					tlsList[0] = usedTLS
					network["tls"] = tlsList
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating tls-name in service network config")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					serviceNetwork := network[asdbv1.ServicePortName].(map[string]interface{})
					serviceNetwork["tls-name"] = "unknown-tls"
					network[asdbv1.ServicePortName] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					By("Updating tls-port in service network config")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					serviceNetwork = network[asdbv1.ServicePortName].(map[string]interface{})
					serviceNetwork["tls-port"] = float64(4000)
					network[asdbv1.ServicePortName] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					// Should fail when changing network config from tls to non-tls in a single step.
					// Ideally first tls and non-tls config both has to set and then remove tls config.
					By("Updating tls to non-tls in single step in service network config")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					serviceNetwork = network[asdbv1.ServicePortName].(map[string]interface{})
					delete(serviceNetwork, "port")
					network[asdbv1.ServicePortName] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					network = aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
					serviceNetwork = network[asdbv1.ServicePortName].(map[string]interface{})
					delete(serviceNetwork, "tls-port")
					delete(serviceNetwork, "tls-name")
					delete(serviceNetwork, "tls-authenticate-client")
					serviceNetwork["port"] = float64(serviceNonTLSPort)
					network[asdbv1.ServicePortName] = serviceNetwork
					aeroCluster.Spec.AerospikeConfig.Value["network"] = network
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

// Test cluster cr update
func UpdateClusterTest(ctx goctx.Context) {
	clusterName := "update-cluster"
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	// Note: this storage will be used by dynamically added namespace after deployment of cluster
	dynamicNsPath := "/test/dev/dynamicns"
	dynamicNsVolume := asdbv1.VolumeSpec{
		Name: "dynamicns",
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   v1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: dynamicNsPath,
		},
	}
	dynamicNsPath1 := "/test/dev/dynamicns1"
	dynamicNsVolume1 := asdbv1.VolumeSpec{
		Name: "dynamicns1",
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   v1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: dynamicNsPath1,
		},
	}
	dynamicNs := map[string]interface{}{
		"name":               "dynamicns",
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{dynamicNsPath, dynamicNsPath1},
		},
	}

	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, dynamicNsVolume, dynamicNsVolume1,
			)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	AfterEach(
		func() {
			aeroCluster, err := getCluster(
				k8sClient, ctx, clusterNamespacedName,
			)
			Expect(err).ToNot(HaveOccurred())

			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

	Context(
		"When doing valid operations", func() {
			It(
				"Try update operations", func() {
					By("ScaleUp")

					err := scaleUpClusterTest(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("ScaleDown")

					// TODO:
					// How to check if it is checking cluster stability before killing node
					// Check if tip-clear, alumni-reset is done or not
					err = scaleDownClusterTest(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart")

					// TODO: How to check if it is checking cluster stability before killing node
					err = rollingRestartClusterTest(
						logger, k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Updating NamespaceStorage")

					err = rollingRestartClusterByUpdatingNamespaceStorageTest(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Adding Namespace Dynamically")

					err = rollingRestartClusterByAddingNamespaceDynamicallyTest(
						k8sClient, ctx, dynamicNs, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Scaling up along with modifying Namespace storage Dynamically")

					err = scaleUpClusterTestWithNSDeviceHandling(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("Scaling down along with modifying Namespace storage Dynamically")

					err = scaleDownClusterTestWithNSDeviceHandling(
						k8sClient, ctx, clusterNamespacedName, 1,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Removing Namespace Dynamically")

					err = rollingRestartClusterByRemovingNamespaceDynamicallyTest(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By Re-using Previously Removed Namespace Storage")

					err = rollingRestartClusterByAddingNamespaceDynamicallyTest(
						k8sClient, ctx, dynamicNs, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By changing non-tls to tls")

					err = rollingRestartClusterByEnablingTLS(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceTLSPort})
					Expect(err).ToNot(HaveOccurred())

					By("RollingRestart By changing tls to non-tls")

					err = rollingRestartClusterByDisablingTLS(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceNonTLSPort})
					Expect(err).ToNot(HaveOccurred())

					By("Upgrade/Downgrade")

					// TODO: How to check if it is checking cluster stability before killing node
					// dont change image, it upgrade, check old version
					err = upgradeClusterTest(
						k8sClient, ctx, clusterNamespacedName, nextImage,
					)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)

	Context(
		"When doing invalid operations", func() {
			Context(
				"ValidateUpdate", func() {
					// TODO: No jump version yet but will be used
					// It("Image", func() {
					// 	old := aeroCluster.Spec.Image
					// 	aeroCluster.Spec.Image = "aerospike/aerospike-server-enterprise:4.0.0.5"
					// 	err = k8sClient.Update(ctx, aeroCluster)
					// 	validateError(err, "should fail for upgrading to jump version")
					// 	aeroCluster.Spec.Image = old
					// })
					It(
						"MultiPodPerHost: should fail for updating MultiPodPerHost. Cannot be updated",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							multiPodPerHost := !*aeroCluster.Spec.PodSpec.MultiPodPerHost
							aeroCluster.Spec.PodSpec.MultiPodPerHost = &multiPodPerHost

							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"Should fail for Re-using Namespace Storage Dynamically",
						func() {
							err := rollingRestartClusterByReusingNamespaceStorageTest(
								k8sClient, ctx, clusterNamespacedName, dynamicNs,
							)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"StorageValidation: should fail for updating Storage. Cannot be updated",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							newVolumeSpec := []asdbv1.VolumeSpec{
								{
									Name: "ns",
									Source: asdbv1.VolumeSource{
										PersistentVolume: &asdbv1.PersistentVolumeSpec{
											StorageClass: storageClass,
											VolumeMode:   v1.PersistentVolumeBlock,
											Size:         resource.MustParse("1Gi"),
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/dev/xvdf2",
									},
								},
								{
									Name: "workdir",
									Source: asdbv1.VolumeSource{
										PersistentVolume: &asdbv1.PersistentVolumeSpec{
											StorageClass: storageClass,
											VolumeMode:   v1.PersistentVolumeFilesystem,
											Size:         resource.MustParse("1Gi"),
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/opt/aeropsike/ns1",
									},
								},
							}
							aeroCluster.Spec.Storage.Volumes = newVolumeSpec

							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					Context(
						"AerospikeConfig", func() {
							Context(
								"Namespace", func() {
									It(
										"UpdateReplicationFactor: should fail for updating namespace"+
											"replication-factor on SC namespace. Cannot be updated",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											namespaceConfig["replication-factor"] = 5
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig

											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"UpdateReplicationFactor: should fail for updating namespace"+
											"replication-factor on non-SC namespace. Cannot be updated",
										func() {
											By("RollingRestart By Adding Namespace Dynamically")

											err := rollingRestartClusterByAddingNamespaceDynamicallyTest(
												k8sClient, ctx, dynamicNs, clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
											namespaceConfig := nsList[len(nsList)-1].(map[string]interface{})
											namespaceConfig["replication-factor"] = 3
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[len(nsList)-1] = namespaceConfig

											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)
								},
							)

							Context(
								"Network", func() {
									// Should fail when changing network config from non-tls to tls in a single step.
									// Ideally first tls and non-tls config both has to set and then remove non-tls config.
									It(
										"UpdateService: should fail for updating non-tls to tls in single step. Cannot be updated",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx, clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											network := getNetworkTLSConfig()
											serviceNetwork := network[asdbv1.ServicePortName].(map[string]interface{})
											delete(serviceNetwork, "port")
											network[asdbv1.ServicePortName] = serviceNetwork
											aeroCluster.Spec.AerospikeConfig.Value["network"] = network
											aeroCluster.Spec.OperatorClientCertSpec = &asdbv1.AerospikeOperatorClientCertSpec{
												AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
													SecretCertSource: &asdbv1.AerospikeSecretCertSource{
														SecretName:         test.AerospikeSecretName,
														CaCertsFilename:    "cacert.pem",
														ClientCertFilename: "svc_cluster_chain.pem",
														ClientKeyFilename:  "svc_key.pem",
													},
												},
											}
											err = updateCluster(k8sClient, ctx, aeroCluster)
											Expect(err).Should(HaveOccurred())
										},
									)
								},
							)
						},
					)
				},
			)
		},
	)
}

// Test cluster validation Common for deployment and update both
func NegativeClusterValidationTest(ctx goctx.Context) {
	clusterName := "invalid-cluster"
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	Context(
		"NegativeDeployClusterValidationTest", func() {
			negativeDeployClusterValidationTest(ctx, clusterNamespacedName)
		},
	)

	Context(
		"NegativeUpdateClusterValidationTest", func() {
			negativeUpdateClusterValidationTest(ctx, clusterNamespacedName)
		},
	)
}

func negativeDeployClusterValidationTest(
	ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) {
	Context(
		"Validation", func() {
			It(
				"EmptyClusterName: should fail for EmptyClusterName", func() {
					cName := getNamespacedName(
						"", clusterNamespacedName.Namespace,
					)

					aeroCluster := createDummyAerospikeCluster(cName, 1)
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"EmptyNamespaceName: should fail for EmptyNamespaceName",
				func() {
					cName := getNamespacedName("validclustername", "")

					aeroCluster := createDummyAerospikeCluster(cName, 1)
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidImage: should fail for InvalidImage", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					aeroCluster.Spec.Image = "InvalidImage"
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					aeroCluster.Spec.Image = invalidImage
					err = deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidSize: should fail for zero size", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 0,
					)
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			Context(
				"InvalidOperatorClientCertSpec: should fail for invalid OperatorClientCertSpec", func() {
					It(
						"MultipleCertSource: should fail if both SecretCertSource and CertPathInOperator is set",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator = &asdbv1.AerospikeCertPathInOperatorSource{}
							err := deployCluster(
								k8sClient, ctx, aeroCluster,
							)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"MissingClientKeyFilename: should fail if ClientKeyFilename is missing",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.ClientKeyFilename = ""

							err := deployCluster(
								k8sClient, ctx, aeroCluster,
							)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"Should fail if both CaCertsFilename and CaCertsSource is set",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.CaCertsSource = &asdbv1.CaCertsSource{}

							err := deployCluster(
								k8sClient, ctx, aeroCluster,
							)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"MissingClientCertPath: should fail if clientCertPath is missing",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource = nil
							aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator =
								&asdbv1.AerospikeCertPathInOperatorSource{
									CaCertsPath:    "cacert.pem",
									ClientKeyPath:  "svc_key.pem",
									ClientCertPath: "",
								}

							err := deployCluster(
								k8sClient, ctx, aeroCluster,
							)
							Expect(err).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfig: should fail for empty/invalid aerospikeConfig",
				func() {
					It(
						"should fail for empty/invalid aerospikeConfig",
						func() {
							aeroCluster := createDummyAerospikeCluster(
								clusterNamespacedName, 1,
							)
							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())

							aeroCluster = createDummyAerospikeCluster(
								clusterNamespacedName, 1,
							)
							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
								Value: map[string]interface{}{
									"namespaces": "invalidConf",
								},
							}
							err = deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					Context(
						"InvalidNamespace", func() {
							It(
								"NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace",
								func() {
									aeroCluster := createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nil
									err := deployCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())
								},
							)

							// Should we test for overridden fields
							Context(
								"InvalidStorage", func() {
									It(
										"NilStorageEngine: should fail for nil storage-engine",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											namespaceConfig["storage-engine"] = nil
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
											err := deployCluster(
												k8sClient, ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"NilStorageEngineDevice: should fail for nil storage-engine.device",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})

											if _, ok :=
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = nil
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err := deployCluster(
													k8sClient, ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"InvalidStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot have 3 devices in single device string",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})

											if _, ok :=
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												aeroCluster.Spec.Storage.Volumes = []asdbv1.VolumeSpec{
													{
														Name: "nsvol1",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf1",
														},
													},
													{
														Name: "nsvol2",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf2",
														},
													},
													{
														Name: "nsvol3",
														Source: asdbv1.VolumeSource{
															PersistentVolume: &asdbv1.PersistentVolumeSpec{
																Size:         resource.MustParse("1Gi"),
																StorageClass: storageClass,
																VolumeMode:   v1.PersistentVolumeBlock,
															},
														},
														Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
															Path: "/dev/xvdf3",
														},
													},
												}

												namespaceConfig :=
													aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] =
													[]string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err := deployCluster(
													k8sClient, ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"NilStorageEngineFile: should fail for nil storage-engine.file",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})

											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["files"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["files"] = nil
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err := deployCluster(
													k8sClient, ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"ExtraStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot use a device which doesn't exist in storage",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})

											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
												devList = append(
													devList, "andRandomDevice",
												)
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err := deployCluster(
													k8sClient, ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"DuplicateStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot use a device which already exist in another namespace",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											secondNs := map[string]interface{}{
												"name":               "ns1",
												"replication-factor": 2,
												"storage-engine": map[string]interface{}{
													"type":    "device",
													"devices": []interface{}{"/test/dev/xvdf"},
												},
											}

											nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
											nsList = append(nsList, secondNs)
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList
											err := deployCluster(
												k8sClient, ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"InvalidxdrConfig: should fail for invalid xdr config. mountPath for digestlog not present in storage",
										func() {
											aeroCluster := createDummyAerospikeCluster(
												clusterNamespacedName, 1,
											)
											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})

											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
												aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
													"enable-xdr":         false,
													"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
												}
												err := deployCluster(
													k8sClient, ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)
								},
							)
						},
					)

					Context(
						"ChangeDefaultConfig", func() {
							It(
								"NsConf", func() {
									// Ns conf
									// Rack-id
									// aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
									// aeroCluster.Spec.AerospikeConfig.Value["namespaces"].
									// ([]interface{})[0].(map[string]interface{})["rack-id"] = 1
									// aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
									// err := deployCluster(k8sClient, ctx, aeroCluster)
									// validateError(err, "should fail for setting rack-id")
								},
							)

							It(
								"ServiceConf: should fail for setting node-id/cluster-name",
								func() {
									// Service conf
									// 	"node-id"
									// 	"cluster-name"
									aeroCluster := createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["node-id"] = "a1"
									err := deployCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())

									aeroCluster = createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
									err = deployCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())
								},
							)

							It(
								"NetworkConf: should fail for setting network conf/tls network conf",
								func() {
									// Network conf
									// "port"
									// "access-port"
									// "access-addresses"
									// "alternate-access-port"
									// "alternate-access-addresses"
									aeroCluster := createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									networkConf := map[string]interface{}{
										"service": map[string]interface{}{
											"port":             serviceNonTLSPort,
											"access-addresses": []string{"<access_addresses>"},
										},
									}
									aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
									err := deployCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())

									// if "tls-name" in conf
									// "tls-port"
									// "tls-access-port"
									// "tls-access-addresses"
									// "tls-alternate-access-port"
									// "tls-alternate-access-addresses"
									aeroCluster = createDummyAerospikeCluster(
										clusterNamespacedName, 1,
									)
									networkConf = map[string]interface{}{
										"service": map[string]interface{}{
											"tls-name":             "aerospike-a-0.test-runner",
											"tls-port":             3001,
											"tls-access-addresses": []string{"<tls-access-addresses>"},
										},
									}
									aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
									err = deployCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).Should(HaveOccurred())
								},
							)
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfigSecret", func() {
					It(
						"WhenFeatureKeyExist: should fail for no feature-key-file path in storage volume",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{
								"feature-key-file": "/randompath/features.conf",
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"WhenTLSExist: should fail for no tls path in storage volume",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
								"tls": []interface{}{
									map[string]interface{}{
										"name":      "aerospike-a-0.test-runner",
										"cert-file": "/randompath/svc_cluster_chain.pem",
									},
								},
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"WhenTLSExist: should fail for both ca-file and ca-path in tls",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
								"tls": []interface{}{
									map[string]interface{}{
										"name":      "aerospike-a-0.test-runner",
										"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
										"key-file":  "/etc/aerospike/secret/svc_key.pem",
										"ca-file":   "/etc/aerospike/secret/cacert.pem",
										"ca-path":   "/etc/aerospike/secret/cacerts",
									},
								},
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"WhenTLSExist: should fail for ca-file path pointing to Secret Manager",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
								"tls": []interface{}{
									map[string]interface{}{
										"name":      "aerospike-a-0.test-runner",
										"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
										"key-file":  "/etc/aerospike/secret/svc_key.pem",
										"ca-file":   "secrets:Test-secret:Key",
									},
								},
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"WhenTLSExist: should fail for ca-path pointing to Secret Manager",
						func() {
							aeroCluster := createAerospikeClusterPost640(
								clusterNamespacedName, 1, latestImage,
							)
							aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
								"tls": []interface{}{
									map[string]interface{}{
										"name":      "aerospike-a-0.test-runner",
										"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
										"key-file":  "/etc/aerospike/secret/svc_key.pem",
										"ca-path":   "secrets:Test-secret:Key",
									},
								},
							}
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidDNSConfiguration", func() {
					It(
						"InvalidDnsPolicy: should fail when dnsPolicy is set to 'Default'",
						func() {
							aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
							defaultDNS := v1.DNSDefault
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS
							err := deployCluster(k8sClient, ctx, aeroCluster)

							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"MissingDnsConfig: should fail when dnsPolicy is set to 'None' and no dnsConfig given",
						func() {
							aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
							noneDNS := v1.DNSNone
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS
							err := deployCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)
				},
			)

			It(
				"InvalidLogging: should fail for using syslog param with file or console logging", func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 1,
					)
					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}

					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
					err := k8sClient.Create(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}

func negativeUpdateClusterValidationTest(
	ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) {
	// Will be used in Update
	Context(
		"Validation", func() {
			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(
						clusterNamespacedName, 3,
					)

					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It(
				"InvalidImage: should fail for InvalidImage, should fail for image lower than base",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Image = "InvalidImage"
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Image = invalidImage
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"InvalidSize: should fail for zero size", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.Size = 0
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			Context(
				"InvalidDNSConfiguration", func() {
					It(
						"InvalidDnsPolicy: should fail when dnsPolicy is set to 'Default'",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							defaultDNS := v1.DNSDefault
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS
							err = updateCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					It(
						"MissingDnsConfig: Should fail when dnsPolicy is set to 'None' and no dnsConfig given",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							noneDNS := v1.DNSNone
							aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS
							err = updateCluster(k8sClient, ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)
				},
			)

			Context(
				"InvalidAerospikeConfig: should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig",
				func() {
					It(
						"should fail for empty aerospikeConfig, should fail for invalid aerospikeConfig",
						func() {
							aeroCluster, err := getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())

							aeroCluster, err = getCluster(
								k8sClient, ctx, clusterNamespacedName,
							)
							Expect(err).ToNot(HaveOccurred())

							aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
								Value: map[string]interface{}{
									"namespaces": "invalidConf",
								},
							}
							err = k8sClient.Update(ctx, aeroCluster)
							Expect(err).Should(HaveOccurred())
						},
					)

					Context(
						"InvalidNamespace", func() {
							It(
								"NilAerospikeNamespace: should fail for nil aerospikeConfig.namespace",
								func() {
									aeroCluster, err := getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nil
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())
								},
							)

							// Should we test for overridden fields
							Context(
								"InvalidStorage", func() {
									It(
										"NilStorageEngine: should fail for nil storage-engine",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											namespaceConfig["storage-engine"] = nil
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] =
												namespaceConfig
											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"NilStorageEngineDevice: should fail for nil storage-engine.device",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = nil
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"NilStorageEngineFile: should fail for nil storage-engine.file",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["files"]; ok {
												namespaceConfig["storage-engine"].(map[string]interface{})["files"] = nil
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"ExtraStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot add a device which doesn't exist in BlockStorage",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
												devList = append(
													devList, "andRandomDevice",
												)
												namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)

									It(
										"DuplicateStorageEngineDevice: should fail for invalid storage-engine.device,"+
											" cannot add a device which already exist in another namespace",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											secondNs := map[string]interface{}{
												"name":               "ns1",
												"replication-factor": 2,
												"storage-engine": map[string]interface{}{
													"type":    "device",
													"devices": []interface{}{"/test/dev/xvdf"},
												},
											}

											nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
											nsList = append(nsList, secondNs)
											aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList
											err = k8sClient.Update(
												ctx, aeroCluster,
											)
											Expect(err).Should(HaveOccurred())
										},
									)

									It(
										"InvalidxdrConfig: should fail for invalid xdr config. mountPath for digestlog not present in fileStorage",
										func() {
											aeroCluster, err := getCluster(
												k8sClient, ctx,
												clusterNamespacedName,
											)
											Expect(err).ToNot(HaveOccurred())

											namespaceConfig :=
												aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
											if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
												aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
													"enable-xdr":         false,
													"xdr-digestlog-path": "randomPath 100G",
												}
												err = k8sClient.Update(
													ctx, aeroCluster,
												)
												Expect(err).Should(HaveOccurred())
											}
										},
									)
								},
							)
						},
					)

					Context(
						"ChangeDefaultConfig", func() {
							It(
								"ServiceConf: should fail for setting node-id, should fail for setting cluster-name",
								func() {
									// Service conf
									// 	"node-id"
									// 	"cluster-name"
									aeroCluster, err := getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["node-id"] = "a10"
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())

									aeroCluster, err = getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["cluster-name"] = "cluster-name"
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())
								},
							)

							It(
								"NetworkConf: should fail for setting network conf, should fail for setting tls network conf",
								func() {
									// Network conf
									// "port"
									// "access-port"
									// "access-addresses"
									// "alternate-access-port"
									// "alternate-access-addresses"
									aeroCluster, err := getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									networkConf := map[string]interface{}{
										"service": map[string]interface{}{
											"port":             serviceNonTLSPort,
											"access-addresses": []string{"<access_addresses>"},
										},
									}
									aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())

									// if "tls-name" in conf
									// "tls-port"
									// "tls-access-port"
									// "tls-access-addresses"
									// "tls-alternate-access-port"
									// "tls-alternate-access-addresses"
									aeroCluster, err = getCluster(
										k8sClient, ctx, clusterNamespacedName,
									)
									Expect(err).ToNot(HaveOccurred())

									networkConf = map[string]interface{}{
										"service": map[string]interface{}{
											"tls-name":             "aerospike-a-0.test-runner",
											"tls-port":             3001,
											"tls-access-addresses": []string{"<tls-access-addresses>"},
										},
									}
									aeroCluster.Spec.AerospikeConfig.Value["network"] = networkConf
									err = k8sClient.Update(ctx, aeroCluster)
									Expect(err).Should(HaveOccurred())
								},
							)
						},
					)
				},
			)

			It(
				"InvalidLogging: should fail for using syslog param with file or console logging", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					loggingConf := []interface{}{
						map[string]interface{}{
							"name":     "anyFileName",
							"path":     "/dev/log",
							"tag":      "asd",
							"facility": "local0",
						},
					}

					aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)

	Context(
		"InvalidAerospikeConfigSecret", func() {
			BeforeEach(
				func() {
					aeroCluster := createAerospikeClusterPost640(
						clusterNamespacedName, 2, latestImage,
					)

					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It(
				"WhenFeatureKeyExist: should fail for no feature-key-file path in storage volumes",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{
						"feature-key-file": "/randompath/features.conf",
					}
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)

			It(
				"WhenTLSExist: should fail for no tls path in storage volumes",
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.AerospikeConfig.Value["network"] = map[string]interface{}{
						"tls": []interface{}{
							map[string]interface{}{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/randompath/svc_cluster_chain.pem",
							},
						},
					}
					err = k8sClient.Update(ctx, aeroCluster)
					Expect(err).Should(HaveOccurred())
				},
			)
		},
	)
}
