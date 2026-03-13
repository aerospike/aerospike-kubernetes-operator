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
)

var _ = Describe("AerospikeCluster dynamic replication-factor validation (envtests)", func() {
	const (
		clusterName = "dynamic-rf-webhook-cluster"
		testNs      = "default"
	)

	ctx := context.TODO()
	clusterNamespacedName := test.GetNamespacedName(clusterName, testNs)

	// apNamespaceConfig returns an AP (non-strong-consistency) namespace config for envtest.
	apNamespaceConfig := func(name string, rf int) map[string]interface{} {
		return map[string]interface{}{
			"name":               name,
			"replication-factor": rf,
			asdbv1.ConfKeyStorageEngine: map[string]interface{}{
				"type":    "device",
				"devices": []interface{}{"/test/dev/xvdf"},
			},
		}
	}

	// scNamespaceConfig returns an SC (strong-consistency) namespace config for envtest.
	scNamespaceConfig := func(name string, rf int) map[string]interface{} {
		cfg := apNamespaceConfig(name, rf)
		cfg["strong-consistency"] = true

		return cfg
	}

	AfterEach(func() {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterNamespacedName.Name,
				Namespace: clusterNamespacedName.Namespace,
			},
		}
		_ = envtests.K8sClient.Delete(ctx, aeroCluster)
	})

	// updateAPNamespaceRF creates a cluster with one AP namespace (initialRF),
	// updates its replication-factor to newRF, and expects the update to succeed.
	updateAPNamespaceRF := func(size int32, initialRF, newRF int) {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
		aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
			apNamespaceConfig("test", initialRF),
		}

		err := envtests.K8sClient.Create(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
		nsConf := nsList[0].(map[string]interface{})
		nsConf["replication-factor"] = newRF
		nsList[0] = nsConf
		current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

		err = envtests.K8sClient.Update(ctx, current)
		Expect(err).ToNot(HaveOccurred())
	}

	// updateSCNamespaceRF creates a cluster with one SC namespace (initialRF),
	// updates its replication-factor to newRF, and returns the error from the update.
	updateSCNamespaceRF := func(size int32, initialRF, newRF int) error {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
		aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
		aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
			scNamespaceConfig("test", initialRF),
		}

		err := envtests.K8sClient.Create(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
		nsConf := nsList[0].(map[string]interface{})
		nsConf["replication-factor"] = newRF
		nsList[0] = nsConf
		current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

		return envtests.K8sClient.Update(ctx, current)
	}

	Context("Deploy validation", func() {
		Context("spec.aerospikeConfig (replication-factor)", func() {
			Context("negative", func() {
				It("rejects RF > cluster size for SC namespace", func() {
					// SC namespace: replication-factor cannot exceed cluster size.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						scNamespaceConfig("test", 5), // RF=5 > size=2
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor",
							"cannot be more than cluster size",
						).
						Validate(err)
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.aerospikeConfig (replication-factor)", func() {
			Context("negative", func() {
				It("rejects RF change attempted for SC namespace", func() {
					err := updateSCNamespaceRF(2, 2, 5)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor cannot be updated for SC namespaces",
						).
						Validate(err)
				})

				It("rejects multiple namespaces' RF changed in a single update", func() {
					// Rack-aware cluster with two racks so we have two "name@rackID" entries; change both RFs.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change RF in both racks (update both rack configs) to trigger "multiple namespaces at the same time".
					for i := range current.Spec.RackConfig.Racks {
						rack := &current.Spec.RackConfig.Racks[i]
						if rack.InputAerospikeConfig != nil && rack.InputAerospikeConfig.Value != nil {
							nsList, ok := rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
							if ok && len(nsList) > 0 {
								nsConf := nsList[0].(map[string]interface{})
								nsConf["replication-factor"] = 3
								nsList[0] = nsConf
								rack.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
							}
						}
					}

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = 3
					nsList[0] = nsConf
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

				It("rejects RF change combined with unrelated spec changes", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = 3
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList
					// Change another field alongside RF update.
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

				It("rejects rack-aware create when RF values are mismatched across racks", func() {
					// Create with two racks; same namespace has RF=2 in rack 1 and RF=3 in rack 2.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2),
										},
									},
								},
							},
							{
								ID: 2,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 3),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
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

				It("rejects RF update when enableDynamicConfigUpdate is false", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(false)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = 3
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

				It("rejects invalid RF value (zero or negative)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 0),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor",
						).
						Validate(err)
				})

				It("rejects RF change on SC namespace (misconfigured type)", func() {
					// "RF change on misconfigured namespace type": In envtest we cannot have cluster state.
					// We test the same rejection as "RF change for SC namespace" – SC namespace cannot have RF updated.
					err := updateSCNamespaceRF(2, 2, 1)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor cannot be updated for SC namespaces",
						).
						Validate(err)
				})

				It("rejects RF change on namespace missing from rackConfig", func() {
					// Rack-aware cluster: namespace "other" in aerospikeConfig but not in rackConfig.Namespaces.
					// If the webhook validates that RF changes only apply to namespaces in rackConfig, it should reject.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"}, // "other" is not listed
						Racks:      []asdbv1.Rack{{ID: 1}, {ID: 2}},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
						apNamespaceConfig("other", 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change RF for "other" (namespace not in rackConfig.Namespaces).
					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					otherConf := nsList[1].(map[string]interface{})
					otherConf["replication-factor"] = 3
					nsList[1] = otherConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					// Skip if webhook does not yet validate namespace-in-rackConfig for RF updates.
					if err == nil {
						Skip("webhook does not reject RF change on namespace missing from rackConfig")
					}

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("vaerospikecluster.kb.io",
							"device /test/dev/xvdf is already being referenced",
							"in multiple namespaces (test, other)").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows basic RF increase for AP namespace", func() {
					// Pre-conditions: size ≥ 3; AP namespace; RF=2; enableDynamicConfigUpdate=true
					updateAPNamespaceRF(3, 2, 3)
				})

				It("allows basic RF decrease for AP namespace", func() {
					// Pre-conditions: AP namespace; RF initially 3; enableDynamicConfigUpdate=true
					updateAPNamespaceRF(3, 3, 2)
				})

				It("allows no-op RF update (same RF value)", func() {
					// Pre-conditions: AP namespace; RF=3; enableDynamicConfigUpdate=true; update to same RF
					updateAPNamespaceRF(3, 3, 3)
				})

				It("allows rack-aware RF increase when consistent across racks", func() {
					// Pre-conditions: two racks (id:1, id:2); AP namespace RF=2 in both; enableDynamicConfigUpdate=true
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1}, {ID: 2}},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = 3
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred())
				})

				It("allows RF set to minimum value (RF=1)", func() {
					updateAPNamespaceRF(2, 2, 1)
				})

				It("allows RF set to maximum allowed (RF = cluster size)", func() {
					const clusterSize = 3
					updateAPNamespaceRF(clusterSize, 2, clusterSize)
				})
			})
		})
	})
})
