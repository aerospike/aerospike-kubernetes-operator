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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeCluster dynamic replication-factor validation (envtests)", func() {
	const (
		clusterName = "dynamic-rf-webhook-cluster"
		testNs      = "default"
	)

	ctx := context.TODO()
	clusterNamespacedName := test.GetNamespacedName(clusterName, testNs)

	// apNamespaceConfig returns an AP namespace config for envtest.
	apNamespaceConfig := func(name string, rf int, device string) map[string]interface{} {
		return map[string]interface{}{
			"name":               name,
			"replication-factor": rf,
			asdbv1.ConfKeyStorageEngine: map[string]interface{}{
				"type": "device",
				// "devices": []interface{}{"/test/dev/xvdf"},
				"devices": []interface{}{device},
			},
		}
	}

	// scNamespaceConfig returns an SC namespace config for envtest.
	scNamespaceConfig := func(name string, rf int) map[string]interface{} {
		cfg := apNamespaceConfig(name, rf, "/test/dev/xvdf")
		cfg["strong-consistency"] = true

		return cfg
	}

	// apNamespaceWithDevice is like apNamespaceConfig(RF=2) but uses a distinct device path per namespace
	// (required when multiple AP namespaces coexist).
	// apNamespaceWithDevice := func(name string, device string) map[string]interface{} {
	// 	return map[string]interface{}{
	// 		"name":               name,
	// 		"replication-factor": 2,
	// 		asdbv1.ConfKeyStorageEngine: map[string]interface{}{
	// 			"type":    "device",
	// 			"devices": []interface{}{device},
	// 		},
	// 	}
	// }

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
			apNamespaceConfig("test", initialRF, "/test/dev/xvdf"),
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

			Context("positive", func() {
				It("allows AP namespace with RF greater than cluster size on deploy", func() {
					// AP mode: replication-factor may exceed cluster size (unlike SC); see validateNamespaceReplicationFactor.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					ns := apNamespaceConfig("test", 4, "/test/dev/xvdf")
					ns["replication-factor"] = 4
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
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2, "/test/dev/xvdf"),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 3, "/test/dev/xvdf"),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
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
					const maxSchemaRF = 256
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
											apNamespaceConfig("test", maxSchemaRF, "/test/dev/xvdf"),
										},
									},
								},
							},
							{
								ID: 2,
								AerospikeConfig: asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", maxSchemaRF, "/test/dev/xvdf"),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", maxSchemaRF, "/test/dev/xvdf"),
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
					err := updateSCNamespaceRF(3, 2, 3)
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
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
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
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
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

				// Bug: https://aerospike.atlassian.net/browse/KO-484
				It("rejects invalid RF value (zero or negative)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 0, "/test/dev/xvdf"),
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

				It("rejects RF update in spec.aerospikeConfig.namespaces (above schema max 256)", func() {
					// Pre-conditions: AP namespace; RF=2; enableDynamicConfigUpdate=true;
					// update to RF=257 (above schema max 256)
					const rfAboveSchemaMax = 257
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 3)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = rfAboveSchemaMax
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor Must be less than or equal to 256 ",
						).
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

				It("allows RF set to minimum value (RF=1)", func() {
					updateAPNamespaceRF(2, 2, 1)
				})

				It("allows RF set to maximum allowed (RF = cluster size)", func() {
					const clusterSize = 3
					updateAPNamespaceRF(clusterSize, 2, clusterSize)
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
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2, "/test/dev/xvdf"),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2, "/test/dev/xvdf"),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
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
					nsConf["replication-factor"] = 3
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

				It("rejects simultaneous RF update across multiple namespaces", func() {
					// Preconditons: Global namespace test, RF 2.
					// Rack 1 namespace - test2, RF 2; rack 2 namespace test3, RF 2.
					// Single update bumps RF for test2 and test3 to 3 → must fail (one namespace per RF update).
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
					}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test", "test2", "test3"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test2", 2, "/test/dev/xvdg"),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test3", 2, "/test/dev/xvdh"),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes,
						asdbv1.VolumeSpec{
							Name: "ns-xvdg",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("1Gi"),
									StorageClass: testutil.StorageClass,
									VolumeMode:   corev1.PersistentVolumeBlock,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/test/dev/xvdg",
							},
						},
						asdbv1.VolumeSpec{
							Name: "ns-xvdh",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("1Gi"),
									StorageClass: testutil.StorageClass,
									VolumeMode:   corev1.PersistentVolumeBlock,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/test/dev/xvdh",
							},
						},
					)

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					rack1 := &current.Spec.RackConfig.Racks[0]
					Expect(rack1.InputAerospikeConfig).ToNot(BeNil())
					nsList1, ok := rack1.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					Expect(ok).To(BeTrue())
					ns2 := nsList1[0].(map[string]interface{})
					ns2["replication-factor"] = 3
					nsList1[0] = ns2
					rack1.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList1

					rack2 := &current.Spec.RackConfig.Racks[1]
					Expect(rack2.InputAerospikeConfig).ToNot(BeNil())
					nsList2, ok := rack2.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					Expect(ok).To(BeTrue())
					ns3 := nsList2[0].(map[string]interface{})
					ns3["replication-factor"] = 3
					nsList2[0] = ns3
					rack2.InputAerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList2

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"cannot update replication-factor for multiple namespaces at the same time",
						).
						Validate(err)
				})

				It("rejects RF update if RF is inconsistent in spec.aerospikeConfig.namespaces"+
					"and spec.rackConfig.aerospikeConfig for the same namespace", func() {
					// Create: namespace test via rack 1 input, test2 via rack 2 input; both RF 2.
					// spec.rackConfig.aerospikeConfig.namespaces: test has RF = 2 during cluster creation.
					// update RF for test to 3 in spec.rackConfig.aerospikeConfig.namespaces.
					// must fail because RF is inconsistent in spec.aerospikeConfig.namespaces
					// and spec.rackConfig.aerospikeConfig for the same namespace(test)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
						apNamespaceConfig("test2", 2, "/test/dev/xvdg"),
					}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test", "test2"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test", 2, "/test/dev/xvdf"),
										},
									},
								},
							},
							{
								ID: 2,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{
											apNamespaceConfig("test2", 2, "/test/dev/xvdg"),
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes,
						asdbv1.VolumeSpec{
							Name: "ns-xvdg",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("1Gi"),
									StorageClass: testutil.StorageClass,
									VolumeMode:   corev1.PersistentVolumeBlock,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/test/dev/xvdg",
							},
						},
					)

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Update replication-factor for both namespaces in one request.
					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					for i := range nsList {
						nsConf := nsList[i].(map[string]interface{})
						nsConf["replication-factor"] = 3
						nsList[i] = nsConf
					}
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

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

				It("rejects RF update in rackConfig for value above schema max 256)", func() {
					// Pre-conditions: AP namespace; RF=2; enableDynamicConfigUpdate=true;
					// update to RF=257 (above schema max 256)
					const rfAboveSchemaMax = 257
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1}, {ID: 2}},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					nsList := current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
					nsConf := nsList[0].(map[string]interface{})
					nsConf["replication-factor"] = rfAboveSchemaMax
					nsList[0] = nsConf
					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = nsList

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"vaerospikecluster.kb.io",
							"replication-factor Must be less than or equal to 256 ",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows rack-aware RF increase when consistent across racks", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1}, {ID: 2}},
					}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						apNamespaceConfig("test", 2, "/test/dev/xvdf"),
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

				It("allow adding namespace to rackConfig.namespaces with RF", func() {
					// Preconditions: Global namespace test, RF=2.
					// Rack 1 namespace test, RF=2.
					// Update: add namespace "other" to rackConfig.namespaces with RF=3.
					// must succeed because namespace "other" is not in spec.rackConfig.namespaces
					memNS := func(name string, rf int) map[string]interface{} {
						return map[string]interface{}{
							"name":               name,
							"replication-factor": rf,
							asdbv1.ConfKeyStorageEngine: map[string]interface{}{
								"type":      "memory",
								"data-size": 1073741824,
							},
						}
					}

					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 1)
					aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = []interface{}{
						memNS("test", 2),
					}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID: 1,
								InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										asdbv1.ConfKeyNamespace: []interface{}{memNS("test", 2)},
									},
								},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Namespace "other" is not in spec.rackConfig.namespaces
					current.Spec.RackConfig.Racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							asdbv1.ConfKeyNamespace: []interface{}{memNS("other", 3)},
						},
					}

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})
	})
})
