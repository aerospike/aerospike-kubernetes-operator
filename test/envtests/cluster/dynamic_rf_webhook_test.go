/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeCluster dynamic replication-factor validation", func() {
	const (
		clusterName      = "dynamic-rf-webhook-cluster"
		maxSchemaRF      = 256
		rfAboveSchemaMax = 257
		namespaceName    = "test"
		namespaceName2   = "test2"
		namespaceName3   = "test3"
	)

	ctx := context.TODO()
	clusterNamespacedName := test.GetNamespacedName(clusterName, testutil.DefaultNamespace)

	// apNamespaceConfig returns an AP namespace config for envtest.
	apNamespaceConfig := func(name string, rf int, device string) map[string]interface{} {
		return map[string]interface{}{
			asdbv1.ConfKeyName:              name,
			asdbv1.ConfKeyReplicationFactor: rf,
			asdbv1.ConfKeyStorageEngine: map[string]interface{}{
				"type":    "device",
				"devices": []interface{}{device},
			},
		}
	}

	// scNamespaceConfig returns an SC namespace config for envtest.
	scNamespaceConfig := func(name string, rf int) map[string]interface{} {
		cfg := apNamespaceConfig(name, rf, testutil.DefaultDevicePath)
		cfg[asdbv1.ConfKeyStrongConsistency] = true

		return cfg
	}

	inMemoryNS := func(name string, rf int) map[string]interface{} {
		return map[string]interface{}{
			asdbv1.ConfKeyName:              name,
			asdbv1.ConfKeyReplicationFactor: rf,
			asdbv1.ConfKeyStorageEngine: map[string]interface{}{
				"type":      "memory",
				"data-size": 1073741824,
			},
		}
	}

	// updateNamespaceRF creates a cluster with one namespace (AP or SC), updates its RF to newRF,
	// and returns the Update error. If rackCount is 0, the RF change is applied to spec.aerospikeConfig
	// only. If rackCount > 0, RackConfig is populated with that many racks (each with InputAerospikeConfig
	// for the namespace), and the RF change is applied only to each rack's InputAerospikeConfig.
	updateNamespaceRF := func(isSCNamespace bool, size int32, initialRF, newRF int, rackCount int) error {
		var nsConfig map[string]interface{}
		if isSCNamespace {
			nsConfig = scNamespaceConfig(namespaceName, initialRF)
		} else {
			nsConfig = apNamespaceConfig(namespaceName, initialRF, testutil.DefaultDevicePath)
		}

		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
		aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{nsConfig}

		if rackCount > 0 {
			racks := make([]asdbv1.Rack, rackCount)
			for i := 0; i < rackCount; i++ {
				var rackNS map[string]interface{}
				if isSCNamespace {
					rackNS = scNamespaceConfig(namespaceName, initialRF)
				} else {
					rackNS = apNamespaceConfig(namespaceName, initialRF, testutil.DefaultDevicePath)
				}

				racks[i] = asdbv1.Rack{
					ID: i + 1,
					InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							asdbv1.ConfKeyNamespace: []interface{}{rackNS},
						},
					},
				}
			}

			aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
				Namespaces: []string{namespaceName},
				Racks:      racks,
			}
		}

		err := envtests.K8sClient.Create(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		if rackCount > 0 {
			for i := range current.Spec.RackConfig.Racks {
				rack := &current.Spec.RackConfig.Racks[i]
				Expect(rack.InputAerospikeConfig).ToNot(BeNil())
				nsList := rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
				nsConf := nsList[0].(map[string]interface{})
				nsConf[asdbv1.ConfKeyReplicationFactor] = newRF
				nsList[0] = nsConf
				rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
			}

			return envtests.K8sClient.Update(ctx, current)
		}

		nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
		nsConf := nsList[0].(map[string]interface{})
		nsConf[asdbv1.ConfKeyReplicationFactor] = newRF
		nsList[0] = nsConf
		current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

		return envtests.K8sClient.Update(ctx, current)
	}

	AfterEach(func() {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterNamespacedName.Name,
				Namespace: clusterNamespacedName.Namespace,
			},
		}
		Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
	})

	Context("Deploy validation", func() {
		Context("spec.aerospikeConfig (replication-factor)", func() {
			Context("negative", func() {
				// Bug: https://aerospike.atlassian.net/browse/KO-484
				It("rejects invalid RF value (zero or negative)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 0, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor can not be zero or negative",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows AP namespace with RF greater than cluster size on deploy", func() {
					// AP mode: replication-factor may exceed cluster size (unlike SC); see validateNamespaceReplicationFactor.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					ns := apNamespaceConfig(namespaceName, 4, testutil.DefaultDevicePath)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{ns}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})

		Context("spec.rackConfig (replication-factor)", func() {
			Context("negative", func() {
				It("rejects rack-aware create when RF values are mismatched across racks", func() {
					// Per-rack RF must be set via InputAerospikeConfig. Preset AerospikeConfig is ignored on create:
					// mutating webhook copies global config into each rack when InputAerospikeConfig is nil.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{namespaceName},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, 3, testutil.DefaultDevicePath),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"different replication-factor values across racks",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows rack-aware deploy with replication-factor 256 in aerospikeConfig and all racks (schema max)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{namespaceName},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
										},
									},
								},
							},
							{
								ID: 2,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.aerospikeConfig (replication-factor)", func() {
			Context("negative", func() {
				It("rejects RF change attempted for SC namespace", func() {
					err := updateNamespaceRF(true, 3, 2, 3, 0)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor cannot be updated for SC namespaces",
						).
						Validate(err)
				})

				It("rejects RF change combined with unrelated spec changes", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf[asdbv1.ConfKeyReplicationFactor] = 3
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
					current.Spec.Size = 4

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"when updating replication-factor",
							"no other fields",
						).
						Validate(err)
				})

				It("rejects RF update when enableDynamicConfigUpdate is false", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(false)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf[asdbv1.ConfKeyReplicationFactor] = 3
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"enableDynamicConfigUpdate",
							"to be set to true",
						).
						Validate(err)
				})

				It("rejects RF update in spec.aerospikeConfig.namespaces (above schema max 256)", func() {
					// Pre-conditions: AP namespace; RF=2; enableDynamicConfigUpdate=true;
					// update to RF=257 (above schema max 256)
					err := updateNamespaceRF(false, 3, 2, rfAboveSchemaMax, 0)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor Must be less than or equal to 256 ",
						).
						Validate(err)
				})

				It("rejects simultaneous RF update across multiple namespaces", func() {
					// Multiple namespaces in global aerospikeConfig;
					// single update changes RF for 2+ namespaces → must fail.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						inMemoryNS(namespaceName, 2),
						inMemoryNS(namespaceName2, 2),
						inMemoryNS(namespaceName3, 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					// Update RF for test2 and test3 in one update
					for i := range nsList {
						nsConf := nsList[i].(map[string]interface{})
						if nsConf[asdbv1.ConfKeyName] == namespaceName2 || nsConf["name"] == namespaceName3 {
							nsConf[asdbv1.ConfKeyReplicationFactor] = 3
							nsList[i] = nsConf
						}
					}

					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"cannot update replication-factor for multiple namespaces at the same time",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows RF increase", func() {
					// Pre-conditions: size ≥ 3; AP namespace; RF=2; enableDynamicConfigUpdate=true
					Expect(updateNamespaceRF(false, 3, 2, 3, 0)).To(Succeed())
				})

				It("allows RF decrease", func() {
					// Pre-conditions: AP namespace; RF initially 3; enableDynamicConfigUpdate=true
					Expect(updateNamespaceRF(false, 3, 3, 2, 0)).To(Succeed())
				})

				It("allows no-op RF update (same RF value)", func() {
					// Pre-conditions: AP namespace; RF=3; enableDynamicConfigUpdate=true; update to same RF
					Expect(updateNamespaceRF(false, 3, 3, 3, 0)).To(Succeed())
				})
				It("allows RF increase greater than cluster size", func() {
					// Pre-conditions: size ≥ 3; AP namespace; RF=2; enableDynamicConfigUpdate=true
					Expect(updateNamespaceRF(false, 3, 2, 4, 0)).To(Succeed())
				})

				It("allows RF set to minimum value (RF=1)", func() {
					Expect(updateNamespaceRF(false, 2, 2, 1, 0)).To(Succeed())
				})

				It("allows RF set to maximum allowed (RF = cluster size)", func() {
					Expect(updateNamespaceRF(false, 3, 2, 3, 0)).To(Succeed())
				})

				It("allow RF remove from spec.aerospikeConfig", func() {
					// Create: one AP namespace with RF=2.
					// Update: remove replication-factor from that namespace block. Must succeed.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					apNamespaceNoRF := map[string]interface{}{
						"name": namespaceName,
						asdbv1.ConfKeyStorageEngine: map[string]interface{}{
							"type":    "device",
							"devices": []interface{}{testutil.DefaultDevicePath},
						},
					}
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{apNamespaceNoRF}

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred())
				})

				It("allows adding replication-factor to namespace that had none on create", func() {
					// Create: AP namespace in spec.aerospikeConfig without replication-factor key.
					// Update: set replication-factor to 3 in spec.aerospikeConfig only.
					apNamespaceNoRF := map[string]interface{}{
						asdbv1.ConfKeyName: namespaceName,
						asdbv1.ConfKeyStorageEngine: map[string]interface{}{
							"type":    "device",
							"devices": []interface{}{testutil.DefaultDevicePath},
						},
					}

					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{apNamespaceNoRF}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf[asdbv1.ConfKeyReplicationFactor] = 3
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})

		Context("spec.rackConfig (replication-factor)", func() {
			Context("negative", func() {
				It("rejects RF update if RF is inconsistent across racks for same namespace)", func() {
					// Same namespace "test" on rack 1 and 2;
					// update RF only in rack 1 Input — must stay consistent across racks.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{namespaceName},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					rack := &current.Spec.RackConfig.Racks[0]
					Expect(rack.InputAerospikeConfig).ToNot(BeNil())
					nsList, ok := rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					Expect(ok).To(BeTrue())

					nsConf := nsList[0].(map[string]interface{})
					nsConf[asdbv1.ConfKeyReplicationFactor] = 3
					nsList[0] = nsConf
					rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"different replication-factor values across racks",
							"replication-factor must be the same in every rack",
						).
						Validate(err)
				})

				It("rejects RF update in rackConfig for value above schema max 256", func() {
					// Pre-conditions: AP namespace; RF=2; enableDynamicConfigUpdate=true;
					// update to RF=257 (above schema max 256)
					err := updateNamespaceRF(false, 3, 2, rfAboveSchemaMax, 1)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor Must be less than or equal to 256 ",
						).
						Validate(err)
				})
				It("rejects RF update when namespace addition rollout is still in progress", func() {
					// Spec: desired config includes test + test2. Status: deployed rack config only lists "test"
					// (simulates test2 not yet rolled out). RF change on test2 must be rejected.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						inMemoryNS(namespaceName, 2),
						inMemoryNS(namespaceName2, 2),
					}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{namespaceName, namespaceName2},
						Racks:      []asdbv1.Rack{{ID: 1}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current := &asdbv1.AerospikeCluster{}
					err = envtests.K8sClient.Get(ctx, clusterNamespacedName, current)
					Expect(err).ToNot(HaveOccurred())

					// Status must be non-nil on AerospikeConfig or the webhook skips the rollout check.
					current.Status.AerospikeConfig = current.Spec.AerospikeConfig.DeepCopy()
					// Deployed / effective rack config: only "test" is present — "test2" not in status yet.
					current.Status.RackConfig = asdbv1.RackConfig{
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											inMemoryNS(namespaceName, 2),
										},
									},
								},
							},
						},
					}

					err = envtests.K8sClient.Status().Update(ctx, current)
					Expect(err).ToNot(HaveOccurred())

					err = envtests.K8sClient.Get(ctx, clusterNamespacedName, current)
					Expect(err).ToNot(HaveOccurred())

					// Single-field RF change for test2 only (spec still lists both namespaces).
					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					for i := range nsList {
						nsConf := nsList[i].(map[string]interface{})
						if nsConf[asdbv1.ConfKeyName] == namespaceName2 {
							nsConf[asdbv1.ConfKeyReplicationFactor] = 3
							nsList[i] = nsConf
						}
					}

					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"namespace addition rollout is still in progress",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				DescribeTable("allows rack-aware RF increase when consistent across racks",
					func(size int32, rackCount int, newRF int) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)

						racks := make([]asdbv1.Rack, rackCount)
						for i := 0; i < rackCount; i++ {
							racks[i] = asdbv1.Rack{ID: i + 1}
						}

						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName},
							Racks:      racks,
						}
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						}

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
						nsConf := nsList[0].(map[string]interface{})
						nsConf[asdbv1.ConfKeyReplicationFactor] = newRF
						nsList[0] = nsConf
						current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

						err = envtests.K8sClient.Update(ctx, current)
						Expect(err).ToNot(HaveOccurred())
					},
					Entry("RF within cluster size", int32(4), 2, 3),
					Entry("RF greater than cluster size", int32(3), 1, 4),
				)

				It("allow removing replication-factor from namespace in rackConfig.aerospikeConfig", func() {
					// Create: global + rack have test and test2, both RF=2; rackConfig.namespaces lists both.
					// Update: rack patch for "test" omits replication-factor (inherits via merge with global).
					removeRF := map[string]interface{}{
						"name": namespaceName2,
						asdbv1.ConfKeyStorageEngine: map[string]interface{}{
							"type":      "memory",
							"data-size": 1073741824,
						},
					}

					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						inMemoryNS(namespaceName, 2), inMemoryNS(namespaceName2, 2),
					}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{namespaceName, namespaceName2},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											inMemoryNS(namespaceName, 2), inMemoryNS(namespaceName2, 2),
										},
									},
								},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							asdbv1.ConfKeyNamespace: []interface{}{
								inMemoryNS(namespaceName, 2),
								removeRF,
							},
						},
					}

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})
})
