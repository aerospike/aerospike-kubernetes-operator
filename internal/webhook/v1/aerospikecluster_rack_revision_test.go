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

package v1

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

func nsConfig(name string, replicationFactor int, strongConsistency bool) map[string]interface{} {
	ns := map[string]interface{}{
		asdbv1.ConfKeyName:              name,
		asdbv1.ConfKeyReplicationFactor: replicationFactor,
	}

	if strongConsistency {
		ns[asdbv1.ConfKeyStrongConsistency] = true
	}

	return ns
}

// rackRevisionCluster builds racks numbered from 1 in spec-list order.
func rackRevisionCluster(
	size int32, revisions []string, batch *intstr.IntOrString, namespaces []interface{},
) *asdbv1.AerospikeCluster {
	racks := make([]asdbv1.Rack, 0, len(revisions))

	for idx := range revisions {
		racks = append(racks, asdbv1.Rack{
			ID:       idx + 1,
			Revision: revisions[idx],
			AerospikeConfig: asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					asdbv1.ConfKeyNamespace: namespaces,
				},
			},
		})
	}

	return &asdbv1.AerospikeCluster{
		Spec: asdbv1.AerospikeClusterSpec{
			Size: size,
			RackConfig: asdbv1.RackConfig{
				Racks:                  racks,
				RollingUpdateBatchSize: batch,
			},
		},
	}
}

// withStatusRevisions records revisions as the last reconciled state. Rack IDs match the
// 1-based, spec-order IDs rackRevisionCluster assigns, so a status revision that differs
// from the spec one marks that rack as mid-migration.
func withStatusRevisions(cluster *asdbv1.AerospikeCluster, revisions []string) *asdbv1.AerospikeCluster {
	racks := make([]asdbv1.Rack, 0, len(revisions))

	for idx := range revisions {
		racks = append(racks, asdbv1.Rack{ID: idx + 1, Revision: revisions[idx]})
	}

	cluster.Status.RackConfig.Racks = racks
	cluster.Status.Size = cluster.Spec.Size

	return cluster
}

