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
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeCluster dynamic replication-factor validation", func() {
	const (
		dynamicRFClusterName = "dynamic-rf-webhook-cluster"
		maxSchemaRF          = 256
		rfAboveSchemaMax     = 257
		namespaceName        = "test"
		namespaceName2       = "test2"
		namespaceName3       = "test3"
	)

	ctx := context.TODO()
	clusterNamespacedName := uniqueNamespacedName(dynamicRFClusterName)

	// rfValidationTests registers all replication-factor validation It blocks for
	// the given server image.  Extracted so the same suite runs for both config
	// formats; delete the legacy context below when pre-8.1.1 support is dropped.
	rfValidationTests := func(image string) {
		// ── Namespace config builders ──────────────────────────────────────────
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

		// updateNamespaceRF creates a cluster with one namespace (AP or SC), updates
		// its RF to newRF, and returns the Update error.  If rackCount is 0, the RF
		// change is applied to spec.aerospikeConfig only.  If rackCount > 0,
		// RackConfig is populated with that many racks (each with InputAerospikeConfig
		// for the namespace), and the RF change is applied only to each rack's
		// InputAerospikeConfig.
		updateNamespaceRF := func(isSCNamespace bool, size int32, initialRF, newRF int, rackCount int) error {
			var nsConfig map[string]interface{}
			if isSCNamespace {
				nsConfig = scNamespaceConfig(namespaceName, initialRF)
			} else {
				nsConfig = apNamespaceConfig(namespaceName, initialRF, testutil.DefaultDevicePath)
			}

			aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, size, image)
			aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
			aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(image, nsConfig)

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
								asdbv1.ConfKeyNamespace: buildNSConfig(image, rackNS),
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
					setNSRFInConfig(rack.InputAerospikeConfig.Value, namespaceName, newRF)
				}

				return envtests.K8sClient.Update(ctx, current)
			}

			setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName, newRF)

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

		// ── Deploy validation ──────────────────────────────────────────────────
		Context("Deploy validation", func() {
			Context("spec.aerospikeConfig (replication-factor)", func() {
				Context("negative", func() {
					// Bug: https://aerospike.atlassian.net/browse/KO-484
					It("rejects invalid RF value (zero or negative)", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 0, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).To(HaveOccurred())

						envtests.NewStatusErrorMatcher().
							WithMessageSubstrings(
								"vaerospikecluster.kb.io",
								"replication-factor Must be greater than or equal to 1",
							).
							Validate(err)
					})
				})

				Context("positive", func() {
					It("allows AP namespace with RF greater than cluster size on deploy", func() {
						// AP mode: replication-factor may exceed cluster size (unlike SC).
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 3, image)
						ns := apNamespaceConfig(namespaceName, 4, testutil.DefaultDevicePath)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(image, ns)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					})
				})
			})

			Context("spec.rackConfig (replication-factor)", func() {
				Context("negative", func() {
					It("rejects rack-aware create when RF values are mismatched across racks", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 4, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName},
							Racks: []asdbv1.Rack{
								{
									ID: 1,
									InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
											),
										},
									},
								},
								{
									ID: 2,
									InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, 3, testutil.DefaultDevicePath),
											),
										},
									},
								},
							},
						}
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						)

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
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 4, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName},
							Racks: []asdbv1.Rack{
								{
									ID: 1,
									AerospikeConfig: asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
											),
										},
									},
								},
								{
									ID: 2,
									AerospikeConfig: asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
											),
										},
									},
								},
							},
						}
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, maxSchemaRF, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					})
				})
			})
		})

		// ── Update validation ──────────────────────────────────────────────────
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
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 3, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName, 3)
						current.Spec.Size = 4

						err = envtests.K8sClient.Update(ctx, current)
						Expect(err).To(HaveOccurred())

						envtests.NewStatusErrorMatcher().
							WithMessageSubstrings(
								"vaerospikecluster.kb.io",
								"cannot update replication-factor for namespace",
								"alongside any other spec change or in-progress namespace rollout",
							).
							Validate(err)
					})

					It("rejects RF update when enableDynamicConfigUpdate is false", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 3, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(false)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName, 3)

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
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 4, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							inMemoryNS(namespaceName, 2),
							inMemoryNS(namespaceName2, 2),
							inMemoryNS(namespaceName3, 2),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName2, 3)
						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName3, 3)

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
						Expect(updateNamespaceRF(false, 3, 2, 3, 0)).To(Succeed())
					})

					It("allows RF decrease", func() {
						Expect(updateNamespaceRF(false, 3, 3, 2, 0)).To(Succeed())
					})

					It("allows no-op RF update (same RF value)", func() {
						Expect(updateNamespaceRF(false, 3, 3, 3, 0)).To(Succeed())
					})

					It("allows RF increase greater than cluster size", func() {
						Expect(updateNamespaceRF(false, 3, 2, 4, 0)).To(Succeed())
					})

					It("allows RF set to minimum value (RF=1)", func() {
						Expect(updateNamespaceRF(false, 2, 2, 1, 0)).To(Succeed())
					})

					It("allows RF set to maximum allowed (RF = cluster size)", func() {
						Expect(updateNamespaceRF(false, 3, 2, 3, 0)).To(Succeed())
					})

					It("allow RF remove from spec.aerospikeConfig", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						apNamespaceNoRF := map[string]interface{}{
							asdbv1.ConfKeyName: namespaceName,
							asdbv1.ConfKeyStorageEngine: map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{testutil.DefaultDevicePath},
							},
						}
						current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(image, apNamespaceNoRF)

						err = envtests.K8sClient.Update(ctx, current)
						Expect(err).ToNot(HaveOccurred())
					})

					It("allows adding replication-factor to namespace that had none on create", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)

						apNamespaceNoRF := map[string]interface{}{
							asdbv1.ConfKeyName: namespaceName,
							asdbv1.ConfKeyStorageEngine: map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{testutil.DefaultDevicePath},
							},
						}
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(image, apNamespaceNoRF)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName, 3)

						err = envtests.K8sClient.Update(ctx, current)
						Expect(err).ToNot(HaveOccurred())
					})
				})
			})

			Context("spec.rackConfig (replication-factor)", func() {
				Context("negative", func() {
					It("rejects RF update if RF is inconsistent across racks for same namespace", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 4, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName},
							Racks: []asdbv1.Rack{
								{
									ID: 1,
									InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
											),
										},
									},
								},
								{
									ID: 2,
									InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
											),
										},
									},
								},
							},
						}
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
						)

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						rack := &current.Spec.RackConfig.Racks[0]
						Expect(rack.InputAerospikeConfig).ToNot(BeNil())
						setNSRFInConfig(rack.InputAerospikeConfig.Value, namespaceName, 3)

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
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 2, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							inMemoryNS(namespaceName, 2),
							inMemoryNS(namespaceName2, 2),
						)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName, namespaceName2},
							Racks:      []asdbv1.Rack{{ID: 1}},
						}

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current := &asdbv1.AerospikeCluster{}
						err = envtests.K8sClient.Get(ctx, clusterNamespacedName, current)
						Expect(err).ToNot(HaveOccurred())

						current.Status.AerospikeConfig = current.Spec.AerospikeConfig.DeepCopy()
						current.Status.RackConfig = asdbv1.RackConfig{
							Racks: []asdbv1.Rack{
								{
									ID: 1,
									AerospikeConfig: asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												current.Spec.Image,
												inMemoryNS(namespaceName, 2),
											),
										},
									},
								},
							},
						}

						err = envtests.K8sClient.Status().Update(ctx, current)
						Expect(err).ToNot(HaveOccurred())

						err = envtests.K8sClient.Get(ctx, clusterNamespacedName, current)
						Expect(err).ToNot(HaveOccurred())

						setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName2, 3)

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
							aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, size, image)
							aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)

							racks := make([]asdbv1.Rack, rackCount)
							for i := 0; i < rackCount; i++ {
								racks[i] = asdbv1.Rack{ID: i + 1}
							}

							aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
								Namespaces: []string{namespaceName},
								Racks:      racks,
							}
							aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
								image,
								apNamespaceConfig(namespaceName, 2, testutil.DefaultDevicePath),
							)

							err := envtests.K8sClient.Create(ctx, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							setNSRFInConfig(current.Spec.AerospikeConfig.Value, namespaceName, newRF)

							err = envtests.K8sClient.Update(ctx, current)
							Expect(err).ToNot(HaveOccurred())
						},
						Entry("RF within cluster size", int32(4), 2, 3),
						Entry("RF greater than cluster size", int32(3), 1, 4),
					)

					It("allow removing replication-factor from namespace in rackConfig.aerospikeConfig", func() {
						aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(clusterNamespacedName, 1, image)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
						aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNamespace] = buildNSConfig(
							image,
							inMemoryNS(namespaceName, 2), inMemoryNS(namespaceName2, 2),
						)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{namespaceName, namespaceName2},
							Racks: []asdbv1.Rack{
								{
									ID: 1,
									InputAerospikeConfig: &asdbv1.AerospikeConfigSpec{
										Value: map[string]interface{}{
											asdbv1.ConfKeyNamespace: buildNSConfig(
												image,
												inMemoryNS(namespaceName, 2), inMemoryNS(namespaceName2, 2),
											),
										},
									},
								},
							},
						}

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						removeRF := map[string]interface{}{
							asdbv1.ConfKeyName: namespaceName2,
							asdbv1.ConfKeyStorageEngine: map[string]interface{}{
								"type":      "memory",
								"data-size": 1073741824,
							},
						}

						current.Spec.RackConfig.Racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
							Value: map[string]interface{}{
								asdbv1.ConfKeyNamespace: buildNSConfig(
									current.Spec.Image,
									inMemoryNS(namespaceName, 2),
									removeRF,
								),
							},
						}

						err = envtests.K8sClient.Update(ctx, current)
						Expect(err).ToNot(HaveOccurred())
					})
				})
			})
		})
	}

	// ── Legacy list format (server < 8.1.1) ──────────────────────────────────
	// Delete this Context block when pre-8.1.1 server support is dropped.
	Context("[legacy list format]", func() {
		rfValidationTests(testutil.Pre811EnterpriseImage)
	})

	// ── New YAML map format (server >= 8.1.1) ─────────────────────────────────
	Context("[new YAML map format]", func() {
		rfValidationTests(testutil.LatestEnterpriseImage)
	})
})
