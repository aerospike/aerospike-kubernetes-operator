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
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

var _ = Describe("Rack enabled cluster webhook validation", func() {
	ctx := context.TODO()

	var nsName types.NamespacedName

	BeforeEach(func() {
		nsName = uniqueNamespacedName("rack-enabled")
	})

	AfterEach(func() {
		deleteCluster(ctx, nsName)
	})

	Context("Deploy validation", func() {
		Context("spec.rackConfig", func() {
			Context("negative", func() {
				It("rejects when rack namespace device path is not covered by that rack's InputStorage", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					s := getStorageSpecForDevice("/wrong/path/not-in-namespace")
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
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

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"namespace storage device related devicePath /expected/missing/device not found in Storage config",
						).
						Validate(err)
				})

				It("rejects when rack InputStorage is incomplete for rack aerospike namespace device paths", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					incomplete := asdbv1.AerospikeStorageSpec{
						BlockVolumePolicy:      aeroCluster.Spec.Storage.BlockVolumePolicy,
						FileSystemVolumePolicy: aeroCluster.Spec.Storage.FileSystemVolumePolicy,
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
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
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

					err := envtests.K8sClient.Create(ctx, aeroCluster)
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
				It("allows explicit racks where each rack has InputStorage volumes "+
					"satisfying that rack's aerospike namespace device paths (no spec-level storage config required)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					s1 := getStorageSpecForDevice("/rack1/xvda")
					s2 := getStorageSpecForDevice("/rack2/xvdb")
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s1, InputAerospikeConfig: rackNSOverride("/rack1/xvda")},
							{ID: 2, Revision: "v1", InputStorage: &s2, InputAerospikeConfig: rackNSOverride("/rack2/xvdb")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})

				It("allows one rack with InputStorage and another rack falling back to spec-level storage", func() {
					// Leave aeroCluster.Spec.Storage as from CreateDummy: includes /test/dev/xvdf for the
					// default merged namespace config on rack 2 (no InputAerospikeConfig → global config).
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					rack1Storage := getStorageSpecForDevice("/rack1/xvda")

					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{
								ID:                   1,
								Revision:             "v1",
								InputStorage:         &rack1Storage,
								InputAerospikeConfig: rackNSOverride("/rack1/xvda"),
							},
							{
								ID:                   2,
								Revision:             "v1",
								InputStorage:         nil, // uses cluster.Spec.Storage after mutating webhook
								InputAerospikeConfig: nil, // uses merged global AerospikeConfig (/test/dev/xvdf)
							},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.rackConfig", func() {
			Context("positive", func() {
				It("allows update that adjusts only spec.storage while rack InputStorage stays unchanged", func() {
					r := getStorageSpecForDevice("/r-only/dev")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &r, InputAerospikeConfig: rackNSOverride("/r-only/dev")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					patchFirstPVStorageClassSpec(&current.Spec.Storage)

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
	})
})
