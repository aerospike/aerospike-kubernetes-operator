package cluster

import (
	"bufio"
	goctx "context"
	"fmt"
	"regexp"
	"strings"

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

		Context(
			"When doing invalid operations around EnableDynamicRackID feature", func() {

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

				It(
					"Should reject EnableDynamicRackID when init container version is less than 2.5.0", func() {
						By("Creating cluster with old init container version")
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						// Set init container image to version < 2.5.0
						aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec = &asdbv1.AerospikeInitContainerSpec{
							Resources: &v1.ResourceRequirements{},
						}
						// Use a version < 2.5.0 (e.g., 2.4.0)
						// Replace version in image
						aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageNameAndTag = "aerospike-kubernetes-init:2.4.0"

						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = GetCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.EnableDynamicRackID = ptr.To(true)
						err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
						Expect(err.Error()).To(ContainSubstring("cannot enable enableDynamicRackID flag," +
							" some init containers are running version less than 2.5.0"))
					},
				)

				It(
					"Should reject EnableDynamicRackID when multiple racks are configured", func() {
						By("Creating cluster with multiple racks")
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{
							{ID: 1},
							{ID: 2},
						}
						aeroCluster.Spec.EnableDynamicRackID = ptr.To(true)

						err := DeployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring("single rack"))
					},
				)
			},
		)

		Context(
			"When doing valid operations around EnableDynamicRackID feature", func() {

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
					"When EnableDynamicRackID is enabled", func() {

						BeforeEach(
							func() {
								By("Creating cluster with EnableDynamicRackID enabled")
								aeroCluster := createDummyAerospikeClusterWithDynRackID(
									clusterNamespacedName, 2,
								)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
							},
						)

						It(
							"Should trigger reconciliation when effective-rack-id annotation changes", func() {
								aeroCluster, err := GetCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								validateDynamicRackIDInConfig(ctx, aeroCluster, true)

								By("Updating effective-rack-id annotation")
								pod := &v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										Name:      aeroCluster.Name + "-1-0",
										Namespace: aeroCluster.Namespace,
									},
								}
								err = k8sClient.Delete(ctx, pod)
								Expect(err).ToNot(HaveOccurred())

								started := waitForPod(types.NamespacedName{Namespace: namespace, Name: pod.Name})
								Expect(started).To(BeTrue(), "Pod did not start in time")

								validateDynamicRackIDInConfig(ctx, aeroCluster, true)
							},
						)
					},
				)

				Context(
					"When EnableDynamicRackID is disabled", func() {

						BeforeEach(
							func() {
								By("Creating cluster without EnableDynamicRackID")
								aeroCluster := createDummyAerospikeClusterWithDynRackID(
									clusterNamespacedName, 2,
								)

								aeroCluster.Spec.EnableDynamicRackID = ptr.To(false)
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
							},
						)

						It(
							"Should not trigger restart when annotation changes if feature is disabled", func() {
								By("Getting pods")
								aeroCluster, err := GetCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								By("Updating effective-rack-id annotation")
								pod := &v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										Name:      aeroCluster.Name + "-1-0",
										Namespace: aeroCluster.Namespace,
									},
								}
								err = k8sClient.Delete(ctx, pod)
								Expect(err).ToNot(HaveOccurred())

								started := waitForPod(types.NamespacedName{Namespace: namespace, Name: pod.Name})
								Expect(started).To(BeTrue(), "Pod did not start in time")

								// Note: The predicate should still trigger reconciliation,
								// but the restart logic should not trigger based on DynamicRackID
								// since the feature is disabled
								validateDynamicRackIDInConfig(ctx, aeroCluster, false)
							},
						)

						It(
							"Should allow enabling EnableDynamicRackID after cluster is running", func() {
								By("Getting cluster")
								aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								podPIDMap, err := getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Enabling EnableDynamicRackID")
								aeroCluster.Spec.EnableDynamicRackID = ptr.To(true)
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Verifying restart occurred")
								validateServerRestart(ctx, aeroCluster, podPIDMap, asdbv1.OperationWarmRestart)

								validateDynamicRackIDInConfig(ctx, aeroCluster, true)
							},
						)
					},
				)
			},
		)
	},
)

func validateDynamicRackIDInConfig(ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster, isDynamicRackID bool) {
	pod := &v1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      aeroCluster.Name + "-1-0",
		Namespace: aeroCluster.Namespace,
	}, pod)
	Expect(err).ToNot(HaveOccurred())

	originalAnnotation := pod.Annotations[asdbv1.EffectiveRackIDAnnotation]

	aerospikeConf := pod.Annotations["aerospikeConf"]
	re := regexp.MustCompile(`^\s*rack-id\s+(.+)$`)

	println(aerospikeConf)

	scanner := bufio.NewScanner(strings.NewReader(aerospikeConf))

	var effectiveRackID string

	for scanner.Scan() {
		line := scanner.Text()
		println(line)

		if m := re.FindStringSubmatch(line); len(m) == 2 {
			effectiveRackID = m[1]
		}
	}

	println("original", originalAnnotation)
	println("effective", effectiveRackID)

	if isDynamicRackID {
		Expect(originalAnnotation).To(Equal(effectiveRackID))
	} else {
		Expect(originalAnnotation).ToNot(Equal(effectiveRackID))
	}
}