func TestValidateRackRevisionChange(t *testing.T) {
	tests := []struct {
		name string
		// statusRevisions is the last reconciled revision per rack, applied to oldObj only.
		// Nil means no status, i.e. a settled cluster or a first install.
		statusRevisions []string
		batch           *intstr.IntOrString
		oldRevisions    []string
		newRevisions    []string
		wantReject      []string
		oldSize         int32
		newSize         int32
		rf              int
		sc              bool
	}{
		// Gate skipped (no revision change).
		{
			name:         "allows an update that changes no rack revision",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v1", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},
		{
			name:         "allows a pure resize with no rack revision change",
			oldSize:      12,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v1", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},
		{
			name:         "allows adding a new rack that carries a revision",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1"},
			newRevisions: []string{"v1", "v2"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},

		// Size 1, AP and SC.
		{
			name:         "rejects a bump on a single-node SC cluster",
			oldSize:      1,
			newSize:      1,
			oldRevisions: []string{"v1"},
			newRevisions: []string{"v2"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromInt32(1)),
			wantReject:   []string{"spec.size is 1"},
		},
		{
			name:         "rejects a bump on a single-node AP cluster",
			oldSize:      1,
			newSize:      1,
			oldRevisions: []string{"v1"},
			newRevisions: []string{"v2"},
			rf:           2,
			batch:        ptr.To(intstr.FromInt32(1)),
			wantReject:   []string{"spec.size is 1"},
		},
		{
			name:         "rejects a bump bundled with a scale-down to one node",
			oldSize:      6,
			newSize:      1,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromInt32(1)),
			wantReject:   []string{"spec.size is 1"},
		},

		// RF 1, AP and SC.
		{
			name:         "rejects a bump when an SC namespace is at replication-factor 1",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           1,
			sc:           true,
			batch:        ptr.To(intstr.FromInt32(1)),
			wantReject:   []string{"replication-factor 1"},
		},
		{
			name:         "rejects a bump when an AP namespace is at replication-factor 1",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           1,
			batch:        ptr.To(intstr.FromInt32(1)),
			wantReject:   []string{"replication-factor 1"},
		},

		// SC floor.
		{
			name:         "rejects a three-rack bump at full batch when survivors fall below RF",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"2 of 6 nodes", "leaving 4", "replication-factor 5"},
		},
		{
			name:         "allows a three-rack bump at full batch when survivors meet RF",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           3,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},
		{
			name:         "sizes the batch against the largest rack when pods divide unevenly",
			oldSize:      7,
			newSize:      7,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"3 of 7 nodes", "leaving 4"},
		},
		{
			name:         "allows an uneven three-rack bump when survivors meet RF",
			oldSize:      7,
			newSize:      7,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           4,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},
		{
			name:         "allows a single-rack bump when the batch is one pod",
			oldSize:      5,
			newSize:      5,
			oldRevisions: []string{"v1"},
			newRevisions: []string{"v2"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromInt32(1)),
		},
		{
			name:         "rejects a single-rack bump at full batch",
			oldSize:      5,
			newSize:      5,
			oldRevisions: []string{"v1"},
			newRevisions: []string{"v2"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"5 of 5 nodes", "leaving 0"},
		},

		// Batch resolution.
		{
			name:         "rounds a percentage batch up against the first rack",
			oldSize:      4,
			newSize:      4,
			oldRevisions: []string{"v1", "v1"},
			newRevisions: []string{"v2", "v1"},
			rf:           4,
			sc:           true,
			batch:        ptr.To(intstr.FromString("50%")),
			wantReject:   []string{"1 of 4 nodes", "leaving 3", "replication-factor 4"},
		},
		{
			name:         "allows a rounded percentage batch when survivors meet RF",
			oldSize:      4,
			newSize:      4,
			oldRevisions: []string{"v1", "v1"},
			newRevisions: []string{"v2", "v1"},
			rf:           3,
			sc:           true,
			batch:        ptr.To(intstr.FromString("50%")),
		},
		{
			name:         "treats an unset batch as one pod",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			sc:           true,
		},
		{
			name:         "rejects an unset batch only when one pod is already too many",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           6,
			sc:           true,
			wantReject:   []string{"1 of 6 nodes", "leaving 5", "replication-factor 6"},
		},
		{
			name:         "caps an integer batch at the first rack's pod count",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           4,
			sc:           true,
			batch:        ptr.To(intstr.FromInt32(10)),
		},

		// SC majority: survivors must be more than half the roster.
		{
			name:         "rejects a two-rack bump at full batch when survivors are half the roster",
			oldSize:      4,
			newSize:      4,
			oldRevisions: []string{"v1", "v1"},
			newRevisions: []string{"v2", "v1"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"2 of 4 nodes", "leaving 2", "majority"},
		},
		{
			name:         "rejects a three-rack bump at full batch when survivors are half the roster",
			oldSize:      4,
			newSize:      4,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           2,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"2 of 4 nodes", "leaving 2", "majority"},
		},
		{
			name:         "allows an AP bump when survivors are half the roster",
			oldSize:      4,
			newSize:      4,
			oldRevisions: []string{"v1", "v1"},
			newRevisions: []string{"v2", "v1"},
			rf:           2,
			batch:        ptr.To(intstr.FromString("100%")),
		},

		// AP: no RF floor.
		{
			name:         "allows an AP bump even when survivors fall below RF",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			batch:        ptr.To(intstr.FromString("100%")),
		},

		// Bundled resize uses new spec.size.
		{
			name:         "sizes a bundled scale-down with the new cluster size",
			oldSize:      12,
			newSize:      6,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"2 of 6 nodes", "leaving 4"},
		},
		{
			name:         "sizes a bundled scale-up with the new cluster size",
			oldSize:      3,
			newSize:      9,
			oldRevisions: []string{"v1", "v1", "v1"},
			newRevisions: []string{"v2", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
		},

		// Revert is a revision change.
		{
			name:         "treats a revert like a bump",
			oldSize:      6,
			newSize:      6,
			oldRevisions: []string{"v2", "v1", "v1"},
			newRevisions: []string{"v1", "v1", "v1"},
			rf:           5,
			sc:           true,
			batch:        ptr.To(intstr.FromString("100%")),
			wantReject:   []string{"2 of 6 nodes"},
		},

		// In-flight migration: status still holds the revision the operator has actually
		// reconciled, so a follow-up update must stay armed even when it changes no revision.
		{
			// Bump rack 1, then scale down before it finishes. [2,1,1] at size 4 means a
			// full batch takes 2 of the 4 nodes down, leaving 2 for an RF 3 namespace.
			name:            "rejects a scale-down while a rack revision migration is in flight",
			statusRevisions: []string{"v1", "v1", "v1"},
			oldSize:         6,
			newSize:         4,
			oldRevisions:    []string{"v2", "v1", "v1"},
			newRevisions:    []string{"v2", "v1", "v1"},
			rf:              3,
			sc:              true,
			batch:           ptr.To(intstr.FromString("100%")),
			wantReject:      []string{"2 of 4 nodes", "leaving 2", "replication-factor 3"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			namespaces := []interface{}{nsConfig("test", test.rf, test.sc)}
			oldObj := rackRevisionCluster(test.oldSize, test.oldRevisions, test.batch, namespaces)
			newObj := rackRevisionCluster(test.newSize, test.newRevisions, test.batch, namespaces)

			// The gate reads the old object's status, matching validateConcurrentRackRevisions.
			if test.statusRevisions != nil {
				oldObj = withStatusRevisions(oldObj, test.statusRevisions)
			}

			err := validateRackRevisionChange(oldObj, newObj)

			if len(test.wantReject) == 0 {
				if err != nil {
					t.Fatalf("expected the update to be allowed, got error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected a rejection containing %v, got nil", test.wantReject)
			}

			for _, fragment := range test.wantReject {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("expected error to contain %q, got: %v", fragment, err)
				}
			}
		})
	}
}

