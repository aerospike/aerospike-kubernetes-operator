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
		// wantDelay: -1 = MFD management disabled; 0 = zero only (no raise); >0 = override value.
		wantDelay int
	}{
		{
			name:           "nil RestartStrategy, no deleteLocalStorageOnRestart → disabled",
			delay:          nil,
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      -1,
		},
		{
			name:           "zero OverrideMigrateFillDelay → disabled",
			delay:          ptrInt64(0),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      -1,
		},
		{
			name:           "nil restartTypeMap with OverrideMigrateFillDelay assumes pod restart",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: nil,
			wantDelay:      120,
		},
		{
			name:           "at least one pod has podRestart with OverrideMigrateFillDelay → override",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			wantDelay:      120,
		},
		{
			name:  "all pods are warm (quickRestart) only → disabled",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": quickRestart,
			},
			wantDelay: -1,
		},
		{
			name:  "mix: one quickRestart and one podRestart with OverrideMigrateFillDelay → override",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": podRestart,
			},
			wantDelay: 120,
		},
		{
			name:           "no pods to restart and MFD is configured → disabled",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{},
			restartTypeMap: map[string]RestartType{},
			wantDelay:      -1,
		},
		// DeleteLocalStorageOnRestart (original master behavior — aerospike.conf MFD not set → 0)
		{
			name:                        "deleteLocalStorageOnRestart + local storage volume → aerospike.conf delay (0)",
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
		},
		{
			name:                        "deleteLocalStorageOnRestart but no local storage volumes → disabled",
			delay:                       nil,
			deleteLocalStorageOnRestart: &trueVal,
			localStorageClasses:         []string{localStorageClass},
			localStorageVolumes:         []asdbv1.VolumeSpec{},
			pods:                        []*corev1.Pod{makePod("pod-0")},
			restartTypeMap:              map[string]RestartType{"pod-0": podRestart},
			wantDelay:                   -1,
		},
		{
			name:                        "deleteLocalStorageOnRestart + local storage + only warm restart → disabled",
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
			wantDelay:      -1,
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
			got, err := r.mfdDelayForRestart(rackState, tc.pods, tc.restartTypeMap)
			require.NoError(t, err)
			require.Equal(t, tc.wantDelay, got)
		})
	}
}
