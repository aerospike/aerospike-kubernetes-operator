package cluster

import (
	goctx "context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = FDescribe("AutoScaler", func() {
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
				// Testing over upgrade as it is a long-running operation
				By("Scale up the cluster")

				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				gvr := schema.GroupVersionResource{
					Group:    "asdb.aerospike.com", // Replace with your CRD group
					Version:  "v1",                 // API version
					Resource: "aerospikeclusters",  // Replace with your resource
				}

				scale, err := dynamicClient.Resource(gvr).Namespace(aeroCluster.Namespace).Get(context.TODO(),
					aeroCluster.GetName(), metav1.GetOptions{}, "scale")
				Expect(err).ToNot(HaveOccurred())

				Expect(scale.Object["spec"].(map[string]interface{})["replicas"]).To(Equal(int64(2)))

				scale.Object["spec"].(map[string]interface{})["replicas"] = 3

				_, err = dynamicClient.Resource(gvr).Namespace(aeroCluster.Namespace).Update(context.TODO(),
					scale, metav1.UpdateOptions{}, "scale")
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func() int32 {
						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						return aeroCluster.Spec.Size
					}, 1*time.Minute,
				).Should(Equal(int32(3)))
			},
		)
	})
})
