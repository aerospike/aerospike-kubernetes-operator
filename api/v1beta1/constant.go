package v1beta1

// Backup Config relate keys
const (
	ServiceKey              = "service"
	AerospikeClustersKey    = "aerospike-clusters"
	AerospikeClusterKey     = "aerospike-cluster"
	StorageKey              = "storage"
	BackupRoutinesKey       = "backup-routines"
	BackupPoliciesKey       = "backup-policies"
	SecretAgentsKey         = "secret-agents"
	SourceClusterKey        = "source-cluster"
	BackupServiceConfigYAML = "aerospike-backup-service.yml"
)

// Restore config fields
const (
	RoutineKey        = "routine"
	TimeKey           = "time"
	SourceKey         = "source"
	BackupDataPathKey = "backup-data-path"
)

const (
	HTTPKey                   = "http"
	AerospikeBackupServiceKey = "aerospike-backup-service"
	RefreshTimeKey            = AerospikeBackupServiceKey + "/last-refresh"
)
