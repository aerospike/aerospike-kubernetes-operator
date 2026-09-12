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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
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
	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})

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

// - Test the transient failures and recovery with retry
// - Deleted pod should abort the retry loop
func TestMarkPodCheckpointParked(t *testing.T) {
	const containerID = "containerd://park-1111"

	newParkedPod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-0", Namespace: namespace},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: asdbv1.AerospikeServerContainerName, ContainerID: containerID},
				},
			},
		}
	}

	t.Run("a transient patch failure is retried and the annotation still lands", func(t *testing.T) {
		pod := newParkedPod()
		failedOnce := false

		r := newTestReconciler(t, newTestAerospikeCluster(namespace, clusterName), &interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object,
				patch client.Patch, opts ...client.PatchOption,
			) error {
				if !failedOnce {
					failedOnce = true
					return errors.New("simulated transient API failure")
				}

				return c.Patch(ctx, obj, patch, opts...)
			},
		}, pod)

		r.markPodCheckpointParked(context.TODO(), pod)

		var got corev1.Pod

		require.NoError(t, r.Get(context.TODO(), client.ObjectKeyFromObject(pod), &got))
		require.Equal(t, containerID, got.Annotations[asdbv1.IndexCheckpointParkedAnnotation],
			"the retried patch must still carry the annotation — an empty second patch is the regression")
	})

	t.Run("a deleted pod stops the retry immediately", func(t *testing.T) {
		pod := newParkedPod()
		patchCalls := 0

		r := newTestReconciler(t, newTestAerospikeCluster(namespace, clusterName), &interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object,
				patch client.Patch, opts ...client.PatchOption,
			) error {
				patchCalls++
				return k8serrors.NewNotFound(corev1.Resource("pods"), obj.GetName())
			},
		})

		r.markPodCheckpointParked(context.TODO(), pod)

		require.Equal(t, 1, patchCalls, "NotFound must not be retried")
	})
}

// TestCheckpointDone pins that the wait is driven entirely by what the server reports —
// no expected set — so a namespace AKO does not know about still holds the pod back.
func TestCheckpointDone(t *testing.T) {
	status := func(state string) deployment.CheckpointNamespaceStatus {
		return deployment.CheckpointNamespaceStatus{State: state}
	}

	response := func(nss map[string]deployment.CheckpointNamespaceStatus) deployment.CheckpointResponse {
		return deployment.CheckpointResponse{Namespaces: nss}
	}

	tests := []struct {
		resp         deployment.CheckpointResponse
		name         string
		wantFailed   []string
		expectedDone bool
	}{
		{
			name:         "all done",
			resp:         response(map[string]deployment.CheckpointNamespaceStatus{"a": status(deployment.CheckpointStateDone)}),
			expectedDone: true,
		},
		{
			// One still copying holds the whole pod back — the anti-premature-delete rule.
			name: "one still copying blocks the pod",
			resp: response(map[string]deployment.CheckpointNamespaceStatus{
				"a": status(deployment.CheckpointStateDone),
				"b": status(deployment.CheckpointStateCopying),
			}),
			expectedDone: false,
		},
		{
			name: "failed namespaces are reported but terminal",
			resp: response(map[string]deployment.CheckpointNamespaceStatus{
				"a": status(deployment.CheckpointStateDone),
				"b": status(deployment.CheckpointStateFailed),
			}),
			expectedDone: true,
			wantFailed:   []string{"b"},
		},
		{
			// An unrecognised state must NOT count as terminal.
			name:         "unknown state is not terminal",
			resp:         response(map[string]deployment.CheckpointNamespaceStatus{"a": status("verifying")}),
			expectedDone: false,
		},
		{
			// A node whose namespaces have ALL opted out parks without writing anything and
			// reports no namespace record. It is done and must be deleted — it has left the
			// cluster. Reading an empty response as "nothing to do" strands it until its
			// park times out.
			name: "parked with no namespaces is done",
			resp: deployment.CheckpointResponse{
				Namespaces: map[string]deployment.CheckpointNamespaceStatus{},
				IsParked:   true,
				ParkMS:     1500,
			},
			expectedDone: true,
		},
		{
			// Vacuously done. The caller screens this out before reaching here — empty and
			// NOT parked is a restarted asd, so there is nothing to delete.
			name:         "no statuses",
			resp:         response(map[string]deployment.CheckpointNamespaceStatus{}),
			expectedDone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, failed := checkpointDone(tt.resp)

			if done != tt.expectedDone {
				t.Fatalf("checkpointDone() done = %v, expected %v", done, tt.expectedDone)
			}

			if len(failed) != len(tt.wantFailed) {
				t.Errorf("failedNSs = %v, expected %v", failed, tt.wantFailed)
			}
		})
	}
}
