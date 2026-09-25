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

var _ = Describe("EnableParallelScaleDownAcrossRacks webhook validation", func() {
	ctx := context.TODO()

	var clusterNamespacedName types.NamespacedName

	BeforeEach(func() {
		clusterNamespacedName = uniqueNamespacedName("parallel-scaledown")
	})

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	// setStatus stamps status.Size and status.RackConfig.Racks on the live object to simulate
	// the last-completed-reconcile state. Returns the re-fetched object for the next spec update.
	setStatus := func(size int32, racks []asdbv1.Rack) *asdbv1.AerospikeCluster {
		GinkgoHelper()

		cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		cur.Status.Size = size
		cur.Status.RackConfig.Racks = racks
		Expect(envtests.K8sClient.Status().Update(ctx, cur)).To(Succeed())

		cur, err = testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		return cur
	}

	// createCluster creates a cluster with the given size, racks, and flag value.
	// Returns the re-fetched live object (with ResourceVersion).
	createCluster := func(size int32, racks []asdbv1.Rack, flagEnabled *bool) *asdbv1.AerospikeCluster {
		GinkgoHelper()

		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
		aeroCluster.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = flagEnabled

		if len(racks) > 0 {
			aeroCluster.Spec.RackConfig.Racks = racks
		}

		Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

		cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		return cur
	}

	// simpleRacks returns n minimal racks with sequential IDs starting at 1.
	simpleRacks := func(n int) []asdbv1.Rack {
		racks := make([]asdbv1.Rack, n)
		for i := range racks {
			racks[i] = asdbv1.Rack{ID: i + 1}
		}

		return racks
	}

	// ─────────────────────────────────────────────────────────────────────────

	Context("Deploy validation", func() {
		Context("spec.rackConfig.enableParallelScaleDownAcrossRacks", func() {
			Context("positive", func() {
				It("allows create with flag unset", func() {
					Expect(envtests.K8sClient.Create(
						ctx, testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4),
					)).To(Succeed())
				})

				It("allows create with flag explicitly enabled", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})

				It("allows create with flag explicitly disabled", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(false)

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.rackConfig.enableParallelScaleDownAcrossRacks", func() {
			Context("positive", func() {
				It("allows enabling on a stable cluster", func() {
					cur := createCluster(4, nil, nil)
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows enabling while a scale-down is already in flight", func() {
					cur := createCluster(3, nil, nil)
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // status.Size > spec.Size

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows enabling together with initiating a scale-down", func() {
					cur := createCluster(4, nil, nil)
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

					cur.Spec.Size = 3
					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows enabling while a rack deletion is in flight", func() {
					createCluster(4, simpleRacks(2), nil)
					cur := setStatus(4, simpleRacks(3)) // status still has rack 3

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows disabling when cluster is fully stable", func() {
					cur := createCluster(4, nil, ptr.To(true))
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows disabling atomically with initiating a fresh scale-down on a stable cluster", func() {
					cur := createCluster(4, nil, ptr.To(true))
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

					cur.Spec.Size = 3
					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows disabling atomically with a rack addition that grows total size (no per-rack shrink)", func() {
					createCluster(4, simpleRacks(2), ptr.To(true))
					cur := setStatus(4, simpleRacks(2)) // stable

					cur.Spec.Size = 6
					cur.Spec.RackConfig.Racks = simpleRacks(3)
					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})

				It("allows disabling atomically with a rack addition at constant size (no prior quiesce ran)", func() {
					createCluster(6, simpleRacks(2), ptr.To(true))
					cur := setStatus(6, simpleRacks(2)) // stable

					cur.Spec.RackConfig.Racks = simpleRacks(3)
					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
				})
			})

			Context("negative", func() {
				expectDenied := func(err error) {
					GinkgoHelper()

					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"cannot disable enableParallelScaleDownAcrossRacks",
							"scale-down is already in progress",
						).
						Validate(err)
				}

				It("denies disabling when a size scale-down is in flight", func() {
					cur := createCluster(3, nil, ptr.To(true))
					cur = setStatus(4, cur.Spec.RackConfig.Racks) // status.Size > spec.Size

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					expectDenied(envtests.K8sClient.Update(ctx, cur))
				})

				It("denies disabling even when reducing size further while a prior scale-down is in flight", func() {
					// Prior edit: 10→8 (status still 10). This update: 8→6 + disable.
					cur := createCluster(8, nil, ptr.To(true))
					cur = setStatus(10, cur.Spec.RackConfig.Racks)

					cur.Spec.Size = 6
					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					expectDenied(envtests.K8sClient.Update(ctx, cur))
				})

				It("denies disabling while a rack deletion is in flight", func() {
					createCluster(4, simpleRacks(2), ptr.To(true))
					cur := setStatus(4, simpleRacks(3)) // rack 3 still in status

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					expectDenied(envtests.K8sClient.Update(ctx, cur))
				})

				It("denies disabling while a rack replacement is in flight", func() {
					// Spec already has [1,2,4]; status still reflects [1,2,3].
					createCluster(6, []asdbv1.Rack{{ID: 1}, {ID: 2}, {ID: 4}}, ptr.To(true))
					cur := setStatus(6, []asdbv1.Rack{{ID: 1}, {ID: 2}, {ID: 3}})

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					expectDenied(envtests.K8sClient.Update(ctx, cur))
				})

				It("denies disabling when a rack addition at constant size left a per-rack scale-down in flight", func() {
					// Spec added rack 3 at size=6; status still has 2 racks (reconcile not done).
					createCluster(6, simpleRacks(3), ptr.To(true))
					cur := setStatus(6, simpleRacks(2))

					cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
					expectDenied(envtests.K8sClient.Update(ctx, cur))
				})
			})
		})
	})
})
