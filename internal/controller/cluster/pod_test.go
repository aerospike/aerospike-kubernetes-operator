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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// ptrInt64 returns a pointer to v.
func ptrInt64(v int64) *int64 { return &v }

// makePod creates a minimal Pod with the given name for use in restart-type maps.
func makePod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// ---------------------------------------------------------------------------
// mfdDelayForRestart
// ---------------------------------------------------------------------------

func TestMFDDelayForRestart(t *testing.T) {
	trueVal := true
	localStorageClass := "local-ssd"

	tests := []struct {
		delay                       *int64
		restartTypeMap              map[string]RestartType
		name                        string
		pods                        []*corev1.Pod
		deleteLocalStorageOnRestart *bool
		localStorageClasses         []string
		localStorageVolumes         []asdbv1.VolumeSpec
		// wantDelay: configMFD value (0 if unset) when pod restarts are needed; 0 when no pod
		// restart is needed (warm-only / empty batch). >0 = override raise value.
		// waitForMultipleNodesSafeStopReady raises to this value before quiesce (no-op via guard
		// when already there); it is not a signal to skip MFD management.
		wantDelay int
		// wantDrain: true = MFD was transiently raised (override / DeleteLocalStorageOnRestart);
		//            zero it before the stability check. false = no transient raise; skip drain.
		wantDrain bool
	}{
		{
			name:           "nil RestartStrategy, no deleteLocalStorageOnRestart → configMFD (0), no drain",
			delay:          nil,
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      0,
			wantDrain:      false,
		},
		{
			name:           "zero OverrideMigrateFillDelay → configMFD (0), no drain",
			delay:          ptrInt64(0),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      0,
			wantDrain:      false,
		},
		{
			name:           "nil restartTypeMap with OverrideMigrateFillDelay assumes pod restart",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: nil,
			wantDelay:      120,
			wantDrain:      true,
		},
		{
			name:           "at least one pod has podRestart with OverrideMigrateFillDelay → override, drain",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      120,
			wantDrain:      true,
		},
		{
			name:  "all pods are warm (quickRestart) only → no pod restart needed",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": quickRestart,
			},
			wantDelay: 0,
			wantDrain: false,
		},
		{
			name:  "mix: one quickRestart and one podRestart with OverrideMigrateFillDelay → override, drain",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": podRestart,
			},
			wantDelay: 120,
			wantDrain: true,
		},
		{
			name:           "no pods to restart → no pod restart needed",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{},
			restartTypeMap: map[string]RestartType{},
			wantDelay:      0,
			wantDrain:      false,
		},
		// DeleteLocalStorageOnRestart (original master behavior — aerospike.conf MFD not set → 0)
		{
			name:                        "deleteLocalStorageOnRestart + local storage volume → aerospike.conf delay (0), drain",
			delay:                       nil,
			deleteLocalStorageOnRestart: &trueVal,
			localStorageClasses:         []string{localStorageClass},
			localStorageVolumes: []asdbv1.VolumeSpec{
				{
					Name: "data",
					Source: asdbv1.VolumeSource{
						PersistentVolume: &asdbv1.PersistentVolumeSpec{StorageClass: localStorageClass},
					},
				},
			},
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      0,
			wantDrain:      true,
		},
		{
			name:                        "deleteLocalStorageOnRestart but no local storage volumes → configMFD (0), no drain",
			delay:                       nil,
			deleteLocalStorageOnRestart: &trueVal,
			localStorageClasses:         []string{localStorageClass},
			localStorageVolumes:         []asdbv1.VolumeSpec{},
			pods:                        []*corev1.Pod{makePod("pod-0")},
			restartTypeMap:              map[string]RestartType{"pod-0": podRestart},
			wantDelay:                   0,
			wantDrain:                   false,
		},
		{
			name: "deleteLocalStorageOnRestart + local storage + " +
				"only warm restart → no pod restart needed",
			delay:                       nil,
			deleteLocalStorageOnRestart: &trueVal,
			localStorageClasses:         []string{localStorageClass},
			localStorageVolumes: []asdbv1.VolumeSpec{
				{
					Name: "data",
					Source: asdbv1.VolumeSource{
						PersistentVolume: &asdbv1.PersistentVolumeSpec{StorageClass: localStorageClass},
					},
				},
			},
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": quickRestart},
			wantDelay:      0,
			wantDrain:      false,
		},
		{
			name: "OverrideMigrateFillDelay takes precedence when " +
				"deleteLocalStorageOnRestart is also set",
			delay:                       ptrInt64(120),
			deleteLocalStorageOnRestart: &trueVal,
			localStorageClasses:         []string{localStorageClass},
			localStorageVolumes: []asdbv1.VolumeSpec{
				{
					Name: "data",
					Source: asdbv1.VolumeSource{
						PersistentVolume: &asdbv1.PersistentVolumeSpec{StorageClass: localStorageClass},
					},
				},
			},
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      120,
			wantDrain:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newTestAerospikeCluster("test-ns", "test-cluster")
			if tc.delay != nil {
				cluster.Spec.RestartStrategy = &asdbv1.RestartStrategy{OverrideMigrateFillDelay: tc.delay}
			}

			rackState := newTestRackState(cluster)
			rackState.Rack.Storage.DeleteLocalStorageOnRestart = tc.deleteLocalStorageOnRestart
			rackState.Rack.Storage.LocalStorageClasses = tc.localStorageClasses
			rackState.Rack.Storage.Volumes = tc.localStorageVolumes

			r := newTestReconciler(t, cluster, &interceptor.Funcs{})
			gotDelay, gotDrain, err := r.mfdDelayForRestart(rackState, tc.pods, tc.restartTypeMap)
			require.NoError(t, err)
			require.Equal(t, tc.wantDelay, gotDelay, "delay mismatch")
			require.Equal(t, tc.wantDrain, gotDrain, "drainBeforeStability mismatch")
		})
	}
}
=======
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestGetServerFailedAndActivePods(t *testing.T) {
	serverFailedPod := runningPod("server-fail", serverCrashLoopContainer())
	serverFailedRecentPod := recentPod("server-fail-recent", serverCrashLoopContainer())
	healthyPod := runningPod("healthy",
		serverContainer(true),
		sidecarContainer("monitor", true, false),
	)
	sidecarNotReadyPod := runningPod("sidecar-fail",
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
			// Sidecar-not-ready pod must always be classified as active regardless of
			// withGracePeriod, because CheckServerFailedWithGrace ignores sidecars.
			name:       "sidecar-not-ready pod is always active",
			pods:       []*corev1.Pod{sidecarNotReadyPod},
			wantActive: []string{"sidecar-fail"},
		},
		{
			name:       "mixed: server-failed, sidecar-not-ready and healthy",
			pods:       []*corev1.Pod{serverFailedPod, sidecarNotReadyPod, healthyPod},
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

func TestGetSidecarNotReadyPods(t *testing.T) {
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
			// Server ready, sidecar not ready: this is the definition of a sidecar-not-ready pod.
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
			// Server not ready: IsAerospikeServerReady returns false, so this is
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
			got := getSidecarNotReadyPods(tt.pods)
			assertPodNames(t, "sidecar-not-ready", podNames(got), tt.wantPods)
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

	wantSet := sets.New(want...)

	for _, n := range got {
		if !wantSet.Has(n) {
			t.Errorf("%s: unexpected pod %q in result %v (wanted %v)", label, n, got, want)
		}
	}
}
