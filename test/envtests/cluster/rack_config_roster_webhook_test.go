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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

// nRackStrongConsistencyCluster returns a deployable multi-rack cluster (SC namespace per rack)
// with per-rack storage and no spec-level storage, matching other rack envtests.
// Cluster size is fixed at 4 so pod distribution across racks matches these webhook cases.
func nRackStrongConsistencyCluster(ns types.NamespacedName, devicePaths ...string) *asdbv1.AerospikeCluster {
	const clusterSize int32 = 4

	aeroCluster := testCluster.CreateDummyAerospikeCluster(ns, clusterSize)
	racks := make([]asdbv1.Rack, len(devicePaths))

	for i, p := range devicePaths {
		s := getStorageSpecForDevice(p)
		racks[i] = asdbv1.Rack{
			ID:                   i + 1,
			Revision:             "v1",
			InputStorage:         &s,
			InputAerospikeConfig: rackNSOverride(p),
		}
	}

	aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
	aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
		Namespaces: []string{"test"},
		Racks:      racks,
	}

	return aeroCluster
}

// seedClusterStatusFromSpec mirrors spec into status (AerospikeConfig + rackConfig, etc.) so update
// webhooks that require populated status (e.g. validateForceBlockFromRosterUpdate) can run.
func seedClusterStatusFromSpec(ctx context.Context, ns types.NamespacedName) {
	GinkgoHelper()

	cur, err := testCluster.GetCluster(envtests.K8sClient, ctx, ns)
	Expect(err).ToNot(HaveOccurred())

	statusSpec, err := asdbv1.CopySpecToStatus(&cur.Spec)
	Expect(err).ToNot(HaveOccurred())

	patched := cur.DeepCopy()
	patched.Status.AerospikeClusterStatusSpec = *statusSpec

	Expect(envtests.K8sClient.Status().Update(ctx, patched)).To(Succeed())
}

