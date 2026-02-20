package envtests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
)

const (
	clusterNameConfig = "cluster-name"
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

	It("InvalidStorage: should fail when storage volumes are missing", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		aeroCluster.Spec.Storage.Volumes = nil // Remove volumes

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"namespace storage device related devicePath /test/dev/xvdf not found in Storage config",
				"<nil>", "deleteFiles deleteFiles false}",
				"{<nil> <nil>", "none dd false} 1 [] <nil> []}").
			Validate(err)
	})

	It("InvalidStorage: should fail for nil storage-engine", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace]
		namespaces, ok := rawNs.([]interface{})
		if !ok || len(namespaces) == 0 {
			Fail("Namespace configuration is missing or not a slice")
		}

		namespaceConfig := namespaces[0].(map[string]interface{})
		namespaceConfig["storage-engine"] = nil
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"storage-engine cannot be nil for namespace map[name:test replication-factor:2 storage-engine:<nil>",
				"strong-consistency:true]").
			Validate(err)
	})

	It("InvalidStorage: NilStorageEngineDevice - should fail for nil storage-engine.device", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace]
		namespaces, ok := rawNs.([]interface{})
		if !ok || len(namespaces) == 0 {
			Fail("Namespace configuration is missing or not a slice")
		}

		namespaceConfig := namespaces[0].(map[string]interface{})
		if storageEngine, ok := namespaceConfig["storage-engine"].(map[string]interface{}); ok {
			// Force the invalid state
			storageEngine["devices"] = nil

			// Re-assign back up the chain to ensure the pointer/reference is updated
			namespaceConfig["storage-engine"] = storageEngine
		}
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"aerospikeConfig not valid: generated config not valid for version",
				"config schema error",
				"{map[devices:<nil> type:device] number_one_of (root).",
				"namespaces.0.storage-engine Must validate one and only one schema (oneOf) namespaces.0.storage-engine}",
				"{<nil> invalid_type (root).namespaces.0.storage-engine.devices Invalid type.",
				"Expected: array, given: null namespaces.0.storage-engine.devices}").
			Validate(err)
		// }
	})

	It("InvalidStorage: NilStorageEngineFile - should fail for nil storage-engine.file", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)

		// Navigate the AerospikeConfig.Value structure
		config := aeroCluster.Spec.AerospikeConfig.Value
		if namespaces, ok := config[asdbv1.ConfKeyNamespace].([]interface{}); ok && len(namespaces) > 0 {
			ns := namespaces[0].(map[string]interface{})

			if storageEngine, ok := ns["storage-engine"].(map[string]interface{}); ok {
				// Force the invalid state
				storageEngine["files"] = nil

				// Re-assign back up the chain to ensure the pointer/reference is updated
				ns["storage-engine"] = storageEngine
				namespaces[0] = ns
				config[asdbv1.ConfKeyNamespace] = namespaces
			}
		}

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				" aerospikeConfig not valid: generated config not valid for version",
				"config schema error [\t{map[devices:[/test/dev/xvdf] files:<nil> type:device]",
				"number_one_of (root).namespaces.0.storage-engine Must validate one and only one schema",
				"(oneOf) namespaces.0.storage-engine}\n \t{map[devices:[/test/dev/xvdf] files:<nil> type:device]",
				"number_one_of (root).namespaces.0.storage-engine Must validate one and only one schema",
				"(oneOf) namespaces.0.storage-engine}\n \t{<nil> invalid_type (root).namespaces.0.storage-engine.",
				"files Invalid type. Expected: array, given: null namespaces.0.storage-engine.files}\n]").
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

	It("InvalidNamespace: should fail when namespace configuration is empty", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)

		// Set Namepsace to nil
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook",
				"\"maerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig.namespaces cannot be nil").
			Validate(err)
	})

	It("EmptyClusterName: should fail for EmptyClusterName", func() {
		cName := test.GetNamespacedName("", clusterNamespacedName.Namespace)
		aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
			WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
				"\"\" is invalid:",
				"metadata.name: Required value: name or generateName is required").
			WithCauses(metav1.StatusCause{
				Type:    metav1.CauseTypeFieldValueRequired,
				Message: "Required value: name or generateName is required",
				Field:   "metadata.name",
			}).
			Validate(err)
	})

	It("InvalidRackConfig: should fail for negative rack ID", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		// Add a rack with an ID that is too large or invalid structure
		aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
			Namespaces: []string{"test"},
			Racks: []asdbv1.Rack{
				{ID: -1}, // Negative rack ID
			},
		}

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"aerospikeConfig not valid: generated config not valid for version",
				"config schema error",
				"{-1 number_gte (root).namespaces.0.rack-id Must be greater than or equal to 0 namespaces.0.rack-id}").
			Validate(err)
	})

	It("InvalidReplicationFactor: should fail when replication factor exceeds cluster size", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1) // Size 1

		// 1. Get the namespaces slice from the Value map
		rawNamespaces := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})

		// 2. Modify the first namespace's replication factor
		if len(rawNamespaces) > 0 {
			nsMap := rawNamespaces[0].(map[string]interface{})
			nsMap["replication-factor"] = 3
		}

		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
				"denied the request: strong-consistency namespace replication-factor 3 cannot be more than cluster size 1").
			Validate(err)
	})

	It("InvalidAerospikeConfig: should fail for empty aerospikeConfig", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook ",
				"\"maerospikecluster.kb.io\"",
				"denied the request: spec.aerospikeConfig cannot be nil").
			Validate(err)
	})

	It("InvalidAerospikeConfig: should fail for invalid aerospikeConfig", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{
			Value: map[string]interface{}{
				asdbv1.ConfKeyNamespace: "invalidConf",
			},
		}
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook ",
				"\"maerospikecluster.kb.io\"",
				"denied the request: aerospikeConfig.namespaces not valid namespace list invalidConf").
			Validate(err)
	})

	It("AerospikeConfig-ServiceConf: should fail for setting advertise-ipv6", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["advertise-ipv6"] = true

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
				"denied the request: advertise-ipv6 is not supported").
			Validate(err)
	})

	It("ChangeDefaultConfig-ServiceConf: should fail for setting node-id/cluster-name", func() {
		// Service conf: "node-id"
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a1"

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"maerospikecluster.kb.io\"",
				"denied the request: failed to set default aerospikeConfig.service config:",
				"config node-id can not have non-default value (string a1).",
				"It will be set internally (string ENV_NODE_ID)").
			Validate(err)
	})

	It("ChangeDefaultConfig-ServiceConf: should fail for setting cluster-name", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
		// 1. Extract the service configuration map
		serviceConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})

		// 2. Assign the value to the specific key
		serviceConf[clusterNameConfig] = clusterNameConfig

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"maerospikecluster.kb.io\"",
				"denied the request: failed to set default aerospikeConfig.service config:",
				"config cluster-name can not have non-default value (string cluster-name).",
				"It will be set internally (string invalid-size-cluster)").
			Validate(err)
	})

	It("InvalidDNSConfiguration: InvalidDnsPolicy: should fail when dnsPolicy is set to 'Default'", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		defaultDNS := v1.DNSDefault
		aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
				"denied the request: dnsPolicy: Default is not supported").
			Validate(err)
	})

	It("InvalidDNSConfiguration: should fail when dnsPolicy is set to 'None' and no dnsConfig given", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		noneDNS := v1.DNSNone
		aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"dnsConfig is required field when dnsPolicy is set to None").
			Validate(err)
	})

	It("InvalidLogging: should fail for using syslog param with file or console logging", func() {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		loggingConf := []interface{}{
			map[string]interface{}{
				"name":     "anyFileName",
				"path":     "/dev/log",
				"tag":      "asd",
				"facility": "local0",
			},
		}
		aeroCluster.Spec.AerospikeConfig.Value["logging"] = loggingConf

		// Deploy cluster
		err := testCluster.DeployCluster(k8sClient, ctx, aeroCluster)
		Expect(err).To(HaveOccurred())

		// Webhook response validation
		NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
			WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
				"can use facility only with `syslog` in aerospikeConfig.logging",
				"map[facility:local0 name:anyFileName path:/dev/log tag:asd]").
			Validate(err)
	})
})
