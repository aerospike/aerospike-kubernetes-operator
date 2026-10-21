package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

const (
	testBackupSvcName = "backup-service"
	testNamespace     = "aerospike"
	testConfig        = "service:\n  http:\n    port: 8081\n"
)

func TestSetBackupSvcConfigData(t *testing.T) {
	t.Parallel()

	data := map[string]string{}
	SetBackupSvcConfigData(data, testConfig)

	require.Equal(t, testConfig, data[v1beta1.BackupServiceConfigYAML])
	sum := sha256.Sum256([]byte(testConfig))
	require.Equal(t, hex.EncodeToString(sum[:]), data[v1beta1.BackupServiceConfigHashKey])
}

// fakeExec answers the hash file read with the successive outputs given per pod; the last one repeats.
// A pod without outputs has no hash file.
func fakeExec(files map[string][]string) PodExecFunc {
	hashFile := v1beta1.BackupServiceConfigMountPath + "/" + v1beta1.BackupServiceConfigHashKey
	calls := map[string]int{}

	return func(pod types.NamespacedName, container string, cmd []string) (string, error) {
		if container != v1beta1.AerospikeBackupServiceKey || len(cmd) != 2 || cmd[0] != "cat" || cmd[1] != hashFile {
			return "", errors.New("unexpected exec")
		}

		outputs, ok := files[pod.Name]
		if !ok {
			return "", errors.New("cat: can't open file: No such file or directory")
		}

		idx := min(calls[pod.Name], len(outputs)-1)
		calls[pod.Name]++

		return outputs[idx], nil
	}
}

func backupSvcPod(name string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    utils.LabelsForAerospikeBackupService(testBackupSvcName),
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// waitForTestConfig runs waitForConfigInPods against a ConfigMap holding testConfig and the given pods.
func waitForTestConfig(t *testing.T, files map[string][]string, pods ...*corev1.Pod) error {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	data := map[string]string{}
	SetBackupSvcConfigData(data, testConfig)

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: testBackupSvcName, Namespace: testNamespace},
		Data:       data,
	})

	for _, pod := range pods {
		builder = builder.WithObjects(pod)
	}

	return waitForConfigInPods(context.Background(), logr.Discard(), builder.Build(), fakeExec(files),
		&v1beta1.BackupService{Name: testBackupSvcName, Namespace: testNamespace}, time.Millisecond,
		200*time.Millisecond)
}

func TestWaitForConfigInPods(t *testing.T) {
	t.Parallel()

	want := BackupSvcConfigHash(testConfig)
	old := BackupSvcConfigHash("old config")

	tests := []struct {
		files   map[string][]string
		name    string
		wantErr string
		pods    []*corev1.Pod
	}{
		{
			name:  "pod already has the config, with the TTY line ending",
			pods:  []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning)},
			files: map[string][]string{"abs-0": {want + "\r\n"}},
		},
		{
			name:  "pod gets the config while waiting",
			pods:  []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning)},
			files: map[string][]string{"abs-0": {old, old, want}},
		},
		{
			name: "pod not running and terminating pod are not checked",
			pods: []*corev1.Pod{
				backupSvcPod("abs-0", corev1.PodRunning),
				backupSvcPod("abs-1", corev1.PodPending),
				func() *corev1.Pod {
					pod := backupSvcPod("abs-2", corev1.PodRunning)
					pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					pod.Finalizers = []string{"test"}

					return pod
				}(),
			},
			files: map[string][]string{"abs-0": {want}},
		},
		{
			name:    "pod keeps the old config",
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning)},
			files:   map[string][]string{"abs-0": {old}},
			wantErr: "has config hash",
		},
		{
			name:    "hash file missing, as in a pod started before the key existed",
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning)},
			files:   map[string][]string{},
			wantErr: "No such file",
		},
		{
			name: "every running pod must have the config",
			pods: []*corev1.Pod{
				backupSvcPod("abs-0", corev1.PodRunning),
				backupSvcPod("abs-1", corev1.PodRunning),
			},
			files:   map[string][]string{"abs-0": {want}, "abs-1": {old}},
			wantErr: "abs-1",
		},
		{
			name: "crashing pod fails at once",
			pods: []*corev1.Pod{func() *corev1.Pod {
				pod := backupSvcPod("abs-0", corev1.PodRunning)
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
					Name: v1beta1.AerospikeBackupServiceKey,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				}}

				return pod
			}()},
			files:   map[string][]string{"abs-0": {want}},
			wantErr: "CrashLoopBackOff",
		},
		{
			name:    "no running pod",
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodPending)},
			files:   map[string][]string{},
			wantErr: "no running backup service Pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := waitForTestConfig(t, tt.files, tt.pods...)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, "wait for backup service Pods to get the latest ConfigMap")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
