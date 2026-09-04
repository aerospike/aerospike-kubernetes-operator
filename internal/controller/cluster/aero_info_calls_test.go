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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/aerospike/aerospike-management-lib/deployment"
)

// TestNewPodsHostConnWithOption verifies the classification logic inside
// newPodsHostConnWithOption that was changed as part of the sidecar-failure
// handling PR:
//
//   - A terminating pod is always skipped (IsPodTerminating guard).
//   - A sidecar-failed pod whose server container is running must be INCLUDED
//     in the returned host connections (server is reachable even with a broken sidecar).
//   - A server-failed pod that is in ignorablePodNames must be SKIPPED silently.
//   - A server-failed pod that is NOT ignorable must produce an error so the
//     reconcile loop retries rather than issuing incomplete cluster info calls.
func TestNewPodsHostConnWithOption(t *testing.T) {
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	r := newReconcilerWithObjects(newTestScheme(), aeroCluster)

	// terminatingPod must be built directly (not through the fake client) because
	// DeletionTimestamp is a server-managed field.
	now := metav1.Now()
	terminatingPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pod-terminating",
			DeletionTimestamp: &now,
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
		},
	}

	sidecarFailedPod := *sidecarCrashPod("pod-sidecar-fail", namespace, clusterName, 0)
	serverFailedPod := *crashLoopServerPod("pod-server-fail", namespace, clusterName, 0)

	tests := []struct {
		ignorable sets.Set[string]
		name      string
		pods      []corev1.Pod
		wantConns int
		wantErr   bool
	}{
		{
			name:      "terminating pod is always skipped",
			pods:      []corev1.Pod{terminatingPod},
			ignorable: sets.New[string](),
			wantConns: 0,
		},
		{
			name:      "sidecar-failed pod with running server is included",
			pods:      []corev1.Pod{sidecarFailedPod},
			ignorable: sets.New[string](),
			wantConns: 1,
		},
		{
			name:      "server-failed pod in ignorablePodNames is silently skipped",
			pods:      []corev1.Pod{serverFailedPod},
			ignorable: sets.New(serverFailedPod.Name),
			wantConns: 0,
		},
		{
			name:      "server-failed pod not in ignorablePodNames returns an error",
			pods:      []corev1.Pod{serverFailedPod},
			ignorable: sets.New[string](),
			wantErr:   true,
		},
		{
			name:      "mixed list: sidecar-failed included, server-failed ignorable and terminating skipped",
			pods:      []corev1.Pod{terminatingPod, sidecarFailedPod, serverFailedPod},
			ignorable: sets.New(serverFailedPod.Name),
			wantConns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conns, err := r.newPodsHostConnWithOption(tt.pods, tt.ignorable)

			if tt.wantErr {
				if err == nil {
					t.Error("expected an error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(conns) != tt.wantConns {
				t.Errorf("expected %d connections, got %d", tt.wantConns, len(conns))
			}
		})
	}
}

// TestCheckpointStarted covers AKO's parked-detection over the parsed statuses.
func TestCheckpointStarted(t *testing.T) {
	status := func(state string) deployment.CheckpointNamespaceStatus {
		return deployment.CheckpointNamespaceStatus{State: state}
	}

	tests := []struct {
		statuses map[string]deployment.CheckpointNamespaceStatus
		name     string
		expected bool
	}{
		{
			name:     "still none",
			statuses: map[string]deployment.CheckpointNamespaceStatus{"test": status(deployment.CheckpointStateNone)},
			expected: false,
		},
		{
			name:     "copying",
			statuses: map[string]deployment.CheckpointNamespaceStatus{"test": status(deployment.CheckpointStateCopying)},
			expected: true,
		},
		{
			name:     "done",
			statuses: map[string]deployment.CheckpointNamespaceStatus{"test": status(deployment.CheckpointStateDone)},
			expected: true,
		},
		{
			name:     "failed still counts as started",
			statuses: map[string]deployment.CheckpointNamespaceStatus{"test": status(deployment.CheckpointStateFailed)},
			expected: true,
		},
		{
			// The trigger sets the flag on every configured namespace at once, so one
			// non-none state proves the pod is parked even if others lag.
			name: "one of several has moved",
			statuses: map[string]deployment.CheckpointNamespaceStatus{
				"a": status(deployment.CheckpointStateNone),
				"b": status(deployment.CheckpointStateCopying),
			},
			expected: true,
		},
		{
			name:     "no statuses at all",
			statuses: map[string]deployment.CheckpointNamespaceStatus{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := checkpointStarted(tt.statuses); result != tt.expected {
				t.Errorf("checkpointStarted(%v) = %v, expected %v",
					tt.statuses, result, tt.expected)
			}
		})
	}
}

// TestCheckpointDone pins that the wait is driven entirely by what the server reports —
// no expected set — so a namespace AKO does not know about still holds the pod back.
func TestCheckpointDone(t *testing.T) {
	status := func(state string) deployment.CheckpointNamespaceStatus {
		return deployment.CheckpointNamespaceStatus{State: state}
	}

	tests := []struct {
		statuses     map[string]deployment.CheckpointNamespaceStatus
		name         string
		wantFailed   []string
		expectedDone bool
	}{
		{
			name:         "all done",
			statuses:     map[string]deployment.CheckpointNamespaceStatus{"a": status(deployment.CheckpointStateDone)},
			expectedDone: true,
		},
		{
			// One still copying holds the whole pod back — the anti-premature-delete rule.
			name: "one still copying blocks the pod",
			statuses: map[string]deployment.CheckpointNamespaceStatus{
				"a": status(deployment.CheckpointStateDone),
				"b": status(deployment.CheckpointStateCopying),
			},
			expectedDone: false,
		},
		{
			name: "failed namespaces are reported but terminal",
			statuses: map[string]deployment.CheckpointNamespaceStatus{
				"a": status(deployment.CheckpointStateDone),
				"b": status(deployment.CheckpointStateFailed),
			},
			expectedDone: true,
			wantFailed:   []string{"b"},
		},
		{
			// An unrecognised state must NOT count as terminal.
			name:         "unknown state is not terminal",
			statuses:     map[string]deployment.CheckpointNamespaceStatus{"a": status("verifying")},
			expectedDone: false,
		},
		{
			// Vacuously done. The caller handles "the server reports nothing" before
			// reaching here, since an empty map means the node checkpoints nothing.
			name:         "no statuses",
			statuses:     map[string]deployment.CheckpointNamespaceStatus{},
			expectedDone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, failed := checkpointDone(tt.statuses)

			if done != tt.expectedDone {
				t.Fatalf("checkpointDone() done = %v, expected %v", done, tt.expectedDone)
			}

			if len(failed) != len(tt.wantFailed) {
				t.Errorf("failedNSs = %v, expected %v", failed, tt.wantFailed)
			}
		})
	}
}
