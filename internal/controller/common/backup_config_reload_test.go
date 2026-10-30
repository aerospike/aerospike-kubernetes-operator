package common

import (
	"context"
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

// fakeExec answers the hash file read with the successive outputs given; the last one repeats.
// No outputs means the pod has no hash file.
func fakeExec(outputs []string) PodExecFunc {
	hashFile := v1beta1.BackupServiceConfigMountPath + "/" + v1beta1.BackupServiceConfigHashKey
	calls := 0

	return func(_ types.NamespacedName, container string, cmd []string) (string, error) {
		if container != v1beta1.AerospikeBackupServiceKey || len(cmd) != 2 || cmd[0] != "cat" || cmd[1] != hashFile {
			return "", errors.New("unexpected exec")
		}

		if len(outputs) == 0 {
			return "", errors.New("cat: can't open file: No such file or directory")
		}

		idx := min(calls, len(outputs)-1)
		calls++

		return outputs[idx], nil
	}
}

func backupSvcPod(phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testBackupSvcName + "-0",
			Namespace: testNamespace,
			Labels:    utils.LabelsForAerospikeBackupService(testBackupSvcName),
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// waitForTestConfig runs waitForConfigInPods against a ConfigMap holding testConfig, with its hash unless noHash, and
// the given pod, whose hash file reads return outputs.
func waitForTestConfig(t *testing.T, outputs []string, pod *corev1.Pod, noHash bool) error {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	data := map[string]string{v1beta1.BackupServiceConfigYAML: testConfig}
	if !noHash {
		require.NoError(t, SetBackupSvcConfigData(data, testConfig))
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: testBackupSvcName, Namespace: testNamespace},
		Data:       data,
	}, pod).Build()

	return waitForConfigInPods(context.Background(), logr.Discard(), k8sClient, fakeExec(outputs),
		&v1beta1.BackupService{Name: testBackupSvcName, Namespace: testNamespace}, time.Millisecond,
		200*time.Millisecond)
}

func TestWaitForConfigInPods(t *testing.T) {
	t.Parallel()

	want, err := utils.GetHash(testConfig)
	require.NoError(t, err)

	old, err := utils.GetHash("old config")
	require.NoError(t, err)

	tests := []struct {
		pod     *corev1.Pod
		name    string
		wantErr string
		outputs []string
		noHash  bool
	}{
		{
			name:    "pod already has the config, with the TTY line ending",
			pod:     backupSvcPod(corev1.PodRunning),
			outputs: []string{want + "\r\n"},
		},
		{
			name:    "pod gets the config while waiting",
			pod:     backupSvcPod(corev1.PodRunning),
			outputs: []string{old, old, want},
		},
		{
			name:    "pod keeps the old config",
			pod:     backupSvcPod(corev1.PodRunning),
			outputs: []string{old},
			wantErr: "has config hash",
		},
		{
			name:    "hash file missing, as in a pod started before the key existed",
			pod:     backupSvcPod(corev1.PodRunning),
			wantErr: "No such file",
		},
		{
			name: "crashing pod fails at once",
			pod: func() *corev1.Pod {
				pod := backupSvcPod(corev1.PodRunning)
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
					Name: v1beta1.AerospikeBackupServiceKey,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				}}

				return pod
			}(),
			outputs: []string{want},
			wantErr: "CrashLoopBackOff",
		},
		{
			name:    "pod not running is not checked",
			pod:     backupSvcPod(corev1.PodPending),
			outputs: []string{old},
			wantErr: "no running backup service Pod",
		},
		{
			name: "terminating pod is not checked",
			pod: func() *corev1.Pod {
				pod := backupSvcPod(corev1.PodRunning)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				pod.Finalizers = []string{"test"}

				return pod
			}(),
			outputs: []string{old},
			wantErr: "no running backup service Pod",
		},
		{
			name:    "ConfigMap written without its hash fails at once",
			pod:     backupSvcPod(corev1.PodRunning),
			noHash:  true,
			wantErr: "has no " + v1beta1.BackupServiceConfigHashKey + " key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := waitForTestConfig(t, tt.outputs, tt.pod, tt.noHash)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, "wait for backup service Pods to get the latest ConfigMap")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