func TestValidateRackRevisionChangeIgnoresBatchOnlyChange(t *testing.T) {
	// Batch-only updates are not a rack revision change.
	revisions := []string{"v1", "v1", "v1"}
	namespaces := []interface{}{nsConfig("test", 5, true)}
	oldObj := rackRevisionCluster(6, revisions, ptr.To(intstr.FromInt32(1)), namespaces)
	newObj := rackRevisionCluster(6, revisions, ptr.To(intstr.FromString("100%")), namespaces)

	if err := validateRackRevisionChange(oldObj, newObj); err != nil {
		t.Fatalf("expected a batch-only change to be allowed, got error: %v", err)
	}
}

func TestValidateRackRevisionChangeUsesStrictestSCNamespace(t *testing.T) {
	// Mixed AP+SC: only the SC namespace's RF is a floor.
	namespaces := []interface{}{
		nsConfig("ap-ns", 6, false),
		nsConfig("sc-ns", 5, true),
	}

	oldObj := rackRevisionCluster(
		6, []string{"v1", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)
	newObj := rackRevisionCluster(
		6, []string{"v2", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)

	err := validateRackRevisionChange(oldObj, newObj)
	if err == nil {
		t.Fatal("expected the SC namespace to be rejected, got nil")
	}

	if !strings.Contains(err.Error(), `"sc-ns"`) {
		t.Errorf("expected the SC namespace to be named, got: %v", err)
	}

	if !strings.Contains(err.Error(), "replication-factor 5") {
		t.Errorf("expected the SC namespace's RF to be the floor, got: %v", err)
	}

	if strings.Contains(err.Error(), "ap-ns") {
		t.Errorf("expected the AP namespace not to drive the rejection, got: %v", err)
	}
}

// TestValidateRackRevisionChangeIgnoresAPReplicationFactorBelowSC pins that an AP
// namespace never lowers the floor. A reduction over every namespace would use the
// AP namespace's RF 2 here and wrongly allow the update.
func TestValidateRackRevisionChangeIgnoresAPReplicationFactorBelowSC(t *testing.T) {
	namespaces := []interface{}{
		nsConfig("ap-ns", 2, false),
		nsConfig("sc-ns", 5, true),
	}

	// Size 6 over 3 racks with a full batch: 2 leave, 4 survive, below the SC RF 5.
	oldObj := rackRevisionCluster(
		6, []string{"v1", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)
	newObj := rackRevisionCluster(
		6, []string{"v2", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)

	err := validateRackRevisionChange(oldObj, newObj)
	if err == nil {
		t.Fatal("expected the SC namespace's RF 5 to reject, got nil")
	}

	for _, fragment := range []string{`"sc-ns"`, "replication-factor 5", "leaving 4"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("expected the rejection to contain %q, got: %v", fragment, err)
		}
	}

	if strings.Contains(err.Error(), "ap-ns") {
		t.Errorf("expected the AP namespace not to drive the rejection, got: %v", err)
	}
}

// TestValidateRackRevisionChangeUsesHighestSCReplicationFactor pins the direction of the
// reduction across SC namespaces. Survivors must clear every SC namespace, so the
// binding one is the highest RF; reducing with min would allow the rejecting case.
func TestValidateRackRevisionChangeUsesHighestSCReplicationFactor(t *testing.T) {
	namespaces := []interface{}{
		nsConfig("sc-low", 3, true),
		nsConfig("sc-high", 5, true),
	}

	t.Run("rejects when only the lower RF is met", func(t *testing.T) {
		// Size 6 over 3 racks, full batch: 2 leave, 4 survive. Clears RF 3, not RF 5.
		oldObj := rackRevisionCluster(
			6, []string{"v1", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
		)
		newObj := rackRevisionCluster(
			6, []string{"v2", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
		)

		err := validateRackRevisionChange(oldObj, newObj)
		if err == nil {
			t.Fatal("expected the highest SC RF to reject, got nil")
		}

		for _, fragment := range []string{`"sc-high"`, "replication-factor 5", "leaving 4"} {
			if !strings.Contains(err.Error(), fragment) {
				t.Errorf("expected the rejection to contain %q, got: %v", fragment, err)
			}
		}
	})

	t.Run("allows when the highest RF is met", func(t *testing.T) {
		// Size 8 over 3 racks, full batch: first rack holds 3, so 3 leave and 5
		// survive, which meets RF 5 and is a majority of 8.
		oldObj := rackRevisionCluster(
			8, []string{"v1", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
		)
		newObj := rackRevisionCluster(
			8, []string{"v2", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
		)

		if err := validateRackRevisionChange(oldObj, newObj); err != nil {
			t.Fatalf("expected 5 survivors to meet RF 5, got error: %v", err)
		}
	})
}

func TestDistributeItemsPutsMaximumOnFirstRack(t *testing.T) {
	// First spec rack always has the most pods (worst batch), and holds exactly
	// ceil(size/racks) — the closed form podsLeavingFirstRack relies on.
	for size := int32(1); size <= 12; size++ {
		for racks := int32(1); racks <= 3; racks++ {
			topology := asdbv1.DistributeItems(size, racks)

			for idx := range topology {
				if topology[idx] > topology[0] {
					t.Fatalf(
						"size %d over %d racks: rack at position %d holds %d pods, more than the first rack's %d",
						size, racks, idx, topology[idx], topology[0],
					)
				}
			}

			if want := (size + racks - 1) / racks; topology[0] != want {
				t.Fatalf(
					"size %d over %d racks: first rack holds %d pods, want ceil = %d",
					size, racks, topology[0], want,
				)
			}
		}
	}
}

func TestPodsLeavingFirstRack(t *testing.T) {
	tests := []struct {
		batch *intstr.IntOrString
		name  string
		racks int
		size  int32
		want  int32
	}{
		{name: "unset batch is one pod", size: 6, racks: 3, want: 1},
		{name: "full batch takes the whole first rack", size: 6, racks: 3, batch: ptr.To(intstr.FromString("100%")), want: 2},
		{name: "full batch on an uneven split", size: 7, racks: 3, batch: ptr.To(intstr.FromString("100%")), want: 3},
		{name: "percentage rounds up", size: 4, racks: 2, batch: ptr.To(intstr.FromString("50%")), want: 1},
		{name: "percentage rounds up to more than one", size: 9, racks: 3, batch: ptr.To(intstr.FromString("50%")), want: 2},
		{name: "integer batch is capped at the first rack", size: 6, racks: 3, batch: ptr.To(intstr.FromInt32(10)), want: 2},
		{name: "integer batch below the rack size is kept", size: 9, racks: 3, batch: ptr.To(intstr.FromInt32(2)), want: 2},
		{name: "single rack holds the whole cluster", size: 5, racks: 1, batch: ptr.To(intstr.FromString("100%")), want: 5},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			revisions := make([]string, test.racks)
			for idx := range revisions {
				revisions[idx] = "v1"
			}

			cluster := rackRevisionCluster(
				test.size, revisions, test.batch, []interface{}{nsConfig("test", 2, true)},
			)

			if got := podsLeavingFirstRack(cluster); got != test.want {
				topology := asdbv1.DistributeItems(
					cluster.Spec.Size, utils.Len32(cluster.Spec.RackConfig.Racks),
				)
				t.Errorf("got %d pods leaving, want %d (topology %v)", got, test.want, topology)
			}
		})
	}
}

// TestPodsLeavingFirstRackMatchesDistributeItems proves the closed form is equivalent
// to indexing the topology DistributeItems builds, across the whole supported range.
func TestPodsLeavingFirstRackMatchesDistributeItems(t *testing.T) {
	batches := []*intstr.IntOrString{
		nil,
		ptr.To(intstr.FromInt32(1)),
		ptr.To(intstr.FromInt32(10)),
		ptr.To(intstr.FromString("50%")),
		ptr.To(intstr.FromString("100%")),
	}

	for _, batch := range batches {
		for size := int32(1); size <= 64; size++ {
			for rackCount := 1; rackCount <= 8; rackCount++ {
				revisions := make([]string, rackCount)
				for idx := range revisions {
					revisions[idx] = "v1"
				}

				cluster := rackRevisionCluster(
					size, revisions, batch, []interface{}{nsConfig("test", 2, true)},
				)

				topology := asdbv1.DistributeItems(size, utils.Len32(cluster.Spec.RackConfig.Racks))
				want := clampedBatch(batch, topology[0])

				if got := podsLeavingFirstRack(cluster); got != want {
					t.Fatalf(
						"batch %v, size %d over %d racks: got %d, want %d (topology %v)",
						batch, size, rackCount, got, want, topology,
					)
				}
			}
		}
	}
}

// TestValidateRackRevisionChangeIgnoresMetadataOnlyUpdate pins that an unchanged spec is never
// re-validated. Without this the status arm would re-run the check on finalizer removal and
// leave a mid-migration cluster stuck in Terminating.
func TestValidateRackRevisionChangeIgnoresMetadataOnlyUpdate(t *testing.T) {
	namespaces := []interface{}{nsConfig("test", 3, true)}
	revisions := []string{"v2", "v1", "v1"}

	// Size 4 at a full batch: this spec would be rejected if the check ran.
	oldObj := withStatusRevisions(
		rackRevisionCluster(4, revisions, ptr.To(intstr.FromString("100%")), namespaces),
		[]string{"v1", "v1", "v1"},
	)
	newObj := rackRevisionCluster(4, revisions, ptr.To(intstr.FromString("100%")), namespaces)

	newObj.Finalizers = nil
	oldObj.Finalizers = []string{"asdb.aerospike.com/storage-finalizer"}

	if err := validateRackRevisionChange(oldObj, newObj); err != nil {
		t.Fatalf("expected a metadata-only update to be allowed, got error: %v", err)
	}
}

func TestHasRackRevisionChange(t *testing.T) {
	racks := func(pairs ...any) []asdbv1.Rack {
		out := make([]asdbv1.Rack, 0, len(pairs)/2)
		for idx := 0; idx+1 < len(pairs); idx += 2 {
			out = append(out, asdbv1.Rack{ID: pairs[idx].(int), Revision: pairs[idx+1].(string)})
		}

		return out
	}

	tests := []struct {
		name    string
		status  []asdbv1.Rack
		oldSpec []asdbv1.Rack
		newSpec []asdbv1.Rack
		want    bool
	}{
		{
			name:    "no change anywhere",
			status:  racks(1, "v1"),
			oldSpec: racks(1, "v1"),
			newSpec: racks(1, "v1"),
		},
		{
			name:    "spec arm: this update bumps the revision",
			oldSpec: racks(1, "v1"),
			newSpec: racks(1, "v2"),
			want:    true,
		},
		{
			name:    "spec arm: a revert counts",
			status:  racks(1, "v1"),
			oldSpec: racks(1, "v2"),
			newSpec: racks(1, "v1"),
			want:    true,
		},
		{
			name:    "status arm alone: migration in flight, spec unchanged",
			status:  racks(1, "v1"),
			oldSpec: racks(1, "v2"),
			newSpec: racks(1, "v2"),
			want:    true,
		},
		{
			name:    "a newly added rack is not a rack revision change",
			status:  racks(1, "v1"),
			oldSpec: racks(1, "v1"),
			newSpec: racks(1, "v1", 2, "v9"),
		},
		{
			name:    "a rack only in status is ignored",
			status:  racks(1, "v1", 2, "v1"),
			oldSpec: racks(1, "v1"),
			newSpec: racks(1, "v1"),
		},
		{
			name:    "empty status is inert",
			oldSpec: racks(1, "v1"),
			newSpec: racks(1, "v1"),
		},
		{
			// Keyed by ID, not position: a positional comparison would read rack 2's
			// status revision against rack 1's spec revision and wrongly fire.
			name:    "status listed in the opposite order still keys by ID",
			status:  racks(2, "v2", 1, "v1"),
			oldSpec: racks(1, "v1", 2, "v2"),
			newSpec: racks(1, "v1", 2, "v2"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasRackRevisionChange(test.status, test.oldSpec, test.newSpec); got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}
