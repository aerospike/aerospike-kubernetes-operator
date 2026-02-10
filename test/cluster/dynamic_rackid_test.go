package cluster

import (
	"bufio"
	goctx "context"
	"fmt"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"DynamicRackID", func() {
		ctx := goctx.Background()
		clusterName := fmt.Sprintf("dynamic-rack-id-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

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

		Context(
			"When doing invalid operations around EnableRackIDOverride feature", func() {
				It(
					"Deploy: Should reject EnableRackIDOverride when multiple racks are configured", func() {
						By("Creating cluster with multiple racks")
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{
							{ID: 1},
							{ID: 2},
						}
						aeroCluster.Spec.EnableRackIDOverride = ptr.To(true)

						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring("single rack"))
					},
				)

				It(
					"Update: Should reject EnableRackIDOverride when multiple racks are configured", func() {
						By("Creating cluster with multiple racks")
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{
							{ID: 1},
							{ID: 2},
						}

						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						By("Enabling EnableRackIDOverride")
						aeroCluster.Spec.EnableRackIDOverride = ptr.To(true)
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring("single rack"))
					},
				)

				It(
					"Update: Should fail if EnableRackIDOverride is set but annotation is missing", func() {
						By("Creating cluster with multiple racks")
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{
							{ID: 1},
						}

						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						By("Enabling EnableRackIDOverride")
						aeroCluster.Spec.EnableRackIDOverride = ptr.To(true)
						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).To(HaveOccurred())
					},
				)
			},
		)

		Context(
			"When doing valid operations around EnableRackIDOverride feature", func() {

				Context(
					"When EnableRackIDOverride is enabled", func() {

						BeforeEach(
							func() {
								By("Creating cluster with EnableRackIDOverride enabled")
								aeroCluster := createDummyAerospikeClusterWithDynRackID(
									clusterNamespacedName, 2,
								)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
							},
						)

						It(
							"Should trigger reconciliation when override-rack-id annotation changes", func() {
								aeroCluster, err := GetCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								podNamespaceName := types.NamespacedName{
									Name:      aeroCluster.Name + "-1-0",
									Namespace: aeroCluster.Namespace,
								}

								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, true)

								By("Updating override-rack-id annotation")
								pod := &v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										Name:      podNamespaceName.Name,
										Namespace: podNamespaceName.Namespace,
									},
								}
								err = k8sClient.Delete(ctx, pod)
								Expect(err).ToNot(HaveOccurred())

								started := waitForPod(podNamespaceName)
								Expect(started).To(BeTrue(), "Pod did not start in time")

								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
									retryInterval, getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, true)
							},
						)
					},
				)

				Context(
					"When EnableRackIDOverride is disabled", func() {

						BeforeEach(
							func() {
								By("Creating cluster without EnableRackIDOverride")
								aeroCluster := createDummyAerospikeClusterWithDynRackID(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.EnableRackIDOverride = ptr.To(false)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
							},
						)

						It(
							"Should not trigger restart when annotation changes if feature is disabled", func() {
								By("Getting pods")
								aeroCluster, err := GetCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								podNamespaceName := types.NamespacedName{
									Name:      aeroCluster.Name + "-1-0",
									Namespace: aeroCluster.Namespace,
								}

								By("Updating override-rack-id annotation")
								pod := &v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										Name:      podNamespaceName.Name,
										Namespace: podNamespaceName.Namespace,
									},
								}
								err = k8sClient.Delete(ctx, pod)
								Expect(err).ToNot(HaveOccurred())

								started := waitForPod(podNamespaceName)
								Expect(started).To(BeTrue(), "Pod did not start in time")

								err = waitForAerospikeCluster(
									k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
									retryInterval, getTimeout(aeroCluster.Spec.Size),
									[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
								)
								Expect(err).ToNot(HaveOccurred())

								// Note: The predicate should still trigger reconciliation,
								// but the restart logic should not trigger based on OverrideRackID
								// since the feature is disabled
								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, false)
							},
						)

						It(
							"Should allow enabling/disabling EnableRackIDOverride after cluster is running", func() {
								By("Getting cluster")
								aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								podNamespaceName := types.NamespacedName{
									Name:      aeroCluster.Name + "-1-0",
									Namespace: aeroCluster.Namespace,
								}

								podPIDMap, err := getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Enabling EnableRackIDOverride")
								aeroCluster.Spec.EnableRackIDOverride = ptr.To(true)
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Verifying restart occurred")
								validateServerRestart(ctx, aeroCluster, podPIDMap, asdbv1.OperationWarmRestart)

								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, true)

								aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								By("Disabling EnableRackIDOverride")
								aeroCluster.Spec.EnableRackIDOverride = ptr.To(false)
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Verifying restart occurred")
								validateServerRestart(ctx, aeroCluster, podPIDMap, asdbv1.OperationWarmRestart)

								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, false)
							},
						)
					},
				)

				Context(
					"When updating cluster with EnableRackIDOverride disabled", func() {
						It(
							"Should do pod restart if enabling EnableRackIDOverride with old init container", func() {
								By("Getting cluster")
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)

								racks := []asdbv1.Rack{{ID: 1}}
								rackConf := asdbv1.RackConfig{Racks: racks,
									Namespaces: []string{
										"test",
									}}
								aeroCluster.Spec.RackConfig = rackConf

								aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageNameAndTag = "aerospike-kubernetes-init:2.4.0"
								aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistryNamespace = ptr.To("aerospike")

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.EnableRackIDOverride = ptr.To(true)

								podPIDMap, err := getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
								Expect(err).Should(HaveOccurred())

								By("Verifying restart occurred")
								Eventually(func(g Gomega) {
									restartedPod := &v1.Pod{}

									err = k8sClient.Get(ctx, types.NamespacedName{
										Name:      aeroCluster.Name + "-1-1",
										Namespace: aeroCluster.Namespace,
									}, restartedPod)
									g.Expect(err).ToNot(HaveOccurred())

									g.Expect(string(restartedPod.UID)).
										ToNot(Equal(podPIDMap[aeroCluster.Name+"-1-1"].podUID))
								}).Should(Succeed())

								By("Enabling EnableRackIDOverride")
								//nolint:goconst // this will be removed
								aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageNameAndTag = "aerospike-kubernetes-init:2.5.0-dev8"
								aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistryNamespace = ptr.To("tanmayj10")
								aeroCluster.Spec.PodSpec.InitContainers = []v1.Container{
									randomAnnotatorInitContainer(),
									{
										Name: asdbv1.AerospikeInitContainerName,
									},
								}

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								podNamespaceName := types.NamespacedName{
									Name:      aeroCluster.Name + "-1-0",
									Namespace: aeroCluster.Namespace,
								}

								validateDynamicRackIDInConfig(ctx, podNamespaceName.Name, aeroCluster, true)
							},
						)
					},
				)
			},
		)
	},
)

