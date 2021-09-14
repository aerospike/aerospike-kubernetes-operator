package test

import (
	goctx "context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("DeployMultiCluster", func() {
	ctx := goctx.TODO()

	Context("When DeployMultiClusterMultiNs", func() {
		// 1st cluster
		clusterName1 := "multicluster"
		clusterNamespacedName1 := getClusterNamespacedName(clusterName1, multiClusterNs1)

		// 2nd cluster
		clusterName2 := "multicluster"
		clusterNamespacedName2 := getClusterNamespacedName(clusterName2, multiClusterNs2)

		Context("multiClusterGenChangeTest", func() {
			multiClusterGenChangeTest(ctx, clusterNamespacedName1, clusterNamespacedName2)
		})
		Context("multiClusterPVCTest", func() {
			multiClusterPVCTest(ctx, clusterNamespacedName1, clusterNamespacedName2)
		})
	})

	Context("When DeployMultiClusterSingleNsTest", func() {
		// 1st cluster
		clusterName1 := "multicluster1"
		clusterNamespacedName1 := getClusterNamespacedName(clusterName1, multiClusterNs1)

		// 2nd cluster
		clusterName2 := "multicluster2"
		clusterNamespacedName2 := getClusterNamespacedName(clusterName2, multiClusterNs1)

		Context("multiClusterGenChangeTest", func() {
			multiClusterGenChangeTest(ctx, clusterNamespacedName1, clusterNamespacedName2)
		})
		Context("multiClusterPVCTest", func() {
			multiClusterPVCTest(ctx, clusterNamespacedName1, clusterNamespacedName2)
		})
	})
})

// multiClusterGenChangeTest tests if state of one cluster gets impacted by another
func multiClusterGenChangeTest(ctx goctx.Context, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)

	It("multiClusterGenChangeTest", func() {
		// Deploy 1st cluster
		err := deployCluster(k8sClient, ctx, aeroCluster1)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster1, err = getCluster(k8sClient, ctx, clusterNamespacedName1)
		Expect(err).ToNot(HaveOccurred())

		// Deploy 2nd cluster and run lifecycle ops
		aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
		err = deployCluster(k8sClient, ctx, aeroCluster2)
		Expect(err).ToNot(HaveOccurred())

		validateLifecycleOperation(ctx, clusterNamespacedName2)

		deleteCluster(k8sClient, ctx, aeroCluster2)

		// Validate if there is any change in aeroCluster1
		newaeroCluster1, err := getCluster(k8sClient, ctx, clusterNamespacedName1)
		Expect(err).ToNot(HaveOccurred())

		Expect(aeroCluster1.Generation).To(Equal(newaeroCluster1.Generation), "Generation for cluster1 is changed affter deleting cluster2")

		deleteCluster(k8sClient, ctx, aeroCluster1)
	})
}

// multiClusterPVCTest tests if pvc of one cluster gets impacted by another
func multiClusterPVCTest(ctx goctx.Context, clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName) {
	cascadeDelete := true
	aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)

	It("multiClusterPVCTest", func() {
		// Deploy 1st cluster
		aeroCluster1.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
		aeroCluster1.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
		err := deployCluster(k8sClient, ctx, aeroCluster1)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster1, err = getCluster(k8sClient, ctx, clusterNamespacedName1)
		Expect(err).ToNot(HaveOccurred())

		aeroClusterPVCList1, err := getAeroClusterPVCList(aeroCluster1, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		// Deploy 2nd cluster
		aeroCluster2 := createDummyAerospikeCluster(clusterNamespacedName2, 2)
		aeroCluster2.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
		aeroCluster2.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
		err = deployCluster(k8sClient, ctx, aeroCluster2)
		Expect(err).ToNot(HaveOccurred())

		// Validate 1st cluster pvc before delete
		newaeroCluster1, err := getCluster(k8sClient, ctx, clusterNamespacedName1)
		Expect(err).ToNot(HaveOccurred())

		newaeroClusterPVCList1, err := getAeroClusterPVCList(newaeroCluster1, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		err = matchPVCList(aeroClusterPVCList1, newaeroClusterPVCList1)
		Expect(err).ToNot(HaveOccurred())

		// Delete 2nd cluster
		deleteCluster(k8sClient, ctx, aeroCluster2)

		// Validate 1st cluster pvc after delete
		newaeroCluster1, err = getCluster(k8sClient, ctx, clusterNamespacedName1)
		Expect(err).ToNot(HaveOccurred())

		newaeroClusterPVCList1, err = getAeroClusterPVCList(newaeroCluster1, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		err = matchPVCList(aeroClusterPVCList1, newaeroClusterPVCList1)
		Expect(err).ToNot(HaveOccurred())

		// Delete 1st cluster
		deleteCluster(k8sClient, ctx, aeroCluster1)
	})
}

func validateLifecycleOperation(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	By("Scaleup")
	err := scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)

	By("ScaleDown")
	err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)

	By("RollingRestart")
	err = rollingRestartClusterTest(&logger, k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)

	By("Upgrade/Downgrade")
	// dont change image, it upgrade, check old version
	err = upgradeClusterTest(k8sClient, ctx, clusterNamespacedName, imageToUpgrade)
	Expect(err).ToNot(HaveOccurred())

	validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
}