var _ = Describe("RackConfig maxIgnorablePods and forceBlockFromRoster webhook validation", func() {
	ctx := context.TODO()

	var nsName types.NamespacedName

	BeforeEach(func() {
		nsName = uniqueNamespacedName("rack-roster-webhook")
	})

	AfterEach(func() {
		deleteCluster(ctx, nsName)
	})

	Context("Deploy validation", func() {
		Context("spec.rackConfig.maxIgnorablePods", func() {
			Context("positive", func() {
				// maxIgnorablePods=0 is valid (no pods ignorable; not treated as negative).
				It("allows create with maxIgnorablePods integer zero", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(0))

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})

				It("allows create with a valid positive integer maxIgnorablePods", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(2))

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})

				It("allows create with a valid maxIgnorablePods percentage not exceeding 100%", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("50%"))

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})
			})

			Context("negative", func() {
				It("rejects create with negative maxIgnorablePods", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(-1))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"can not use negative spec.rackConfig.maxIgnorablePods",
						).
						Validate(err)
				})

				It("rejects create when maxIgnorablePods percentage is greater than 100%", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("101%"))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"spec.rackConfig.maxIgnorablePods",
							"must not be greater than 100 percent",
						).
						Validate(err)
				})

				It("rejects create with an invalid maxIgnorablePods IntOrString value", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("not-a-number"))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"invalid value for IntOrString: invalid type: string is not a percentage",
						).
						Validate(err)
				})
			})
		})

		Context("spec.rackConfig.racks[].forceBlockFromRoster", func() {
			Context("positive", func() {
				It("allows create when exactly one rack has forceBlockFromRoster and at least one rack stays in roster", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
				})
			})

			Context("negative", func() {
				It("rejects create when every rack has forceBlockFromRoster enabled", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RackConfig.Racks[1].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RackConfig.Racks[2].ForceBlockFromRoster = ptr.To(true)

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"all racks cannot have forceBlockFromRoster enabled. At least one rack must remain in the roster",
						).
						Validate(err)
				})

				It("rejects create when forceBlockFromRoster is used together with maxIgnorablePods", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(1))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"forceBlockFromRoster cannot be used together with maxIgnorablePods",
						).
						Validate(err)
				})

				It("rejects create when forceBlockFromRoster is used together with rosterNodeBlockList", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RosterNodeBlockList = []string{"1a0"}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"forceBlockFromRoster cannot be used together with RosterNodeBlockList",
						).
						Validate(err)
				})

				It("rejects create when only one rack remains in roster and rollingUpdateBatchSize is set", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromInt32(2))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"with only one rack in roster, cannot use rollingUpdateBatchSize or scaleDownBatchSize",
						).
						Validate(err)
				})

				It("rejects create when only one rack remains in roster and scaleDownBatchSize is set", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					aeroCluster.Spec.RackConfig.ScaleDownBatchSize = ptr.To(intstr.FromInt32(1))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"with only one rack in roster, cannot use rollingUpdateBatchSize or scaleDownBatchSize",
						).
						Validate(err)
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.rackConfig.maxIgnorablePods", func() {
			Context("positive", func() {
				It("allows update adding maxIgnorablePods as a positive integer", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(1))

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows update adding maxIgnorablePods as a percentage string", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("25%"))

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})

			Context("negative", func() {
				It("rejects update changing maxIgnorablePods to a negative integer", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(2))
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(-1))

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"can not use negative spec.rackConfig.maxIgnorablePods",
						).
						Validate(err)
				})

				It("rejects update changing maxIgnorablePods to a percentage greater than 100%", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("50%"))
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromString("101%"))

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"spec.rackConfig.maxIgnorablePods",
							"must not be greater than 100 percent",
						).
						Validate(err)
				})

				It("rejects update adding maxIgnorablePods when forceBlockFromRoster is already enabled", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.MaxIgnorablePods = ptr.To(intstr.FromInt32(1))

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"forceBlockFromRoster cannot be used together with maxIgnorablePods",
						).
						Validate(err)
				})
			})
		})

		Context("spec.rackConfig.racks[].forceBlockFromRoster", func() {
			Context("positive", func() {
				It("allows update enabling forceBlockFromRoster on a single rack when status is populated", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows sequential updates enabling forceBlockFromRoster on another rack after status reflects the first",
					func() {
						aeroCluster := nRackStrongConsistencyCluster(nsName,
							"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
						Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
						seedClusterStatusFromSpec(ctx, nsName)

						first, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
						Expect(err).ToNot(HaveOccurred())

						first.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
						Expect(envtests.K8sClient.Update(ctx, first)).To(Succeed())

						seedClusterStatusFromSpec(ctx, nsName)

						second, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
						Expect(err).ToNot(HaveOccurred())

						second.Spec.RackConfig.Racks[1].ForceBlockFromRoster = ptr.To(true)

						Expect(envtests.K8sClient.Update(ctx, second)).To(Succeed())
					})
			})

			Context("negative", func() {
				It("rejects update toggling forceBlockFromRoster when status.aerospikeConfig is not populated", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"status is not updated yet, cannot change forceBlockFromRoster in rack",
						).
						Validate(err)
				})

				It("rejects update enabling forceBlockFromRoster on more than one new rack in a single change", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb", "/rack3/xvdc")
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					current.Spec.RackConfig.Racks[1].ForceBlockFromRoster = ptr.To(true)

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"the forceBlockFromRoster flag can be applied to only one rack at a time",
						).
						Validate(err)
				})

				It("rejects update adding rosterNodeBlockList when forceBlockFromRoster is enabled", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RosterNodeBlockList = []string{"1a0"}

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"forceBlockFromRoster cannot be used together with RosterNodeBlockList",
						).
						Validate(err)
				})

				It("rejects update adding rollingUpdateBatchSize when only one rack remains in roster", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromInt32(2))

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"with only one rack in roster, cannot use rollingUpdateBatchSize or scaleDownBatchSize",
						).
						Validate(err)
				})

				It("rejects update adding scaleDownBatchSize when only one rack remains in roster", func() {
					aeroCluster := nRackStrongConsistencyCluster(nsName,
						"/rack1/xvda", "/rack2/xvdb")
					aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					seedClusterStatusFromSpec(ctx, nsName)

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.ScaleDownBatchSize = ptr.To(intstr.FromInt32(1))

					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							testutil.WebhookErrorPrefix,
							"with only one rack in roster, cannot use rollingUpdateBatchSize or scaleDownBatchSize",
						).
						Validate(err)
				})
			})
		})
	})
})
