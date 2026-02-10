package envtests

import (
	"context"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	. "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("AerospikeCluster validation (envtests)", func() {
	const (
		clusterName = "invalid-size-cluster"
		testNs      = "default" // use same test namespace as suite_test.go
	)

	ctx := context.TODO()

	// Create namespaced name for cluster
	clusterNamespacedName := test.GetNamespacedName(clusterName, testNs)

	Context("DeployValidation", func() {
		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			// Delete the cluster after each test
			Expect(k8sClient.Delete(ctx, aeroCluster)).Should(Succeed())
		})
	})

	It("InvalidSize: should fail for zero size", func() {
		// Create AerospikeCluster with invalid size (0)
		aeroCluster := CreateDummyAerospikeCluster(clusterNamespacedName, 0)
		// Size = 0 (invalid!) - webhook will reject this
		// Expects error because size is invalid
		Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
	})

	It("InvalidSize: should fail for negative size", func() {
		aeroCluster := CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.Size = -1
		Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
	})
})
