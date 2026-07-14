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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// serverContainer builds a ContainerStatus for the Aerospike server container.
func serverContainer(ready bool) corev1.ContainerStatus {
	cs := corev1.ContainerStatus{
		Name:  asdbv1.AerospikeServerContainerName,
		Ready: ready,
	}
	if ready {
		cs.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	}

	return cs
}

// sidecarContainer builds a ContainerStatus for a sidecar container.
//
//nolint:unparam // for future use
func sidecarContainer(name string, ready, crashLoop bool) corev1.ContainerStatus {
	cs := corev1.ContainerStatus{Name: name, Ready: ready}
	if crashLoop {
		cs.State = corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		}
	}

	return cs
}

// runningPod creates a pod in Running phase created well outside the grace period.
func runningPod(name string, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: statuses,
		},
	}
}

// recentPod creates a pod that is within the default grace period (~10 s old).
func recentPod(name string, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Second)),
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: statuses,
		},
	}
}

// TestGetServerFailedAndActivePods_SidecarFailedGoesToActive is the central
// invariant: a pod with a crashing sidecar but a healthy server must be
// classified as active, never as failed.  This ensures sidecar-failed pods
// are not incorrectly skipped during safety checks or batching.
func TestGetServerFailedAndActivePods_SidecarFailedGoesToActive(t *testing.T) {
	sidecarFailedPod := runningPod("sidecar-fail",
		serverContainer(true),
		sidecarContainer("monitor", false, true),
	)

	failed, grace, active := getServerFailedAndActivePods([]*corev1.Pod{sidecarFailedPod}, false)

	if len(active) != 1 || active[0].Name != sidecarFailedPod.Name {
		t.Errorf("sidecar-failed pod should be active, got active=%v failed=%v grace=%v",
			podNames(active), podNames(failed), podNames(grace))
	}

	if len(failed) != 0 || len(grace) != 0 {
		t.Errorf("expected no failed/grace pods, got failed=%v grace=%v",
			podNames(failed), podNames(grace))
	}
}

func TestGetServerFailedAndActivePods(t *testing.T) {
	serverFailedPod := runningPod("server-fail",
		corev1.ContainerStatus{
			Name: asdbv1.AerospikeServerContainerName,
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
			},
		},
	)
	serverFailedRecentPod := recentPod("server-fail-recent",
		corev1.ContainerStatus{
			Name: asdbv1.AerospikeServerContainerName,
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
			},
		},
	)
	healthyPod := runningPod("healthy",
		serverContainer(true),
		sidecarContainer("monitor", true, false),
	)
	sidecarFailedPod := runningPod("sidecar-fail",
		serverContainer(true),
		sidecarContainer("monitor", false, true),
	)

	tests := []struct {
		name            string
		pods            []*corev1.Pod
		wantFailed      []string
		wantGrace       []string
		wantActive      []string
		withGracePeriod bool
	}{
		{
			name:       "empty list returns all empty",
			pods:       []*corev1.Pod{},
			wantFailed: nil,
			wantGrace:  nil,
			wantActive: nil,
		},
		{
			name:       "healthy pod is active",
			pods:       []*corev1.Pod{healthyPod},
			wantActive: []string{"healthy"},
		},
		{
			name:       "server-failed pod (past grace) goes to failed",
			pods:       []*corev1.Pod{serverFailedPod},
			wantFailed: []string{"server-fail"},
		},
		{
			// Within grace period and withGracePeriod=true: should be in grace slice.
			name:            "server-failed pod within grace period goes to grace slice",
			pods:            []*corev1.Pod{serverFailedRecentPod},
			withGracePeriod: true,
			wantGrace:       []string{"server-fail-recent"},
		},
		{
			// withGracePeriod=false: grace is disabled, pod goes directly to failed.
			name:            "server-failed pod within grace period but grace disabled goes to failed",
			pods:            []*corev1.Pod{serverFailedRecentPod},
			withGracePeriod: false,
			wantFailed:      []string{"server-fail-recent"},
		},
		{
			// Sidecar-failed pod must always be classified as active regardless of
			// withGracePeriod, because CheckServerFailedWithGrace ignores sidecars.
			name:       "sidecar-failed pod is always active",
			pods:       []*corev1.Pod{sidecarFailedPod},
			wantActive: []string{"sidecar-fail"},
		},
		{
			name:       "mixed: server-failed, sidecar-failed and healthy",
			pods:       []*corev1.Pod{serverFailedPod, sidecarFailedPod, healthyPod},
			wantFailed: []string{"server-fail"},
			wantActive: []string{"sidecar-fail", "healthy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed, grace, active := getServerFailedAndActivePods(tt.pods, tt.withGracePeriod)

			assertPodNames(t, "failed", podNames(failed), tt.wantFailed)
			assertPodNames(t, "grace", podNames(grace), tt.wantGrace)
			assertPodNames(t, "active", podNames(active), tt.wantActive)
		})
	}
}

