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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("Storage webhook validation", func() {
	ctx := context.TODO()

	var nsName types.NamespacedName

	BeforeEach(func() {
		nsName = uniqueNamespacedName("storage-webhook")
	})

	AfterEach(func() {
		deleteCluster(ctx, nsName)
	})

	// storageDeployValidationTests registers all storage deploy-validation It blocks
	// for the given server image.  Storage validation walks aerospikeConfig namespace
	// entries to verify device paths, so it must run for both config formats.
	// Delete the legacy context below when pre-8.1.1 server support is dropped.
	storageDeployValidationTests := func(image string) {
		Context("spec.storage", func() {
			Context("negative", func() {
				It("rejects when global namespace device is not on rack InputStorage (spec vs rack mismatch)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					wrongRack := getStorageSpecForDevice("/other/wrong/device")
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &wrongRack},
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
				It("allows CREATE when spec.storage is not set"+
					"and validation is satisfied via per-rack InputStorage", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					fullRack := getStorageSpecForDevice("/test/dev/xvdf")
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &fullRack},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})
			})
		})
	}

	// storageUpdateValidationTests registers all storage update-validation It blocks
	// for the given server image.
	storageUpdateValidationTests := func(image string) {
		Context("spec.storage", func() {
			Context("negative", func() {
				It("rejects update that only mutates top-level spec.storage when there is no rackConfig", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					patchFirstPVStorageClassSpec(&current.Spec.Storage)

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"rack storage config cannot be updated",
							"cannot change volumes old").
						Validate(err)
				})

				It("rejects update that mutates top-level spec.storage"+
					"together with another forbidden field (MultiPodPerHost)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

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

				It("rejects update when rack#1 InputStorage is removed and spec.storage has no volumes", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					r1 := getStorageSpecForDevice("/test/dev/xvdf")
					r2 := getStorageSpecForDevice("/rack2/xvdb")
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &r1, InputAerospikeConfig: rackNSOverride("/test/dev/xvdf", image)},
							{ID: 2, Revision: "v1", InputStorage: &r2, InputAerospikeConfig: rackNSOverride("/rack2/xvdb", image)},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					for i := range current.Spec.RackConfig.Racks {
						if current.Spec.RackConfig.Racks[i].ID == 1 {
							current.Spec.RackConfig.Racks[i].InputStorage = nil
							break
						}
					}

					err = envtests.K8sClient.Update(ctx, current)
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
				It("allows removing top-level spec.storage when rack use InputStorage", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					rackSt := getStorageSpecForDevice("/test/dev/xvdf")
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &rackSt, InputAerospikeConfig: rackNSOverride("/test/dev/xvdf", image)},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.Storage = asdbv1.AerospikeStorageSpec{}

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows removing rack#1 InputStorage when top-level spec.storage is defined", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					r1 := getStorageSpecForDevice("/test/dev/xvdf")
					r2 := getStorageSpecForDevice("/rack2/xvdb")
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &r1, InputAerospikeConfig: rackNSOverride("/test/dev/xvdf", image)},
							{ID: 2, Revision: "v1", InputStorage: &r2, InputAerospikeConfig: rackNSOverride("/rack2/xvdb", image)},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					for i := range current.Spec.RackConfig.Racks {
						if current.Spec.RackConfig.Racks[i].ID == 1 {
							current.Spec.RackConfig.Racks[i].InputStorage = nil
							break
						}
					}

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allow moving from spec level storage to rack level storage by removing the spec level storage", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					rackSt := getStorageSpecForDevice("/test/dev/xvdf")
					current.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &rackSt, InputAerospikeConfig: rackNSOverride("/test/dev/xvdf", image)},
						},
					}
					current.Spec.Storage = asdbv1.AerospikeStorageSpec{}

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows moving from per-rack InputStorage to spec-level storage", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					rackSt := getStorageSpecForDevice("/test/dev/xvdf")
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &rackSt, InputAerospikeConfig: rackNSOverride("/test/dev/xvdf", image)},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					dummy := testCluster.CreateDummyAerospikeClusterForImage(nsName, 2, image)
					current.Spec.Storage = dummy.Spec.Storage

					for i := range current.Spec.RackConfig.Racks {
						current.Spec.RackConfig.Racks[i].InputStorage = nil
					}

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
	}

	// ── Legacy list format (server < 8.1.1) ──────────────────────────────────
	// Delete these two Context blocks when pre-8.1.1 server support is dropped.
	Context("Deploy validation [legacy list format]", func() {
		storageDeployValidationTests(testutil.Pre811EnterpriseImage)
	})

	Context("Update validation [legacy list format]", func() {
		storageUpdateValidationTests(testutil.Pre811EnterpriseImage)
	})

	// ── New YAML map format (server >= 8.1.1) ─────────────────────────────────
	Context("Deploy validation [new YAML map format]", func() {
		storageDeployValidationTests(testutil.LatestEnterpriseImage)
	})

	Context("Update validation [new YAML map format]", func() {
		storageUpdateValidationTests(testutil.LatestEnterpriseImage)
	})
})
