package cluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
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
			err := envtests.K8sClient.Delete(ctx, aeroCluster)
			Expect(err).To(Or(Succeed(), MatchError(errors.IsNotFound, "should be NotFound or Succeed")))
		})
	})

	Context("negativeDeployClusterValidationTest", func() {
		It("InvalidSize: should fail for zero size", func() {
			// Create AerospikeCluster with invalid size (0)
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 0)

			// Deploy the cluster and expect an error due to invalid cluster size.
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)

			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
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
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)

			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
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

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: CommunityEdition Cluster not supported").
				Validate(err)
		})

		It("InvalidImage: should fail for invalid image format", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.Image = "aerospike/nosuchimage:latest@invalid-digest"

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: CommunityEdition Cluster not supported").
				Validate(err)
		})

		It("InvalidStorage: should fail when storage volumes are missing", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.Storage.Volumes = nil // Remove volumes

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
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
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
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

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"aerospikeConfig not valid: generated config not valid for version",
					"config schema error",
					"{map[devices:<nil> type:device] number_one_of (root).",
					"namespaces.0.storage-engine Must validate one and only one schema (oneOf) namespaces.0.storage-engine}",
					"{<nil> invalid_type (root).namespaces.0.storage-engine.devices Invalid type.",
					"Expected: array, given: null namespaces.0.storage-engine.devices}").
				Validate(err)
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

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
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

		It("InvalidStorage: InvalidStorageEngineDevice - should fail for invalid storage-engine.device,"+
			" cannot have 3 devices in single device string", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
			namespaceConfig := rawNs[0].(map[string]interface{})
			if _, ok :=
				namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
				aeroCluster.Spec.Storage.Volumes = []asdbv1.VolumeSpec{
					{
						Name: "nsvol1",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: testutil.StorageClass,
								VolumeMode:   v1.PersistentVolumeBlock,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/dev/xvdf1",
						},
					},
					{
						Name: "nsvol2",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: testutil.StorageClass,
								VolumeMode:   v1.PersistentVolumeBlock,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/dev/xvdf2",
						},
					},
					{
						Name: "nsvol3",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: testutil.StorageClass,
								VolumeMode:   v1.PersistentVolumeBlock,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/dev/xvdf3",
						},
					},
				}

				namespaceConfig :=
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0].(map[string]interface{})
				namespaceConfig["storage-engine"].(map[string]interface{})["devices"] =
					[]string{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"}
				aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				// Webhook response validation
				envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
					WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
						"invalid device name /dev/xvdf1 /dev/xvdf2 /dev/xvdf3.",
						"Max 2 device can be mentioned in single line (Shadow device config)").
					Validate(err)
			}
		})

		It("InvalidStorage: ExtraStorageEngineDevice - should fail for invalid storage-engine.device,"+
			" cannot use a device which doesn't exist in storage", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			rawNs := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
			namespaceConfig := rawNs[0].(map[string]interface{})
			if _, ok := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
				devList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"].([]interface{})
				devList = append(
					devList, "andRandomDevice",
				)
				namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = devList
				aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})[0] = namespaceConfig
				err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				// Webhook response validation
				envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
					WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
						"namespace storage device related devicePath andRandomDevice not found in Storage config").
					Validate(err)
			}
		})

		It("InvalidStorage: DuplicateStorageEngineDevice - should fail for invalid storage-engine.device,"+
			" cannot use a device which already exist in another namespace", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			secondNs := map[string]interface{}{
				"name":               "ns1",
				"replication-factor": 2,
				"storage-engine": map[string]interface{}{
					"type":    "device",
					"devices": []interface{}{"/test/dev/xvdf"},
				},
			}

			nsList := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
			nsList = append(nsList, secondNs)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"device /test/dev/xvdf is already being referenced in multiple namespaces (test, ns1)").
				Validate(err)
		})
		// Bug: aerospikeConfig map should not be listed in Failure message.
		// It is making failure message unnecessarily long.
		// Message: "admission webhook \"maerospikecluster.kb.io\" denied the request:
		// aerospikeConfig.namespaces not present.
		// aerospikeConfig map[network:map[fabric:map[port:3001] heartbeat:map[port:3002] service:map[port:3000]]
		// security:map[] service:map[auto-pin:none
		// feature-key-file:/etc/aerospike/secret/features.conf proto-fd-max:15000]]",
		It("InvalidNamespace: should fail when namespace configuration is missing", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			// Remove namespaces from AerospikeConfigSpec
			delete(aeroCluster.Spec.AerospikeConfig.Value, "namespaces")

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"maerospikecluster.kb.io\"",
					"denied the request: aerospikeConfig.namespaces not present.").
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

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
					"denied the request: strong-consistency namespace replication-factor 3 cannot be more than cluster size 1").
				Validate(err)
		})

		It("InvalidAerospikeConfig: should fail for empty aerospikeConfig", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.AerospikeConfig = &asdbv1.AerospikeConfigSpec{}
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
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
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook ",
					"\"maerospikecluster.kb.io\"",
					"denied the request: aerospikeConfig.namespaces not valid namespace list invalidConf").
				Validate(err)
		})

		It("AerospikeConfig-ServiceConf: should fail for setting advertise-ipv6", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["advertise-ipv6"] = true

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
					"denied the request: advertise-ipv6 is not supported").
				Validate(err)
		})

		It("ChangeDefaultConfig: ServiceConf - should fail for setting node-id/cluster-name", func() {
			// Service conf: "node-id"
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})["node-id"] = "a1"

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"maerospikecluster.kb.io\"",
					"denied the request: failed to set default aerospikeConfig.service config:",
					"config node-id can not have non-default value (string a1).",
					"It will be set internally (string ENV_NODE_ID)").
				Validate(err)
		})

		It("ChangeDefaultConfig: ServiceConf - should fail for setting cluster-name", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			// 1. Extract the service configuration map
			serviceConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})

			// 2. Assign the value to the specific key
			serviceConf[clusterNameConfig] = clusterNameConfig

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"maerospikecluster.kb.io\"",
					"denied the request: failed to set default aerospikeConfig.service config:",
					"config cluster-name can not have non-default value (string cluster-name).",
					"It will be set internally (string invalid-size-cluster)").
				Validate(err)
		})

		It("InvalidDNSConfiguration: InvalidDnsPolicy - should fail when dnsPolicy is set to 'Default'", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			defaultDNS := v1.DNSDefault
			aeroCluster.Spec.PodSpec.InputDNSPolicy = &defaultDNS

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
					"denied the request: dnsPolicy: Default is not supported").
				Validate(err)
		})

		It("InvalidDNSConfiguration: should fail when dnsPolicy is set to 'None' and no dnsConfig given", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			noneDNS := v1.DNSNone
			aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"dnsConfig is required field when dnsPolicy is set to None").
				Validate(err)
		})

		It("InvalidAerospikeConfigSecret: WhenFeatureKeyExist: should fail for no feature-key-file"+
			"path in storage volume", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService] = map[string]interface{}{
				"feature-key-file": "/randompath/features.conf",
			}
			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"feature-key-file paths or tls paths or default-password-file path are not mounted",
					"- create an entry for '/randompath/features.conf' in 'storage.volumes'").
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
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"can use facility only with `syslog` in aerospikeConfig.logging",
					"map[facility:local0 name:anyFileName path:/dev/log tag:asd]").
				Validate(err)
		})

		It("InvalidOperatorClientCertSpec: MultipleCertSource: should fail"+
			"if both SecretCertSource and CertPathInOperator is set", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator = &asdbv1.AerospikeCertPathInOperatorSource{}

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"either `secretCertSource` or `certPathInOperator` must be set in `operatorClientCertSpec` but not both").
				Validate(err)
		})

		It("MissingClientKeyFilename: should fail if ClientKeyFilename is missing", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.ClientKeyFilename = ""

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"both `clientCertFilename` and `clientKeyFilename` should be either set or not set in `secretCertSource`").
				Validate(err)
		})

		It("InvalidCaCerts: Should fail if both CaCertsFilename and CaCertsSource is set", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource.CaCertsSource = &asdbv1.CaCertsSource{}

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"both `caCertsFilename` or `caCertsSource` cannot be set in `secretCertSource`").
				Validate(err)
		})

		It("MissingClientCertPath: should fail if clientCertPath is missing", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.OperatorClientCertSpec.SecretCertSource = nil
			aeroCluster.Spec.OperatorClientCertSpec.CertPathInOperator =
				&asdbv1.AerospikeCertPathInOperatorSource{
					CaCertsPath:    "cacert.pem",
					ClientKeyPath:  "svc_key.pem",
					ClientCertPath: "",
				}

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"both `clientCertPath` and `clientKeyPath` should be either set or not set in `certPathInOperator`").
				Validate(err)
		})

		It("InvalidOperatorClientCertSpec: MissingOperatorClientCert: should fail"+
			"if operator client cert is not configured", func() {
			aeroCluster := testCluster.CreateAerospikeClusterPost640(
				clusterNamespacedName, 1, testutil.LatestEnterpriseImage,
			)
			aeroCluster.Spec.OperatorClientCertSpec = nil

			// Deploy cluster
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"operator client cert is not specified").
				Validate(err)
		})

		It("InvalidClusterName: should fail for EmptyClusterName", func() {
			cName := test.GetNamespacedName("", clusterNamespacedName.Namespace)
			aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
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

		It("InvalidClusterName: should fail when cluster name contains spaces", func() {
			cName := test.GetNamespacedName("my cluster", clusterNamespacedName.Namespace)
			aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
				WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com \"my cluster\" is invalid:",
					"metadata.name: Invalid value: \"my cluster\": a lowercase RFC 1123 subdomain ",
					"must consist of lower case alphanumeric characters,",
					"'-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com',",
					"regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')").
				Validate(err)
		})

		It("InvalidClusterName: should fail when cluster name contains uppercase characters", func() {
			cName := test.GetNamespacedName("MyCluster", clusterNamespacedName.Namespace)
			aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(422), metav1.StatusReasonInvalid).
				WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com \"MyCluster\" is invalid:",
					"metadata.name: Invalid value: \"MyCluster\": a lowercase RFC 1123 subdomain",
					"must consist of lower case alphanumeric characters,",
					"'-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com',",
					"regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')").
				Validate(err)
		})

		It("InvalidImageVersion: should fail for image version below base (6.0.0.0)", func() {
			oldImage := testutil.DefaultEnterpriseImage("5.0.0.0")
			aeroCluster := testCluster.CreateAerospikeClusterPost640(clusterNamespacedName, 1, oldImage)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: image version 5.0.0.0 not supported. Base version 6.0.0.0").
				Validate(err)
		})

		It("InvalidSize: should fail when cluster size exceeds max (256)", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 257)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: cluster size cannot be more than 256").
				Validate(err)
		})

		// Bug: Improve response message string to indicate acceptable value is 1 and not 2.
		// Current string "maxUnavailable 2 cannot be greater than or equal to 2" is not a clear message
		It("InvalidMaxUnavailable: should fail when maxUnavailable >= 1", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			maxUnav := intstr.FromInt32(2)
			aeroCluster.Spec.MaxUnavailable = &maxUnav
			// Ensure PDB is not disabled so MaxUnavailable is validated
			aeroCluster.Spec.DisablePDB = nil

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\"",
					"denied the request: maxUnavailable 2 cannot be greater than or equal to 2 ",
					"as it may result in data loss. Set it to a lower value").
				Validate(err)
		})

		It("InvalidAccessControl: should fail when security is disabled but access control is specified", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			// Remove security config so IsSecurityEnabled returns false
			delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)
			// AerospikeAccessControl is already set on the dummy cluster; keep it set

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: security is disabled but access control is specified").
				Validate(err)
		})

		It("InvalidPodSpec: should fail when hostNetwork and MultiPodPerHost are both enabled", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.PodSpec.HostNetwork = true
			aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(true)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: host networking cannot be enabled with multi pod per host").
				Validate(err)
		})

		It("InvalidNamespace: should fail when cluster namespace is not found", func() {
			cName := test.GetNamespacedName(clusterNamespacedName.Name, "default ns")
			aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 1)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(404), metav1.StatusReasonNotFound).
				WithMessageSubstrings("namespaces \"default ns\" not found").
				Validate(err)
		})

		It("InvalidNamespace: should fail when namespace configuration is empty", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)

			// Set Namespace to nil
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nil
			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"maerospikecluster.kb.io\"",
					"denied the request: aerospikeConfig.namespaces cannot be nil").
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

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			// Webhook response validation
			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook \"vaerospikecluster.kb.io\" denied the request:",
					"aerospikeConfig not valid: generated config not valid for version",
					"config schema error",
					"{-1 number_gte (root).namespaces.0.rack-id Must be greater than or equal to 0 namespaces.0.rack-id}").
				Validate(err)
		})

		It("InvalidRackConfig: should fail for negative rollingUpdateBatchSize", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			negVal := intstr.FromInt32(-1)
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &negVal

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: can not use negative spec.rackConfig.rollingUpdateBatchSize: -1").
				Validate(err)
		})

		It("InvalidRackConfig: should fail for negative scaleDownBatchSize", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
			negVal := intstr.FromInt32(-1)
			aeroCluster.Spec.RackConfig.ScaleDownBatchSize = &negVal

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: can not use negative spec.rackConfig.scaleDownBatchSize: -1").
				Validate(err)
		})

		It("InvalidRackConfig: should fail when namespace name in rackConfig.Namespaces contains spaces", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
				Namespaces: []string{"test ns"},
				Racks:      []asdbv1.Rack{{ID: 1}},
			}

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: namespace name `test ns` cannot have spaces",
					"Namespaces [test ns]").
				Validate(err)
		})

	})

	Context("UpdateValidation", func() {
		const updateValidationClusterName = "update-validation-cluster"
		updateValidationClusterNamespacedName := test.GetNamespacedName(updateValidationClusterName, testNs)

		It("UpdateValidation:Storage:Should fail when storage config is updated", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(updateValidationClusterNamespacedName, 2)
			err := envtests.K8sClient.Create(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				_ = envtests.K8sClient.Delete(ctx, aeroCluster)
			})

			current := &asdbv1.AerospikeCluster{}
			err = envtests.K8sClient.Get(ctx, types.NamespacedName{
				Name:      updateValidationClusterNamespacedName.Name,
				Namespace: updateValidationClusterNamespacedName.Namespace}, current)
			Expect(err).ToNot(HaveOccurred())

			// Change storage (e.g. StorageClass) to trigger "storage config cannot be updated"
			for i := range current.Spec.Storage.Volumes {
				if current.Spec.Storage.Volumes[i].Source.PersistentVolume != nil {
					current.Spec.Storage.Volumes[i].Source.PersistentVolume.StorageClass = "other-storage-class"
					break
				}
			}
			err = envtests.K8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"storage config cannot be updated",
					"cannot change volumes").
				Validate(err)
		})

		It("UpdateValidation:MultiPodPerHost:Should fail when MultiPodPerHost is changed", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(updateValidationClusterNamespacedName, 2)
			err := envtests.K8sClient.Create(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				_ = envtests.K8sClient.Delete(ctx, aeroCluster)
			})

			current := &asdbv1.AerospikeCluster{}
			err = envtests.K8sClient.Get(ctx, types.NamespacedName{
				Name:      updateValidationClusterNamespacedName.Name,
				Namespace: updateValidationClusterNamespacedName.Namespace}, current)
			Expect(err).ToNot(HaveOccurred())

			// Toggle MultiPodPerHost to trigger "cannot update MultiPodPerHost setting"
			current.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
			err = envtests.K8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"cannot update MultiPodPerHost setting").
				Validate(err)
		})
	})
})
