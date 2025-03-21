package cluster

import (
	goctx "context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var _ = Describe("AutoScaler", func() {
	ctx := goctx.TODO()

	Context("When doing scale operations", func() {
		clusterName := "autoscale"
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
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
				aeroCluster := &asdbv1.AerospikeCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: namespace,
					},
				}

				_ = deleteCluster(k8sClient, ctx, aeroCluster)
				_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
			},
		)

		It(
			"Should trigger scale up/down via scale subresource", func() {
				By("Scale up the cluster")
				validateScaleSubresourceOperation(2, 3, clusterNamespacedName)

				By("Scale down the cluster")
				validateScaleSubresourceOperation(3, 2, clusterNamespacedName)
			},
		)
	})
})

func validateScaleSubresourceOperation(currentSize, desiredSize int, clusterNamespacedName types.NamespacedName) {
	gvr := schema.GroupVersionResource{
		Group:    "asdb.aerospike.com", // Replace with your CRD group
		Version:  "v1",                 // API version
		Resource: "aerospikeclusters",  // Replace with your resource
	}

	dynamicClient := dynamic.NewForConfigOrDie(cfg)
	Expect(dynamicClient).ToNot(BeNil())

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		scale, err := dynamicClient.Resource(gvr).Namespace(clusterNamespacedName.Namespace).Get(goctx.TODO(),
			clusterNamespacedName.Name, metav1.GetOptions{}, "scale")
		Expect(err).ToNot(HaveOccurred())

		Expect(scale.Object["spec"].(map[string]interface{})["replicas"]).To(Equal(int64(currentSize)))

		scale.Object["spec"].(map[string]interface{})["replicas"] = desiredSize
		_, err = dynamicClient.Resource(gvr).Namespace(clusterNamespacedName.Namespace).Update(goctx.TODO(),
			scale, metav1.UpdateOptions{}, "scale")

		return err
	})
	Expect(err).ToNot(HaveOccurred())

	Eventually(
		func() int32 {
			aeroCluster, err := getCluster(k8sClient, goctx.TODO(), clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			return aeroCluster.Spec.Size
		}, time.Minute, time.Second,
	).Should(Equal(int32(desiredSize)))
}
