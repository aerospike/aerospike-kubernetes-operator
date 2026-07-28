// Package backupconfig provides shared backup-service YAML config fixtures used by
// AerospikeBackupService, AerospikeBackup, and AerospikeRestore tests (integration and envtests).
package backupconfig

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const (
	BackupServiceImage  = "aerospike/aerospike-backup-service:3.6.1"
	DefaultClusterHost  = "aerocluster.test.svc.cluster.local"
	DefaultBackupPolicy = "test-policy"
)

// BackupRoutineCrons holds cron expressions for backup routine config.
type BackupRoutineCrons struct {
	IntervalCron     string
	IncrIntervalCron string
}

var (
	// IntegrationRoutineCrons is used by integration backup tests.
	IntegrationRoutineCrons = BackupRoutineCrons{
		IntervalCron:     "*/30 * * * * *",
		IncrIntervalCron: "@hourly",
	}
	// EnvtestRoutineCrons is used by envtest webhook tests.
	EnvtestRoutineCrons = BackupRoutineCrons{
		IntervalCron:     "@daily",
		IncrIntervalCron: "@hourly",
	}
)

// RestoreOnlyStorageConfig returns a minimal ABS config for restore-side deployments:
// service, backup-policies, and storage only (no aerospike-clusters or backup-routines).
// Used to exercise BKRS-212 cross-region timestamp restores where the routine is not
// configured locally but storage is overridden via source-name.
func RestoreOnlyStorageConfig() map[string]interface{} {
	return BackupServiceBaseConfig()
}

// BackupServiceBaseConfig returns the base backup-service config (service, policies, storage).
func BackupServiceBaseConfig() map[string]interface{} {
	return map[string]interface{}{
		asdbv1beta1.ServiceKey: map[string]interface{}{
			"http": map[string]interface{}{
				"port": 8081,
			},
		},
		asdbv1beta1.BackupPoliciesKey: map[string]interface{}{
			DefaultBackupPolicy: map[string]interface{}{
				"parallel": 3,
			},
		},
		asdbv1beta1.StorageKey: map[string]interface{}{
			"local": map[string]interface{}{
				"local-storage": map[string]interface{}{
					"path": "/tmp/localStorage",
				},
			},
		},
	}
}

func clusterName(prefix string) string {
	return fmt.Sprintf("%s-test-cluster", prefix)
}

func routineName(prefix string) string {
	return fmt.Sprintf("%s-test-routine", prefix)
}

// BuildRoutineNameForBackup returns the backup-routine name in BackupCRConfig for the given backup CR.
func BuildRoutineNameForBackup(backupNsNm types.NamespacedName) string {
	return routineName(asdbv1beta1.NamePrefix(backupNsNm))
}

// BackupCRConfig returns backup CR config with aerospike-cluster and backup-routines sections.
func BackupCRConfig(prefix, clusterHost string, crons BackupRoutineCrons) map[string]interface{} {
	clusterName := clusterName(prefix)
	routineName := routineName(prefix)

	return map[string]interface{}{
		asdbv1beta1.AerospikeClusterKey: map[string]interface{}{
			clusterName: clusterConfig(clusterHost),
		},
		asdbv1beta1.BackupRoutinesKey: map[string]interface{}{
			routineName: routineConfig(clusterName, crons),
		},
	}
}

// BackupServiceMergedConfig returns merged ABS ConfigMap config with aerospike-clusters and backup-routines.
// Used by restore webhook tests where the controller would normally merge backup CR config into the ConfigMap.
func BackupServiceMergedConfig(clusterHost, clusterName,
	routineName string, crons BackupRoutineCrons) map[string]interface{} {
	config := BackupServiceBaseConfig()

	config[asdbv1beta1.AerospikeClustersKey] = map[string]interface{}{
		clusterName: clusterConfig(clusterHost),
	}
	config[asdbv1beta1.BackupRoutinesKey] = map[string]interface{}{
		routineName: routineConfig(clusterName, crons),
	}

	return config
}

// DefaultRestoreMergedConfig returns the merged ABS ConfigMap config used by restore envtests.
func DefaultRestoreMergedConfig() map[string]interface{} {
	return BackupServiceMergedConfig(
		DefaultClusterHost,
		"test-cluster",
		"test-routine",
		EnvtestRoutineCrons,
	)
}

func clusterConfig(clusterHost string) map[string]interface{} {
	return map[string]interface{}{
		"credentials": map[string]interface{}{
			"password": "admin123",
			"user":     "admin",
		},
		"seed-nodes": []map[string]interface{}{
			{
				"host-name": clusterHost,
				"port":      3000,
			},
		},
	}
}

func routineConfig(clusterName string, crons BackupRoutineCrons) map[string]interface{} {
	return map[string]interface{}{
		"backup-policy":      DefaultBackupPolicy,
		"interval-cron":      crons.IntervalCron,
		"incr-interval-cron": crons.IncrIntervalCron,
		"namespaces":         []string{"test"},
		"source-cluster":     clusterName,
		"storage":            "local",
	}
}

// MarshalConfig marshals a config map to JSON bytes.
func MarshalConfig(config map[string]interface{}) ([]byte, error) {
	return json.Marshal(config)
}

// MustMarshalConfig marshals a config map to JSON bytes and panics on error.
func MustMarshalConfig(config map[string]interface{}) []byte {
	configBytes, err := MarshalConfig(config)
	if err != nil {
		panic(err)
	}

	return configBytes
}
