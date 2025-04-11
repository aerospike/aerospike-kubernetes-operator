package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"DeployMultiCluster", func() {
		ctx := goctx.TODO()

		Context(
			"When DeployMultiClusterMultiNs", func() {
				// 1st cluster
				clusterName1 := fmt.Sprintf("multicluster-%d", GinkgoParallelProcess())
				clusterNamespacedName1 := test.GetNamespacedName(
					clusterName1, test.MultiClusterNs1,
				)

				// 2nd cluster
				clusterName2 := fmt.Sprintf("multicluster-%d", GinkgoParallelProcess())
				clusterNamespacedName2 := test.GetNamespacedName(
					clusterName2, test.MultiClusterNs2,
				)

				Context(
					"multiClusterGenChangeTest", func() {
						multiClusterGenChangeTest(
							ctx, clusterNamespacedName1, clusterNamespacedName2,
						)
					},
				)
				Context(
					"multiClusterPVCTest", func() {
						multiClusterPVCTest(
							ctx, clusterNamespacedName1, clusterNamespacedName2,
						)
					},
				)
			},
		)

		Context(
			"When DeployMultiClusterSingleNsTest", func() {
				// 1st cluster
				clusterName1 := fmt.Sprintf("multicluster1-%d", GinkgoParallelProcess())
				clusterNamespacedName1 := test.GetNamespacedName(
					clusterName1, test.MultiClusterNs1,
				)

				// 2nd cluster
				clusterName2 := fmt.Sprintf("multicluster2-%d", GinkgoParallelProcess())
				clusterNamespacedName2 := test.GetNamespacedName(
					clusterName2, test.MultiClusterNs1,
				)

				Context(
					"multiClusterGenChangeTest", func() {
						multiClusterGenChangeTest(
							ctx, clusterNamespacedName1, clusterNamespacedName2,
						)
					},
				)
				Context(
					"multiClusterPVCTest", func() {
						multiClusterPVCTest(
							ctx, clusterNamespacedName1, clusterNamespacedName2,
						)
					},
				)
			},
		)
	},
)

// multiClusterGenChangeTest tests if state of one cluster gets impacted by another
func multiClusterGenChangeTest(
	ctx goctx.Context,
	clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName,
) {
	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName1.Name,
					Namespace: clusterNamespacedName1.Namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())

			aeroCluster.Name = clusterNamespacedName2.Name
			aeroCluster.Namespace = clusterNamespacedName2.Namespace

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	It(
		"multiClusterGenChangeTest", func() {
			// Deploy 1st cluster
			aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster1)).ToNot(HaveOccurred())

			aeroCluster1, err := getCluster(
				k8sClient, ctx, clusterNamespacedName1,
			)
			Expect(err).ToNot(HaveOccurred())

			// Deploy 2nd cluster and run lifecycle ops
			aeroCluster2 := createDummyAerospikeCluster(
				clusterNamespacedName2, 2,
			)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster2)).ToNot(HaveOccurred())

			validateLifecycleOperationInRackCluster(ctx, clusterNamespacedName2)

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster2)).ToNot(HaveOccurred())

			// Validate if there is any change in aeroCluster1
			newaeroCluster1, err := getCluster(
				k8sClient, ctx, clusterNamespacedName1,
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(aeroCluster1.Generation).To(
				Equal(newaeroCluster1.Generation),
				"Generation for cluster1 is changed after deleting cluster2",
			)
		},
	)
}

// multiClusterPVCTest tests if pvc of one cluster gets impacted by another
func multiClusterPVCTest(
	ctx goctx.Context,
	clusterNamespacedName1, clusterNamespacedName2 types.NamespacedName,
) {
	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName1.Name,
					Namespace: clusterNamespacedName1.Namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())

			aeroCluster.Name = clusterNamespacedName2.Name
			aeroCluster.Namespace = clusterNamespacedName2.Namespace

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)

	It(
		"multiClusterPVCTest", func() {
			// Deploy 1st cluster
			cascadeDelete := true
			aeroCluster1 := createDummyAerospikeCluster(clusterNamespacedName1, 2)
			aeroCluster1.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
			aeroCluster1.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
			Expect(DeployCluster(k8sClient, ctx, aeroCluster1)).ToNot(HaveOccurred())

			aeroCluster1, err := getCluster(
				k8sClient, ctx, clusterNamespacedName1,
			)
			Expect(err).ToNot(HaveOccurred())

			aeroClusterPVCList1, err := getAeroClusterPVCList(
				aeroCluster1, k8sClient,
			)
			Expect(err).ToNot(HaveOccurred())

			// Deploy 2nd cluster
			aeroCluster2 := createDummyAerospikeCluster(
				clusterNamespacedName2, 2,
			)
			aeroCluster2.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDelete
			aeroCluster2.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDelete
			Expect(DeployCluster(k8sClient, ctx, aeroCluster2)).ToNot(HaveOccurred())

			// Validate 1st cluster pvc before delete
			newAeroCluster1, err := getCluster(
				k8sClient, ctx, clusterNamespacedName1,
			)
			Expect(err).ToNot(HaveOccurred())

			newAeroClusterPVCList1, err := getAeroClusterPVCList(
				newAeroCluster1, k8sClient,
			)
			Expect(err).ToNot(HaveOccurred())

			err = matchPVCList(aeroClusterPVCList1, newAeroClusterPVCList1)
			Expect(err).ToNot(HaveOccurred())

			// Delete 2nd cluster
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster2)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster2.Namespace, aeroCluster2.Name)).ToNot(HaveOccurred())

			// Validate 1st cluster pvc after delete
			newAeroCluster1, err = getCluster(
				k8sClient, ctx, clusterNamespacedName1,
			)
			Expect(err).ToNot(HaveOccurred())

			newAeroClusterPVCList1, err = getAeroClusterPVCList(
				newAeroCluster1, k8sClient,
			)
			Expect(err).ToNot(HaveOccurred())

			err = matchPVCList(aeroClusterPVCList1, newAeroClusterPVCList1)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func validateLifecycleOperationInRackCluster(
	ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) {
	By("Scaleup")

	err := scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	By("ScaleDown")

	err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	By("RollingRestart")

	err = rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	By("Upgrade/Downgrade")
	// don't change image, it upgrades, check old version
	err = upgradeClusterTest(
		k8sClient, ctx, clusterNamespacedName, nextImage,
	)
	Expect(err).ToNot(HaveOccurred())

	err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())
}
