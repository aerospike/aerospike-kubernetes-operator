package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

			current, desiredOut, err := ComparableBackupSvcConfigs(apiConfig, desired)
			require.NoError(t, err)
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

	current, desiredOut, err := ComparableBackupSvcConfigs(apiConfig, desired)
	require.NoError(t, err)
	require.True(t, mapsEqual(current, desiredOut))
}

func TestComparableBackupSvcConfigs_InvalidConfigMap(t *testing.T) {
	t.Parallel()

	desired := desiredConfig(t)
	// A routine referencing an unknown policy cannot be converted to the backup service model.
	routine(t, desired)["backup-policy"] = "missing-policy"

	apiConfig := mustNormalize(t, desiredConfig(t))

	_, _, err := ComparableBackupSvcConfigs(apiConfig, desired)
	require.ErrorContains(t, err, "validate backup service config in ConfigMap")
	require.ErrorContains(t, err, "missing-policy")
}

func TestComparableBackupSvcConfigs_UnreadableAPIConfig(t *testing.T) {
	t.Parallel()

	// A newer backup service could support a storage type the vendored module does not know: the unknown key is
	// dropped on decode, and the storage is left without any configuration.
	apiConfig := mustNormalize(t, desiredConfig(t))
	apiConfig["storage"].(map[string]interface{})["local"] = map[string]interface{}{
		"nfs-storage": map[string]interface{}{"path": "/backups"},
	}

	_, _, err := ComparableBackupSvcConfigs(apiConfig, desiredConfig(t))
	require.ErrorContains(t, err, "read backup service config from API")
	require.ErrorContains(t, err, "no storage configuration provided")
}

func TestIsBackupSvcFullConfigSynced_InvalidConfigMap(t *testing.T) {
	t.Parallel()

	desired := desiredConfig(t)
	routine(t, desired)["backup-policy"] = "missing-policy"

	desiredData, err := yaml.Marshal(desired)
	require.NoError(t, err)

	synced, err := IsBackupSvcFullConfigSynced(mustNormalize(t, desiredConfig(t)), string(desiredData), logr.Discard())
	require.Error(t, err)
	require.False(t, synced)
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

// The reload branch needs a running backup service and is covered by the envtests and E2E suites.
func TestApplyBackupSvcConfigAction(t *testing.T) {
	tests := []struct {
		name          string
		action        BackupSvcConfigAction
		wantRestarted bool
	}{
		{name: "restart calls the controller's restart", action: BackupSvcConfigRestart, wantRestarted: true},
		{name: "no action does nothing", action: BackupSvcConfigNoAction, wantRestarted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restarted := false
			restart := func(context.Context) error {
				restarted = true
				return nil
			}

			err := ApplyBackupSvcConfigAction(context.Background(), logr.Discard(), nil, nil, nil, tt.action, restart)
			require.NoError(t, err)
			require.Equal(t, tt.wantRestarted, restarted)
		})
	}
}

func TestApplyBackupSvcConfigAction_RestartError(t *testing.T) {
	restartErr := fmt.Errorf("delete pods failed")

	err := ApplyBackupSvcConfigAction(context.Background(), logr.Discard(), nil, nil, nil, BackupSvcConfigRestart,
		func(context.Context) error { return restartErr })
	require.ErrorIs(t, err, restartErr)
}

func TestBackupSvcConfigChangeAction(t *testing.T) {
	tests := []struct {
		name   string
		change BackupSvcConfigChange
		want   BackupSvcConfigAction
	}{
		{name: "no change", change: BackupSvcConfigChange{Synced: true, SpecConfigApplied: true},
			want: BackupSvcConfigNoAction},
		{name: "non-secret change", change: BackupSvcConfigChange{}, want: BackupSvcConfigReload},
		{name: "static change", change: BackupSvcConfigChange{StaticChange: true}, want: BackupSvcConfigRestart},
		{name: "drift with spec applied", change: BackupSvcConfigChange{SpecConfigApplied: true},
			want: BackupSvcConfigReload},
		{name: "change the API cannot show", change: BackupSvcConfigChange{Synced: true, HiddenChange: true},
			want: BackupSvcConfigRestart},
		// The change is already running: for example the pod restarted but the status update failed.
		{name: "change already applied, status not written", change: BackupSvcConfigChange{Synced: true},
			want: BackupSvcConfigNoAction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.change.Action())
		})
	}
}

