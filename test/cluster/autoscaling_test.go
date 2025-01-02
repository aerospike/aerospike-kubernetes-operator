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
)

var _ = Describe("AutoScaler", func() {
	ctx := goctx.TODO()

	Context("When doing scale operations", func() {
		clusterName := "autoscale"
		clusterNamespacedName := getNamespacedName(
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
				aeroCluster, err := getCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())

				_ = deleteCluster(k8sClient, ctx, aeroCluster)
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

	scale, err := dynamicClient.Resource(gvr).Namespace(clusterNamespacedName.Namespace).Get(goctx.TODO(),
		clusterNamespacedName.Name, metav1.GetOptions{}, "scale")
	Expect(err).ToNot(HaveOccurred())

	Expect(scale.Object["spec"].(map[string]interface{})["replicas"]).To(Equal(int64(currentSize)))

	scale.Object["spec"].(map[string]interface{})["replicas"] = desiredSize

	_, err = dynamicClient.Resource(gvr).Namespace(clusterNamespacedName.Namespace).Update(goctx.TODO(),
		scale, metav1.UpdateOptions{}, "scale")
	Expect(err).ToNot(HaveOccurred())

	Eventually(
		func() int32 {
			aeroCluster, err := getCluster(k8sClient, goctx.TODO(), clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			return aeroCluster.Spec.Size
		}, 1*time.Minute,
	).Should(Equal(int32(desiredSize)))
}
