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

// Envtests for the EnableBatchScaleDownQuiesce webhook validation.
//
// The validating webhook only restricts the DISABLE direction (true → false)
// while a scale-down is in flight. Enabling is always permitted.
//
// "Scale-down in flight" is detected via status.Size > spec.Size: status.Size
// is updated only at the end of a successful reconcile, so it still holds the
// pre-scale-down value while AKO is removing pods.
//
// All tests follow the same three-step pattern:
//  1. Create the cluster (spec + flag in desired initial state).
//  2. Stamp status.Size via the status subresource to simulate cluster state.
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

var _ = Describe("EnableBatchScaleDownQuiesce webhook validation", func() {
	ctx := context.TODO()

	var clusterNamespacedName types.NamespacedName

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	// setStatusSize stamps status.Size on the live object to simulate the
	// last-completed-reconcile state.
	setStatusSize := func(size int32) {
		cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		snap := cur.DeepCopy()
		snap.Status.Size = size
		Expect(envtests.K8sClient.Status().Update(ctx, snap)).To(Succeed())
	}

	// createCluster creates an AerospikeCluster with the given size and flag
	// value and returns the freshly-fetched live object (with ResourceVersion).
	createCluster := func(size int32, flagEnabled *bool) *asdbv1.AerospikeCluster {
		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, size)
		aeroCluster.Spec.RackConfig.EnableBatchScaleDownQuiesce = flagEnabled
		Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

		cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		return cur
	}

	// ── Enabling (false → true) — always allowed ─────────────────────────────

	Context("enabling the flag", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-enable")
		})

		It("allows enabling on a stable cluster", func() {
			cur := createCluster(4, nil)

			setStatusSize(4) // stable: status == spec

			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows enabling while a scale-down is already in flight", func() {
			// spec=3 was committed in a prior edit; status still shows 4
			cur := createCluster(3, nil)

			setStatusSize(4) // in-flight: status > spec

			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows enabling together with a scale-down initiation", func() {
			cur := createCluster(4, nil)

			setStatusSize(4) // stable

			cur.Spec.Size = 3
			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = ptr.To(true)
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})
	})

	// ── Disabling (true → false) on stable cluster — allowed ─────────────────

	Context("disabling the flag on a stable cluster", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-disable-stable")
		})

		It("allows disabling when size is unchanged and cluster is stable", func() {
			cur := createCluster(4, ptr.To(true))

			setStatusSize(4) // stable: status == spec

			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})

		It("allows disabling atomically with a fresh scale-down on a stable cluster", func() {
			cur := createCluster(4, ptr.To(true))

			setStatusSize(4) // stable

			cur.Spec.Size = 3
			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = nil
			Expect(envtests.K8sClient.Update(ctx, cur)).To(Succeed())
		})
	})

	// ── Disabling while scale-down is in flight — denied ─────────────────────

	Context("disabling the flag while scale-down is in flight", func() {
		BeforeEach(func() {
			clusterNamespacedName = uniqueNamespacedName("batch-quiesce-disable-inflight")
		})

		It("denies disabling when a prior scale-down is in flight (size unchanged in this update)", func() {
			// spec was already reduced to 3; status still 4
			cur := createCluster(3, ptr.To(true))

			setStatusSize(4) // in-flight: status > spec

			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = nil
			err := envtests.K8sClient.Update(ctx, cur)
			Expect(err).To(HaveOccurred())
			envtests.NewStatusErrorMatcher().
				WithMessageSubstrings(
					testutil.WebhookErrorPrefix,
					"cannot disable enableBatchScaleDownQuiesce",
					"scale-down is already in progress",
				).
				Validate(err)
		})

		It("denies disabling even when this update further reduces size but prior scale-down is in flight", func() {
			// Prior edit: 10→8 (status still 10). This update: 8→6 + disable.
			// newSpec(6) < oldSpec(8) looks like "fresh" but the 10→8 is not done.
			cur := createCluster(8, ptr.To(true))

			setStatusSize(10) // prior 10→8 still in flight

			cur.Spec.Size = 6
			cur.Spec.RackConfig.EnableBatchScaleDownQuiesce = nil
			err := envtests.K8sClient.Update(ctx, cur)
			Expect(err).To(HaveOccurred())
			envtests.NewStatusErrorMatcher().
				WithMessageSubstrings(
					testutil.WebhookErrorPrefix,
					"cannot disable enableBatchScaleDownQuiesce",
					"scale-down is already in progress",
				).
				Validate(err)
		})
	})
})
