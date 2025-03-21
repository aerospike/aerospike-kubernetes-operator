package cluster

import (
	goctx "context"
	"fmt"
	"time"

	set "github.com/deckarep/golang-set/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

const batchScaleDownClusterName = "batch-scaledown"

var _ = Describe("BatchScaleDown", func() {
	ctx := goctx.TODO()
	clusterName := fmt.Sprintf(batchScaleDownClusterName+"-%d", GinkgoParallelProcess())
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
			Expect(deleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
		},
	)

	Context("When doing valid operations", func() {
		BeforeEach(
			func() {
				aeroCluster := createNonSCDummyAerospikeCluster(clusterNamespacedName, 8)
				racks := getDummyRackConf(1, 2)
				aeroCluster.Spec.RackConfig.Racks = racks
				aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It("Should scale-down one pod at a time", func() {
			By("Using default ScaleDownBatchSize PCT/ScaleDownBatchSize Count")
			err := batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, nil, 2)
			Expect(err).ToNot(HaveOccurred())

			By("Using ScaleDownBatchSize PCT which is not enough eg. 1%")
			err = batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, percent("1%"), 2)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should do ScaleDownBatch when ScaleDownBatchSize is greater than the actual numbers of pods per rack "+
			"to be scaled down", func() {
			err := batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, count(3), 4)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should do ScaleDownBatch when ScaleDownBatchSize is less than the actual number of pods per rack "+
			"to be scaled down", func() {
			err := batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, count(2), 6)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should remove pods of deleted rack in ScaleDownBatch size", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Remove rack with id 2
			scaleDownBatchSize := 4
			aeroCluster.Spec.RackConfig.Racks = getDummyRackConf(1, 3) // Remove rack 2
			aeroCluster.Spec.RackConfig.ScaleDownBatchSize = count(scaleDownBatchSize)
			err = k8sClient.Update(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validating batch scale-down for deleted rack 2")
			validateRackBatchDelete(aeroCluster, scaleDownBatchSize, 2)

			err = waitForClusterScaleDown(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(aeroCluster.Spec.Size),
			)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	// TODO: Do we need to add all the invalid operation test-cases here?
	// Skipped for now as they are exactly same as RollingUpdateBatchSize invalid operation test-cases
	Context("When doing invalid operations", func() {

		BeforeEach(
			func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 8)
				racks := getDummyRackConf(1, 2)
				aeroCluster.Spec.RackConfig.Racks = racks
				aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It("Should fail batch scale-down if SC namespace is present", func() {
			err := batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, count(3), 2)
			Expect(err).Should(HaveOccurred())
		})
	})
})

func batchScaleDownTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
	batchSize *intstr.IntOrString, decreaseBy int32,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.RackConfig.ScaleDownBatchSize = batchSize
	aeroCluster.Spec.Size -= decreaseBy

	if err := k8sClient.Update(ctx, aeroCluster); err != nil {
		return err
	}

	By("Validating batch scale-down")

	validateBatchScaleDown(aeroCluster, batchSize)

	return waitForClusterScaleDown(
		k8sClient, ctx, aeroCluster,
		int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func validateBatchScaleDown(aeroCluster *asdbv1.AerospikeCluster, batchSize *intstr.IntOrString) {
	oldPodsPerRack := podsPerRack(int(aeroCluster.Status.Size), len(aeroCluster.Status.RackConfig.Racks))
	newPodsPerRack := podsPerRack(int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks))

	rackTested := set.NewSet[int]()

	Eventually(func() bool {
		for idx := range aeroCluster.Spec.RackConfig.Racks {
			if rackTested.Contains(aeroCluster.Spec.RackConfig.Racks[idx].ID) {
				continue
			}

			scaleDownBatchSize := oldPodsPerRack[idx] - newPodsPerRack[idx]

			if batchSize != nil && batchSize.IntVal > 0 && batchSize.IntVal < int32(scaleDownBatchSize) {
				scaleDownBatchSize = int(batchSize.IntVal)
			}

			sts, err := getSTSFromRackID(aeroCluster, aeroCluster.Spec.RackConfig.Racks[idx].ID)
			Expect(err).ToNot(HaveOccurred())

			currentSize := int(*sts.Spec.Replicas)

			pkgLog.Info("Waiting for batch scale-down",
				"rack", aeroCluster.Spec.RackConfig.Racks[idx].ID,
				"batchSize", scaleDownBatchSize, "currentSize", currentSize, "oldSize", oldPodsPerRack[idx])

			if currentSize == oldPodsPerRack[idx] {
				return false
			}

			// Check if scale-down happened in batch
			if currentSize > oldPodsPerRack[idx]-scaleDownBatchSize {
				Fail("scale-down didn't happen in batch")
			}

			pkgLog.Info("Batch scale-down finished for rack", "rack", aeroCluster.Spec.RackConfig.Racks[idx].ID)
			rackTested.Add(aeroCluster.Spec.RackConfig.Racks[idx].ID)
		}

		return true
	}, getTimeout(aeroCluster.Spec.Size), 20*time.Second).Should(BeTrue())
}

func validateRackBatchDelete(aeroCluster *asdbv1.AerospikeCluster, scaleDownBatchSize, rackID int) {
	sts, err := getSTSFromRackID(aeroCluster, rackID)
	Expect(err).ToNot(HaveOccurred())

	oldSize := int(sts.Status.Replicas)

	Eventually(func() bool {
		sts, err = getSTSFromRackID(aeroCluster, rackID)
		if err != nil {
			if errors.IsNotFound(err) {
				pkgLog.Info("STS deleted", "rack", rackID)
				return true
			}

			Fail("failed to get sts")
		}

		currentSize := int(*sts.Spec.Replicas)

		pkgLog.Info("Waiting for batch scale-down for deleted rack",
			"rack", rackID,
			"batchSize", scaleDownBatchSize, "currentSize", currentSize, "oldSize", oldSize)

		if currentSize == oldSize {
			return false
		}

		if currentSize > oldSize-scaleDownBatchSize {
			Fail("scale-down didn't happen in batch")
		}

		pkgLog.Info("Batch scale-down finished for deleted rack", "rack", rackID)

		return true
	}, getTimeout(aeroCluster.Spec.Size), 20*time.Second).Should(BeTrue())
}

func podsPerRack(size, racks int) []int {
	nodesPerRack, extraNodes := size/racks, size%racks

	// Distributing nodes in given racks
	var topology []int

	for rackIdx := 0; rackIdx < racks; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}

		topology = append(topology, nodesForThisRack)
	}

	return topology
}
