package test

import (
	goctx "context"
	"fmt"

	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

const batchScaleDownClusterName = "batch-scaledown"

var _ = Describe("BatchScaleDown", func() {
	ctx := goctx.TODO()

	Context("When doing valid operations", func() {
		clusterNamespacedName := getNamespacedName(
			batchScaleDownClusterName, namespace,
		)
		aeroCluster := &asdbv1.AerospikeCluster{}

		BeforeEach(
			func() {
				aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 8)
				racks := getDummyRackConf(1, 2)
				aeroCluster.Spec.RackConfig.Racks = racks
				aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		AfterEach(
			func() {
				Expect(deleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			},
		)

		It("Should scale-down one pod at a time", func() {
			By("Using default ScaleDownBatchSize PCT/ScaleDownBatchSize Count")
			err := batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, nil, 2)
			Expect(err).ToNot(HaveOccurred())

			By("Using ScaleDownBatchSize PCT which is not enough eg. 1%")
			err = batchScaleDownTest(k8sClient, ctx, clusterNamespacedName, percent("1%"), 1)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should do ScaleDownBatch when ScaleDownBatchSize is greater than the actual numbers of pods per rack"+
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
			isRackBatchDelete := isRackBatchDelete(aeroCluster, scaleDownBatchSize, 2)
			Expect(isRackBatchDelete).To(BeTrue())

			err = waitForClusterScaleDown(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
				getTimeout(aeroCluster.Spec.Size),
			)
			Expect(err).ToNot(HaveOccurred())

		})
	})

	// TODO: Do we need to add all the invalid operation test-cases here?
	// Skipped for now as they are exactly same as RollingUpdateBatchSize invalid operation test-cases
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

	oldPodsPerRack := int(aeroCluster.Spec.Size) / len(aeroCluster.Spec.RackConfig.Racks)
	aeroCluster.Spec.RackConfig.ScaleDownBatchSize = batchSize
	aeroCluster.Spec.Size -= decreaseBy
	newPodsPerRack := int(aeroCluster.Spec.Size) / len(aeroCluster.Spec.RackConfig.Racks)
	scaleDownBatchSize := oldPodsPerRack - newPodsPerRack

	if batchSize != nil && batchSize.IntVal > 0 && batchSize.IntVal < int32(scaleDownBatchSize) {
		scaleDownBatchSize = int(batchSize.IntVal)
	}

	if err := k8sClient.Update(ctx, aeroCluster); err != nil {
		return err
	}

	By("Validating batch scale-down")

	if !isBatchScaleDown(aeroCluster, scaleDownBatchSize) {
		return fmt.Errorf("looks like pods are not scaling down in batch")
	}

	return waitForClusterScaleDown(
		k8sClient, ctx, aeroCluster,
		int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func isBatchScaleDown(aeroCluster *asdbv1.AerospikeCluster, scaleDownBatchSize int) bool {
	oldSize := int(aeroCluster.Status.Size)

	// Wait for scale-down
	for {
		readyPods := getReadyPods(aeroCluster)

		unreadyPods := oldSize - len(readyPods)
		if unreadyPods > 0 {
			break
		}
	}

	// Operator should scale down multiple pods at a time
	for i := 0; i < 100; i++ {
		readyPods := getReadyPods(aeroCluster)
		unreadyPods := oldSize - len(readyPods)

		if unreadyPods == scaleDownBatchSize {
			return true
		}
	}

	return false
}

func isRackBatchDelete(aeroCluster *asdbv1.AerospikeCluster, scaleDownBatchSize, rackID int) bool {
	oldSize := aeroCluster.Status.Size

	// Rack to be deleted
	sts, err := getSTSFromRackID(aeroCluster, rackID)
	Expect(err).ToNot(HaveOccurred())

	newSize := int(oldSize + *sts.Spec.Replicas)

	// Wait for new rack addition
	for {
		readyPods := getReadyPods(aeroCluster)

		// This means the new rack is added before deleting the old rack
		if len(readyPods) == newSize {
			break
		}
	}

	// Operator should scale down multiple pods at a time for the rack to be deleted
	for i := 0; i < 100; i++ {
		readyPods := getReadyPods(aeroCluster)
		unreadyPods := newSize - len(readyPods)

		if unreadyPods == scaleDownBatchSize {
			return true
		}

		time.Sleep(1 * time.Second)
	}

	return false
}
