package backup

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const (
	testNamespace = "aerospike"
	// testBackupConfig is the spec config of the test AerospikeBackup "bk1".
	testBackupConfig = `{"aerospike-cluster":{"aerospike-bk1-cluster":{"seed-nodes":[{"host-name":"aerocluster",` +
		`"port":3000}]}},"backup-routines":{"aerospike-bk1-r1":{"backup-policy":"test-policy",` +
		`"interval-cron":"@daily","source-cluster":"aerospike-bk1-cluster","storage":"local"}}}`
)

// newTestReconciler returns a reconciler for AerospikeBackup "bk1" of AerospikeBackupService "abs1", with
// statusConfig as its status config, and a fake client holding the backup and objs.
func newTestReconciler(t *testing.T, statusConfig string, objs ...client.Object) (*SingleBackupReconciler,
	client.Client) {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, asdbv1beta1.AddToScheme(scheme))

	backupSvc := asdbv1beta1.BackupService{Name: "abs1", Namespace: testNamespace}
	backup := &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "bk1", Namespace: testNamespace},
		Spec: asdbv1beta1.AerospikeBackupSpec{
			BackupService: backupSvc,
			Config:        k8sruntime.RawExtension{Raw: []byte(testBackupConfig)},
		},
		Status: asdbv1beta1.AerospikeBackupStatus{
			BackupService: backupSvc,
			Config:        k8sruntime.RawExtension{Raw: []byte(statusConfig)},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, backup)...).
		WithStatusSubresource(backup).Build()

	return &SingleBackupReconciler{
		Client:     k8sClient,
		Recorder:   record.NewFakeRecorder(10),
		Log:        logr.Discard(),
		aeroBackup: backup.DeepCopy(),
	}, k8sClient
}

func TestReconcileConfigMapChanged(t *testing.T) {
	t.Parallel()

	// The backup service config as the backup service controller writes it when it creates the ConfigMap.
	const serviceOnly = `{"service":{"http":{"port":8081}},"backup-policies":{"test-policy":{"parallel":3}}}`

	tests := []struct {
		name         string
		configMapCfg string
		wantChanged  bool
	}{
		{
			name:         "recreated backup service ConfigMap without the backup routines",
			configMapCfg: serviceOnly,
			wantChanged:  true,
		},
		{
			name: "ConfigMap already has the backup routines, in another format",
			configMapCfg: serviceOnly[:len(serviceOnly)-1] + `,"aerospike-clusters":{"aerospike-bk1-cluster":` +
				`{"seed-nodes":[{"host-name":"aerocluster","port":3000}]}},"backup-routines":{"aerospike-bk1-r1":` +
				`{"backup-policy":"test-policy","interval-cron":"@daily","source-cluster":"aerospike-bk1-cluster",` +
				`"storage":"local"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmNsNm := types.NamespacedName{Name: "abs1", Namespace: testNamespace}
			r, k8sClient := newTestReconciler(t, testBackupConfig, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmNsNm.Name, Namespace: cmNsNm.Namespace},
				Data:       map[string]string{asdbv1beta1.BackupServiceConfigYAML: tt.configMapCfg},
			})

			changed, err := r.reconcileConfigMap(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)

			// Both ConfigMaps start without a hash, so the first call writes the config and its hash either way.
			cm := &corev1.ConfigMap{}
			require.NoError(t, k8sClient.Get(context.Background(), cmNsNm, cm))
			require.Contains(t, cm.Data[asdbv1beta1.BackupServiceConfigYAML], "aerospike-bk1-r1")
			require.NotEmpty(t, cm.Data[asdbv1beta1.BackupServiceConfigHashKey])

			// The ConfigMap now has this backup's entries and a hash, so the second call skips the update. The fake
			// client changes resourceVersion on every update, so an unchanged one shows that no update was sent.
			changed, err = r.reconcileConfigMap(context.Background())
			require.NoError(t, err)
			require.False(t, changed)

			after := &corev1.ConfigMap{}
			require.NoError(t, k8sClient.Get(context.Background(), cmNsNm, after))
			require.Equal(t, cm.ResourceVersion, after.ResourceVersion)
		})
	}
}

func TestReconcileScheduledBackupMarksFailedReload(t *testing.T) {
	t.Parallel()

	const oldConfig = `{"backup-routines":{}}`

	tests := []struct {
		name             string
		statusConfig     string
		wantStatusConfig string
	}{
		{
			name:             "spec applied, ConfigMap changed: the reload is owed only by the ConfigMap",
			statusConfig:     testBackupConfig,
			wantStatusConfig: "{}",
		},
		{
			name:             "spec not applied: status already differs from spec",
			statusConfig:     oldConfig,
			wantStatusConfig: oldConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No AerospikeBackupService exists, so the reload fails before calling the backup service.
			r, k8sClient := newTestReconciler(t, tt.statusConfig)

			require.Error(t, r.reconcileScheduledBackup(context.Background(), true))

			stored := &asdbv1beta1.AerospikeBackup{}
			require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: "bk1",
				Namespace: testNamespace}, stored))
			require.JSONEq(t, tt.wantStatusConfig, string(stored.Status.Config.Raw))
		})
	}
}
