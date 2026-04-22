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
				//   • length (CREATE only): the webhook projects label rune count using
				//     max(len(revision), minRevisionReservation=3) with fixed Aerospike
				//     and K8s (controller-revision hash) overheads. On UPDATE, a related
				//     check uses the longest real StatefulSet name and the same K8s bound.
				It("rejects revision that makes projected Pod label value too long at CREATE", func() {
					// cluster = 3, Aerospike fixed 10, revision 48, K8s 10: 3+10+48+10 = 71.
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
							"Pod label value would exceed the",
							"63-character DNS label limit",
							"revision placeholder = 48",
							"total = 71",
							"reduce by 8",
						).Validate(err)
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

				// 48 (name) + 10 (Aerospike fixed) + 3 (placeholder) + 10 (K8s) = 71.
				It("rejects cluster name too long for projected Pod label (with rack config)", func() {
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
							"Pod label value would exceed the",
							"63-character DNS label limit",
							"revision placeholder = 3",
							"total = 71",
							"reduce by 8",
						).Validate(err)
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
				// drives the check. If that makes the projected value > 63 runes, CREATE fails.
				It("rejects CREATE when the rack with the longest revision drives projected Pod label too long", func() {
					// 3 + 10 + 48 + 10 = 71.
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
							"Pod label value would exceed the",
							"63-character DNS label limit",
							"revision placeholder = 48",
							"total = 71",
							"reduce by 8",
						).Validate(err)
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

				// CREATE admission projects label rune count: name + 10 (Aerospike) + revision + 10 (K8s) ≤ 63.
				DescribeTable("accepts CREATE when a rack revision keeps the projected Pod label value within limit",
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

					// 3 + 10 + 9 + 10 = 32.
					Entry("revision longer than 3 chars when Pod label value projection stays within limit",
						"rev-alpha"),

					// 3 + 10 + 40 + 10 = 63.
					Entry("revision at projected Pod label boundary (40 chars)", strings.Repeat("a", 40)),
				)

				// Two racks: maxPlaceholderLen 40; 3+10+40+10=63.
				It("accepts multiple racks where the longest revision fits within the Pod label value limit", func() {
					shortName := "def"
					cName = test.GetNamespacedName(shortName, clusterNamespacedName.Namespace)
					aeroCluster := testCluster.CreateDummyAerospikeCluster(cName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1, Revision: "v1"},
							{ID: 2, Revision: strings.Repeat("a", 40)},
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
					patchFirstPVStorageClassSpec(r0.InputStorage)

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

				// On UPDATE, validate longest StatefulSet name + joiner + controller-revision budget.
				It("rejects UPDATE when the new revision makes the projected Pod label value too long", func() {
					// At CREATE: 20 + 10 + 3 + 10 = 43. On UPDATE, STS name = 20+3+42 = 65, label 65+1+10=76.
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

					current.Spec.RackConfig.Racks[0].Revision = strings.Repeat("b", 42)
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred(), "UPDATE with oversized revision should be rejected")

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"would generate pod label value exceeding the",
							"63-character DNS label limit",
							"reduce by 13",
						).Validate(err)
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

				// Longest StatefulSet name among racks drives the UPDATE label check.
				It("rejects UPDATE when one rack's revision makes the Pod label value exceed the limit (multi-rack)", func() {
					// Same 65+1+10=76 as single-rack case; rack 2 is shorter.
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
							"would generate pod label value exceeding the",
							"63-character DNS label limit",
							"reduce by 13",
						).Validate(err)
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
				// because the CREATE-time projected label rune check is not re-run on every UPDATE.
				It("allows UPDATE without re-applying CREATE-only projected Pod label value checks", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current, err := testCluster.GetCluster(envtests.K8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Change size — an otherwise valid update.
					current.Spec.Size = 3
					err = envtests.K8sClient.Update(ctx, current)
					// The webhook must not surface any CREATE-only naming error on UPDATE.
					Expect(err).ToNot(HaveOccurred())
				})

				// validateActualPodNames uses the longest real StatefulSet name and the
				// joiner + controller-revision rune model.
				It("allows UPDATE when revision grows but stays within the Pod label value model", func() {
					// "abc" + short STS; label << 63.
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