func TestGetSidecarFailedPods(t *testing.T) {
	now := metav1.Now()

	tests := []struct {
		name     string
		pods     []*corev1.Pod
		wantPods []string
	}{
		{
			name:     "empty list returns empty",
			pods:     []*corev1.Pod{},
			wantPods: nil,
		},
		{
			// Server ready, sidecar not ready: this is the definition of a sidecar-failed pod.
			name: "server ready and sidecar not ready is returned",
			pods: []*corev1.Pod{
				runningPod("sidecar-fail",
					serverContainer(true),
					sidecarContainer("monitor", false, true),
				),
			},
			wantPods: []string{"sidecar-fail"},
		},
		{
			// All containers ready: not a sidecar-failed pod.
			name: "fully ready pod is not returned",
			pods: []*corev1.Pod{
				runningPod("all-ready",
					serverContainer(true),
					sidecarContainer("monitor", true, false),
				),
			},
			wantPods: nil,
		},
		{
			// Server not ready: IsAerospikeServerRunning returns false, so this is
			// a server-failed pod, not a sidecar-failed pod.
			name: "server not ready pod is not returned",
			pods: []*corev1.Pod{
				runningPod("server-fail",
					serverContainer(false),
				),
			},
			wantPods: nil,
		},
		{
			// Terminating pods must always be excluded.
			name: "terminating pod is not returned",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "terminating",
						DeletionTimestamp: &now,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							serverContainer(true),
							sidecarContainer("monitor", false, true),
						},
					},
				},
			},
			wantPods: nil,
		},
		{
			name: "only sidecar-failed pods are returned from a mixed list",
			pods: []*corev1.Pod{
				runningPod("sidecar-fail",
					serverContainer(true),
					sidecarContainer("monitor", false, true),
				),
				runningPod("all-ready",
					serverContainer(true),
					sidecarContainer("monitor", true, false),
				),
				runningPod("server-fail",
					serverContainer(false),
				),
			},
			wantPods: []string{"sidecar-fail"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSidecarFailedPods(tt.pods)
			assertPodNames(t, "sidecar-failed", podNames(got), tt.wantPods)
		})
	}
}

// podNames extracts pod names from a slice for easier assertion output.
func podNames(pods []*corev1.Pod) []string {
	if len(pods) == 0 {
		return nil
	}

	names := make([]string, len(pods))
	for i, p := range pods {
		names[i] = p.Name
	}

	return names
}

// assertPodNames checks that got contains exactly the same names as want
// (order-independent).
func assertPodNames(t *testing.T, label string, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("%s: got %v (len %d), want %v (len %d)", label, got, len(got), want, len(want))
		return
	}

	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}

	for _, n := range got {
		if !wantSet[n] {
			t.Errorf("%s: unexpected pod %q in result %v (wanted %v)", label, n, got, want)
		}
	}
}
