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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

const (
	newRackRevision         = "v2"
	rackRevisionClusterName = "rack-revision-webhook-cluster"
	threeCharClusterName    = "abc"
)

// invalidRevisionDeployCase is a struct that contains the revision string and the expected error substrings.
type invalidRevisionDeployCase struct {
	revision string
	subs     []string
}

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

				// Table: invalid rack revision strings rejected at CREATE with admission errors.
				// Each row exercises a different IsDNS1123Label / rack-revision rule: uppercase,
				// leading/trailing hyphen, dot (invalid inside a single DNS label), underscore.
				// Expectations use stable message substrings from the validating webhook.
				DescribeTable("rejects invalid rack revision on CREATE",
					func(c invalidRevisionDeployCase) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{"test"},
							Racks:      []asdbv1.Rack{{ID: 1, Revision: c.revision}},
						}

						err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())

						subs := append([]string{"\"vaerospikecluster.kb.io\""}, c.subs...)
						envtests.NewStatusErrorMatcher().
							WithMessageSubstrings(subs...).
							Validate(err)
					},
					Entry("Rack revision with uppercase letters", invalidRevisionDeployCase{
						revision: "V1",
						subs: []string{
							`rack revision "V1" for rack ID 1 is invalid`,
							"must consist of lower case alphanumeric characters or '-'",
						},
					}),
					Entry("Rack revision with hyphen", invalidRevisionDeployCase{
						revision: "v-",
						subs: []string{
							`rack revision "v-" for rack ID 1 is invalid`,
							"must start and end with an alphanumeric character",
						},
					}),
					Entry("Rack revision with dot", invalidRevisionDeployCase{
						revision: "v.1",
						subs: []string{
							`rack revision "v.1" for rack ID 1 is invalid`,
							"must not contain dots",
						},
					}),
					Entry("Rack revision with underscore", invalidRevisionDeployCase{
						revision: "v_1",
						subs: []string{
							`rack revision "v_1" for rack ID 1 is invalid`,
							"must consist of lower case alphanumeric characters or '-'",
						},
					}),
				)

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
				// Table: valid rack revision strings accepted at CREATE when other fields are fixed
				// (single rack ID=1, dummy cluster, default namespace device path).
				// Covers DNS-1123–style revision rules that should pass: empty revision, lowercase short revision,
				// rack revision with only digits and alphanumeric rack revision.
				DescribeTable("accepts valid rack revision on CREATE",
					func(revision string) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{"test"},
							Racks:      []asdbv1.Rack{{ID: 1, Revision: revision}},
						}

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).To(Succeed())
					},
					Entry("empty revision", ""),
					Entry("1-char lowercase rack revision", "v"),
					Entry("2-char rack revision with only digits", "01"),
					Entry("3-char alphanumeric rack revision", "v1b"),
				)

				// CREATE admission uses a conservative projected pod name (short cluster name placeholder,
				// max rack ID, longest rack revision vs minRevisionReservation, max ordinal) to ensure
				// the resulting DNS label cannot exceed 63 characters. Rows below are positive cases:
				// a comfortably short projection, and one that lands exactly on the 63-character limit.
				// Per-row arithmetic is in the comments above each Entry.
				DescribeTable("accepts CREATE when a rack revision keeps the projected pod name within limit",
					func(revision string) {
						cName = test.GetNamespacedName(threeCharClusterName, clusterNamespacedName.Namespace)
						aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Namespaces: []string{"test"},
							Racks:      []asdbv1.Rack{{ID: 1, Revision: revision}},
						}

						err := envtests.K8sClient.Create(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},

					// Math: cluster name "abc" (3), revision "rev-alpha" (9).
					// Projected pod = 3 + "-1000000-" + 9 + "-255" = 3 + 9 + 9 + 4 = 25 ≤ 63 → should succeed
					Entry("revision longer than 3 chars when pod name stays within limit", "rev-alpha"),

					// Math: cluster name "abc" (3), revision = 47 'a' chars.
					// Projected pod = 3 + "-1000000-" + 47 + "-255" = 3 + 9 + 47 + 4 = 63 → exactly at DNS label limit.
					Entry("revision exactly at the projected pod name boundary (pod name = 63 chars)", strings.Repeat("a", 47)),
				)

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
		Context("spec.rackConfig (rack revision)", func() {
			Context("negative", func() {
				It("rejects changes in rack Storage without changing rack Revision", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rackSt := aeroCluster.Spec.Storage.DeepCopy()
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: rackSt},
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
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rackSt := aeroCluster.Spec.Storage.DeepCopy()
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: rackSt},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					base, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					statusSnap := base.DeepCopy()
					statusSnap.Status.RackConfig.Racks = []asdbv1.Rack{
						{ID: 1, Revision: newRackRevision, Storage: *rackSt},
					}
					Expect(envtests.K8sClient.Status().Update(ctx, statusSnap)).To(Succeed())
					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					sV2Conflict := rackSt.DeepCopy()
					patchFirstPVStorageClassSpec(sV2Conflict)

					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					current.Spec.RackConfig.Racks[0].InputStorage = sV2Conflict
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"old rack with same revision v2 already exists with different storage",
						).
						Validate(err)
				})

				// Mirrors test/cluster/rack_revision_test.go:
				// "Should reject storage update validation bypass via revision bump and rollback".
				// 1) Bump rack revision and expand InputStorage (append volume) — allowed.
				// 2) Status records baseline v1 storage; spec rolls back to v1 but keeps expanded
				//    storage — must be rejected (it needs status row so validateRackUpdate can compare).
				It("rejects rollback of rack revision after storage expansion (revision bump bypass)", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					rackSt := aeroCluster.Spec.Storage.DeepCopy()
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: rackSt},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					r0 := &current.Spec.RackConfig.Racks[0]
					is := r0.InputStorage.DeepCopy()
					is.Volumes = append(is.Volumes, asdbv1.VolumeSpec{
						Name: "bypass-volume",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("2Gi"),
								StorageClass: testutil.StorageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike/bypass",
						},
					})
					r0.InputStorage = is
					r0.Revision = newRackRevision

					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed(),
						"revision bump with extra volume should be admitted")

					current, err = testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Record that revision v1 used baseline storage only (no bypass volume).
					statusSnap := current.DeepCopy()
					statusSnap.Status.RackConfig.Racks = []asdbv1.Rack{
						{ID: 1, Revision: "v1", Storage: *rackSt},
					}
					Expect(envtests.K8sClient.Status().Update(ctx, statusSnap)).To(Succeed())

					current, err = testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Roll back revision label to v1 while keeping expanded InputStorage
					current.Spec.RackConfig.Racks[0].Revision = "v1"
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"old rack with same revision v1 already exists with different storage",
						).
						Validate(err)
				})

				// rejects UPDATE when the new revision makes the actual pod name too long (actual pod name is validated on UPDATE)
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
				It("rejects UPDATE when only one rack's pod name exceeds the DNS label limit"+
					"due to rack revision change (multi-rack)", func() {
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
				It("allows update that changes rack InputStorage when rack Revision changes"+
					"and status does not imply a conflict", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					s1 := aeroCluster.Spec.Storage.DeepCopy()
					aeroCluster.Spec.Storage = asdbv1.AerospikeStorageSpec{}
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1", InputStorage: s1},
						},
					}
					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())
					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					s2 := s1.DeepCopy()
					patchFirstPVStorageClassSpec(s2)
					current.Spec.RackConfig.Racks[0].InputStorage = s2
					current.Spec.RackConfig.Racks[0].Revision = newRackRevision
					Expect(envtests.K8sClient.Update(ctx, current)).To(Succeed())
				})

				// Create a valid cluster then update an unrelated field (size).
				// The webhook must not surface any CREATE-only naming error on UPDATE
				// because the CREATE-time projected pod name is not validated on UPDATE.
				It("allows UPDATE without re-applying CREATE-only projected pod name checks", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change size — an otherwise valid update.
					current.Spec.Size = 3
					err = envtests.K8sClient.Update(ctx, current)
					// The webhook must not surface any CREATE-only naming error on UPDATE.
					if err != nil {
						Expect(err).NotTo(MatchError(ContainSubstring("computed at max rack ID")),
							"CREATE-only pod name length check must not be re-run on UPDATE")
					}
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
					cName = uniqueNamespacedName("actpod-zero-rack")
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
			})
		})
	})
})
