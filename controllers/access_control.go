package controllers

// Aerospike access control reconciliation of access control.

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	as "github.com/aerospike/aerospike-client-go/v7"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	acl "github.com/aerospike/aerospike-management-lib/accesscontrol"
)

// AerospikeAdminCredentials to use for aerospike clients.
//
// Returns a tuple of admin username and password to use. If the cluster is not security
// enabled both username and password will be zero strings.
func AerospikeAdminCredentials(
	desiredState, currentState *asdbv1.AerospikeClusterSpec,
	passwordProvider AerospikeUserPasswordProvider,
) (user, pass string, err error) {
	var (
		enabled            bool
		currentSecurityErr error
		desiredSecurityErr error
	)

	incomingVersion, incomingVersionErr := asdbv1.GetImageVersion(desiredState.Image)
	if incomingVersionErr == nil {
		enabled, desiredSecurityErr = asdbv1.IsSecurityEnabled(
			incomingVersion, desiredState.AerospikeConfig,
		)
	} else {
		desiredSecurityErr = incomingVersionErr
	}

	if !enabled {
		outgoingVersion, outgoingVersionErr := asdbv1.GetImageVersion(currentState.Image)
		if outgoingVersionErr == nil {
			// It is possible that this is a new cluster and current state is empty.
			enabled, currentSecurityErr = asdbv1.IsSecurityEnabled(
				outgoingVersion, currentState.AerospikeConfig,
			)
		} else {
			currentSecurityErr = outgoingVersionErr
		}

		if currentSecurityErr != nil && desiredSecurityErr != nil {
			return "", "", desiredSecurityErr
		}
	}

	if !enabled {
		// Return zero strings if this is not a security enabled cluster.
		return "", "", nil
	}

	if currentState.AerospikeAccessControl == nil {
		// We haven't yet set up access control. Use default password.
		return asdbv1.AdminUsername, asdbv1.DefaultAdminPassword, nil
	}

	adminUserSpec, ok := asdbv1.GetUsersFromSpec(currentState)[asdbv1.AdminUsername]
	if !ok {
		// Should not happen on a validated spec.
		return "", "", fmt.Errorf(
			"%s user missing in access control", asdbv1.AdminUsername,
		)
	}

	password, err := passwordProvider.Get(
		asdbv1.AdminUsername, &adminUserSpec,
	)
	if err != nil {
		return "", "", err
	}

	return asdbv1.AdminUsername, password, nil
}

// reconcileAccessControl reconciles access control to ensure current state moves to the desired state.
func (r *SingleClusterReconciler) reconcileAccessControl(
	client *as.Client,
	passwordProvider AerospikeUserPasswordProvider,
) error {
	desired := &r.aeroCluster.Spec

	currentState, err := asdbv1.CopyStatusToSpec(&r.aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		r.Log.Error(err, "Failed to copy spec in status", "err", err)
		return err
	}

	// Get admin policy based in desired state so that new timeout updates can be applied. It is safe.
	adminPolicy := GetAdminPolicy(desired)
	desiredRoles := asdbv1.GetRolesFromSpec(desired)
	currentRoles := asdbv1.GetRolesFromSpec(currentState)

	if err := r.reconcileRoles(
		desiredRoles, currentRoles, client, adminPolicy,
	); err != nil {
		return err
	}

	desiredUsers := asdbv1.GetUsersFromSpec(desired)
	currentUsers := asdbv1.GetUsersFromSpec(currentState)

	return r.reconcileUsers(
		desiredUsers, currentUsers, passwordProvider, client, adminPolicy,
	)
}

// GetAdminPolicy returns the AdminPolicy to use for performing access control operations.
func GetAdminPolicy(clusterSpec *asdbv1.AerospikeClusterSpec) as.AdminPolicy {
	if clusterSpec.AerospikeAccessControl == nil || clusterSpec.AerospikeAccessControl.AdminPolicy == nil {
		return *as.NewAdminPolicy()
	}

	specAdminPolicy := *clusterSpec.AerospikeAccessControl.AdminPolicy

	return as.AdminPolicy{Timeout: time.Duration(specAdminPolicy.Timeout) * time.Millisecond}
}

// reconcileRoles reconciles roles to take them from current to desired.
func (r *SingleClusterReconciler) reconcileRoles(
	desired map[string]asdbv1.AerospikeRoleSpec,
	current map[string]asdbv1.AerospikeRoleSpec, client *as.Client,
	adminPolicy as.AdminPolicy,
) error {
	// List roles in the cluster.
	currentRoleNames := make([]string, 0, len(current))
	for roleName := range current {
		currentRoleNames = append(currentRoleNames, roleName)
	}

	// List roles needed in the desired list.
	requiredRoleNames := make([]string, 0, len(desired))
	for roleName := range desired {
		requiredRoleNames = append(requiredRoleNames, roleName)
	}

	// Create a list of role commands to drop.
	rolesToDrop := acl.SliceSubtract(currentRoleNames, requiredRoleNames)
	roleReconcileCmds := make([]acl.AerospikeAccessControlReconcileCmd, 0, len(rolesToDrop)+len(desired))

	for _, roleToDrop := range rolesToDrop {
		if _, ok := asdbv1.PredefinedRoles[roleToDrop]; !ok {
			// Not a predefined role and can be dropped.
			roleReconcileCmds = append(
				roleReconcileCmds, acl.AerospikeRoleDrop{Name: roleToDrop},
			)
		}
	}

	for roleName, roleSpec := range desired {
		roleReconcileCmds = append(
			roleReconcileCmds, acl.AerospikeRoleCreateUpdate{
				Name: roleName, Privileges: roleSpec.Privileges,
				Whitelist: roleSpec.Whitelist, ReadQuota: roleSpec.ReadQuota,
				WriteQuota: roleSpec.WriteQuota,
			},
		)
	}

	// execute all commands.
	for _, cmd := range roleReconcileCmds {
		if err := cmd.Execute(client, &adminPolicy, r.Log); err != nil {
			r.recordACLEvent(cmd, true)
			return err
		}

		r.recordACLEvent(cmd, false)
	}

	return nil
}

