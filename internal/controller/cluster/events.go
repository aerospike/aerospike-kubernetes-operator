package cluster

import (
	"fmt"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// Shared Event Reason constants: used in two or more places across the cluster controller.
// Single-use reasons are inlined as PascalCase string literals at their call sites.
const (
	ReasonServiceCreateFailed = "ServiceCreateFailed"
	ReasonStatusUpdateFailed  = "StatusUpdateFailed"
	ReasonACLUpdateFailed     = "ACLUpdateFailed"
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