func validateDynamicRackIDInConfig(ctx goctx.Context, podName string, aeroCluster *asdbv1.AerospikeCluster,
	isOverrideRackID bool) {
	pods, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	podRackIDMap := make(map[string]string)

	pod := &v1.Pod{}

	for i := range pods.Items {
		podRackIDMap[pods.Items[i].Name] = pods.Items[i].Annotations[asdbv1.OverrideRackIDAnnotation]
		if pods.Items[i].Name == podName {
			pod = &pods.Items[i]
		}
	}

	originalAnnotation := pod.Annotations[asdbv1.OverrideRackIDAnnotation]

	aerospikeConf := pod.Annotations["aerospikeConf"]
	re := regexp.MustCompile(`^\s*rack-id\s+(.+)$`)

	scanner := bufio.NewScanner(strings.NewReader(aerospikeConf))

	var overrideRackID string

	for scanner.Scan() {
		line := scanner.Text()

		if m := re.FindStringSubmatch(line); len(m) == 2 {
			overrideRackID = m[1]
			break
		}
	}

	expectedRoster := ""

	if isOverrideRackID {
		Expect(originalAnnotation).To(Equal(overrideRackID))

		for name := range aeroCluster.Status.Pods {
			// Remove 0 from start of nodeID (we add this dummy rack)
			expectedRoster = fmt.Sprintf("%s@%s,",
				strings.ToUpper(strings.TrimLeft(aeroCluster.Status.Pods[name].Aerospike.NodeID,
					"0")), podRackIDMap[name]) + expectedRoster
		}
	} else {
		Expect(originalAnnotation).ToNot(Equal(overrideRackID))

		for name := range aeroCluster.Status.Pods {
			// Remove 0 from start of nodeID (we add this dummy rack)
			expectedRoster = fmt.Sprintf("%s@1,",
				strings.ToUpper(strings.TrimLeft(aeroCluster.Status.Pods[name].Aerospike.NodeID,
					"0"))) + expectedRoster
		}
	}

	hostConns, err := newAllHostConn(logger, aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	expectedRoster = strings.TrimRight(expectedRoster, ",")

	isEqual, currentRoster, err := compareRoster(hostConns[0], expectedRoster, aeroCluster)
	Expect(err).ToNot(HaveOccurred())
	Expect(isEqual).To(BeTrue(), fmt.Sprintf(
		"expected %v to equal %v", currentRoster, expectedRoster,
	))
}
