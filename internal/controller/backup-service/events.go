package backupservice

const (
	EventReasonDeleted = "Deleted"

	EventReasonConfigMapReconcileFailed  = "ConfigMapReconcileFailed"
	EventReasonServiceReconcileFailed    = "ServiceReconcileFailed"
	EventReasonDeploymentReconcileFailed = "DeploymentReconcileFailed"
	EventReasonStatusUpdateFailed        = "StatusUpdateFailed"

	EventReasonConfigMapCreated  = "ConfigMapCreated"
	EventReasonConfigMapUpdated  = "ConfigMapUpdated"
	EventReasonDeploymentCreated = "DeploymentCreated"
	EventReasonDeploymentUpdated = "DeploymentUpdated"
	EventReasonServiceCreated    = "ServiceCreated"
	EventReasonServiceUpdated    = "ServiceUpdated"
)
