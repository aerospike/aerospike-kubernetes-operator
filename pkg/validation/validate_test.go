package validation

import (
	"errors"
	"strings"
	"testing"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestValidateAdvertiseIPv6(t *testing.T) {
	testCases := []struct {
		// advertiseIPv6 is the raw aerospikeConfig.service value; nil means the key is absent.
		advertiseIPv6 interface{}
		// ipv6ProbeErr is the reason node IPv6 capability could not be determined, if any.
		ipv6ProbeErr error
		name         string
		errSubstring string
		ipv6Capable  bool
	}{
		{
			name:          "rejected when enabled and the cluster has no IPv6-capable node",
			advertiseIPv6: true,
			ipv6Capable:   false,
			errSubstring:  "no Kubernetes node reports an IPv6 InternalIP address",
		},
		{
			name:          "rejected with the probe failure when enabled and capability could not be determined",
			advertiseIPv6: true,
			ipv6Capable:   false,
			ipv6ProbeErr:  errors.New("listing nodes: connection refused"),
			errSubstring:  "node IPv6 capability could not be determined: listing nodes: connection refused",
		},
		{
			name:          "allowed when disabled even though the probe failed",
			advertiseIPv6: false,
			ipv6Capable:   false,
			ipv6ProbeErr:  errors.New("listing nodes: connection refused"),
		},
		{
			name:          "allowed when absent even though the probe failed",
			advertiseIPv6: nil,
			ipv6Capable:   false,
			ipv6ProbeErr:  errors.New("listing nodes: connection refused"),
		},
		{
			name:          "allowed when enabled and the cluster has an IPv6-capable node",
			advertiseIPv6: true,
			ipv6Capable:   true,
		},
		{
			name:          "allowed when disabled on a cluster with no IPv6-capable node",
			advertiseIPv6: false,
			ipv6Capable:   false,
		},
		{
			name:          "allowed when disabled on an IPv6-capable cluster",
			advertiseIPv6: false,
			ipv6Capable:   true,
		},
		{
			name:          "allowed when absent on a cluster with no IPv6-capable node",
			advertiseIPv6: nil,
			ipv6Capable:   false,
		},
		{
			name:          "allowed when absent on an IPv6-capable cluster",
			advertiseIPv6: nil,
			ipv6Capable:   true,
		},
		{
			name:          "rejected as invalid config when a string",
			advertiseIPv6: "true",
			ipv6Capable:   true,
			errSubstring:  "advertise-ipv6 must be a boolean, got string (true)",
		},
		{
			name:          "rejected as invalid config when a number",
			advertiseIPv6: 1,
			ipv6Capable:   false,
			errSubstring:  "advertise-ipv6 must be a boolean, got int (1)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			serviceConf := map[string]interface{}{"cluster-name": "test-cluster"}
			if testCase.advertiseIPv6 != nil {
				serviceConf[confKeyAdvertiseIPv6] = testCase.advertiseIPv6
			}

			err := validateAdvertiseIPv6(serviceConf, testCase.ipv6Capable, testCase.ipv6ProbeErr)

			if testCase.errSubstring == "" {
				if err != nil {
					t.Fatalf("expected the config to be accepted, got error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", testCase.errSubstring)
			}

			if !strings.Contains(err.Error(), testCase.errSubstring) {
				t.Fatalf("expected an error containing %q, got: %v", testCase.errSubstring, err)
			}
		})
	}
}

// TestValidateAerospikeConfigRejectsNilConfig covers the early return that the
// advertise-ipv6 guard sits behind, and pins the ipv6Capable and ipv6ProbeErr
// parameters into the exported signature.
func TestValidateAerospikeConfigRejectsNilConfig(t *testing.T) {
	err := ValidateAerospikeConfig(logf.Log, "8.1.2.0", nil, 1, true, nil)
	if err == nil {
		t.Fatal("expected a nil aerospikeConfig to be rejected")
	}

	if !strings.Contains(err.Error(), "aerospikeConfig cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}
