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

// verticalScaleCluster builds racks numbered from 1 in spec-list order.
func verticalScaleCluster(
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

func TestValidateVerticalScaling(t *testing.T) {
	tests := []struct {
		name         string
		batch        *intstr.IntOrString
		oldRevisions []string
		newRevisions []string
		wantReject   []string
		oldSize      int32
		newSize      int32
		rf           int
		sc           bool
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			namespaces := []interface{}{nsConfig("test", test.rf, test.sc)}
			oldObj := verticalScaleCluster(test.oldSize, test.oldRevisions, test.batch, namespaces)
			newObj := verticalScaleCluster(test.newSize, test.newRevisions, test.batch, namespaces)

			err := validateVerticalScaling(oldObj, newObj)

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

func TestValidateVerticalScalingIgnoresBatchOnlyChange(t *testing.T) {
	// Batch-only updates are not vertical scaling.
	revisions := []string{"v1", "v1", "v1"}
	namespaces := []interface{}{nsConfig("test", 5, true)}
	oldObj := verticalScaleCluster(6, revisions, ptr.To(intstr.FromInt32(1)), namespaces)
	newObj := verticalScaleCluster(6, revisions, ptr.To(intstr.FromString("100%")), namespaces)

	if err := validateVerticalScaling(oldObj, newObj); err != nil {
		t.Fatalf("expected a batch-only change to be allowed, got error: %v", err)
	}
}

func TestValidateVerticalScalingChecksEverySCNamespace(t *testing.T) {
	// Mixed AP+SC: only the SC namespace's RF is a floor.
	namespaces := []interface{}{
		nsConfig("ap-ns", 6, false),
		nsConfig("sc-ns", 5, true),
	}

	oldObj := verticalScaleCluster(
		6, []string{"v1", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)
	newObj := verticalScaleCluster(
		6, []string{"v2", "v1", "v1"}, ptr.To(intstr.FromString("100%")), namespaces,
	)

	err := validateVerticalScaling(oldObj, newObj)
	if err == nil {
		t.Fatal("expected the SC namespace to be rejected, got nil")
	}

	if !strings.Contains(err.Error(), `"sc-ns"`) {
		t.Errorf("expected the SC namespace to be named, got: %v", err)
	}

	if strings.Contains(err.Error(), "ap-ns") {
		t.Errorf("expected the AP namespace not to drive the rejection, got: %v", err)
	}
}

func TestDistributeItemsPutsMaximumOnFirstRack(t *testing.T) {
	// First spec rack always has the most pods (worst batch).
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

			cluster := verticalScaleCluster(
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
