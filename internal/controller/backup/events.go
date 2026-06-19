package backup

const (
	EventReasonDeleted = "Deleted"

	EventReasonConfigMapReconcileFailed = "ConfigMapReconcileFailed"
	EventReasonBackupReconcileFailed    = "BackupReconcileFailed"
	EventReasonStatusUpdateFailed       = "StatusUpdateFailed"
	EventReasonConfigMapUpdated         = "ConfigMapUpdated"

	EventReasonOnDemandBackupTriggered = "OnDemandBackupTriggered"
	EventReasonBackupScheduled         = "BackupScheduled"
)
