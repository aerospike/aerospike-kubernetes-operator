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

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings(
				"admission webhook ",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: invalid cluster size 0",
			).
			Validate(err)
	})

	It("InvalidSize: should fail for negative size", func() {
		// Create AerospikeCluster with invalid size (-1)
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, -1)
		aeroCluster.Spec.Size = -1

		// Deploy the cluster and expect an error due to invalid cluster size (negative integer).
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)

		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
			WithCauses(metav1.StatusCause{
				Type:    metav1.CauseTypeFieldValueInvalid,
				Message: "Invalid value: -1: should be a non-negative integer",
				Field:   ".spec.size",
			}).
			Validate(err)
	})
	// Bug: For Empty image the error message is "CommunityEdition Cluster not supported".
	// This error messsage is not appropriate for an empty image.
	It("InvalidImage: should fail for empty image", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.Image = "" // Empty image

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: CommunityEdition Cluster not supported").
			Validate(err)
	})

	It("InvalidImage: should fail for invalid image format", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.Image = "aerospike/nosuchimage:latest@invalid-digest"

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: CommunityEdition Cluster not supported").
			Validate(err)
	})

	// Bug: Failure message string is redundant.
	It("InvalidStorage: should fail when storage volumes are missing", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.Storage.Volumes = nil // Remove volumes

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig not valid: failed to validate config for the version 8.1.1.0:",
				"failed to get aerospike config schema for version 8.1.1.0: unsupported version").
			Validate(err)
	})
	// Bug: aerospikeConfig map should not be listed in Failure message.
	// It is making failure message unnecessarily long.
	//  Message: "admission webhook \"maerospikecluster.kb.io\" denied the request: aerospikeConfig.namespaces not present.
	// aerospikeConfig map[network:map[fabric:map[port:3001] heartbeat:map[port:3002] service:map[port:3000]]
	// security:map[] service:map[auto-pin:none
	// feature-key-file:/etc/aerospike/secret/features.conf proto-fd-max:15000]]",
	It("InvalidNamespace: should fail when namespace configuration is missing", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		// Remove namespaces from AerospikeConfigSpec
		delete(aeroCluster.Spec.AerospikeConfig.Value, "namespaces")

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"maerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig.namespaces not present.").
			Validate(err)
	})

	It("InvalidRackConfig: should fail for invalid rack configuration", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		// Add a rack with an ID that is too large or invalid structure
		aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
			Namespaces: []string{"test"},
			Racks: []asdbv1.Rack{
				{ID: 0}, // ID 0 is often reserved or invalid depending on version
			},
		}

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig not valid:",
				"failed to validate config for the version 8.1.1.0:",
				"failed to get aerospike config schema for version 8.1.1.0:",
				"unsupported version").
			Validate(err)
	})

	It("InvalidReplicationFactor: should fail when replication factor exceeds cluster size", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1) // Size 1

		// 1. Get the namespaces slice from the Value map
		rawNamespaces := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})

		// 2. Modify the first namespace's replication factor
		if len(rawNamespaces) > 0 {
			nsMap := rawNamespaces[0].(map[string]interface{})
			nsMap["replication-factor"] = 2
		}

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"vaerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig not valid:",
				"failed to validate config for the version 8.1.1.0:",
				"failed to get aerospike config schema for version 8.1.1.0: unsupported version").
			Validate(err)
	})
})
