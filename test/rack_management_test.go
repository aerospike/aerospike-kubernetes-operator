package test

import (
	goctx "context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"RackManagement", func() {
		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {

				It(
					"Should validate rack management flow", func() {
						clusterName := "rack-management1"
						clusterNamespacedName := getNamespacedName(
							clusterName, namespace,
						)

						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						// Setup: Deploy cluster without rack
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Op1: AddRackInCluster
						By("Adding 1st rack in the cluster")
						err = addRack(
							k8sClient, ctx, clusterNamespacedName,
							&asdbv1.Rack{ID: 1},
						)
						Expect(err).ToNot(HaveOccurred())
						err = validateRackEnabledCluster(
							k8sClient, ctx,
							clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Adding rack in existing racks list")
						err = addRack(
							k8sClient, ctx, clusterNamespacedName,
							&asdbv1.Rack{ID: 2},
						)
						Expect(err).ToNot(HaveOccurred())
						err = validateRackEnabledCluster(
							k8sClient, ctx,
							clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Op2: UpdateRackEnabledNamespacesList
						nsListInterface := aeroCluster.Spec.AerospikeConfig.Value["namespaces"]
						nsList := nsListInterface.([]interface{})
						nsName := nsList[0].(map[string]interface{})["name"].(string)

						By("Adding rack enabled namespace")

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.RackConfig.Namespaces = []string{nsName}

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						enabled, err := isNamespaceRackEnabled(
							logger, k8sClient, ctx, clusterNamespacedName, nsName,
						)
						Expect(err).ToNot(HaveOccurred())
						Expect(enabled).Should(BeTrue())

						By("Removing rack enabled namespace")

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.RackConfig.Namespaces = []string{}

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						enabled, err = isNamespaceRackEnabled(
							logger, k8sClient, ctx, clusterNamespacedName, nsName,
						)
						Expect(err).ToNot(HaveOccurred())
						Expect(enabled).Should(BeFalse())

						// Op3: RemoveRack
						By("Removing single rack")

						err = removeLastRack(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Removing all racks")

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						rackConf := asdbv1.RackConfig{}
						aeroCluster.Spec.RackConfig = rackConf

						// This will also indirectly check if older rack is removed or not.
						// If older node is not deleted then cluster sz will not be as expected
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// cleanup: Remove the cluster
						By("Cleaning up the cluster")

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"should allow Cluster sz less than number of racks",
					func() {
						clusterName := "rack-management2"
						clusterNamespacedName := getNamespacedName(
							clusterName, namespace,
						)

						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						racks := getDummyRackConf(1, 2, 3, 4, 5)
						aeroCluster.Spec.RackConfig.Racks = racks

						// Setup: Deploy cluster without rack
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Op1: AddRackInCluster
						By("Adding 1st rack in the cluster")
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						racks = getDummyRackConf(1, 2, 3, 4, 5, 6)
						aeroCluster.Spec.RackConfig.Racks = racks

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Op2: RemoveRack
						By("Removing single rack")
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						racks = getDummyRackConf(1, 2, 3, 4, 5)
						aeroCluster.Spec.RackConfig.Racks = racks

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						err = validateRackEnabledCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						// cleanup: Remove the cluster
						By("Cleaning up the cluster")

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				Context(
					"When using valid rack aerospike config", func() {
						// WARNING: Tests assume that only "service" is updated in aerospikeConfig, Validation is hardcoded

						clusterName := "rack-config-update"
						clusterNamespacedName := getNamespacedName(
							clusterName, namespace,
						)
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						racks := getDummyRackConf(1, 2)

						It(
							"Should validate whole flow of rack.AerospikeConfig use",
							func() {

								// Op1: Add rack.AerospikeConfig
								By("Deploying cluster having rack.AerospikeConfig")

								racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max":       10000,
											"migrate-fill-delay": 30,
										},
									},
								}
								racks[1].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max":       12000,
											"migrate-fill-delay": 30,
										},
									},
								}

								// Make a copy to validate later
								var racksCopy []asdbv1.Rack
								err := Copy(&racksCopy, &racks)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racksCopy}
								err = deployCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								err = validateRackEnabledCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())
								for rackIndex := range racks {
									err = validateAerospikeConfigServiceUpdate(
										logger, k8sClient, ctx, clusterNamespacedName, &racks[rackIndex],
									)
									Expect(err).ToNot(HaveOccurred())
								}

								// Op2: Update rack.AerospikeConfig
								By("Update rack.AerospikeConfig")

								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max": 12000,
										},
									},
								}
								racks[1].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max": 14000,
										},
									},
								}

								// Make a copy to validate later
								racksCopy = []asdbv1.Rack{}
								err = Copy(&racksCopy, &racks)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racksCopy}

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								err = validateRackEnabledCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())
								for rackIndex := range racks {
									err = validateAerospikeConfigServiceUpdate(
										logger, k8sClient, ctx, clusterNamespacedName, &racks[rackIndex],
									)
									Expect(err).ToNot(HaveOccurred())
								}

								// Op3: Remove rack.AerospikeConfig
								By("Remove rack.AerospikeConfig")

								aeroCluster, err = getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								racks[0].InputAerospikeConfig = nil
								racks[1].InputAerospikeConfig = nil

								// Make a copy to validate later
								racksCopy = []asdbv1.Rack{}
								err = Copy(&racksCopy, &racks)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racksCopy}
								// Increase size also so that below wait func wait for new cluster
								aeroCluster.Spec.Size++

								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								err = validateRackEnabledCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								// Config for both rack should have been taken from default config
								// Default proto-fd-max is 15000. So check for default value
								racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max": defaultProtofdmax,
										},
									},
								}
								racks[1].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"service": map[string]interface{}{
											"proto-fd-max": defaultProtofdmax,
										},
									},
								}
								for rackIndex := range racks {
									err = validateAerospikeConfigServiceUpdate(
										logger, k8sClient, ctx, clusterNamespacedName, &racks[rackIndex],
									)
									Expect(err).ToNot(HaveOccurred())
								}

								// cleanup: Remove the cluster
								By("Cleaning up the cluster")

								err = deleteCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)

				Context(
					"When using valid rack storage config", func() {

						clusterName := "rack-specific-storage"
						clusterNamespacedName := getNamespacedName(
							clusterName, namespace,
						)
						aeroCluster := createDummyRackAwareWithStorageAerospikeCluster(
							clusterNamespacedName, 2,
						)

						It(
							"Should validate empty common storage if per rack storage is provided",
							func() {

								err := deployCluster(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								err = validateRackEnabledCluster(
									k8sClient, ctx,
									clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {
				clusterName := "invalid-rack-config"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				Context(
					"when deploy cluster with invalid rack ", func() {
						Context(
							"InvalidRackID", func() {
								It(
									"should fail for DuplicateRackID", func() {
										aeroCluster := createDummyAerospikeCluster(
											clusterNamespacedName, 2,
										)
										rackConf := asdbv1.RackConfig{
											Racks: []asdbv1.Rack{
												{ID: 2}, {ID: 2},
											},
										}
										aeroCluster.Spec.RackConfig = rackConf
										err := deployCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)
								It(
									"should fail for OutOfRangeRackID", func() {
										aeroCluster := createDummyAerospikeCluster(
											clusterNamespacedName, 2,
										)
										rackConf := asdbv1.RackConfig{
											Racks: []asdbv1.Rack{
												{ID: 1},
												{ID: asdbv1.MaxRackID + 1},
											},
										}
										aeroCluster.Spec.RackConfig = rackConf
										err := deployCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)
								It(
									"should fail for using defaultRackID",
									func() {
										aeroCluster := createDummyAerospikeCluster(
											clusterNamespacedName, 2,
										)
										rackConf := asdbv1.RackConfig{
											Racks: []asdbv1.Rack{
												{ID: 1},
												{ID: asdbv1.DefaultRackID},
											},
										}
										aeroCluster.Spec.RackConfig = rackConf
										err := deployCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)
							},
						)

						Context(
							"When using invalid rack storage config", func() {

								It(
									"Should fail for empty common storage if per rack storage is not provided",
									func() {

										aeroCluster := createDummyRackAwareWithStorageAerospikeCluster(
											clusterNamespacedName, 2,
										)

										aeroCluster.Spec.RackConfig.Racks[0].InputStorage = nil

										err := deployCluster(k8sClient, ctx, aeroCluster)
										Expect(err).Should(HaveOccurred())

									},
								)
							},
						)

						Context(
							"When using invalid rack aerospikeConfig to deploy cluster",
							func() {

								It(
									"should fail for invalid aerospikeConfig",
									func() {
										aeroCluster := createDummyRackAwareAerospikeCluster(
											clusterNamespacedName, 2,
										)

										aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
											Value: map[string]interface{}{
												"namespaces": "invalidConf",
											},
										}
										err := deployCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)

								It(
									"should fail for different aerospikeConfig.service.migrate-fill-delay value across racks",
									func() {
										aeroCluster := createDummyRackAwareAerospikeCluster(
											clusterNamespacedName, 2,
										)

										RackASConfig := &asdbv1.AerospikeConfigSpec{
											Value: map[string]interface{}{
												"service": map[string]interface{}{
													"migrate-fill-delay": 200,
												},
											},
										}
										// set migrate-fill-delay only in rack 2
										aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks,
											asdbv1.Rack{ID: 2, InputAerospikeConfig: RackASConfig})

										err := deployCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)

								Context(
									"When using invalid rack.AerospikeConfig.namespace config",
									func() {
										// Should we test for overridden fields
										Context(
											"When using invalid rack.AerospikeConfig.namespace.storage config",
											func() {
												It(
													"should fail for invalid storage-engine.device, cannot have 3 devices in single device string",
													func() {
														aeroCluster := createDummyRackAwareAerospikeCluster(
															clusterNamespacedName,
															2,
														)
														namespaceConfig :=
															aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
														if _, ok :=
															namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
															vd := []asdbv1.VolumeSpec{
																{
																	Name: "nsvol1",
																	Source: asdbv1.VolumeSource{
																		PersistentVolume: &asdbv1.PersistentVolumeSpec{
																			Size:         resource.MustParse("1Gi"),
																			StorageClass: storageClass,
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
																			StorageClass: storageClass,
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
																			StorageClass: storageClass,
																			VolumeMode:   v1.PersistentVolumeBlock,
																		},
																	},
																	Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
																		Path: "/dev/xvdf3",
																	},
																},
															}
															aeroCluster.Spec.Storage.Volumes = append(
																aeroCluster.Spec.Storage.Volumes,
																vd...,
															)

															aeroConfig := asdbv1.AerospikeConfigSpec{
																Value: map[string]interface{}{
																	"namespaces": []interface{}{
																		map[string]interface{}{
																			"name": "test",
																			"storage-engine": map[string]interface{}{
																				"type":    "device",
																				"devices": []interface{}{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"},
																			},
																		},
																	},
																},
															}
															aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
															err := deployCluster(
																k8sClient, ctx,
																aeroCluster,
															)
															Expect(err).Should(HaveOccurred())
														}
													},
												)

												It(
													"should fail for invalid storage-engine.device, cannot a device which doesn't exist in BlockStorage",
													func() {
														aeroCluster := createDummyRackAwareAerospikeCluster(
															clusterNamespacedName,
															2,
														)
														namespaceConfig :=
															aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
														if _, ok :=
															namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
															aeroConfig := asdbv1.AerospikeConfigSpec{
																Value: map[string]interface{}{
																	"namespaces": []interface{}{
																		map[string]interface{}{
																			"name": "test",
																			"storage-engine": map[string]interface{}{
																				"type":    "device",
																				"devices": []interface{}{"andRandomDevice"},
																			},
																		},
																	},
																},
															}
															aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
															err := deployCluster(
																k8sClient, ctx,
																aeroCluster,
															)
															Expect(err).Should(HaveOccurred())
														}
													},
												)
											},
										)

										It(
											"should fail for invalid xdr config. mountPath for digestlog not present in fileStorage",
											func() {
												aeroCluster := createDummyRackAwareAerospikeCluster(
													clusterNamespacedName, 2,
												)
												namespaceConfig :=
													aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
												if _, ok :=
													namespaceConfig["storage-engine"].(map[string]interface{})["devices"]; ok {
													aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
													aeroConfig := asdbv1.AerospikeConfigSpec{
														Value: map[string]interface{}{
															"xdr": map[string]interface{}{
																"enable-xdr":         false,
																"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
															},
														},
													}
													aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
													err := deployCluster(
														k8sClient, ctx,
														aeroCluster,
													)
													Expect(err).Should(HaveOccurred())

												}
											},
										)
										// Replication-factor can not be updated
									},
								)

							},
						)
					},
				)
				Context(
					"when update cluster with invalid rack", func() {

						BeforeEach(
							func() {
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								rackConf := asdbv1.RackConfig{
									Racks: []asdbv1.Rack{{ID: 1}, {ID: 2}},
								}
								aeroCluster.Spec.RackConfig = rackConf

								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).ToNot(HaveOccurred())
							},
						)

						AfterEach(
							func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								_ = deleteCluster(k8sClient, ctx, aeroCluster)
							},
						)

						It(
							"should fail for updating existing rack", func() {
								aeroCluster, err := getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig.Racks[0].Region = "randomValue"
								err = updateCluster(k8sClient, ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)

						Context(
							"InvalidRackID", func() {
								It(
									"should fail for DuplicateRackID", func() {
										aeroCluster, err := getCluster(
											k8sClient, ctx,
											clusterNamespacedName,
										)
										Expect(err).ToNot(HaveOccurred())

										aeroCluster.Spec.RackConfig.Racks = append(
											aeroCluster.Spec.RackConfig.Racks,
											aeroCluster.Spec.RackConfig.Racks...,
										)
										err = updateCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)
								It(
									"should fail for OutOfRangeRackID", func() {
										aeroCluster, err := getCluster(
											k8sClient, ctx,
											clusterNamespacedName,
										)
										Expect(err).ToNot(HaveOccurred())

										aeroCluster.Spec.RackConfig.Racks = append(
											aeroCluster.Spec.RackConfig.Racks,
											asdbv1.Rack{ID: 20000000000},
										)
										err = updateCluster(
											k8sClient, ctx, aeroCluster,
										)
										Expect(err).Should(HaveOccurred())
									},
								)
							},
						)

						Context(
							"When using invalid rack aerospikeConfig to update cluster",
							func() {

							},
						)
					},
				)
			},
		)

		Context("When testing failed rack recovery by scale down", func() {
			clusterName := "failed-rack-config"
			clusterNamespacedName := getNamespacedName(
				clusterName, namespace,
			)
			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					racks := getDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}
					aeroCluster.Spec.PodSpec.MultiPodPerHost = false

					By("Deploying cluster")
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It("Should recover after scaling down failed rack", func() {
				By("Scaling up the cluster size beyond the available k8s nodes, pods will go in failed state")
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				nodes, err := getNodeList(ctx, k8sClient)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Size = int32(len(nodes.Items) + 1)

				// scaleup, no need to wait for long
				err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*2)
				Expect(err).To(HaveOccurred())

				By("Scaling down the cluster size, failed pods should recover")
				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.Size = int32(len(nodes.Items) - 1)

				err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*10)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context(
			"When testing failed rack recovery by rolling restart", func() {
				clusterName := "cl-resource-insuff"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				BeforeEach(
					func() {
						aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
						racks := getDummyRackConf(1, 2)
						aeroCluster.Spec.RackConfig.Racks = racks
						aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

				It("UpdateClusterWithResource: should recover after reverting back to schedulable resources", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

					err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
					Expect(err).Should(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				It("UpdateClusterWithResource: should recover failed pods first after reverting back"+
					" to schedulable resources", func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

					err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*3)
					Expect(err).To(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")
					aeroCluster.Spec.RackConfig.Racks[0].ID = 2
					aeroCluster.Spec.RackConfig.Racks[1].ID = 1

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})
			})
	},
)
