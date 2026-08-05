package restore

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

func TestStatusToPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   asdbv1beta1.AerospikeRestorePhase
	}{
		// ABS >= 3.6.1 canonical values
		{name: "success", status: "success", want: asdbv1beta1.AerospikeRestoreCompleted},
		{name: "running", status: "running", want: asdbv1beta1.AerospikeRestoreInProgress},
		{name: "failure", status: "failure", want: asdbv1beta1.AerospikeRestoreFailed},
		{name: "canceled", status: "canceled", want: asdbv1beta1.AerospikeRestoreFailed},
		// Legacy ABS values
		{name: "Done", status: "Done", want: asdbv1beta1.AerospikeRestoreCompleted},
		{name: "Running", status: "Running", want: asdbv1beta1.AerospikeRestoreInProgress},
		{name: "Failed", status: "Failed", want: asdbv1beta1.AerospikeRestoreFailed},
		{name: "unknown", status: "unknown", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, statusToPhase(logr.Discard(), tt.status))
		})
	}
}
