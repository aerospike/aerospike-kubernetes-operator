package cluster

import (
	"fmt"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

const (
	EventReasonDeleteFailed = "DeleteFailed"
	EventReasonDeleted      = "Deleted"

	EventReasonServiceCreateFailed                = "ServiceCreateFailed"
	EventReasonRackReconcileFailed                = "UpdateFailed"
	EventReasonPodDisruptionBudgetReconcileFailed = "PodDisruptionBudgetReconcileFailed"
	EventReasonAccessControlUpdateFailed          = "ACLUpdateFailed"
	EventReasonStatusUpdateFailed                 = "StatusUpdateFailed"
	EventReasonAccessControlUpdated               = "ACLUpdated"

	EventReasonRoleCreateFailed = "RoleCreateFailed"
	EventReasonRoleUpdateFailed = "RoleUpdateFailed"
	EventReasonRoleCreated      = "RoleCreated"
	EventReasonRoleUpdated      = "RoleUpdated"
	EventReasonRoleDeleted      = "RoleDeleted"
	EventReasonUserCreateFailed = "UserCreateFailed"
	EventReasonUserUpdateFailed = "UserUpdateFailed"
	EventReasonUserCreated      = "UserCreated"
	EventReasonUserUpdated      = "UserUpdated"
	EventReasonUserDeleted      = "UserDeleted"

	EventReasonRackCreated              = "RackCreated"
	EventReasonRackDeleted              = "RackDeleted"
	EventReasonStatefulSetDeleteFailed  = "STSDeleteFailed"
	EventReasonRackImageUpdateFailed    = "RackImageUpdateFailed"
	EventReasonRackRollingRestartFailed = "RackRollingRestartFailed"
	EventReasonRackDynamicConfigFailed  = "RackDynamicConfigUpdateFailed"
	EventReasonDynamicConfigUpdate      = "DynamicConfigUpdate"
	EventReasonDynamicConfigUpdated     = "DynamicConfigUpdated"
	EventReasonRackScaleDownFailed      = "RackScaleDownFailed"
	EventReasonRackScaleUpFailed        = "RackScaleUpFailed"
	EventReasonRackScaleUp              = "RackScaleUp"
	EventReasonRackScaledUp             = "RackScaledUp"
	EventReasonPodImageUpdate           = "PodImageUpdate"
	EventReasonPodImageUpdated          = "PodImageUpdated"
	EventReasonPodWarmRestarted         = "PodWarmRestarted"
	EventReasonPodConfUpdated           = "PodConfUpdated"
	EventReasonPodRestarted             = "PodRestarted"
	EventReasonPodWaitUpdate            = "PodWaitUpdate"
	EventReasonRackImageUpdated         = "RackImageUpdated"
	EventReasonRackScaleDown            = "RackScaleDown"
	EventReasonPodDeleted               = "PodDeleted"
	EventReasonRackScaledDown           = "RackScaledDown"
	EventReasonRackRollingRestart       = "RackRollingRestart"
	EventReasonRackRollingRestarted     = "RackRollingRestarted"
	EventReasonWaitMigration            = "WaitMigration"
)

func eventNamespacedNames(namespace string, names []string) string {
	return strings.Join(utils.NamespacedNames(namespace, names), ", ")
}

func eventRackScaleMessage(action string, rackID int, objectKind, objectName string, current, desired int32) string {
	return fmt.Sprintf(
		"[rack-%d] %s %s %s from %d to %d replicas",
		rackID, action, objectKind, objectName, current, desired,
	)
}

func eventRackScaleFailureMessage(action string, rackID int, objectKind, objectName string, current, desired int32) string {
	return fmt.Sprintf(
		"[rack-%d] Failed to %s %s %s from %d to %d replicas",
		rackID, action, objectKind, objectName, current, desired,
	)
}
