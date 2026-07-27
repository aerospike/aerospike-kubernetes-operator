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
// shouldSetMigrateFillDelay
// ---------------------------------------------------------------------------

func TestShouldSetMigrateFillDelay(t *testing.T) {
	tests := []struct {
		delay          *int64
		restartTypeMap map[string]RestartType
		name           string
		pods           []*corev1.Pod
		want           bool
	}{
		{
			name:           "nil RestartMigrateFillDelay → never set MFD",
			delay:          nil,
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			want:           false,
		},
		{
			name:           "zero RestartMigrateFillDelay → never set MFD",
			delay:          ptrInt64(0),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			want:           false,
		},
		{
			name:           "nil restartTypeMap assumes pod restart is needed",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: nil,
			want:           true,
		},
		{
			name:           "at least one pod has podRestart → set MFD",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{makePod("pod-0")},
			restartTypeMap: map[string]RestartType{"pod-0": podRestart},
			want:           true,
		},
		{
			name:  "all pods are warm (quickRestart) only → do not set MFD",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": quickRestart,
			},
			want: false,
		},
		{
			name:  "mix: one quickRestart and one podRestart → set MFD",
			delay: ptrInt64(120),
			pods:  []*corev1.Pod{makePod("pod-0"), makePod("pod-1")},
			restartTypeMap: map[string]RestartType{
				"pod-0": quickRestart,
				"pod-1": podRestart,
			},
			want: true,
		},
		{
			name:           "no pods to restart and MFD is configured → false",
			delay:          ptrInt64(120),
			pods:           []*corev1.Pod{},
			restartTypeMap: map[string]RestartType{},
			want:           false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newTestAerospikeCluster("test-ns", "test-cluster")
			cluster.Spec.RestartMigrateFillDelay = tc.delay

			r := newTestReconciler(t, cluster, &interceptor.Funcs{})
			got := r.shouldSetMigrateFillDelay(tc.pods, tc.restartTypeMap)
			require.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// shouldSkipMFDUpdate
// ---------------------------------------------------------------------------

func TestShouldSkipMFDUpdate(t *testing.T) {
	tests := []struct {
		restartMFDDelay *int64
		name            string
		configMFD       int
		oldMFD          int
		want            bool
	}{
		{
			name:            "configMFD non-zero → never skip",
			configMFD:       60,
			oldMFD:          0,
			restartMFDDelay: nil,
			want:            false,
		},
		{
			name:            "oldMFD non-zero → never skip",
			configMFD:       0,
			oldMFD:          30,
			restartMFDDelay: nil,
			want:            false,
		},
		{
			name:            "both zero and no RestartMigrateFillDelay → skip",
			configMFD:       0,
			oldMFD:          0,
			restartMFDDelay: nil,
			want:            true,
		},
		{
			name:            "both zero and zero RestartMigrateFillDelay → skip",
			configMFD:       0,
			oldMFD:          0,
			restartMFDDelay: ptrInt64(0),
			want:            true,
		},
		{
			name:            "both zero but RestartMigrateFillDelay > 0 → do not skip (force-clear stale value)",
			configMFD:       0,
			oldMFD:          0,
			restartMFDDelay: ptrInt64(120),
			want:            false,
		},
		{
			name:            "configMFD and oldMFD both non-zero → never skip",
			configMFD:       60,
			oldMFD:          30,
			restartMFDDelay: nil,
			want:            false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newTestAerospikeCluster("test-ns", "test-cluster")
			cluster.Spec.RestartMigrateFillDelay = tc.restartMFDDelay

			// Populate a status rack so the old-MFD lookup can run.
			if tc.oldMFD != 0 {
				cluster.Status.RackConfig.Racks = []asdbv1.Rack{
					{
						ID: 1,
						AerospikeConfig: asdbv1.AerospikeConfigSpec{
							Value: map[string]interface{}{
								asdbv1.ConfKeyService: map[string]interface{}{
									"migrate-fill-delay": int64(tc.oldMFD),
								},
							},
						},
					},
				}
			}

			r := newTestReconciler(t, cluster, &interceptor.Funcs{})
			got := r.shouldSkipMFDUpdate(tc.configMFD, tc.oldMFD)
			require.Equal(t, tc.want, got)
		})
	}
}
