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
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

const (
	newRackRevision         = "v2"
	rackRevisionClusterName = "rack-revision-webhook-cluster"
	threeCharClusterName    = "abc"
)

var _ = Describe("Rack revision webhook validation", func() {
	ctx := context.TODO()
	// Primary test cluster (fixed object name for most rack-revision webhook cases).
	clusterNamespacedName := uniqueNamespacedName(rackRevisionClusterName)

	// Another test cluster (dynamic object name for some rack-revision webhook test cases).
	var cName types.NamespacedName

	BeforeEach(func() {
		cName = types.NamespacedName{}
	})
	AfterEach(func() {
		// Delete the cluster after each test
		deleteCluster(ctx, clusterNamespacedName)

		if cName.Name != "" {
			deleteCluster(ctx, cName)
		}
	})

	Context("Deploy validation", func() {
		Context("spec.rackConfig (rack revision)", func() {
			Context("negative", func() {
				// Rack revision validation:
				//   • characters (CREATE + UPDATE): lowercase alphanumeric and '-', must
				//     start/end with alphanumeric (IsDNS1123Label — catches uppercase,
				//     dots, underscores, trailing hyphens).
				//   • length (CREATE only): no explicit character cap. At CREATE the
				//     webhook validates the projected pod name using a placeholder of
				//     max(len(revision), minRevisionReservation=3) chars with MaxRackID
				//     and max ordinal (255). On UPDATE, the actual pod name is validated
				//     with real rack ID, real revision, and real max ordinal (Size-1).
				It("rejects revision that makes projected pod name too long at CREATE", func() {
					// cluster name = 3 chars, revision = 48 chars:
					// projected pod = 3 + "-1000000-" + 48 + "-255" = 3+9+48+4 = 64 > 63
					cName = test.GetNamespacedName(threeCharClusterName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: strings.Repeat("a", 48)}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"computed at max rack ID",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})

				It("rejects revision with uppercase letters", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "V1"}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"V1\" for rack ID 1 is invalid",
							"must consist of lower case alphanumeric characters or '-'").
						Validate(err)
				})

				It("rejects revision ending with a hyphen", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v-"}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"v-\" for rack ID 1 is invalid",
							"must start and end with an alphanumeric character").
						Validate(err)
				})

				// Dot in revision is a silent failure: k8s accepts the STS/pod as a
				// DNS subdomain, but the dot breaks the pod's DNS label in the FQDN.
				It("rejects revision with a dot (silent DNS breakage without webhook check)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v.1"}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"v.1\" for rack ID 1 is invalid",
							"must not contain dots").
						Validate(err)
				})

				It("rejects revision with an underscore", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v_1"}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"v_1\" for rack ID 1 is invalid",
							"must consist of lower case alphanumeric characters or '-'").
						Validate(err)
				})

				// The baseline projected pod name uses the 3-char placeholder revision
				// (minRevisionReservation), MaxRackID (1000000), and max ordinal (255):
				//
				//   projected pod = cluster.Name + "-1000000-aaa-255"
				//                                   (8)    (4) (4) = 16 chars overhead
				//
				// So cluster.Name must be ≤ 63 − 16 = 47 chars.
				// "invalid-cluster" = 15 chars → well within the limit.
				// We need a cluster name > 47 chars to trigger this error.
				// (This test overlaps with the cluster-name test above; it is kept
				// here to confirm the error also fires when racks are configured.)
				It("rejects cluster name too long for projected pod name (with rack config)", func() {
					// 48-char cluster name: 48 + 16 = 64 → pod name too long.
					longName := strings.Repeat("a", 48)
					cName = test.GetNamespacedName(longName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1"}},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"computed at max rack ID",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})

				It("rejects invalid revision on second rack when first rack is valid", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: "BAD"}, // uppercase
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"BAD\" for rack ID 2 is invalid").
						Validate(err)
				})

				// maxPlaceholderLen = max(minRevisionReservation=3, longest revision).
				// When multiple racks have different revision lengths the longest one
				// drives the check. If that makes the projected pod name > 63 chars,
				// the whole CREATE is rejected even if the other racks are fine.
				It("rejects CREATE when the rack with the longest revision drives projected pod name too long", func() {
					// cluster name "xyz" (3), 2 racks: "v1" (2 chars) and 48-char revision.
					// maxPlaceholderLen = max(3, 2, 48) = 48
					// projected pod = 3 + "-1000000-" + 48 + "-255" = 3+9+48+4 = 64 > 63.
					shortName := "xyz"
					cName = test.GetNamespacedName(shortName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: strings.Repeat("a", 48)},
						},
					}

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"computed at max rack ID",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("accepts empty revision", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: ""}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				It("accepts a 1-char lowercase revision", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v"}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				It("accepts a 2-char revision with a hyphen (e.g. v1)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1"}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				It("accepts a 3-char revision", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1b"}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				// Revisions longer than 3 chars are accepted as long as the resulting
				// projected pod name (with MaxRackID and max ordinal) stays ≤ 63 chars.
				// cluster name "abc" (3), revision "rev-alpha" (9):
				//   projected pod = 3 + "-1000000-" + 9 + "-255" = 3+9+9+4 = 25 ≤ 63 ✓
				It("accepts a revision longer than 3 chars when pod name stays within limit", func() {
					cName = test.GetNamespacedName(threeCharClusterName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "rev-alpha"}}, // 9 chars
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				// cluster name "xyz" (3), revision = 47 chars:
				// projected pod = 3 + "-1000000-" + 47 + "-255" = 3+9+47+4 = 63 → exactly at limit.
				It("accepts a revision exactly at the projected pod name boundary (pod name = 63 chars)", func() {
					shortName := "xyz"
					cName = test.GetNamespacedName(shortName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: strings.Repeat("a", 47)}},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				// Two racks with different revision lengths. The longer one (47 chars)
				// becomes maxPlaceholderLen. projected pod = 3+9+47+4 = 63 → passes.
				It("accepts multiple racks where the longest revision fits within the pod name limit", func() {
					shortName := "def"
					cName = test.GetNamespacedName(shortName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: strings.Repeat("a", 47)},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})

	// Test update validation for rack revision
	Context("Update validation", func() {
		Context("spec.rackConfig", func() {
			Context("negative", func() {
				It("rejects changes in rack Storage without changing rack Revision", func() {
					s := getStorageSpecForDevice("/r1/dev")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s, InputAerospikeConfig: rackNSOverride("/r1/dev")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
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
					sV1 := getStorageSpecForDevice("/r1/v1")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &sV1, InputAerospikeConfig: rackNSOverride("/r1/v1")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					base, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					statusSnap := base.DeepCopy()
					statusSnap.Status.RackConfig.Racks = []asdbv1.Rack{
						{ID: 1, Revision: newRackRevision, Storage: sV1},
					}
					Expect(envtests.K8sClient.Status().Update(ctx, statusSnap)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					sV2Conflict := getStorageSpecForDevice("/r1/v1")
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
				It("allows update that changes rack InputStorage when rack Revision changes"+
					"and status does not imply a conflict", func() {
					s1 := getStorageSpecForDevice("/r1/a")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: &s1, InputAerospikeConfig: rackNSOverride("/r1/a")},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					s2 := getStorageSpecForDevice("/r1/b")
					patchFirstPVStorageClassSpec(&s2)
					current.Spec.RackConfig.Racks[0].InputStorage = &s2
					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					current.Spec.RackConfig.Racks[0].InputAerospikeConfig = rackNSOverride("/r1/b")

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
		// Conservative projected-name length bounds are checked on CREATE only.
		// On UPDATE, the actual pod name (real rack ID, real revision, real
		// per-rack max ordinal via DistributeItems) is validated as a DNS label
		// to catch any silent overflow introduced by a revision change.
		// Revision character validity IS re-checked on UPDATE because an
		// invalid character (dot, uppercase, underscore) would silently corrupt
		// the StatefulSet name at runtime regardless of when it was introduced.
		Context("naming length bounds (CREATE-only projected check; UPDATE uses actual pod name)", func() {
			Context("negative", func() {
				It("rejects UPDATE when the new revision makes the actual pod name too long", func() {
					// cluster name = 20 chars, 1 rack (ID=1), initial revision = "v1" (2 chars).
					// At CREATE: baseline projected pod = 20+9+3+4 = 36 ≤ 63 → passes.
					// On UPDATE: revision grows to 42 chars.
					// DistributeItems(1, 1) = [1] → max ordinal for this rack = 0.
					// Actual STS  = "a"*20 + "-1-" + "b"*42 = 20+3+42 = 65 chars
					// Actual pod  = STS + "-0"              = 65+2   = 67 chars > 63 → rejected.
					clusterName20 := strings.Repeat("a", 20)
					cName = test.GetNamespacedName(clusterName20, clusterNamespacedName.Namespace)

					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1"}},
					}
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, cName)
					Expect(err).ToNot(HaveOccurred())

					// Grow the revision to 42 chars — actual pod name will be 67 chars.
					current.Spec.RackConfig.Racks[0].Revision = strings.Repeat("b", 42)
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred(), "UPDATE with oversized revision should be rejected")

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"pod name \"",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})

				// validateRackConfig (character validity) still runs on UPDATE.
				// An invalid character introduced via revision update must be caught
				// even though length bounds are only checked at CREATE.
				It("rejects UPDATE when revision is changed to have invalid characters", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1"}},
					}
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change revision to uppercase — caught by validateRackConfig on UPDATE.
					current.Spec.RackConfig.Racks[0].Revision = "V2"
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"rack revision \"V2\" for rack ID 1 is invalid",
							"must consist of lower case alphanumeric characters or '-'").
						Validate(err)
				})

				// validateActualPodNames finds the longest pod name across all racks.
				// Even if only one rack's pod name overflows, the whole UPDATE is rejected.
				It("rejects UPDATE when only one rack's pod name exceeds the DNS label limit (multi-rack)", func() {
					// cluster name = 20 chars, 2 racks (ID=1, ID=2), Size=2.
					// DistributeItems(2, 2) = [1, 1] → max ordinal per rack = 0.
					// UPDATE: rack 1 revision → 42 chars.
					//   pod for rack 1 = 20+"-1-"+"b"*42+"-0" = 20+3+42+2 = 67 > 63 → fails.
					//   pod for rack 2 = 20+"-2-v1-0"         = 27 chars           → fine.
					// Longest pod name (rack 1) fails → UPDATE rejected.
					clusterName20 := strings.Repeat("b", 20)
					cName = test.GetNamespacedName(clusterName20, clusterNamespacedName.Namespace)

					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: "v1"},
						},
					}
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, cName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].Revision = strings.Repeat("c", 42)
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred(), "UPDATE with oversized revision on one rack should be rejected")

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"exceeds the maximum DNS label length",
							"of 63 characters").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows UPDATE without re-checking CREATE-only length bounds", func() {
					// Create a valid cluster then update an unrelated field (size).
					// The webhook must not surface any CREATE-only naming error on UPDATE.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change size — an otherwise valid update.
					current.Spec.Size = 2
					err = envtests.K8sClient.Update(ctx, current)
					// The webhook must not surface any CREATE-only naming error on UPDATE.
					if err != nil {
						Expect(err).NotTo(MatchError(ContainSubstring("computed at max rack ID")),
							"CREATE-only pod name length check must not be re-run on UPDATE")
					}
				})

				// validateActualPodNames returns nil immediately when no user-defined
				// racks are present (the controller manages a default rack internally).
				It("allows UPDATE when cluster has no user-defined racks", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					// No explicit RackConfig — controller will create a default rack.
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.Size = 2
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred(),
						"UPDATE with no user-defined racks must not fail on actual pod name check")
				})

				// Growing a revision is fine as long as the actual pod name stays
				// within 63 chars. validateActualPodNames uses real rack ID and real
				// per-rack ordinal (DistributeItems), not the conservative projected value.
				It("allows UPDATE when revision grows but actual pod name stays within limit", func() {
					// cluster name "abc" (3), rack ID=1, Size=1, initial revision "v1".
					// UPDATE to "rev-alpha" (9 chars):
					//   DistributeItems(1, 1) = [1] → ordinal = 0.
					//   Actual pod = "abc-1-rev-alpha-0" = 17 chars ≤ 63 → passes.
					cName = test.GetNamespacedName(threeCharClusterName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: 1, Revision: "v1"}},
					}
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, cName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].Revision = "rev-alpha"
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).ToNot(HaveOccurred(),
						"revision growth within the DNS label limit must be accepted")
				})

				It("allows UPDATE when one rack has zero pods under DistributeItems"+
					"(validateActualPodNames skips rackSize 0)", func() {
					cName := uniqueNamespacedName("actpod-zero-rack")
					// DistributeItems(2, 3) → [1,1,0]: only rack 1 and 2 gets a pod; rack 3 must be skipped.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: "v1"},
							{ID: 3, Revision: "v1"},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, cName)
					Expect(err).ToNot(HaveOccurred())

					// Only touch the rack that actually has pods; rack 3 remains unused by the distribution.
					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					current.Spec.RackConfig.Racks[1].Revision = newRackRevision
					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				It("allows UPDATE when max pod ordinal is greater than zero (validateActualPodNames uses rackSize-1)", func() {
					cName := uniqueNamespacedName("actpod-ord-gt0")
					// DistributeItems(4, 2) → [2, 2] → pod ordinals 0 and 1 per rack.
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 4)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: "v1"},
						},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, cName)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					current.Spec.RackConfig.Racks[1].Revision = newRackRevision
					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})
			})
		})
	})
})
