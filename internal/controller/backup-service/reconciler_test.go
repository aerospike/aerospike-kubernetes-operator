package backupservice

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

// absConfig returns a backup service config with an HTTP and an HTTPS listener.
func absConfig(httpPort int, keyFilePassword string, parallel int) []byte {
	return fmt.Appendf(nil, `{"service":{"http":{"port":%d},"https":{"port":8443,"cert-file":"/c","key-file":"/k",`+
		`"key-file-password":%q}},"backup-policies":{"p1":{"parallel":%d}},`+
		`"storage":{"s1":{"local-storage":{"path":"/tmp/backups"}}}}`, httpPort, keyFilePassword, parallel)
}

func TestFindStaticSecretChange(t *testing.T) {
	t.Parallel()

	status := absConfig(8081, "pass1", 1)

	tests := []struct {
		name        string
		wantRestart string
		spec        []byte
	}{
		// The backup service API shows the port, so the comparison of its running config with the ConfigMap
		// decides whether it is applied, for example by a Deployment rollout that finished after a failed reconcile.
		{name: "visible static field", spec: absConfig(8080, "pass1", 1)},
		{name: "secret static field", spec: absConfig(8081, "pass2", 1), wantRestart: "KeyFilePassword"},
		{name: "visible and secret static fields", spec: absConfig(8080, "pass2", 1)},
		{name: "dynamic field", spec: absConfig(8081, "pass1", 2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &SingleBackupServiceReconciler{aeroBackupService: &asdbv1beta1.AerospikeBackupService{
				Spec:   asdbv1beta1.AerospikeBackupServiceSpec{Config: runtime.RawExtension{Raw: tt.spec}},
				Status: asdbv1beta1.AerospikeBackupServiceStatus{Config: runtime.RawExtension{Raw: status}},
			}}

			staticChangeErr, err := r.findStaticSecretChange()
			require.NoError(t, err)

			if tt.wantRestart == "" {
				require.NoError(t, staticChangeErr)
				return
			}

			require.ErrorContains(t, staticChangeErr, tt.wantRestart)
		})
	}
}
