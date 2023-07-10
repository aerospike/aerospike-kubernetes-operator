package test

import (
	goctx "context"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

// Test cluster cr updation
var _ = Describe(
	"ClusterStorageCleanUp", func() {
		ctx := goctx.TODO()

		// Check defaults
		// Cleanup all volumes
		// Cleanup selected volumes
		// Update
		Context(
			"When doing valid operations", func() {
				clusterName := "storage-cleanup"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				BeforeEach(
					func() {
						// Deploy cluster with 6 racks to remove rack one by one and check for pvc
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)
						racks := getDummyRackConf(1, 2)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				// Check defaults
				It(
					"Try Defaults", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						oldPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// removeRack should not remove any pvc
						err = removeLastRack(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						newPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// Check for pvc, both list should be same
						err = matchPVCList(oldPVCList, newPVCList)
						Expect(err).ToNot(HaveOccurred())

					},
				)

				It(
					"Try CleanupAllVolumes", func() {

						// Set common FileSystemVolumePolicy, BlockVolumePolicy to true
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						remove := true
						aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &remove
						aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &remove

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// RackID to be used to check if pvc are removed
						racks := aeroCluster.Spec.RackConfig.Racks
						lastRackID := racks[len(racks)-1].ID
						stsName := aeroCluster.Name + "-" + strconv.Itoa(lastRackID)

						// remove Rack should remove all rack's pvc
						aeroCluster.Spec.RackConfig.Racks = racks[:len(racks)-1]
						aeroCluster.Spec.Size--

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						newPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// Check for pvc
						for _, pvc := range newPVCList {
							if strings.Contains(pvc.Name, stsName) {
								err := fmt.Errorf(
									"PVC %s not removed for cluster sts %s",
									pvc.Name, stsName,
								)
								Expect(err).ToNot(HaveOccurred())
							}
						}
					},
				)

				It(
					"Try CleanupSelectedVolumes", func() {
						// Set common FileSystemVolumePolicy, BlockVolumePolicy to false and true for selected volumes
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						remove := true
						aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &remove
						aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &remove
						vRemove := false
						aeroCluster.Spec.Storage.Volumes[0].InputCascadeDelete = &vRemove

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// RackID to be used to check if pvc are removed
						racks := aeroCluster.Spec.RackConfig.Racks
						lastRackID := racks[len(racks)-1].ID
						stsName := aeroCluster.Name + "-" + strconv.Itoa(lastRackID)
						// This should not be removed

						pvcName := aeroCluster.Spec.Storage.Volumes[0].Name
						Expect(err).ToNot(HaveOccurred())

						pvcNamePrefix := pvcName + "-" + stsName

						// remove Rack should remove all rack's pvc
						aeroCluster.Spec.RackConfig.Racks = racks[:len(racks)-1]
						aeroCluster.Spec.Size--

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						newPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// Check for pvc
						var found bool
						for pvcIndex := range newPVCList {
							if strings.HasPrefix(newPVCList[pvcIndex].Name, pvcNamePrefix) {
								found = true
								continue
							}

							if utils.IsPVCTerminating(&newPVCList[pvcIndex]) {
								// Ignore PVC that are being terminated.
								continue
							}

							if strings.Contains(newPVCList[pvcIndex].Name, stsName) {
								err := fmt.Errorf(
									"PVC %s not removed for cluster sts %s",
									newPVCList[pvcIndex].Name, stsName,
								)
								Expect(err).ToNot(HaveOccurred())
							}
						}
						if !found {
							err := fmt.Errorf(
								"PVC with prefix %s should not be removed for cluster sts %s",
								pvcNamePrefix, stsName,
							)
							Expect(err).ToNot(HaveOccurred())

						}
					},
				)

				AfterEach(
					func() {
						// cleanup cluster
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())
						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)
			},
		)
	},
)

// Test cluster cr updation
var _ = Describe(
	"RackUsingLocalStorage", func() {
		ctx := goctx.TODO()

		// Positive
		// Global storage given, no local (already checked in normal cluster tests)
		// Global storage given, local also given
		// Local storage should be used for cascadeDelete, aerospikeConfig
		Context(
			"When doing valid operations", func() {
				clusterName := "rack-storage"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				devName := "/test/dev/rackstorage"
				devPVCName := "ns"
				racks := getDummyRackConf(1)
				// AerospikeConfig is only patched
				racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
					Value: map[string]interface{}{
						"namespaces": []interface{}{
							map[string]interface{}{
								"name": "test",
								"storage-engine": map[string]interface{}{
									"type":    "device",
									"devices": []interface{}{devName},
								},
							},
						},
					},
				}
				remove := true
				// Rack is completely replaced
				racks[0].InputStorage = &asdbv1.AerospikeStorageSpec{
					BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
						InputCascadeDelete: &remove,
					},
					FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
						InputCascadeDelete: &remove,
						InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					},
					Volumes: []asdbv1.VolumeSpec{
						{
							Name: devPVCName,
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("1Gi"),
									StorageClass: storageClass,
									VolumeMode:   corev1.PersistentVolumeBlock,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: devName,
							},
						},
						{
							Name: "workdir",
							Source: asdbv1.VolumeSource{
								PersistentVolume: &asdbv1.PersistentVolumeSpec{
									Size:         resource.MustParse("1Gi"),
									StorageClass: storageClass,
									VolumeMode:   corev1.PersistentVolumeFilesystem,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/opt/aerospike",
							},
						},
						{
							Name: aerospikeConfigSecret,
							Source: asdbv1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: tlsSecretName,
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/etc/aerospike/secret",
							},
						},
					},
				}

				BeforeEach(
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 3,
						)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"UseForAerospikeConfig", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						newPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// There is only single rack
						pvcName := devPVCName
						Expect(err).ToNot(HaveOccurred())

						stsName := aeroCluster.Name + "-" + strconv.Itoa(racks[0].ID)
						pvcNamePrefix := pvcName + "-" + stsName

						// If PVC is created and no error in deployment then it mean aerospikeConfig
						// has successfully used rack storage
						var found bool
						for _, pvc := range newPVCList {
							if strings.HasPrefix(pvc.Name, pvcNamePrefix) {
								found = true
								break
							}
						}
						if !found {
							err := fmt.Errorf(
								"PVC with prefix %s not found in cluster pvcList %v",
								pvcNamePrefix, newPVCList,
							)
							Expect(err).ToNot(HaveOccurred())
						}
					},
				)

				It(
					"UseForCascadeDelete", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// There is only single rack
						podName := aeroCluster.Name + "-" + strconv.Itoa(racks[0].ID) + "-" + strconv.Itoa(int(aeroCluster.Spec.Size-1))

						err = scaleDownClusterTest(
							k8sClient, ctx, clusterNamespacedName, 1,
						)
						Expect(err).ToNot(HaveOccurred())

						newPVCList, err := getAeroClusterPVCList(
							aeroCluster, k8sClient,
						)
						Expect(err).ToNot(HaveOccurred())

						// Check for pvc
						for _, pvc := range newPVCList {
							if strings.Contains(pvc.Name, podName) {
								err := fmt.Errorf(
									"PVC %s not removed for cluster pod %s",
									pvc.Name, podName,
								)
								Expect(err).ToNot(HaveOccurred())
							}
						}
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
			},
		)

		// Negative
		// Update rack storage should fail
		// (nil -> val), (val -> nil), (val1 -> val2)
		Context(
			"When doing invalid operations", func() {
				clusterName := "rack-storage-invalid"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				Context(
					"Deploy", func() {
						It(
							"should fail for not having aerospikeConfig namespace Storage device in rack storage",
							func() {
								// Deploy cluster with 6 racks to remove rack one by one and check for pvc
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 3,
								)
								racks := getDummyRackConf(1)
								// AerospikeConfig is only patched
								racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
									Value: map[string]interface{}{
										"namespaces": []interface{}{
											map[string]interface{}{
												"name": "test",
												"storage-engine": map[string]interface{}{
													"type":    "device",
													"devices": []interface{}{"random/device/name"},
												},
											},
										},
									},
								}

								// Rack is completely replaced
								racks[0].InputStorage = &asdbv1.AerospikeStorageSpec{
									FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
										InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
										InputCascadeDelete: &cascadeDeleteTrue,
									},
									Volumes: []asdbv1.VolumeSpec{
										{
											Name: "workdir",
											Source: asdbv1.VolumeSource{
												PersistentVolume: &asdbv1.PersistentVolumeSpec{
													Size:         resource.MustParse("1Gi"),
													StorageClass: storageClass,
													VolumeMode:   corev1.PersistentVolumeFilesystem,
												},
											},
											Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
												Path: "/opt/aerospike",
											},
										},
									},
								}

								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err := k8sClient.Create(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)

						// Add test while rack using common aeroConfig but local storage, fail for mismatch
						It(
							"CommonConfigLocalStorage: should fail for deploying with wrong Storage. "+
								"Storage doesn't have namespace related volumes",
							func() {
								// Deploy cluster with 6 racks to remove rack one by one and check for pvc
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								racks := getDummyRackConf(1)
								// Rack is completely replaced
								volumes := []asdbv1.VolumeSpec{
									{
										Name: "workdir",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/new",
										},
									},
								}
								racks[0].InputStorage = getStorage(volumes)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err := k8sClient.Create(ctx, aeroCluster)
								Expect(err).Should(HaveOccurred())
							},
						)

					},
				)

				Context(
					"Update", func() {

						It(
							"NilToValue: should fail for updating Storage. Cannot be updated",
							func() {
								// Deploy cluster with 6 racks to remove rack one by one and check for pvc
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								racks := getDummyRackConf(1)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).ToNot(HaveOccurred())

								// Update storage
								aeroCluster, err = getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								// Rack is completely replaced
								volumes := []asdbv1.VolumeSpec{
									{
										Name: "workdirnew",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/new",
										},
									},
								}
								volumes = append(
									volumes,
									aeroCluster.Spec.Storage.Volumes...,
								)
								racks[0].InputStorage = getStorage(volumes)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err = k8sClient.Update(
									goctx.TODO(), aeroCluster,
								)
								Expect(err).Should(HaveOccurred())

								_ = deleteCluster(k8sClient, ctx, aeroCluster)
							},
						)

						It(
							"ValueToNil: should fail for updating Storage. Cannot be updated",
							func() {
								// Deploy cluster with 6 racks to remove rack one by one and check for pvc
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								racks := getDummyRackConf(1)
								// Rack is completely replaced
								volumes := []asdbv1.VolumeSpec{
									{
										Name: "workdirnew",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/new",
										},
									},
								}
								volumes = append(
									volumes,
									aeroCluster.Spec.Storage.Volumes...,
								)
								racks[0].InputStorage = getStorage(volumes)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).ToNot(HaveOccurred())

								// Update storage
								aeroCluster, err = getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								// Rack is completely replaced
								racks[0].InputStorage = &asdbv1.AerospikeStorageSpec{}
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err = k8sClient.Update(
									goctx.TODO(), aeroCluster,
								)
								Expect(err).Should(HaveOccurred())

								_ = deleteCluster(k8sClient, ctx, aeroCluster)
							},
						)

						It(
							"ValueToValue: should fail for updating Storage. Cannot be updated",
							func() {
								// Deploy cluster with 6 racks to remove rack one by one and check for pvc
								aeroCluster := createDummyAerospikeCluster(
									clusterNamespacedName, 2,
								)
								racks := getDummyRackConf(1)
								// Rack is completely replaced
								volumes := []asdbv1.VolumeSpec{
									{
										Name: "workdirnew",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/new",
										},
									},
								}
								volumes = append(
									volumes,
									aeroCluster.Spec.Storage.Volumes...,
								)
								racks[0].InputStorage = getStorage(volumes)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								// Deploy cluster
								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).ToNot(HaveOccurred())

								// Update storage
								aeroCluster, err = getCluster(
									k8sClient, ctx, clusterNamespacedName,
								)
								Expect(err).ToNot(HaveOccurred())

								// Rack is completely replaced
								volumes = []asdbv1.VolumeSpec{
									{
										Name: "workdirnew2",
										Source: asdbv1.VolumeSource{
											PersistentVolume: &asdbv1.PersistentVolumeSpec{
												Size:         resource.MustParse("1Gi"),
												StorageClass: storageClass,
												VolumeMode:   corev1.PersistentVolumeFilesystem,
											},
										},
										Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
											Path: "/opt/aerospike/new2",
										},
									},
								}
								volumes = append(
									volumes,
									aeroCluster.Spec.Storage.Volumes...,
								)
								racks[0].InputStorage = getStorage(volumes)
								aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: racks}

								err = k8sClient.Update(
									goctx.TODO(), aeroCluster,
								)
								Expect(err).Should(HaveOccurred())

								_ = deleteCluster(k8sClient, ctx, aeroCluster)
							},
						)
					},
				)
			},
		)

	},
)

func getStorage(volumes []asdbv1.VolumeSpec) *asdbv1.AerospikeStorageSpec {
	cascadeDelete := true
	storage := asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cascadeDelete,
			InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
		},
		Volumes: volumes,
	}

	return &storage
}

func matchPVCList(oldPVCList, newPVCList []corev1.PersistentVolumeClaim) error {
	for oldPVCIndex := range oldPVCList {
		var found bool

		for newPVCIndex := range newPVCList {
			if oldPVCList[oldPVCIndex].Name == newPVCList[newPVCIndex].Name {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf(
				"PVC %s not found in new PVCList %v", oldPVCList[oldPVCIndex].Name, newPVCList,
			)
		}
	}

	return nil
}
