/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

func xdrConfigSpec(dcs []interface{}) asdbv1.AerospikeConfigSpec {
	return asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			asdbv1.ConfKeyXdr: map[string]interface{}{
				asdbv1.ConfKeyXdrDCs: dcs,
			},
		},
	}
}

func TestGetXDRAuthPasswordFilePaths(t *testing.T) {
	tests := []struct {
		name     string
		spec     asdbv1.AerospikeConfigSpec
		expected []string
	}{
		{
			name:     "no xdr config",
			spec:     asdbv1.AerospikeConfigSpec{Value: map[string]interface{}{}},
			expected: nil,
		},
		{
			name: "dc with auth-password-file",
			//nolint:gosec // G101 test config path, not real credentials
			spec: xdrConfigSpec([]interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "/etc/aerospike/secret/authpwd.txt",
				},
			}),
			expected: []string{"/etc/aerospike/secret/authpwd.txt"},
		},
		{
			name: "dc without auth-password-file is skipped",
			spec: xdrConfigSpec([]interface{}{
				map[string]interface{}{
					"name": "DC1",
				},
			}),
			expected: nil,
		},
		{
			name: "dc with empty name is skipped even if auth-password-file is set",
			//nolint:gosec // G101 test config path, not real credentials
			spec: xdrConfigSpec([]interface{}{
				map[string]interface{}{
					"name":               "",
					"auth-password-file": "/etc/aerospike/secret/authpwd.txt",
				},
			}),
			expected: nil,
		},
		{
			name: "multiple dcs, only named ones with the field contribute paths",
			//nolint:gosec // G101 test config path, not real credentials
			spec: xdrConfigSpec([]interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "/etc/aerospike/secret/dc1-authpwd.txt",
				},
				map[string]interface{}{
					"name":               "",
					"auth-password-file": "/etc/aerospike/secret/dc-noname-authpwd.txt",
				},
				map[string]interface{}{
					"name": "DC2",
				},
				map[string]interface{}{
					"name":               "DC3",
					"auth-password-file": "env:XDR_AUTH_PWD",
				},
			}),
			expected: []string{
				"/etc/aerospike/secret/dc1-authpwd.txt",
				"env:XDR_AUTH_PWD",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getXDRAuthPasswordFilePaths(tt.spec))
		})
	}
}

func TestIsEnvPath(t *testing.T) {
	assert.True(t, isEnvPath("env:XDR_AUTH_PWD"))
	assert.True(t, isEnvPath("env-b64:XDR_AUTH_PWD"))
	assert.False(t, isEnvPath("secrets:AerospikeSecrets:AuthPassword"))
	assert.False(t, isEnvPath("vault:auth-password"))
	assert.False(t, isEnvPath("/etc/aerospike/secret/authpwd.txt"))
}

func serviceConfigSpec() map[string]interface{} {
	return map[string]interface{}{
		asdbv1.ConfKeyService: map[string]interface{}{},
		"network":             map[string]interface{}{},
	}
}

func storageWithVolume(mountPath string) *asdbv1.AerospikeStorageSpec {
	return &asdbv1.AerospikeStorageSpec{
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "secret-volume",
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "test-secret"},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: mountPath,
				},
			},
		},
	}
}

func TestValidateRequiredFileStorageForAerospikeConfig_XDRAuthPasswordFile(t *testing.T) {
	t.Run("mounted auth-password-file passes", func(t *testing.T) {
		config := serviceConfigSpec()
		//nolint:gosec // G101 test config path, not real credentials
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "/etc/aerospike/secret/authpwd.txt",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := storageWithVolume("/etc/aerospike/secret")

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.NoError(t, err)
	})

	t.Run("unmounted auth-password-file fails", func(t *testing.T) {
		config := serviceConfigSpec()
		//nolint:gosec // G101 test config path, not real credentials
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "/etc/aerospike/secret/authpwd.txt",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := &asdbv1.AerospikeStorageSpec{}

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "auth-password-file")
	})

	t.Run("env prefixed auth-password-file skips mount check", func(t *testing.T) {
		config := serviceConfigSpec()
		//nolint:gosec // G101 test config path, not real credentials
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "env:XDR_AUTH_PWD",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := &asdbv1.AerospikeStorageSpec{}

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.NoError(t, err)
	})

	t.Run("secrets prefixed auth-password-file skips mount check", func(t *testing.T) {
		config := serviceConfigSpec()
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "secrets:AerospikeSecrets:AuthPassword",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := &asdbv1.AerospikeStorageSpec{}

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.NoError(t, err)
	})

	t.Run("vault prefixed auth-password-file skips mount check", func(t *testing.T) {
		config := serviceConfigSpec()
		//nolint:gosec // G101 test config path, not real credentials
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "DC1",
					"auth-password-file": "vault:auth-password",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := &asdbv1.AerospikeStorageSpec{}

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.NoError(t, err)
	})

	t.Run("dc with empty name is not validated", func(t *testing.T) {
		config := serviceConfigSpec()
		//nolint:gosec // G101 test config path, not real credentials
		config[asdbv1.ConfKeyXdr] = map[string]interface{}{
			asdbv1.ConfKeyXdrDCs: []interface{}{
				map[string]interface{}{
					"name":               "",
					"auth-password-file": "/etc/aerospike/secret/authpwd.txt",
				},
			},
		}

		configSpec := asdbv1.AerospikeConfigSpec{Value: config}
		storage := &asdbv1.AerospikeStorageSpec{}

		err := validateRequiredFileStorageForAerospikeConfig(configSpec, storage)
		assert.NoError(t, err)
	})
}
