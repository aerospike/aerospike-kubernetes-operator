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

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

const (
	newRackRevision = "v2"
)

var _ = Describe("Rack revision webhook validation", func() {
	ctx := context.TODO()

	// Test update validation for rack revision
	Context("Update validation", func() {
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
						{ID: 1, Revision: newRackRevision, Storage: sV1},
					}
					Expect(envtests.K8sClient.Status().Update(ctx, statusSnap)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					sV2Conflict := storageForDevice("/r1/v1")
					patchFirstPVStorageClassSpec(&sV2Conflict)

					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
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
					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					current.Spec.RackConfig.Racks[0].InputAerospikeConfig = rackNSOverride("/r1/b")

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
	})
})
