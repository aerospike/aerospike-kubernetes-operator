package envtests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

var _ = Describe("AerospikeCluster validation (envtests)", func() {
	const (
		clusterName = "invalid-size-cluster"
		testNs      = "default" // use same test namespace as suite_test.go
	)

	ctx := context.Background()

	It("InvalidSize: should fail for zero size", func() {
		aero := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: testNs,
			},
			Spec: asdbv1.AerospikeClusterSpec{
				// Minimal required fields; set Size=0 to trigger validation
				Size: 0,
				// ensure mandatory fields exist (image, storage, config) if your CRD requires them:
				Image: "aerospike/aerospike-server-enterprise:latest",
			},
		}

		// If validation is enforced by admission webhook or operator running in envtest,
		// Create should return an error. Otherwise Create may succeed and operator reconciler
		// would set status/errors — adjust assertions accordingly.
		err := k8sClient.Create(ctx, aero)
		Expect(err).To(HaveOccurred(), "create should fail for size=0")
	})
})

//cd /Users/rpandey/Projects/aerospike-kubernetes-operator
//make env-test ARGS="--focus=\"InvalidSize\""
// make env-test ARGS="--focus=\"AerospikeCluster validation (envtests)\""
