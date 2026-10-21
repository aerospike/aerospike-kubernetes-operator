package common

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	require.Equal(t, BackupSvcConfigHash(testConfig), data[v1beta1.BackupServiceConfigHashKey])
	require.Len(t, data[v1beta1.BackupServiceConfigHashKey], 64)

	// A change to a secret alone changes the hash, which is what lets the pods be checked for it.
	withPassword := "aerospike-clusters:\n  c1:\n    credentials:\n      password: admin123\n" //nolint:gosec // test data
	rotated := "aerospike-clusters:\n  c1:\n    credentials:\n      password: admin456\n"
	require.NotEqual(t, BackupSvcConfigHash(withPassword), BackupSvcConfigHash(rotated))
	require.Equal(t, BackupSvcConfigHash(withPassword), BackupSvcConfigHash(withPassword))
}

// fakeExec returns the hash file content per pod, as the pods would show it.
type fakeExec struct {
	files map[string][]string // pod name -> successive outputs; the last one repeats
	calls map[string]int
	mu    sync.Mutex
}

func (f *fakeExec) exec(pod types.NamespacedName, container string, cmd []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if container != v1beta1.AerospikeBackupServiceKey ||
		len(cmd) != 2 || cmd[0] != "cat" ||
		cmd[1] != v1beta1.BackupServiceConfigMountPath+"/"+v1beta1.BackupServiceConfigHashKey {
		return "", errors.New("unexpected exec")
	}

	outputs, ok := f.files[pod.Name]
	if !ok {
		return "", errors.New("cat: can't open file: No such file or directory")
	}

	idx := f.calls[pod.Name]
	if idx >= len(outputs) {
		idx = len(outputs) - 1
	}

	f.calls[pod.Name]++

	return outputs[idx], nil
}

func newFakeExec(files map[string][]string) *fakeExec {
	return &fakeExec{files: files, calls: map[string]int{}}
}

func backupSvcPod(name string, phase corev1.PodPhase, terminating bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    utils.LabelsForAerospikeBackupService(testBackupSvcName),
		},
		Status: corev1.PodStatus{Phase: phase},
	}

	if terminating {
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		pod.Finalizers = []string{"test"}
	}

	return pod
}

func newReloadTestClient(t *testing.T, pods ...*corev1.Pod) client.Client {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	data := map[string]string{}
	SetBackupSvcConfigData(data, testConfig)

	objs := make([]client.Object, 0, 1+len(pods))
	objs = append(objs, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: testBackupSvcName, Namespace: testNamespace},
		Data:       data,
	})

	for _, pod := range pods {
		objs = append(objs, pod)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func waitForTestConfig(k8sClient client.Client, exec PodExecFunc, timeout time.Duration) error {
	return waitForConfigInPods(context.Background(), logr.Discard(), k8sClient, exec,
		&v1beta1.BackupService{Name: testBackupSvcName, Namespace: testNamespace},
		time.Millisecond, timeout)
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
			pods:  []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning, false)},
			files: map[string][]string{"abs-0": {want + "\r\n"}},
		},
		{
			name:  "pod gets the config while waiting",
			pods:  []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning, false)},
			files: map[string][]string{"abs-0": {old, old, want}},
		},
		{
			name: "pod not running and terminating pod are not checked",
			pods: []*corev1.Pod{
				backupSvcPod("abs-0", corev1.PodRunning, false),
				backupSvcPod("abs-1", corev1.PodPending, false),
				backupSvcPod("abs-2", corev1.PodRunning, true),
			},
			files: map[string][]string{"abs-0": {want}},
		},
		{
			name:    "pod keeps the old config",
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning, false)},
			files:   map[string][]string{"abs-0": {old}},
			wantErr: "has config hash",
		},
		{
			name:    "hash file missing, as in a pod started before the key existed",
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodRunning, false)},
			files:   map[string][]string{},
			wantErr: "No such file",
		},
		{
			name: "every running pod must have the config",
			pods: []*corev1.Pod{
				backupSvcPod("abs-0", corev1.PodRunning, false),
				backupSvcPod("abs-1", corev1.PodRunning, false),
			},
			files:   map[string][]string{"abs-0": {want}, "abs-1": {old}},
			wantErr: "abs-1",
		},
		{
			name: "crashing pod fails at once",
			pods: []*corev1.Pod{func() *corev1.Pod {
				pod := backupSvcPod("abs-0", corev1.PodRunning, false)
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
			pods:    []*corev1.Pod{backupSvcPod("abs-0", corev1.PodPending, false)},
			files:   map[string][]string{},
			wantErr: "no running backup service Pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			k8sClient := newReloadTestClient(t, tt.pods...)
			exec := newFakeExec(tt.files)

			err := waitForTestConfig(k8sClient, exec.exec, 200*time.Millisecond)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, "wait for backup service Pods to get the latest ConfigMap")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestWaitForConfigInPods_FollowsNewerConfigMap(t *testing.T) {
	t.Parallel()

	k8sClient := newReloadTestClient(t, backupSvcPod("abs-0", corev1.PodRunning, false))

	// The ConfigMap is written again before the pod gets the first write: the wait must end on the newer config.
	newer := testConfig + "backup-policies:\n  p1:\n    parallel: 2\n"

	cm := &corev1.ConfigMap{}
	require.NoError(t, k8sClient.Get(context.Background(),
		types.NamespacedName{Name: testBackupSvcName, Namespace: testNamespace}, cm))
	SetBackupSvcConfigData(cm.Data, newer)
	require.NoError(t, k8sClient.Update(context.Background(), cm))

	exec := newFakeExec(map[string][]string{"abs-0": {BackupSvcConfigHash(testConfig)}})
	require.Error(t, waitForTestConfig(k8sClient, exec.exec, 50*time.Millisecond))

	exec = newFakeExec(map[string][]string{"abs-0": {BackupSvcConfigHash(newer)}})
	require.NoError(t, waitForTestConfig(k8sClient, exec.exec, 50*time.Millisecond))
}
