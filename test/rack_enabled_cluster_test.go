package test

import (
	goctx "context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
)

// Local tests var
// var (
// zone1  = "us-west1-b"
// zone2  = "us-west1-b"
// region = "us-west1"
// )

// Jenkins tests var
var (
	zone1  = "us-west1-a"
	zone2  = "us-west1-a"
	region = "us-west1"
)

// This file needs to be changed based on setup. update zone, region, nodeName according to setup

// Test cluster cr updation
var _ = Describe("RackLifeCycleOp", func() {
	ctx := goctx.TODO()

	Context("When doing valid operations", func() {

		clusterName := "rack-enabled"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		// This needs to be changed based on setup. update zone, region, nodeName according to setup
		racks := []asdbv1alpha1.Rack{
			{ID: 1, Zone: zone1, Region: region},
			{ID: 2, Zone: zone2, Region: region}}
		rackConf := asdbv1alpha1.RackConfig{
			Racks: racks,
		}
		aeroCluster.Spec.RackConfig = rackConf

		It("Should validate rack enabled cluster flow", func() {
			// Op1: deploy
			By("Deploying rack enabled cluster")

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op2: scale up
			By("Scaling up the cluster")

			err = scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op3: scale down
			By("Scaling down the cluster")

			err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op4: rolling restart
			By("RollingRestart the cluster")

			err = rollingRestartClusterTest(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op5: upgrade/downgrade
			By("Upgrade/Downgrade the cluster")

			// dont change image, it upgrade, check old version
			err = upgradeClusterTest(k8sClient, ctx, clusterNamespacedName, imageToUpgrade)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// cleanup: Remove the cluster
			By("Cleaning up the cluster")

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
