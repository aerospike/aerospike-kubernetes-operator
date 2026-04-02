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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

const (
	aerospikeConfigVolName = "aerospike-config-secret"
	workDir                = "/opt/aerospike"
	otherStorageClass      = "other-storage-class"
)

func uniqueNamespacedName(suffix string) types.NamespacedName {
	name := fmt.Sprintf("ko481-%s-%d", suffix, GinkgoParallelProcess())

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func storageForDevice(devicePath string) asdbv1.AerospikeStorageSpec {
	cd := false
	initM := asdbv1.AerospikeVolumeMethodDeleteFiles

	return asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cd,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputInitMethod:    &initM,
			InputCascadeDelete: &cd,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: devicePath,
				},
			},
			{
				Name: "workdir",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: workDir,
				},
			},
			{
				Name: aerospikeConfigVolName,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: test.AerospikeSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}

func rackNSOverride(devicePath string) *asdbv1.AerospikeConfigSpec {
	return &asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			asdbv1.ConfKeyNamespace: []interface{}{
				map[string]interface{}{
					"name":               "test",
					"replication-factor": 2,
					"strong-consistency": true,
					asdbv1.ConfKeyStorageEngine: map[string]interface{}{
						"type":    "device",
						"devices": []interface{}{devicePath},
					},
				},
			},
		},
	}
}

func deleteCluster(ctx context.Context, nsName types.NamespacedName) {
	aeroCluster := &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsName.Name,
			Namespace: nsName.Namespace,
		},
	}
	// Delete the cluster after each test
	Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
}

func patchFirstPVStorageClassSpec(storage *asdbv1.AerospikeStorageSpec) {
	for i := range storage.Volumes {
		if storage.Volumes[i].Source.PersistentVolume != nil {
			storage.Volumes[i].Source.PersistentVolume.StorageClass = otherStorageClass

			return
		}
	}

	Fail("no PersistentVolume volume found in storage spec")
}

