package backupservice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

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
