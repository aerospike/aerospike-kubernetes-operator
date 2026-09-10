package validation

import (
	"strings"
	"testing"
)

// networkConfWithTLS returns a network config declaring a single TLS
// configuration named "dc1-tls", used as the xdr.dcs tls-name reference target.
func networkConfWithTLS() map[string]any {
	return map[string]any{
		"tls": []any{
			map[string]any{"name": "dc1-tls"},
		},
	}
}

func TestValidateXdrConfig(t *testing.T) {
	tests := []struct {
		config     map[string]any
		name       string
		wantErrMsg string
		wantErr    bool
	}{
		{
			name:    "no xdr section",
			config:  map[string]any{},
			wantErr: false,
		},
		{
			name: "tls-name references a configured network.tls entry",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":     "dc1",
							"tls-name": "dc1-tls",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "tls-name does not reference a configured network.tls entry",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":     "dc1",
							"tls-name": "unknown-tls",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "connector true with auth-password-file set",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"connector":          true,
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "connector true without auth-password-file",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"connector": true,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "connector false (default) with auth-password-file set",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-user":          "admin",
							"auth-mode":          "internal",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "connector true with auth-mode set to internal",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"connector": true,
							"auth-mode": "internal",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "connector true with auth-mode set to none",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"connector": true,
							"auth-mode": "none",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "connector false with auth-mode set to internal but no auth-user",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "internal",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "connector true with auth-user set",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"connector": true,
							"auth-user": "admin",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-user set without auth-mode",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-user":          "admin",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "auth-mode is required when auth-user is set",
		},
		{
			name: "auth-user set with auth-mode none",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-user":          "admin",
							"auth-mode":          "none",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "auth-mode must not be 'none' when auth-user is set",
		},
		{
			name: "dc entry missing name field is skipped",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"connector": true,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "dc entry missing name field is skipped even with other issues",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"tls-name": "unknown-tls",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "auth-user set without auth-password-file",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-user": "admin",
							"auth-mode": "internal",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-password-file set without auth-user",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-mode":          "internal",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-mode external without auth-user",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "external",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-mode external-insecure without auth-user",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "external-insecure",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-mode pki without auth-user is allowed",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "pki",
							"tls-name":  "dc1-tls",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "auth-mode pki with auth-user is rejected",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "pki",
							"auth-user": "admin",
							"tls-name":  "dc1-tls",
						},
					},
				},
			},
			wantErr:    true,
			wantErrMsg: "auth-user is not allowed when auth-mode is 'pki'",
		},
		{
			name: "auth-mode pki without tls-name",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						map[string]any{
							"name":      "dc1",
							"auth-mode": "pki",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-mode external with auth-user but without tls-name",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-mode":          "external",
							"auth-user":          "admin",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "auth-mode external with auth-user and tls-name",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-mode":          "external",
							"auth-user":          "admin",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
							"tls-name":           "dc1-tls",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "auth-password-file set without auth-user and without auth-mode",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{
						//nolint:gosec // G101 test config path, not real credentials
						map[string]any{
							"name":               "dc1",
							"auth-password-file": "/etc/aerospike/secret/password.txt",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "xdr.dcs not a valid list",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": "not-a-list",
				},
			},
			wantErr: true,
		},
		{
			name: "xdr.dcs entry not a valid map",
			config: map[string]any{
				"network": networkConfWithTLS(),
				"xdr": map[string]any{
					"dcs": []any{"not-a-map"},
				},
			},
			wantErr: true,
		},
		{
			name:    "xdr not a valid map",
			config:  map[string]any{"xdr": "not-a-map"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateXDRConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateXdrConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErrMsg != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrMsg)) {
				t.Errorf("validateXdrConfig() error = %v, want message containing %q", err, tt.wantErrMsg)
			}
		})
	}
}
