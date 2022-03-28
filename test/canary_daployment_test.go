package test

import (
	goctx "context"
	"fmt"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CanaryDeployment", func() {
	percentages := []int32{5, 10, 30, 50, 100}
	ctx := goctx.TODO()

	Context("Canary deployment on single rack cluster", func() {
		clusterName := "canary-deployment"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)
		dynamicNsVolume := getDynamicNameSpaceVolume()
		BeforeEach(
			func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
				aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, *dynamicNsVolume)

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
		It("Try Canary Update ops for single rack cluster", func() {
			for _, percentage := range percentages {
				By(fmt.Sprintf("CanaryRollingRestart %d%%", percentage))
				err := rollingRestartClusterCanaryDeploymentTest(
					logger, k8sClient, ctx, clusterNamespacedName, percentage)
				Expect(err).ToNot(HaveOccurred())
			}
			By("Downgrade")
			err := upgradeClusterTest(
				k8sClient, ctx, clusterNamespacedName, prevImage,
			)
			Expect(err).ToNot(HaveOccurred())
			for _, percentage := range percentages {
				By(fmt.Sprintf("Canary Upgrade %d%%", percentage))
				err = canaryClusterUpgradeTest(
					k8sClient, ctx, clusterNamespacedName, latestImage, percentage)
				Expect(err).ToNot(HaveOccurred())
			}
		},
		)
	})

	Context("Canary deployment on multi rack cluster", func() {

		clusterName := "rack-enabled-canary-deployment"
		clusterNamespacedName := getClusterNamespacedName(
			clusterName, namespace,
		)
		BeforeEach(
			func() {
				zones, err := getZones(k8sClient)
				Expect(err).ToNot(HaveOccurred())

				zone1 := zones[0]
				zone2 := zones[0]
				if len(zones) > 1 {
					zone2 = zones[1]
				}

				// Will be used in Update also
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 3,
				)
				// This needs to be changed based on setup. update zone, region, nodeName according to setup
				racks := []asdbv1beta1.Rack{
					{ID: 1, Zone: zone1},
					{ID: 2, Zone: zone2},
				}
				rackConf := asdbv1beta1.RackConfig{
					Racks: racks,
				}
				aeroCluster.Spec.RackConfig = rackConf
				err = deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				err = validateRackEnabledCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())
			},
		)
		AfterEach(
			func() {
				aeroCluster, err := getCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)
		It("Try Canary Update for multi rack cluster", func() {
			for _, percentage := range percentages {
				By(fmt.Sprintf("CanaryRollingRestart %d%%", percentage))
				err := rollingRestartClusterCanaryDeploymentTest(
					logger, k8sClient, ctx, clusterNamespacedName, percentage)
				Expect(err).ToNot(HaveOccurred())
			}
			By("Downgrade")
			err := upgradeClusterTest(
				k8sClient, ctx, clusterNamespacedName, prevImage,
			)
			Expect(err).ToNot(HaveOccurred())
			for _, percentage := range percentages {
				By(fmt.Sprintf("Canary Upgrade %d%%", percentage))
				err = canaryClusterUpgradeTest(
					k8sClient, ctx, clusterNamespacedName, latestImage, percentage)
				Expect(err).ToNot(HaveOccurred())
			}
		},
		)
	})
},
)