// reconcileUsers reconciles users to take them from current to desired.
func (r *SingleClusterReconciler) reconcileUsers(
	desired map[string]asdbv1.AerospikeUserSpec,
	current map[string]asdbv1.AerospikeUserSpec,
	passwordProvider AerospikeUserPasswordProvider, client *as.Client,
	adminPolicy as.AdminPolicy,
) error {
	// List users in the cluster.
	currentUserNames := make([]string, 0, len(current))
	for userName := range current {
		currentUserNames = append(currentUserNames, userName)
	}

	// List users needed in the desired list.
	requiredUserNames := make([]string, 0, len(desired))
	for userName := range desired {
		requiredUserNames = append(requiredUserNames, userName)
	}

	// Create a list of user commands to drop.
	usersToDrop := acl.SliceSubtract(currentUserNames, requiredUserNames)
	userReconcileCmds := make([]acl.AerospikeAccessControlReconcileCmd, 0, len(usersToDrop)+len(desired))

	for _, userToDrop := range usersToDrop {
		userReconcileCmds = append(
			userReconcileCmds, acl.AerospikeUserDrop{Name: userToDrop},
		)
	}

	// Admin user update command should be executed last to ensure admin password
	// update does not disrupt reconciliation.
	var adminUpdateCmd *acl.AerospikeUserCreateUpdate

	for userName := range desired {
		userSpec := desired[userName]

		password, err := passwordProvider.Get(userName, &userSpec)
		if err != nil {
			return err
		}

		cmd := acl.AerospikeUserCreateUpdate{
			Name: userName, Password: &password, Roles: userSpec.Roles,
		}
		if userName == asdbv1.AdminUsername {
			adminUpdateCmd = &cmd
		} else {
			userReconcileCmds = append(userReconcileCmds, cmd)
		}
	}

	if adminUpdateCmd != nil {
		// Append admin user update command the last.
		userReconcileCmds = append(userReconcileCmds, *adminUpdateCmd)
	}

	for _, cmd := range userReconcileCmds {
		if err := cmd.Execute(client, &adminPolicy, r.Log); err != nil {
			r.recordACLEvent(cmd, true)
			return err
		}

		r.recordACLEvent(cmd, false)
	}

	return nil
}

// AerospikeUserPasswordProvider provides password for a give user..
type AerospikeUserPasswordProvider interface {
	// Get returns the password for username.
	Get(username string, userSpec *asdbv1.AerospikeUserSpec) (
		string, error,
	)
}

func (r *SingleClusterReconciler) recordACLEvent(cmd acl.AerospikeAccessControlReconcileCmd, failed bool) {
	var eventType, message, reason string

	switch v := cmd.(type) {
	case acl.AerospikeUserCreateUpdate:
		if failed {
			eventType = corev1.EventTypeWarning
			reason = "UserCreateOrUpdateFailed"
			message = fmt.Sprintf("Failed to Create or Update User %s", v.Name)
		} else {
			eventType = corev1.EventTypeNormal
			reason = "UserCreatedOrUpdated"
			message = fmt.Sprintf("Created or Updated User %s", v.Name)
		}

	case acl.AerospikeUserDrop:
		if failed {
			eventType = corev1.EventTypeWarning
			reason = "UserDeleteFailed"
			message = fmt.Sprintf("Failed to Drop User %s", v.Name)
		} else {
			eventType = corev1.EventTypeNormal
			reason = "UserDeleted"
			message = fmt.Sprintf("Dropped User %s", v.Name)
		}

	case acl.AerospikeRoleCreateUpdate:
		if failed {
			eventType = corev1.EventTypeWarning
			reason = "RoleCreateOrUpdateFailed"
			message = fmt.Sprintf("Failed to Create or Update Role %s", v.Name)
		} else {
			eventType = corev1.EventTypeNormal
			reason = "RoleCreatedOrUpdated"
			message = fmt.Sprintf("Created or Updated Role %s", v.Name)
		}

	case acl.AerospikeRoleDrop:
		if failed {
			eventType = corev1.EventTypeWarning
			reason = "RoleDeleteFailed"
			message = fmt.Sprintf("Failed to Drop Role %s", v.Name)
		} else {
			eventType = corev1.EventTypeNormal
			reason = "RoleDeleted"
			message = fmt.Sprintf("Dropped Role %s", v.Name)
		}
	}

	r.Recorder.Eventf(
		r.aeroCluster, eventType, reason,
		message,
	)
}
