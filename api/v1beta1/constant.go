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
	// BackupServiceConfigHashKey is the ConfigMap key that holds the SHA-256 of the BackupServiceConfigYAML value.
	// The backup service pods see it as a file next to the config, so the operator can tell when a pod has the
	// latest config.
	BackupServiceConfigHashKey = "revision-hash"
	// BackupServiceConfigMountPath is where the backup service ConfigMap is mounted in the backup service pods.
	BackupServiceConfigMountPath = "/etc/aerospike-backup-service"
)

// Restore config fields
const (
	RoutineKey        = "routine"
	TimeKey           = "time"
	SourceKey         = "source"
	SourceNameKey     = "source-name"
	BackupDataPathKey = "backup-data-path"
)

const (
	HTTPKey                   = "http"
	HTTPSKey                  = "https"
	AerospikeBackupServiceKey = "aerospike-backup-service"
	RefreshTimeKey            = AerospikeBackupServiceKey + "/last-refresh"
)
