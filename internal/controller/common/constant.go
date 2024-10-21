package common

// Backup Config relate keys
const (
	ServiceKey              = "service"
	AerospikeClustersKey    = "aerospike-clusters"
	AerospikeClusterKey     = "aerospike-cluster"
	StorageKey              = "storage"
	BackupRoutinesKey       = "backup-routines"
	BackupPoliciesKey       = "backup-policies"
	SecretAgentsKey         = "secret-agent"
	SourceClusterKey        = "source-cluster"
	BackupServiceConfigYAML = "aerospike-backup-service.yml"
)

// Restore config fields
const (
	RoutineKey = "routine"
	TimeKey    = "time"
	SourceKey  = "source"
)

const (
	HTTPKey                = "http"
	AerospikeBackupService = "aerospike-backup-service"
)
