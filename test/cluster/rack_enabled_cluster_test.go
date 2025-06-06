package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// This file needs to be changed based on setup. update zone, region, nodeName according to setup

// Local tests var
// var (
// zone1  = "us-west1-b"
// zone2  = "us-west1-b"
// region = "us-west1"
// )

// Jenkins tests var
// var (
// 	zone1  = "us-west1-a"
// 	zone2  = "us-west1-a"
// )

// Test cluster cr updation
var _ = Describe(
	"RackLifeCycleOp", func() {
		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {
				clusterName := fmt.Sprintf("rack-enabled-%d", GinkgoParallelProcess())
				clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

				BeforeEach(
					func() {
						zones, err := getZones(ctx, k8sClient)
						Expect(err).ToNot(HaveOccurred())

						zone1 := zones[0]
						zone2 := zones[0]
						if len(zones) > 1 {
							zone2 = zones[1]
						}

						// Will be used in Update also
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						// This needs to be changed based on setup. update zone, region, nodeName according to setup
						racks := []asdbv1.Rack{
							{ID: 1, Zone: zone1},
							{ID: 2, Zone: zone2},
						}
						rackConf := asdbv1.RackConfig{
							Racks: racks,
						}
						aeroCluster.Spec.RackConfig = rackConf

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
					},
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

				It(
					"Should validate rack enabled cluster flow", func() {

						// Op2: scale up
						By("Scaling up the cluster")

						err := scaleUpClusterTest(
							k8sClient, ctx, clusterNamespacedName, 2,
						)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Op3: scale down
						By("Scaling down the cluster")

						err = scaleDownClusterTest(
							k8sClient, ctx, clusterNamespacedName, 2,
						)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Op4: rolling restart
						By("RollingRestart the cluster")

						err = rollingRestartClusterTest(
							logger, k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Op5: upgrade/downgrade
						By("Upgrade/Downgrade the cluster")

						// don't change image, it upgrade, check old version
						err = upgradeClusterTest(
							k8sClient, ctx, clusterNamespacedName, nextImage,
						)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

					},
				)

				It(
					"Should validate affinity", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						pods, err := getPodList(aeroCluster, k8sClient)
						Expect(err).ToNot(HaveOccurred())

						// All pods will be moved to this node
						nodeName := pods.Items[0].Spec.NodeName

						affinity := &corev1.Affinity{}
						ns := &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "kubernetes.io/hostname",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{nodeName},
										},
									},
								},
							},
						}
						affinity.NodeAffinity = &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: ns,
						}

						rackConf := asdbv1.RackConfig{
							Racks: []asdbv1.Rack{
								{
									ID: 3,
									InputPodSpec: &asdbv1.RackPodSpec{
										SchedulingPolicy: asdbv1.SchedulingPolicy{
											Affinity: affinity,
										},
									},
								},
							},
						}

						aeroCluster.Spec.RackConfig = rackConf

						// All pods should move to node with nodeName
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Verify if all the pods are moved to given node
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						pods, err = getPodList(aeroCluster, k8sClient)
						Expect(err).ToNot(HaveOccurred())

						for _, pod := range pods.Items {
							Expect(pod.Spec.NodeName).Should(Equal(nodeName))
						}
						// Test toleration
						// Test nodeSelector
					},
				)

			},
		)
	},
)
