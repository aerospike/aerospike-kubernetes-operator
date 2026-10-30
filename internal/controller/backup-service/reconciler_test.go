package backupservice

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

func TestFindStaticSpecChange(t *testing.T) {
	t.Parallel()

	const status = `{"service":{"http":{"port":8081},"https":{"port":8443,"cert-file":"/c","key-file":"/k",` +
		`"key-file-password":"pass1"}},"backup-policies":{"p1":{"parallel":1}}}`

	tests := []struct {
		name        string
		spec        string
		wantRestart string
	}{
		{
			name: "dynamic field",
			spec: `{"service":{"http":{"port":8081},"https":{"port":8443,"cert-file":"/c","key-file":"/k",` +
				`"key-file-password":"pass1"}},"backup-policies":{"p1":{"parallel":2}}}`,
		},
		{
			name: "static field",
			spec: `{"service":{"http":{"port":8080},"https":{"port":8443,"cert-file":"/c","key-file":"/k",` +
				`"key-file-password":"pass1"}},"backup-policies":{"p1":{"parallel":1}}}`,
			wantRestart: "Port",
		},
		{
			name: "secret static field",
			spec: `{"service":{"http":{"port":8081},"https":{"port":8443,"cert-file":"/c","key-file":"/k",` +
				`"key-file-password":"pass2"}},"backup-policies":{"p1":{"parallel":1}}}`,
			wantRestart: "KeyFilePassword",
		},
		{
			name: "context-path",
			spec: `{"service":{"http":{"port":8081,"context-path":"/abs"},"https":{"port":8443,"cert-file":"/c",` +
				`"key-file":"/k","key-file-password":"pass1"}},"backup-policies":{"p1":{"parallel":1}}}`,
			wantRestart: "ContextPath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &SingleBackupServiceReconciler{aeroBackupService: &asdbv1beta1.AerospikeBackupService{
				Spec:   asdbv1beta1.AerospikeBackupServiceSpec{Config: runtime.RawExtension{Raw: []byte(tt.spec)}},
				Status: asdbv1beta1.AerospikeBackupServiceStatus{Config: runtime.RawExtension{Raw: []byte(status)}},
			}}

			staticChangeErr, err := r.findStaticSpecChange()
			require.NoError(t, err)

			if tt.wantRestart == "" {
				require.NoError(t, staticChangeErr)
				return
			}

			require.ErrorContains(t, staticChangeErr, tt.wantRestart)
		})
	}
}

// TestUpdateStatusKeepsNulls checks that the status config keeps the nulls of the spec config. A merge patch would
// drop them, and spec would never be equal to status again.
func TestUpdateStatusKeepsNulls(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, asdbv1beta1.AddToScheme(scheme))

	const config = `{"service":{"http":{"port":8081},"logger":null},` +
		`"backup-policies":{"test-policy":{"parallel":3,"retention":null}},` +
		`"storage":{"local":{"local-storage":{"path":"/tmp/localStorage"}}}}`

	nsNm := types.NamespacedName{Name: "abs1", Namespace: "aerospike"}
	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{Name: nsNm.Name, Namespace: nsNm.Namespace},
		Spec:       asdbv1beta1.AerospikeBackupServiceSpec{Config: runtime.RawExtension{Raw: []byte(config)}},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupService).
		WithStatusSubresource(backupService).Build()

	r := &SingleBackupServiceReconciler{
		Client:            k8sClient,
		Log:               logr.Discard(),
		aeroBackupService: backupService.DeepCopy(),
	}
	r.aeroBackupService.Status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted

	require.NoError(t, r.updateStatus(context.Background()))

	got := &asdbv1beta1.AerospikeBackupService{}
	require.NoError(t, k8sClient.Get(context.Background(), nsNm, got))
	require.Equal(t, string(got.Spec.Config.Raw), string(got.Status.Config.Raw))
}
