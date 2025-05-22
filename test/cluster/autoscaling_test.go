package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe("AutoScaler", func() {
	ctx := goctx.TODO()

	Context("When doing scale operations", func() {
		clusterName := fmt.Sprintf("autoscale-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

		BeforeEach(
			func() {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
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
		func() int {
			aeroCluster, err := getCluster(k8sClient, goctx.TODO(), clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			return int(aeroCluster.Spec.Size)
		}, time.Minute, time.Second,
	).Should(Equal(desiredSize))
}