var _ = Describe("AerospikeCluster validation — KO-481 global storage removal", func() {
	ctx := context.TODO()

	Context("Deploy validation", func() {
		Context("spec.rackConfig", func() {
			Context("negative", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("rackcfg-neg")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("rejects when rack namespace device path is not covered by that rack's InputStorage", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					s := storageForDevice("/wrong/path/not-in-namespace")
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID:                   1,
								Revision:             "v1",
								InputStorage:         &s,
								InputAerospikeConfig: rackNSOverride("/expected/missing/device"),
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aero)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"namespace storage device related devicePath /expected/missing/device not found in Storage config",
						).
						Validate(err)
				})

				It("rejects when rack InputStorage is incomplete for rack aerospike namespace device paths", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					incomplete := asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      aero.Spec.Storage.BlockVolumePolicy,
						FileSystemVolumePolicy: aero.Spec.Storage.FileSystemVolumePolicy,
						Volumes: []asdbv1.VolumeSpec{
							{
								Name: aerospikeConfigVolName,
								Source: asdbv1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: test.AerospikeSecretName,
									},
								},
								Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
									Path: "/etc/aerospike/secret",
								},
							},
						},
					}
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID:                   1,
								Revision:             "v1",
								InputStorage:         &incomplete,
								InputAerospikeConfig: rackNSOverride("/test/dev/xvdf"),
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aero)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"namespace storage device related devicePath /test/dev/xvdf not found in Storage config",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("rackcfg-pos")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("allows explicit racks where each rack has InputStorage volumes "+
					"satisfying that rack's aerospike namespace device paths (no spec-level volume list required)", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					s1 := storageForDevice("/rack1/xvda")
					s2 := storageForDevice("/rack2/xvdb")
					stPolicies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      stPolicies.BlockVolumePolicy,
						FileSystemVolumePolicy: stPolicies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s1, InputAerospikeConfig: rackNSOverride("/rack1/xvda")},
							{ID: 2, Revision: "v1", InputStorage: &s2, InputAerospikeConfig: rackNSOverride("/rack2/xvdb")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())
				})

				It("allows multiple racks each using distinct InputStorage paths consistent "+
					"with that rack's namespace config", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					s1 := storageForDevice("/zone-a/ns-dev")
					s2 := storageForDevice("/zone-b/ns-dev")
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "a", InputStorage: &s1, InputAerospikeConfig: rackNSOverride("/zone-a/ns-dev")},
							{ID: 2, Revision: "a", InputStorage: &s2, InputAerospikeConfig: rackNSOverride("/zone-b/ns-dev")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())
				})
			})
		})

		Context("spec.storage", func() {
			Context("negative", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("specstor-neg")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("rejects when global namespace device is not on rack InputStorage (spec vs rack mismatch)", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					wrongRack := storageForDevice("/other/wrong/device")
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &wrongRack},
						},
					}

					err := envtests.K8sClient.Create(ctx, aero)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"namespace storage device related devicePath /test/dev/xvdf not found in Storage config",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("specstor-pos")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("allows CREATE when spec.storage has no volumes"+
					"and validation is satisfied via per-rack InputStorage", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					fullRack := storageForDevice("/test/dev/xvdf")
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &fullRack},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.storage", func() {
			Context("negative", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("updstor-neg")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("rejects update that only mutates top-level spec.storage when there is no rackConfig", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					patchFirstPVStorageClassSpec(&current.Spec.Storage)

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					// Webhook response validation
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"rack storage config cannot be updated",
							"cannot change volumes old").
						Validate(err)
				})

				It("rejects update that mutates top-level spec.storage"+
					"together with another forbidden field (MultiPodPerHost)", func() {
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					patchFirstPVStorageClassSpec(&current.Spec.Storage)
					current.Spec.PodSpec.MultiPodPerHost = ptr.To(false)

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"cannot update MultiPodPerHost setting",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("updstor-pos")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})
			})
		})

		Context("spec.rackConfig", func() {
			Context("negative", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("updrack-neg")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("rejects changes in rack Storage without changing rack Revision", func() {
					s := storageForDevice("/r1/dev")
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s, InputAerospikeConfig: rackNSOverride("/r1/dev")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					r0 := &current.Spec.RackConfig.Racks[0]

					var st *asdbv1.AerospikeStorageSpec
					if r0.InputStorage != nil {
						st = r0.InputStorage.DeepCopy()
					} else {
						st = r0.Storage.DeepCopy()
					}

					patchFirstPVStorageClassSpec(st)
					r0.InputStorage = st

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack storage config cannot be updated",
							"cannot change volumes",
						).
						Validate(err)
				})

				It("rejects update when rack Revision bumps to a revision"+
					"already recorded in status with different Storage", func() {
					sV1 := storageForDevice("/r1/v1")
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &sV1, InputAerospikeConfig: rackNSOverride("/r1/v1")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					base, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					statusSnap := base.DeepCopy()
					statusSnap.Status.RackConfig.Racks = []asdbv1.Rack{
						{ID: 1, Revision: "v2", Storage: sV1},
					}
					Expect(envtests.K8sClient.Status().Update(ctx, statusSnap)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					sV2Conflict := storageForDevice("/r1/v1")
					patchFirstPVStorageClassSpec(&sV2Conflict)

					current.Spec.RackConfig.Racks[0].Revision = "v2"
					current.Spec.RackConfig.Racks[0].InputStorage = &sV2Conflict

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"old rack with same revision v2 already exists with different storage",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				var nsName types.NamespacedName

				BeforeEach(func() {
					nsName = uniqueNamespacedName("updrack-pos")
				})

				AfterEach(func() {
					deleteCluster(ctx, nsName)
				})

				It("allows update that changes rack InputStorage when rack Revision changes"+
					"and status does not imply a conflict", func() {
					s1 := storageForDevice("/r1/a")
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					policies := aero.Spec.Storage
					aero.Spec.Storage = asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      policies.BlockVolumePolicy,
						FileSystemVolumePolicy: policies.FileSystemVolumePolicy,
						Volumes:                nil,
					}
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s1, InputAerospikeConfig: rackNSOverride("/r1/a")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					s2 := storageForDevice("/r1/b")
					patchFirstPVStorageClassSpec(&s2)
					current.Spec.RackConfig.Racks[0].InputStorage = &s2
					current.Spec.RackConfig.Racks[0].Revision = "v2"
					current.Spec.RackConfig.Racks[0].InputAerospikeConfig = rackNSOverride("/r1/b")

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows update that adjusts only spec.storage while rack InputStorage stays unchanged", func() {
					r := storageForDevice("/r-only/dev")
					aero := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aero.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &r, InputAerospikeConfig: rackNSOverride("/r-only/dev")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aero)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					patchFirstPVStorageClassSpec(&current.Spec.Storage)

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
	})
})
