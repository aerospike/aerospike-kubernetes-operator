package controllers

// Aerospike access control reconciliation of access control.

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

// logger type alias.
type logger = logr.Logger

const (

	// Error marker for user not found errors.
	userNotFoundErr = "Invalid user"

	// Error marker for role not found errors.
	roleNotFoundErr = "Invalid role"
)

// AerospikeAdminCredentials to use for aerospike clients.
//
// Returns a tuple of admin username and password to use. If the cluster is not security
// enabled both username and password will be zero strings.
func AerospikeAdminCredentials(
	desiredState, currentState *asdbv1.AerospikeClusterSpec,
	passwordProvider AerospikeUserPasswordProvider,
) (user, pass string, err error) {
	var enabled bool

	outgoingVersion, err := asdbv1.GetImageVersion(currentState.Image)
	if err != nil {
		incomingVersion, newErr := asdbv1.GetImageVersion(desiredState.Image)
		if newErr != nil {
			return "", "", newErr
		}

		enabled, newErr = asdbv1.IsSecurityEnabled(
			incomingVersion, desiredState.AerospikeConfig,
		)
		if newErr != nil {
			return "", "", newErr
		}
	} else {
		enabled, err = asdbv1.IsSecurityEnabled(
			outgoingVersion, currentState.AerospikeConfig,
		)
		if err != nil {
			incomingVersion, newErr := asdbv1.GetImageVersion(desiredState.Image)
			if newErr != nil {
				return "", "", newErr
			}

			// Its possible this is a new cluster and current state is empty.
			enabled, newErr = asdbv1.IsSecurityEnabled(
				incomingVersion, desiredState.AerospikeConfig,
			)
			if newErr != nil {
				return "", "", newErr
			}
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
	rolesToDrop := SliceSubtract(currentRoleNames, requiredRoleNames)
	roleReconcileCmds := make([]aerospikeAccessControlReconcileCmd, 0, len(rolesToDrop)+len(desired))

	for _, roleToDrop := range rolesToDrop {
		if _, ok := asdbv1.PredefinedRoles[roleToDrop]; !ok {
			// Not a predefined role and can be dropped.
			roleReconcileCmds = append(
				roleReconcileCmds, aerospikeRoleDrop{name: roleToDrop},
			)
		}
	}

	for roleName, roleSpec := range desired {
		roleReconcileCmds = append(
			roleReconcileCmds, aerospikeRoleCreateUpdate{
				name: roleName, privileges: roleSpec.Privileges,
				whitelist: roleSpec.Whitelist, readQuota: roleSpec.ReadQuota,
				writeQuota: roleSpec.WriteQuota,
			},
		)
	}

	// execute all commands.
	for _, cmd := range roleReconcileCmds {
		if err := cmd.execute(client, &adminPolicy, r.Log, r.Recorder, r.aeroCluster); err != nil {
			return err
		}
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
	usersToDrop := SliceSubtract(currentUserNames, requiredUserNames)
	userReconcileCmds := make([]aerospikeAccessControlReconcileCmd, 0, len(usersToDrop)+len(desired))

	for _, userToDrop := range usersToDrop {
		userReconcileCmds = append(
			userReconcileCmds, aerospikeUserDrop{name: userToDrop},
		)
	}

	// Admin user update command should be executed last to ensure admin password
	// update does not disrupt reconciliation.
	var adminUpdateCmd *aerospikeUserCreateUpdate

	for userName := range desired {
		userSpec := desired[userName]

		password, err := passwordProvider.Get(userName, &userSpec)
		if err != nil {
			return err
		}

		cmd := aerospikeUserCreateUpdate{
			name: userName, password: &password, roles: userSpec.Roles,
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
		if err := cmd.execute(client, &adminPolicy, r.Log, r.Recorder, r.aeroCluster); err != nil {
			return err
		}
	}

	return nil
}

// privilegeStringToAerospikePrivilege converts privilegeString to an Aerospike privilege.
func privilegeStringToAerospikePrivilege(privilegeStrings []string) (
	[]as.Privilege, error,
) {
	aerospikePrivileges := make([]as.Privilege, 0, len(privilegeStrings))

	for _, privilege := range privilegeStrings {
		parts := strings.Split(privilege, ".")
		if _, ok := asdbv1.Privileges[parts[0]]; !ok {
			// First part of the privilege is not part of defined privileges.
			return nil, fmt.Errorf("invalid privilege %s", privilege)
		}

		privilegeCode := parts[0]
		namespaceName := ""
		setName := ""
		nParts := len(parts)

		switch nParts {
		case 2:
			namespaceName = parts[1]

		case 3:
			namespaceName = parts[1]
			setName = parts[2]
		}

		var code = as.Read //nolint:ineffassign // type is a private type in the pkg

		switch privilegeCode {
		case "read":
			code = as.Read

		case "write":
			code = as.Write

		case "read-write":
			code = as.ReadWrite

		case "read-write-udf":
			code = as.ReadWriteUDF

		case "data-admin":
			code = as.DataAdmin

		case "sys-admin":
			code = as.SysAdmin

		case "user-admin":
			code = as.UserAdmin

		case "truncate":
			code = as.Truncate

		case "sindex-admin":
			code = as.SIndexAdmin

		case "udf-admin":
			code = as.UDFAdmin

		default:
			return nil, fmt.Errorf("unknown privilege %s", privilegeCode)
		}

		aerospikePrivilege := as.Privilege{
			Code: code, Namespace: namespaceName, SetName: setName,
		}
		aerospikePrivileges = append(aerospikePrivileges, aerospikePrivilege)
	}

	return aerospikePrivileges, nil
}

// AerospikePrivilegeToPrivilegeString converts aerospikePrivilege to controller spec privilege string.
func AerospikePrivilegeToPrivilegeString(aerospikePrivileges []as.Privilege) (
	[]string, error,
) {
	privileges := make([]string, 0, len(aerospikePrivileges))

	for _, aerospikePrivilege := range aerospikePrivileges {
		var buffer bytes.Buffer

		switch aerospikePrivilege.Code {
		case as.Read:
			buffer.WriteString("read")

		case as.Write:
			buffer.WriteString("write")

		case as.ReadWrite:
			buffer.WriteString("read-write")

		case as.ReadWriteUDF:
			buffer.WriteString("read-write-udf")

		case as.DataAdmin:
			buffer.WriteString("data-admin")

		case as.SysAdmin:
			buffer.WriteString("sys-admin")

		case as.UserAdmin:
			buffer.WriteString("user-admin")

		case as.Truncate:
			buffer.WriteString("truncate")

		case as.SIndexAdmin:
			buffer.WriteString("sindex-admin")

		case as.UDFAdmin:
			buffer.WriteString("udf-admin")

		default:
			return nil, fmt.Errorf(
				"unknown privilege code %v", aerospikePrivilege.Code,
			)
		}

		if aerospikePrivilege.Namespace != "" {
			buffer.WriteString(".")
			buffer.WriteString(aerospikePrivilege.Namespace)

			if aerospikePrivilege.SetName != "" {
				buffer.WriteString(".")
				buffer.WriteString(aerospikePrivilege.SetName)
			}
		}

		privileges = append(privileges, buffer.String())
	}

	return privileges, nil
}

// AerospikeUserPasswordProvider provides password for a give user..
type AerospikeUserPasswordProvider interface {
	// Get returns the password for username.
	Get(username string, userSpec *asdbv1.AerospikeUserSpec) (
		string, error,
	)
}

// aerospikeAccessControlReconcileCmd commands needed to Reconcile a single access control entry,
// for example a role or a user.
type aerospikeAccessControlReconcileCmd interface {
	// Execute executes the command. The implementation should be idempotent.
	execute(
		client *as.Client, adminPolicy *as.AdminPolicy, logger logger, recorder record.EventRecorder,
		aeroCluster *asdbv1.AerospikeCluster,
	) error
}

// aerospikeRoleCreateUpdate creates or updates an Aerospike role.
type aerospikeRoleCreateUpdate struct {
	// The role's name.
	name string

	// The privileges to set for the role. These privileges and only these privileges will be granted to the role
	// after this operation.
	privileges []string

	// The whitelist to set for the role. These whitelist addresses and only these whitelist addresses will be
	// granted to the role after this operation.
	whitelist []string

	// The readQuota specifies the read query rate that is permitted for the current role.
	readQuota uint32

	// The writeQuota specifies the write rate that is permitted for the current role.
	writeQuota uint32
}

// Execute creates a new Aerospike role or updates an existing one.
func (roleCreate aerospikeRoleCreateUpdate) execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	role, err := client.QueryRole(adminPolicy, roleCreate.name)
	isCreate := false

	if err != nil {
		if strings.Contains(err.Error(), roleNotFoundErr) {
			isCreate = true
		} else {
			// Failure to query for the role.
			return fmt.Errorf(
				"error querying role %s: %v", roleCreate.name, err,
			)
		}
	}

	if isCreate {
		if err := roleCreate.createRole(client, adminPolicy, logger, recorder, aeroCluster); err != nil {
			recorder.Eventf(
				aeroCluster, corev1.EventTypeWarning, "RoleCreateFailed",
				"Failed to Create Role %s", roleCreate.name,
			)
		}

		return err
	}

	if errorUpdate := roleCreate.updateRole(
		client, adminPolicy, role, logger, recorder, aeroCluster,
	); errorUpdate != nil {
		recorder.Eventf(
			aeroCluster, corev1.EventTypeWarning, "RoleUpdateFailed",
			"Failed to Update Role %s", roleCreate.name,
		)

		return errorUpdate
	}

	return nil
}

// createRole creates a new Aerospike role.
func (roleCreate aerospikeRoleCreateUpdate) createRole(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	logger.Info("Creating role", "role name", roleCreate.name)

	aerospikePrivileges, err := privilegeStringToAerospikePrivilege(roleCreate.privileges)
	if err != nil {
		return fmt.Errorf("could not create role %s: %v", roleCreate.name, err)
	}

	if err = client.CreateRole(
		adminPolicy, roleCreate.name, aerospikePrivileges, roleCreate.whitelist,
		roleCreate.readQuota, roleCreate.writeQuota,
	); err != nil {
		return fmt.Errorf("could not create role %s: %v", roleCreate.name, err)
	}

	logger.Info("Created role", "role name", roleCreate.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "RoleCreated",
		"Created Role %s", roleCreate.name,
	)

	return nil
}

// updateRole updates an existing Aerospike role.
func (roleCreate aerospikeRoleCreateUpdate) updateRole(
	client *as.Client, adminPolicy *as.AdminPolicy, role *as.Role,
	logger logger, recorder record.EventRecorder,
	aeroCluster *asdbv1.AerospikeCluster,
) error {
	// Update the role.
	logger.Info("Updating role", "role name", roleCreate.name)

	// Find the privileges to drop.
	currentPrivileges, err := AerospikePrivilegeToPrivilegeString(role.Privileges)
	if err != nil {
		return fmt.Errorf("could not update role %s: %v", roleCreate.name, err)
	}

	desiredPrivileges := roleCreate.privileges
	privilegesToRevoke := SliceSubtract(currentPrivileges, desiredPrivileges)
	privilegesToGrant := SliceSubtract(desiredPrivileges, currentPrivileges)

	if len(privilegesToRevoke) > 0 {
		aerospikePrivileges, err := privilegeStringToAerospikePrivilege(privilegesToRevoke)
		if err != nil {
			return fmt.Errorf(
				"could not update role %s: %v", roleCreate.name, err,
			)
		}

		if err := client.RevokePrivileges(
			adminPolicy, roleCreate.name, aerospikePrivileges,
		); err != nil {
			return fmt.Errorf(
				"error revoking privileges for role %s: %v", roleCreate.name,
				err,
			)
		}

		logger.Info(
			"Revoked privileges for role", "role name", roleCreate.name,
			"privileges", privilegesToRevoke,
		)
	}

	if len(privilegesToGrant) > 0 {
		aerospikePrivileges, err := privilegeStringToAerospikePrivilege(privilegesToGrant)
		if err != nil {
			return fmt.Errorf(
				"could not update role %s: %v", roleCreate.name, err,
			)
		}

		if err := client.GrantPrivileges(
			adminPolicy, roleCreate.name, aerospikePrivileges,
		); err != nil {
			return fmt.Errorf(
				"error granting privileges for role %s: %v", roleCreate.name,
				err,
			)
		}

		logger.Info(
			"Granted privileges to role", "role name", roleCreate.name,
			"privileges", privilegesToGrant,
		)
	}

	if !reflect.DeepEqual(role.Whitelist, roleCreate.whitelist) {
		// Set whitelist.
		if err := client.SetWhitelist(
			adminPolicy, roleCreate.name, roleCreate.whitelist,
		); err != nil {
			return fmt.Errorf(
				"error setting whitelist for role %s: %v", roleCreate.name, err,
			)
		}
	}

	logger.Info("Updated role", "role name", roleCreate.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "RoleUpdated",
		"Updated Role %s", roleCreate.name,
	)

	return nil
}

// aerospikeUserCreateUpdate creates or updates an Aerospike user.
type aerospikeUserCreateUpdate struct {
	// The user's name.
	name string

	// The password to set. Required for create. Optional for update.
	password *string

	// The roles to set for the user. These roles and only these roles will be granted to the user after this operation.
	roles []string
}

// Execute creates a new Aerospike user or updates an existing one.
func (userCreate aerospikeUserCreateUpdate) execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	user, err := client.QueryUser(adminPolicy, userCreate.name)
	isCreate := false

	if err != nil {
		if strings.Contains(err.Error(), userNotFoundErr) {
			isCreate = true
		} else {
			// Failure to query for the user.
			return fmt.Errorf(
				"error querying user %s: %v", userCreate.name, err,
			)
		}
	}

	if isCreate {
		err := userCreate.createUser(client, adminPolicy, logger, recorder, aeroCluster)
		if err != nil {
			recorder.Eventf(
				aeroCluster, corev1.EventTypeWarning, "UserCreateFailed",
				"Failed to Create User %s", userCreate.name,
			)
		}

		return err
	}

	if errorUpdate := userCreate.updateUser(
		client, adminPolicy, user, logger, recorder, aeroCluster,
	); errorUpdate != nil {
		recorder.Eventf(
			aeroCluster, corev1.EventTypeWarning, "UserUpdateFailed",
			"Failed to Update User %s", userCreate.name,
		)

		return errorUpdate
	}

	return nil
}

// createUser creates a new Aerospike user.
func (userCreate aerospikeUserCreateUpdate) createUser(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	logger.Info("Creating user", "username", userCreate.name)

	if userCreate.password == nil {
		return fmt.Errorf(
			"error creating user %s. Password not specified", userCreate.name,
		)
	}

	if err := client.CreateUser(
		adminPolicy, userCreate.name, *userCreate.password, userCreate.roles,
	); err != nil {
		return fmt.Errorf("could not create user %s: %v", userCreate.name, err)
	}

	logger.Info("Created user", "username", userCreate.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "UserCreated",
		"Created User %s", userCreate.name,
	)

	return nil
}

// updateUser updates an existing Aerospike user.
func (userCreate aerospikeUserCreateUpdate) updateUser(
	client *as.Client, adminPolicy *as.AdminPolicy, user *as.UserRoles,
	logger logger, recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	// Update the user.
	logger.Info("Updating user", "username", userCreate.name)

	if userCreate.password != nil {
		logger.Info("Updating password for user", "username", userCreate.name)

		if err := client.ChangePassword(
			adminPolicy, userCreate.name, *userCreate.password,
		); err != nil {
			return fmt.Errorf(
				"error updating password for user %s: %v", userCreate.name, err,
			)
		}

		logger.Info("Updated password for user", "username", userCreate.name)
	}

	// Find the roles to grant and revoke.
	currentRoles := user.Roles
	desiredRoles := userCreate.roles
	rolesToRevoke := SliceSubtract(currentRoles, desiredRoles)
	rolesToGrant := SliceSubtract(desiredRoles, currentRoles)

	if len(rolesToRevoke) > 0 {
		if err := client.RevokeRoles(adminPolicy, userCreate.name, rolesToRevoke); err != nil {
			return fmt.Errorf(
				"error revoking roles for user %s: %v", userCreate.name, err,
			)
		}

		logger.Info(
			"Revoked roles for user", "username", userCreate.name, "roles",
			rolesToRevoke,
		)
	}

	if len(rolesToGrant) > 0 {
		if err := client.GrantRoles(adminPolicy, userCreate.name, rolesToGrant); err != nil {
			return fmt.Errorf(
				"error granting roles for user %s: %v", userCreate.name, err,
			)
		}

		logger.Info(
			"Granted roles to user", "username", userCreate.name, "roles",
			rolesToGrant,
		)
	}

	logger.Info("Updated user", "username", userCreate.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "UserUpdated",
		"Updated User %s", userCreate.name,
	)

	return nil
}

// aerospikeUserDrop drops an Aerospike user.
type aerospikeUserDrop struct {
	// The user's name.
	name string
}

// Execute implements dropping the user.
func (userDrop aerospikeUserDrop) execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	logger.Info("Dropping user", "username", userDrop.name)

	if err := client.DropUser(adminPolicy, userDrop.name); err != nil {
		if !strings.Contains(err.Error(), userNotFoundErr) {
			// Failure to drop for the user.
			return fmt.Errorf("error dropping user %s: %v", userDrop.name, err)
		}
	}

	logger.Info("Dropped user", "username", userDrop.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "UserDeleted",
		"Dropped User %s", userDrop.name,
	)

	return nil
}

// aerospikeRoleDrop drops an Aerospike role.
type aerospikeRoleDrop struct {
	// The role's name.
	name string
}

// Execute implements dropping the role.
func (roleDrop aerospikeRoleDrop) execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger logger,
	recorder record.EventRecorder, aeroCluster *asdbv1.AerospikeCluster,
) error {
	logger.Info("Dropping role", "role", roleDrop.name)

	if err := client.DropRole(adminPolicy, roleDrop.name); err != nil {
		if !strings.Contains(err.Error(), roleNotFoundErr) {
			// Failure to drop for the role.
			return fmt.Errorf("error dropping role %s: %v", roleDrop.name, err)
		}
	}

	logger.Info("Dropped role", "role", roleDrop.name)
	recorder.Eventf(
		aeroCluster, corev1.EventTypeNormal, "RoleDeleted",
		"Dropped Role %s", roleDrop.name,
	)

	return nil
}

// SliceSubtract removes elements of slice2 from slice1 and returns the result.
func SliceSubtract(slice1, slice2 []string) []string {
	var result []string

	for _, s1 := range slice1 {
		found := false

		for _, toSubtract := range slice2 {
			if s1 == toSubtract {
				found = true
				break
			}
		}

		if !found {
			// s1 not found. Should be retained.
			result = append(result, s1)
		}
	}

	return result
}
