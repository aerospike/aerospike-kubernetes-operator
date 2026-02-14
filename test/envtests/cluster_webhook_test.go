package envtests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
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
			err := k8sClient.Delete(ctx, aeroCluster)
			Expect(err).To(Or(Succeed(), MatchError(errors.IsNotFound, "should be NotFound or Succeed")))
		})
	})

	It("InvalidSize: should fail for zero size", func() {
		// Create AerospikeCluster with invalid size (0)
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 0)

		// Deploy the cluster and expect an error due to invalid cluster size.
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)

		Expect(err).To(HaveOccurred())

		// 1. Cast the error to a StatusError pointer
		statusErr, ok := err.(*errors.StatusError)
		Expect(ok).To(BeTrue(), "Error should be a Kubernetes StatusError")

		// 2. Validate the specific fields from response
		Expect(statusErr.ErrStatus.Status).To(Equal(metav1.StatusFailure))
		Expect(statusErr.ErrStatus.Code).To(Equal(int32(403)))
		Expect(statusErr.ErrStatus.Reason).To(Equal(metav1.StatusReasonForbidden))

		// 3. Validate the specific message from response
		Expect(statusErr.ErrStatus.Message).To(ContainSubstring("admission webhook "))
		Expect(statusErr.ErrStatus.Message).To(ContainSubstring("\"vaerospikecluster.kb.io\""))
		Expect(statusErr.ErrStatus.Message).To(ContainSubstring("denied the request: invalid cluster size 0"))

	})

	It("InvalidSize: should fail for negative size", func() {
		// Create AerospikeCluster with invalid size (-1)
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, -1)
		aeroCluster.Spec.Size = -1

		// Deploy the cluster and expect an error due to invalid cluster size (negative integer).
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)

		Expect(err).To(HaveOccurred())

		// Cast to the specific Kubernetes error type
		statusErr, ok := err.(*errors.StatusError)
		Expect(ok).To(BeTrue(), "Expected a Kubernetes StatusError")

		// Verify the response status
		Expect(statusErr.ErrStatus.Status).To(Equal(metav1.StatusFailure))
		Expect(statusErr.ErrStatus.Code).To(Equal(int32(422)))
		Expect(statusErr.ErrStatus.Reason).To(Equal(metav1.StatusReasonInvalid))

		// Verify the specific validation detail (The 'Causes' slice)
		Expect(statusErr.ErrStatus.Details.Causes).To(ContainElement(metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: "Invalid value: -1: should be a non-negative integer",
			Field:   ".spec.size",
		}))
	})
})
