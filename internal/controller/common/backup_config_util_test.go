package common

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	clusterName = "aerospike-backup-test-cluster"
	routineName = "aerospike-backup-test-routine"
)

// desiredConfig mirrors a ConfigMap written by the operator: the AerospikeBackupService sections plus
// one cluster and one routine merged in from an AerospikeBackup.
func desiredConfig(t *testing.T) map[string]interface{} {
	t.Helper()

	return mustParse(t, `
service:
  http:
    port: 8081
backup-policies:
  test-policy:
    parallel: 3
    compact: true
    incr-mode: cumulative
storage:
  local:
    local-storage:
      path: /tmp/localStorage
aerospike-clusters:
  aerospike-backup-test-cluster:
    credentials:
      user: admin
      password: admin123
    seed-nodes:
      - host-name: aerocluster.aerospike.svc.cluster.local
        port: 3000
backup-routines:
  aerospike-backup-test-routine:
    backup-policy: test-policy
    interval-cron: "@daily"
    incr-interval-cron: "@hourly"
    namespaces: ["test"]
    source-cluster: aerospike-backup-test-cluster
    storage: local
`)
}

func mustParse(t *testing.T, data string) map[string]interface{} {
	t.Helper()

	config := make(map[string]interface{})
	require.NoError(t, yaml.Unmarshal([]byte(data), &config))

	return config
}

func mustNormalize(t *testing.T, config map[string]interface{}) map[string]interface{} {
	t.Helper()

	normalized, err := NormalizeBackupSvcConfig(config)
	require.NoError(t, err)

	return normalized
}

// credentials returns the cluster credentials in a config built from desiredConfig.
func credentials(config map[string]interface{}) map[string]interface{} {
	clusters := config["aerospike-clusters"].(map[string]interface{})

	return clusters[clusterName].(map[string]interface{})["credentials"].(map[string]interface{})
}

// routine returns the routine in a config built from desiredConfig.
func routine(config map[string]interface{}) map[string]interface{} {
	return config["backup-routines"].(map[string]interface{})[routineName].(map[string]interface{})
}

func TestNormalizeBackupSvcConfig_RedactsLiteralSecrets(t *testing.T) {
	t.Parallel()

	normalized := mustNormalize(t, desiredConfig(t))

	require.Equal(t, "[secret]", credentials(normalized)["password"])
	require.Equal(t, "admin", credentials(normalized)["user"])

	data, err := json.Marshal(normalized)
	require.NoError(t, err)
	require.NotContains(t, string(data), "admin123")
}

func TestComparableBackupSvcConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// mutateDesired changes the ConfigMap config, mutateAPI the config GET /v1/config returns for it.
		mutateDesired func(desired map[string]interface{})
		mutateAPI     func(api map[string]interface{})
		name          string
		wantErr       string
		wantEqual     bool
	}{
		{name: "unchanged config with literal secret is equal", wantEqual: true},
		{
			// A secret-only change cannot be seen through the API, it is detected from spec versus status.
			name:          "secret-only change is not visible",
			mutateDesired: func(c map[string]interface{}) { credentials(c)["password"] = "newpass" },
			wantEqual:     true,
		},
		{
			name:          "non-secret change is detected",
			mutateDesired: func(c map[string]interface{}) { routine(c)["incr-interval-cron"] = "@midnight" },
		},
		{
			// The backup service stores and reports enum values in canonical form.
			name: "enum written in another case is equal",
			mutateDesired: func(c map[string]interface{}) {
				c["backup-policies"].(map[string]interface{})["test-policy"].(map[string]interface{})["incr-mode"] = "Cumulative"
			},
			wantEqual: true,
		},
		{
			// A routine referencing an unknown policy cannot be converted to the backup service model.
			name:          "invalid ConfigMap is an error",
			mutateDesired: func(c map[string]interface{}) { routine(c)["backup-policy"] = "missing-policy" },
			wantErr:       "validate backup service config in ConfigMap",
		},
		{
			// A newer backup service could support a storage type the vendored module does not know: the unknown key
			// is dropped on decode, and the storage is left without any configuration.
			name: "API config this module cannot read is an error",
			mutateAPI: func(c map[string]interface{}) {
				c["storage"].(map[string]interface{})["local"] = map[string]interface{}{
					"nfs-storage": map[string]interface{}{"path": "/backups"},
				}
			},
			wantErr: "read backup service config from API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// What GET /v1/config returns for the desired config once the backup service has loaded it.
			api := mustNormalize(t, desiredConfig(t))
			desired := desiredConfig(t)

			if tt.mutateAPI != nil {
				tt.mutateAPI(api)
			}

			if tt.mutateDesired != nil {
				tt.mutateDesired(desired)
			}

			current, desiredOut, err := ComparableBackupSvcConfigs(api, desired)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantEqual, reflect.DeepEqual(current, desiredOut))
		})
	}
}

func TestIsBackupSvcFullConfigSynced(t *testing.T) {
	t.Parallel()

	desired, err := yaml.Marshal(desiredConfig(t))
	require.NoError(t, err)

	apiConfig := mustNormalize(t, desiredConfig(t))

	synced, err := IsBackupSvcFullConfigSynced(apiConfig, string(desired), logr.Discard())
	require.NoError(t, err)
	require.True(t, synced)

	routine(apiConfig)["incr-interval-cron"] = "@midnight"

	synced, err = IsBackupSvcFullConfigSynced(apiConfig, string(desired), logr.Discard())
	require.NoError(t, err)
	require.False(t, synced)
}
