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
