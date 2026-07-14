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
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
	)

	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	r := newReconcilerWithObjects(newTestScheme(), aeroCluster)

	// terminatingPod has a non-nil DeletionTimestamp, constructed directly (not
	// through the fake client) because DeletionTimestamp is server-managed.
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

	// sidecarFailedPod has a running server but a crashing sidecar.
	// Its server is reachable, so it must be included in host connections.
	sidecarFailedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-sidecar-fail"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				serverContainer(true),
				sidecarContainer("monitor", false, true),
			},
		},
	}

	// serverFailedPod has a non-running server container.
	serverFailedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-server-fail"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				serverContainer(false),
			},
		},
	}

	t.Run("terminating pod is always skipped", func(t *testing.T) {
		conns, err := r.newPodsHostConnWithOption(
			[]corev1.Pod{terminatingPod},
			sets.New[string](),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(conns) != 0 {
			t.Errorf("expected 0 connections for terminating pod, got %d", len(conns))
		}
	})

	t.Run("sidecar-failed pod with running server is included in host connections", func(t *testing.T) {
		conns, err := r.newPodsHostConnWithOption(
			[]corev1.Pod{sidecarFailedPod},
			sets.New[string](),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(conns) != 1 {
			t.Errorf("expected 1 connection for sidecar-failed pod, got %d", len(conns))
		}
	})

	t.Run("server-failed pod in ignorablePodNames is silently skipped", func(t *testing.T) {
		conns, err := r.newPodsHostConnWithOption(
			[]corev1.Pod{serverFailedPod},
			sets.New(serverFailedPod.Name),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(conns) != 0 {
			t.Errorf("expected 0 connections for ignorable server-failed pod, got %d", len(conns))
		}
	})

	t.Run("server-failed pod not in ignorablePodNames returns an error", func(t *testing.T) {
		_, err := r.newPodsHostConnWithOption(
			[]corev1.Pod{serverFailedPod},
			sets.New[string](),
		)
		if err == nil {
			t.Error("expected an error for non-ignorable server-failed pod, got nil")
		}
	})

	t.Run("mixed list: sidecar-failed included, server-failed ignorable skipped, terminating skipped", func(t *testing.T) {
		conns, err := r.newPodsHostConnWithOption(
			[]corev1.Pod{terminatingPod, sidecarFailedPod, serverFailedPod},
			sets.New(serverFailedPod.Name),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only the sidecar-failed pod (with running server) should appear.
		if len(conns) != 1 {
			t.Errorf("expected exactly 1 connection in mixed list, got %d", len(conns))
		}
	})
}
