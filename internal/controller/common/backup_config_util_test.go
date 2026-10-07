package common

import (
	"bytes"
	"encoding/json"
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

func credentials(t *testing.T, config map[string]interface{}) map[string]interface{} {
	t.Helper()

	clusters, ok := config["aerospike-clusters"].(map[string]interface{})
	require.True(t, ok)
	cluster, ok := clusters[clusterName].(map[string]interface{})
	require.True(t, ok)
	creds, ok := cluster["credentials"].(map[string]interface{})
	require.True(t, ok)

	return creds
}

func routine(t *testing.T, config map[string]interface{}) map[string]interface{} {
	t.Helper()

	routines, ok := config["backup-routines"].(map[string]interface{})
	require.True(t, ok)
	r, ok := routines[routineName].(map[string]interface{})
	require.True(t, ok)

	return r
}

func TestNormalizeBackupSvcConfig_RedactsLiteralSecrets(t *testing.T) {
	t.Parallel()

	normalized := mustNormalize(t, desiredConfig(t))

	require.Equal(t, "[secret]", credentials(t, normalized)["password"])
	require.Equal(t, "admin", credentials(t, normalized)["user"])

	data, err := json.Marshal(normalized)
	require.NoError(t, err)
	require.NotContains(t, string(data), "admin123")
}

func TestNormalizeBackupSvcConfig_KeepsSecretAgentReference(t *testing.T) {
	t.Parallel()

	config := desiredConfig(t)
	config["secret-agents"] = map[string]interface{}{
		"agent": map[string]interface{}{"connection-type": "tcp", "address": "localhost", "port": 3005},
	}
	creds := credentials(t, config)
	creds["password"] = "secrets:abs:pw"
	creds["secret-agent-name"] = "agent"

	require.Equal(t, "secrets:abs:pw", credentials(t, mustNormalize(t, config))["password"])
}

// The backup service API output is itself a normalized config, so normalizing it again must not change it.
// Comparing the normalized API output with the normalized ConfigMap relies on this.
func TestNormalizeBackupSvcConfig_Idempotent(t *testing.T) {
	t.Parallel()

	once := mustNormalize(t, desiredConfig(t))
	twice := mustNormalize(t, once)

	require.Equal(t, once, twice)
}

func TestComparableBackupSvcConfigs(t *testing.T) {
	t.Parallel()

	// What GET /v1/config returns for the desired config once the backup service has loaded it.
	apiConfig := mustNormalize(t, desiredConfig(t))

	tests := []struct {
		mutate    func(t *testing.T, desired map[string]interface{})
		name      string
		wantEqual bool
	}{
		{
			name:      "unchanged config with literal secret is equal",
			mutate:    func(*testing.T, map[string]interface{}) {},
			wantEqual: true,
		},
		{
			// A secret-only change cannot be seen through the API, it is detected from spec versus status.
			name: "secret-only change is not visible",
			mutate: func(t *testing.T, desired map[string]interface{}) {
				t.Helper()
				credentials(t, desired)["password"] = "newpass"
			},
			wantEqual: true,
		},
		{
			name: "non-secret change is detected",
			mutate: func(t *testing.T, desired map[string]interface{}) {
				t.Helper()
				routine(t, desired)["incr-interval-cron"] = "@midnight"
			},
			wantEqual: false,
		},
		{
			name: "user change is detected",
			mutate: func(t *testing.T, desired map[string]interface{}) {
				t.Helper()
				credentials(t, desired)["user"] = "backup-user"
			},
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			desired := desiredConfig(t)
			tt.mutate(t, desired)

			current, desiredOut, normalized := ComparableBackupSvcConfigs(logr.Discard(), apiConfig, desired)
			require.True(t, normalized)
			require.Equal(t, tt.wantEqual, mapsEqual(current, desiredOut))
		})
	}
}

func TestComparableBackupSvcConfigs_CanonicalizesEnums(t *testing.T) {
	t.Parallel()

	desired := desiredConfig(t)
	policies := desired["backup-policies"].(map[string]interface{})
	policies["test-policy"].(map[string]interface{})["incr-mode"] = "Cumulative"

	// The backup service stores and reports the canonical value.
	apiConfig := mustNormalize(t, desired)
	apiPolicy := apiConfig["backup-policies"].(map[string]interface{})["test-policy"].(map[string]interface{})
	require.Equal(t, "cumulative", apiPolicy["incr-mode"])

	current, desiredOut, normalized := ComparableBackupSvcConfigs(logr.Discard(), apiConfig, desired)
	require.True(t, normalized)
	require.True(t, mapsEqual(current, desiredOut))
}

func TestComparableBackupSvcConfigs_FallsBackToRawConfig(t *testing.T) {
	t.Parallel()

	desired := desiredConfig(t)
	// A routine referencing an unknown policy cannot be converted to the backup service model.
	routine(t, desired)["backup-policy"] = "missing-policy"

	apiConfig := mustNormalize(t, desiredConfig(t))

	current, desiredOut, normalized := ComparableBackupSvcConfigs(logr.Discard(), apiConfig, desired)
	require.False(t, normalized)
	require.Equal(t, apiConfig, current)
	require.Equal(t, desired, desiredOut)
}

func TestIsBackupSvcFullConfigSynced(t *testing.T) {
	t.Parallel()

	desired, err := yaml.Marshal(desiredConfig(t))
	require.NoError(t, err)

	apiConfig := mustNormalize(t, desiredConfig(t))

	synced, err := IsBackupSvcFullConfigSynced(apiConfig, string(desired), logr.Discard())
	require.NoError(t, err)
	require.True(t, synced)

	routine(t, apiConfig)["incr-interval-cron"] = "@midnight"

	synced, err = IsBackupSvcFullConfigSynced(apiConfig, string(desired), logr.Discard())
	require.NoError(t, err)
	require.False(t, synced)
}

func mapsEqual(a, b map[string]interface{}) bool {
	aData, _ := json.Marshal(a)
	bData, _ := json.Marshal(b)

	return bytes.Equal(aData, bData)
}