func TestIsHiddenConfigChange(t *testing.T) {
	const serviceConfig = `
service:
  http:
    port: 8081
  https:
    port: 8443
    cert-file: /etc/abs-tls/tls.crt
    key-file: /etc/abs-tls/tls.key
    key-file-password: %s
backup-policies:
  test-policy:
    parallel: %d
storage:
  s3:
    s3-storage:
      bucket: backups
      s3-region: us-east-1
      access-key-id: AKIA1
      secret-access-key: %s
`

	const backupConfig = `
aerospike-cluster:
  aerospike-backup-test-cluster:
    credentials:
      user: admin
      password: %s
    seed-nodes:
      - host-name: aerocluster.aerospike.svc.cluster.local
        port: 3000
`

	svc := func(keyPassword string, parallel int, secretKey string) []byte {
		return []byte(fmt.Sprintf(serviceConfig, keyPassword, parallel, secretKey))
	}

	backup := func(password string) []byte {
		return []byte(fmt.Sprintf(backupConfig, password))
	}

	tests := []struct {
		name      string
		oldConfig []byte
		newConfig []byte
		want      bool
	}{
		{name: "storage secret only", oldConfig: svc("kp1", 3, "old"), newConfig: svc("kp1", 3, "new"), want: true},
		{name: "static secret only", oldConfig: svc("kp1", 3, "old"), newConfig: svc("kp2", 3, "old"), want: true},
		{name: "cluster password only", oldConfig: backup("admin123"), newConfig: backup("newpass"), want: true},
		// A change the API shows is reloaded before this check runs; any difference reports true.
		{name: "non-secret only", oldConfig: svc("kp1", 3, "old"), newConfig: svc("kp1", 5, "old"), want: true},
		{name: "secret and non-secret", oldConfig: svc("kp1", 3, "old"), newConfig: svc("kp1", 5, "new"), want: true},
		// No config was applied yet: the pods started from the current ConfigMap.
		{name: "nothing applied yet", oldConfig: nil, newConfig: svc("kp1", 3, "old")},
		{name: "no change", oldConfig: svc("kp1", 3, "old"), newConfig: svc("kp1", 3, "old")},
		{
			name:      "formatting only",
			oldConfig: backup("admin123"),
			newConfig: []byte(`{"aerospike-cluster": {"aerospike-backup-test-cluster": {"seed-nodes": ` +
				`[{"port": 3000, "host-name": "aerocluster.aerospike.svc.cluster.local"}], ` +
				`"credentials": {"password": "admin123", "user": "admin"}}}}`),
		},
		{
			// A secret agent reference is shown by the API, so in practice its change is reloaded first.
			name:      "secret agent reference",
			oldConfig: backup("secrets:abs:pw1"),
			newConfig: backup("secrets:abs:pw2"),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsHiddenConfigChange(tt.oldConfig, tt.newConfig)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRenderBackupSvcConfig_KeepsBackupClusterSecrets(t *testing.T) {
	rendered, err := renderBackupSvcConfig([]byte(`
aerospike-cluster:
  aerospike-backup-test-cluster:
    credentials:
      user: admin
      password: admin123
`))
	require.NoError(t, err)
	require.Contains(t, string(rendered), `"aerospike-clusters"`)
	require.Contains(t, string(rendered), `"admin123"`)
}

func TestFindStaticBackupSvcConfigChange(t *testing.T) {
	const serviceConfig = `
service:
  http:
    port: %d
  https:
    port: 8443
    cert-file: /etc/abs-tls/tls.crt
    key-file: /etc/abs-tls/tls.key
    key-file-password: %s
  logger:
    level: %s
backup-policies:
  test-policy:
    parallel: %d
`

	svc := func(port int, keyPassword, level string, parallel int) []byte {
		return []byte(fmt.Sprintf(serviceConfig, port, keyPassword, level, parallel))
	}

	tests := []struct {
		name       string
		oldConfig  []byte
		newConfig  []byte
		wantStatic bool
	}{
		{name: "no change", oldConfig: svc(8081, "kp1", "INFO", 3), newConfig: svc(8081, "kp1", "INFO", 3)},
		{name: "dynamic change", oldConfig: svc(8081, "kp1", "INFO", 3), newConfig: svc(8081, "kp1", "INFO", 5)},
		// The backup service keeps the canonical value, so only the case differs.
		{name: "enum case only", oldConfig: svc(8081, "kp1", "INFO", 3), newConfig: svc(8081, "kp1", "info", 5)},
		{name: "static field", oldConfig: svc(8081, "kp1", "INFO", 3), newConfig: svc(8082, "kp1", "INFO", 3),
			wantStatic: true},
		{name: "static secret", oldConfig: svc(8081, "kp1", "INFO", 3), newConfig: svc(8081, "kp2", "INFO", 5),
			wantStatic: true},
		// No config was applied yet: the pods started from the current ConfigMap.
		{name: "nothing applied yet", oldConfig: nil, newConfig: svc(8081, "kp1", "INFO", 3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staticChangeErr, err := FindStaticBackupSvcConfigChange(tt.oldConfig, tt.newConfig)
			require.NoError(t, err)
			require.Equal(t, tt.wantStatic, staticChangeErr != nil, "static change: %v", staticChangeErr)
		})
	}
}

// A secret change sent together with a difference the backup service does not keep, here the case of incr-mode,
// must still restart: the API comparison sees nothing, and a restart is the only way to load the new password.
func TestSecretChangeWithHiddenDifferenceRestarts(t *testing.T) {
	applied := desiredConfig(t)
	applied["backup-policies"].(map[string]interface{})["test-policy"].(map[string]interface{})["incr-mode"] = "cumulative"
	appliedData, err := yaml.Marshal(applied)
	require.NoError(t, err)

	desired := desiredConfig(t)
	desired["backup-policies"].(map[string]interface{})["test-policy"].(map[string]interface{})["incr-mode"] = "Cumulative"
	credentials(t, desired)["password"] = "rotated-password"
	desiredData, err := yaml.Marshal(desired)
	require.NoError(t, err)

	// The running backup service loaded the applied config.
	synced, _, _, err := CompareBackupSvcConfigs(mustNormalize(t, applied), string(desiredData), logr.Discard())
	require.NoError(t, err)
	require.True(t, synced)

	hiddenChange, err := IsHiddenConfigChange(appliedData, desiredData)
	require.NoError(t, err)
	require.True(t, hiddenChange)

	require.Equal(t, BackupSvcConfigRestart, BackupSvcConfigChange{Synced: synced, HiddenChange: hiddenChange}.Action())
}
