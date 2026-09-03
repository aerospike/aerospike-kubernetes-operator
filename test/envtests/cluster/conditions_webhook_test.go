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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

var _ = Describe("AerospikeCluster status conditions schema", func() {
	ctx := context.TODO()

	var clusterNamespacedName types.NamespacedName

	BeforeEach(func() {
		clusterNamespacedName = uniqueNamespacedName("conditions-schema")

		aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
		Expect(envtests.K8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	// writeConditions applies conditions to the status subresource and returns the API server's
	// verdict.
	writeConditions := func(conditions ...metav1.Condition) error {
		aeroCluster := &asdbv1.AerospikeCluster{}
		Expect(envtests.K8sClient.Get(ctx, clusterNamespacedName, aeroCluster)).ToNot(HaveOccurred())

		aeroCluster.Status.Conditions = conditions

		return envtests.K8sClient.Status().Update(ctx, aeroCluster)
	}

	readConditions := func() []metav1.Condition {
		aeroCluster := &asdbv1.AerospikeCluster{}
		Expect(envtests.K8sClient.Get(ctx, clusterNamespacedName, aeroCluster)).ToNot(HaveOccurred())

		return aeroCluster.Status.Conditions
	}

	Context("positive", func() {
		It("accepts a condition with no message", func() {
			Expect(writeConditions(metav1.Condition{
				Type:               string(asdbv1.AerospikeClusterConditionScalingUp),
				Status:             metav1.ConditionFalse,
				Reason:             asdbv1.AerospikeClusterReasonNotScalingUp,
				LastTransitionTime: metav1.Now(),
			})).ToNot(HaveOccurred())

			conds := readConditions()
			Expect(conds).To(HaveLen(1))
			Expect(conds[0].Message).To(BeEmpty())
		})

		DescribeTable("accepts every condition type the operator writes",
			func(condType asdbv1.AerospikeClusterConditionType) {
				Expect(writeConditions(metav1.Condition{
					Type:               string(condType),
					Status:             metav1.ConditionUnknown,
					Reason:             asdbv1.AerospikeClusterReasonInitializing,
					LastTransitionTime: metav1.Now(),
				})).ToNot(HaveOccurred())
			},

			Entry("Ready", asdbv1.AerospikeClusterConditionReady),
			Entry("ScalingUp", asdbv1.AerospikeClusterConditionScalingUp),
			Entry("ScalingDown", asdbv1.AerospikeClusterConditionScalingDown),
			Entry("Upgrading", asdbv1.AerospikeClusterConditionUpgrading),
			Entry("RollingRestart", asdbv1.AerospikeClusterConditionRollingRestart),
			Entry("Paused", asdbv1.AerospikeClusterConditionPaused),
		)

		// reason carries minLength 1, maxLength 1024 and pattern
		// ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$ — a reason tripping any of those would make the
		// whole status patch fail at runtime, and no unit test can catch it because the fake
		// client does not validate.
		//
		// Keep in sync with the reason block in api/v1/aerospikecluster_types.go.
		DescribeTable("accepts every reason constant the operator writes",
			func(reason string) {
				Expect(writeConditions(metav1.Condition{
					Type:               string(asdbv1.AerospikeClusterConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             reason,
					LastTransitionTime: metav1.Now(),
				})).ToNot(HaveOccurred(), "reason %q was rejected by the API server", reason)
			},

			Entry("ReconcileComplete", asdbv1.AerospikeClusterReasonReconcileComplete),
			Entry("Reconciling", asdbv1.AerospikeClusterReasonReconciling),
			Entry("ReconcileFailed", asdbv1.AerospikeClusterReasonReconcileFailed),
			Entry("Initializing", asdbv1.AerospikeClusterReasonInitializing),
			Entry("PausedByUser", asdbv1.AerospikeClusterReasonPausedByUser),
			Entry("Terminating", asdbv1.AerospikeClusterReasonTerminating),
			Entry("NotPaused", asdbv1.AerospikeClusterReasonNotPaused),
			Entry("ScalingUp", asdbv1.AerospikeClusterReasonScalingUp),
			Entry("NotScalingUp", asdbv1.AerospikeClusterReasonNotScalingUp),
			Entry("ScalingDown", asdbv1.AerospikeClusterReasonScalingDown),
			Entry("NotScalingDown", asdbv1.AerospikeClusterReasonNotScalingDown),
			Entry("Upgrading", asdbv1.AerospikeClusterReasonUpgrading),
			Entry("NotUpgrading", asdbv1.AerospikeClusterReasonNotUpgrading),
			Entry("RollingRestart", asdbv1.AerospikeClusterReasonRollingRestart),
			Entry("NotRollingRestart", asdbv1.AerospikeClusterReasonNotRollingRestart),
			Entry("RackRevisionRollingOut", asdbv1.AerospikeClusterReasonRackRevisionRollingOut),
			Entry("NotRackRevisionRollingOut", asdbv1.AerospikeClusterReasonNotRackRevisionRollingOut),
			Entry("RackReconcileFailed", asdbv1.AerospikeClusterReasonRackReconcileFailed),
			Entry("ACLReconcileFailed", asdbv1.AerospikeClusterReasonACLReconcileFailed),
			Entry("PDBReconcileFailed", asdbv1.AerospikeClusterReasonPDBReconcileFailed),
			Entry("ServiceReconcileFailed", asdbv1.AerospikeClusterReasonServiceReconcileFailed),
			Entry("PodStateFetchFailed", asdbv1.AerospikeClusterReasonPodStateFetchFailed),
			Entry("QuiesceUndoFailed", asdbv1.AerospikeClusterReasonQuiesceUndoFailed),
			Entry("ReclusterFailed", asdbv1.AerospikeClusterReasonReclusterFailed),
			Entry("RosterSetFailed", asdbv1.AerospikeClusterReasonRosterSetFailed),
			Entry("MFDSetFailed", asdbv1.AerospikeClusterReasonMFDSetFailed),
			Entry("StatusUpdateFailed", asdbv1.AerospikeClusterReasonStatusUpdateFailed),
			Entry("ClusterSetupFailed", asdbv1.AerospikeClusterReasonClusterSetupFailed),
		)

		DescribeTable("accepts messages up to the cap",
			func(length int) {
				Expect(writeConditions(metav1.Condition{
					Type:               string(asdbv1.AerospikeClusterConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             asdbv1.AerospikeClusterReasonReconcileFailed,
					Message:            strings.Repeat("x", length),
					LastTransitionTime: metav1.Now(),
				})).ToNot(HaveOccurred())
			},

			// truncateConditionMessage bounds messages at 2048; this confirms the bound it picks
			// is comfortably accepted.
			Entry("the operator's truncation bound", 2048),
			Entry("exactly at the CRD cap", 32768),
		)
	})

	Context("negative", func() {
		DescribeTable("rejects a schema-violating condition",
			func(conditions []metav1.Condition, wantSubstrings ...string) {
				err := writeConditions(conditions...)
				Expect(err).To(HaveOccurred())

				envtests.NewStatusErrorMatcher().
					WithMessageSubstrings(wantSubstrings...).Validate(err)
			},

			// This is precisely the failure truncateConditionMessage prevents: an oversized
			// message makes the API server reject the whole patch, taking the phase=Error write
			// down with it.
			Entry("a message one byte over the 32768 cap",
				[]metav1.Condition{{
					Type:               string(asdbv1.AerospikeClusterConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             asdbv1.AerospikeClusterReasonReconcileFailed,
					Message:            strings.Repeat("x", 32769),
					LastTransitionTime: metav1.Now(),
				}},
				"message"),

			Entry("an empty reason, which has minLength 1",
				[]metav1.Condition{{
					Type:               string(asdbv1.AerospikeClusterConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             "",
					LastTransitionTime: metav1.Now(),
				}},
				"reason"),

			Entry("a reason breaking the CamelCase pattern",
				[]metav1.Condition{{
					Type:               string(asdbv1.AerospikeClusterConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             "not a valid reason",
					LastTransitionTime: metav1.Now(),
				}},
				"reason"),
		)
	})
})
