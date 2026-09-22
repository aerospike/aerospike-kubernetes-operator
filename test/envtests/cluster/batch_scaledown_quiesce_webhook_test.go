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

// Envtests for the EnableParallelScaleDownAcrossRacks webhook validation.
//
// The validating webhook only restricts the DISABLE direction (true → false)
// while a scale-down is in flight. Enabling is always permitted.
//
// "Scale-down in flight" is detected by scaleDownInFlight which covers:
//   - Total size decrease     (status.Size > spec.Size)
//   - Rack deletion/replacement (rack ID in status absent from spec)
//   - Per-rack scale-down     (rack topology shrinks even at constant total size)
//
// All tests follow the same three-step pattern:
//  1. Create the cluster (spec + flag in desired initial state).
//  2. Stamp status fields via the status subresource to simulate cluster state.
//  3. Submit the spec update under test and assert allow / deny.

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

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	// setStatus stamps status.Size and status.RackConfig.Racks on the live
	// object to simulate the last-completed-reconcile state.
	// It returns the re-fetched object so callers can use it for the next spec update.
	setStatus := func(size int32, racks []asdbv1.Rack) *asdbv1.AerospikeCluster {
		cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		snap := cur.DeepCopy()
		snap.Status.Size = size
		snap.Status.RackConfig.Racks = racks
		Expect(envtests.K8sClient.Status().Update(ctx, snap)).To(Succeed())

		// Re-fetch so the returned object has the latest resourceVersion.
		cur, err = testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		return cur
	}

	// createCluster creates an AerospikeCluster with the given size, racks, and
	// flag value and returns the freshly-fetched live object (with ResourceVersion).
	createCluster := func(size int32, racks []asdbv1.Rack, flagEnabled *bool) *asdbv1.AerospikeCluster {
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

	// simpleRacks returns a slice of n minimal racks with sequential IDs starting at 1.
	simpleRacks := func(n int) []asdbv1.Rack {
		racks := make([]asdbv1.Rack, n)
		for i := range racks {
			racks[i] = asdbv1.Rack{ID: i + 1}
		}

		return racks
	}

	// ── Enabling (false → true) — always allowed ─────────────────────────────

	Context("enabling the flag", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-enable")
		})

		It("allows enabling on a stable cluster", func() {
			cur := createCluster(4, nil, nil)
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows enabling while a scale-down is already in flight", func() {
			cur := createCluster(3, nil, nil)
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // in-flight: status.Size > spec.Size

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows enabling together with a scale-down initiation", func() {
			cur := createCluster(4, nil, nil)
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

			cur.Spec.Size = 3
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows enabling while a rack deletion is in flight", func() {
			// Status still shows 3 racks while spec already dropped to 2.
			createCluster(4, simpleRacks(2), nil)
			cur := setStatus(4, simpleRacks(3))

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})
	})

	// ── Disabling (true → false) on stable cluster — allowed ─────────────────

	Context("disabling the flag on a stable cluster", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-disable-stable")
		})

		It("allows disabling when cluster is fully stable (size and racks unchanged)", func() {
			cur := createCluster(4, nil, ptr.To(true))
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows disabling atomically with a fresh scale-down on a stable cluster", func() {
			cur := createCluster(4, nil, ptr.To(true))
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // stable

			cur.Spec.Size = 3
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows disabling atomically with adding a new rack (size increases — no per-rack shrink)", func() {
			// Stable: status matches spec exactly.
			createCluster(4, simpleRacks(2), ptr.To(true))
			cur := setStatus(4, simpleRacks(2))

			// Atomically add rack 3 and increase size so existing racks don't shrink.
			cur.Spec.Size = 6
			cur.Spec.RackConfig.Racks = simpleRacks(3)
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows disabling atomically with adding a rack at constant size (no prior quiesce ran)", func() {
			// Cluster is stable. User atomically adds rack 3 at size=6 + disables flag.
			// No pods were ever quiesced — this is the first edit that triggers a per-rack
			// redistribution. The webhook allows it; reconcile runs without batch quiesce.
			createCluster(6, simpleRacks(2), ptr.To(true))
			cur := setStatus(6, simpleRacks(2)) // stable

			cur.Spec.RackConfig.Racks = simpleRacks(3)
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})
	})

	// ── Disabling while scale-down is in flight — denied ─────────────────────

	Context("disabling the flag while scale-down is in flight", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-disable-inflight")
		})

		expectDenied := func(err error) {
			Expect(err).To(HaveOccurred())
			envtests.NewStatusErrorMatcher().
				WithMessageSubstrings(
					testutil.WebhookErrorPrefix,
					"cannot disable enableParallelScaleDownAcrossRacks",
					"scale-down is already in progress",
				).
				Validate(err)
		}

		It("denies disabling when a prior size scale-down is in flight", func() {
			cur := createCluster(3, nil, ptr.To(true))
			cur = setStatus(4, cur.Spec.RackConfig.Racks) // in-flight: status.Size > spec.Size

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			expectDenied(envtests.K8sClient.Update(ctx, cur))
		})

		It("denies disabling even when this update reduces size further but prior scale-down is in flight", func() {
			// Prior edit: 10→8 (status still 10). This update: 8→6 + disable.
			cur := createCluster(8, nil, ptr.To(true))
			cur = setStatus(10, cur.Spec.RackConfig.Racks) // prior 10→8 still in flight

			cur.Spec.Size = 6
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			expectDenied(envtests.K8sClient.Update(ctx, cur))
		})

		It("denies disabling while a rack deletion is in flight", func() {
			// spec already dropped to 2 racks; status still shows 3.
			createCluster(4, simpleRacks(2), ptr.To(true))
			cur := setStatus(4, simpleRacks(3)) // rack 3 still in status → deletion in flight

			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			expectDenied(envtests.K8sClient.Update(ctx, cur))
		})

		It("denies disabling while a rack replacement is in flight", func() {
			// A prior edit already changed the spec to [1,2,4] (replacing rack 3 with 4).
			// Status still reflects [1,2,3] — the reconcile hasn't completed yet.
			createCluster(6, []asdbv1.Rack{{ID: 1}, {ID: 2}, {ID: 4}}, ptr.To(true))
			cur := setStatus(6, []asdbv1.Rack{{ID: 1}, {ID: 2}, {ID: 3}})

			// Now the user tries to disable the flag — webhook should deny.
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			expectDenied(envtests.K8sClient.Update(ctx, cur))
		})

		It("denies disabling when a prior rack addition at constant size left a per-rack scale-down in flight", func() {
			// A prior edit already added rack 3 at size=6: existing racks each shrunk from 3→2.
			// That edit is not yet reconciled (status still shows 2 racks × 3 pods each).
			createCluster(6, simpleRacks(3), ptr.To(true))
			// Status still has 2 racks — the rack addition is in flight.
			cur := setStatus(6, simpleRacks(2))

			// Disabling the flag now would leave rack 1 and 2's already-quiesced pods stuck.
			cur.Spec.RackConfig.EnableParallelScaleDownAcrossRacks = nil
			expectDenied(envtests.K8sClient.Update(ctx, cur))
		})
	})
})
