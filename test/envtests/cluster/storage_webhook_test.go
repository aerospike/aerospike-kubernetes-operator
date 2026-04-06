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
)

var _ = Describe("Storage webhook validation", func() {
	ctx := context.TODO()

	Context("Deploy validation", func() {
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
					wrongRack := getStorageSpecForDevice("/other/wrong/device")
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
					fullRack := getStorageSpecForDevice("/test/dev/xvdf")
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
		})
	})
})
