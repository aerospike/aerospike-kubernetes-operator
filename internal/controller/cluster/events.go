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

func eventRackScaleMessage(
	action string, rackID int, objectName string, current, desired int32,
) string {
	return fmt.Sprintf(
		"[rack-%d] %s StatefulSet %s from %d to %d replicas",
		rackID, action, objectName, current, desired,
	)
}

func eventRackScaleFailureMessage(
	action string, rackID int, objectName string, current, desired int32,
) string {
	return fmt.Sprintf(
		"[rack-%d] Failed to %s StatefulSet %s from %d to %d replicas",
		rackID, action, objectName, current, desired,
	)
}

// eventRackScaleFailureMessageWithCause appends a non-nil error to the rack scale failure message
// (matches legacy Eventf text that ended with the reconcile error string).
func eventRackScaleFailureMessageWithCause(
	action string, rackID int, objectName string, current, desired int32, cause error,
) string {
	msg := eventRackScaleFailureMessage(action, rackID, objectName, current, desired)
	if cause != nil {
		return fmt.Sprintf("%s: %s", msg, cause.Error())
	}

	return msg
}
