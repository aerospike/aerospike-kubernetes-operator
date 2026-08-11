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
