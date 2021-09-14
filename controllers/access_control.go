package controllers

// Aerospike access control reconciliation of access control.

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	"github.com/go-logr/logr"
)

// Logger type alias.
type Logger = logr.Logger

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
	desiredState *asdbv1beta1.AerospikeClusterSpec,
	currentState *asdbv1beta1.AerospikeClusterSpec,
	passwordProvider AerospikeUserPasswordProvider,
) (string, string, error) {
	enabled, err := asdbv1beta1.IsSecurityEnabled(currentState.AerospikeConfig)
	if err != nil {
		// Its possible this is a new cluster and current state is empty.
		enabled, err = asdbv1beta1.IsSecurityEnabled(desiredState.AerospikeConfig)

		if err != nil {
			return "", "", err
		}
	}

	if !enabled {
		// Return zero strings if this is not a security enabled cluster.
		return "", "", nil
	}

	if currentState.AerospikeAccessControl == nil {
		// We haven't yet set up access control. Use default password.
		return asdbv1beta1.AdminUsername, asdbv1beta1.DefaultAdminPAssword, nil
	}

	adminUserSpec, ok := asdbv1beta1.GetUsersFromSpec(currentState)[asdbv1beta1.AdminUsername]

	if !ok {
		// Should not happen on a validated spec.
		return "", "", fmt.Errorf(
			"%s user missing in access control", asdbv1beta1.AdminUsername,
		)
	}

	password, err := passwordProvider.Get(
		asdbv1beta1.AdminUsername, &adminUserSpec,
	)

	if err != nil {
		return "", "", err
	}

	return asdbv1beta1.AdminUsername, password, nil
}

// ReconcileAccessControl reconciles access control to ensure current state moves to the desired state.
func ReconcileAccessControl(
	desired *asdbv1beta1.AerospikeClusterSpec, client *as.Client,
	passwordProvider AerospikeUserPasswordProvider, logger Logger,
) error {
	// Get admin policy based in desired state so that new timeout updates can be applied. It is safe.
	adminPolicy := GetAdminPolicy(desired)

	desiredRoles := asdbv1beta1.GetRolesFromSpec(desired)
	err := reconcileRoles(desiredRoles, client, adminPolicy, logger)
	if err != nil {
		return err
	}

	desiredUsers := asdbv1beta1.GetUsersFromSpec(desired)
	err = reconcileUsers(
		desiredUsers, passwordProvider, client, adminPolicy, logger,
	)
	return err
}

// GetAdminPolicy returns the AdminPolicy to use for performing access control operations.
func GetAdminPolicy(clusterSpec *asdbv1beta1.AerospikeClusterSpec) as.AdminPolicy {
	if clusterSpec.AerospikeAccessControl == nil || clusterSpec.AerospikeAccessControl.AdminPolicy == nil {
		return *as.NewAdminPolicy()
	}

	specAdminPolicy := *clusterSpec.AerospikeAccessControl.AdminPolicy
	return as.AdminPolicy{Timeout: time.Duration(specAdminPolicy.Timeout) * time.Millisecond}
}

// reconcileRoles reconciles roles to take them from current to desired.
func reconcileRoles(
	desired map[string]asdbv1beta1.AerospikeRoleSpec, client *as.Client,
	adminPolicy as.AdminPolicy, logger Logger,
) error {
	var err error

	// Get list of existing roles from the cluster.
	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying roles: %v", err)
	}

	var currentRoleNames []string // List roles in the cluster.
	for _, role := range asRoles {
		currentRoleNames = append(currentRoleNames, role.Name)
	}

	var requiredRoleNames []string

	// List roles needed in the desired list.
	for roleName := range desired {
		requiredRoleNames = append(requiredRoleNames, roleName)
	}

	var roleReconcileCmds []AerospikeAccessControlReconcileCmd

	// Create a list of role commands to drop.
	rolesToDrop := SliceSubtract(currentRoleNames, requiredRoleNames)

	for _, roleToDrop := range rolesToDrop {
		_, ok := asdbv1beta1.PredefinedRoles[roleToDrop]

		if !ok {
			// Not a predefined role and can be dropped.
			roleReconcileCmds = append(
				roleReconcileCmds, AerospikeRoleDrop{name: roleToDrop},
			)
		}
	}

	for roleName, roleSpec := range desired {
		roleReconcileCmds = append(
			roleReconcileCmds, AerospikeRoleCreateUpdate{
				name: roleName, privileges: roleSpec.Privileges,
				whitelist: roleSpec.Whitelist, readQuota: roleSpec.ReadQuota,
				writeQuota: roleSpec.WriteQuota,
			},
		)
	}

	// Execute all commands.
	for _, cmd := range roleReconcileCmds {
		err = cmd.Execute(client, &adminPolicy, logger)

		if err != nil {
			return err
		}
	}

	return nil
}

// reconcileUsers reconciles users to take them from current to desired.
func reconcileUsers(
	desired map[string]asdbv1beta1.AerospikeUserSpec,
	passwordProvider AerospikeUserPasswordProvider, client *as.Client,
	adminPolicy as.AdminPolicy, logger Logger,
) error {
	var err error

	// Get list of existing users from the cluster.
	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying users: %v", err)
	}

	var currentUserNames []string

	// List users in the cluster.
	for _, user := range asUsers {
		currentUserNames = append(currentUserNames, user.User)
	}

	var requiredUserNames []string

	// List users needed in the desired list.
	for userName := range desired {
		requiredUserNames = append(requiredUserNames, userName)
	}

	var userReconcileCmds []AerospikeAccessControlReconcileCmd

	// Create a list of user commands to drop.
	usersToDrop := SliceSubtract(currentUserNames, requiredUserNames)

	for _, userToDrop := range usersToDrop {
		userReconcileCmds = append(
			userReconcileCmds, AerospikeUserDrop{name: userToDrop},
		)
	}

	// Admin user update command should be executed last to ensure admin password
	// update does not disrupt reconciliation.
	var adminUpdateCmd *AerospikeUserCreateUpdate = nil
	for userName, userSpec := range desired {
		password, err := passwordProvider.Get(userName, &userSpec)
		if err != nil {
			return err
		}

		cmd := AerospikeUserCreateUpdate{
			name: userName, password: &password, roles: userSpec.Roles,
		}
		if userName == asdbv1beta1.AdminUsername {
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
		err = cmd.Execute(client, &adminPolicy, logger)

		if err != nil {
			return err
		}
	}

	return nil
}

// privilegeStringToAerospikePrivilege converts privilegeString to an Aerospike privilege.
func privilegeStringToAerospikePrivilege(privilegeStrings []string) (
	[]as.Privilege, error,
) {
	var aerospikePrivileges []as.Privilege

	for _, privilege := range privilegeStrings {
		parts := strings.Split(privilege, ".")

		_, ok := asdbv1beta1.Privileges[parts[0]]
		if !ok {
			// First part of the privilege is not part of defined privileges.
			return nil, fmt.Errorf("invalid privilege %s", privilege)
		}

		nParts := len(parts)
		privilegeCode := parts[0]
		namespaceName := ""
		setName := ""
		switch nParts {
		case 2:
			namespaceName = parts[1]

		case 3:
			namespaceName = parts[1]
			setName = parts[2]
		}

		var code = as.Read
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
	var privileges []string
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
	Get(username string, userSpec *asdbv1beta1.AerospikeUserSpec) (
		string, error,
	)
}

// AerospikeAccessControlReconcileCmd commands needed to Reconcile a single access control entry,
// for example a role or a user.
type AerospikeAccessControlReconcileCmd interface {
	// Execute executes the command. The implementation should be idempotent.
	Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error
}

// AerospikeRoleCreateUpdate creates or updates an Aerospike role.
type AerospikeRoleCreateUpdate struct {
	// The role's name.
	name string

	// The privileges to set for the role. These privileges and only these privileges will be granted to the role after this operation.
	privileges []string

	// The whitelist to set for the role. These whitelist addresses and only these whitelist addresses will be granted to the role after this operation.
	whitelist []string

	// The readQuota specifies the read query rate that is permitted for the current role.
	readQuota uint32

	// The writeQuota specifies the write rate that is permitted for the current role.
	writeQuota uint32
}

// Execute creates a new Aerospike role or updates an existing one.
func (roleCreate AerospikeRoleCreateUpdate) Execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
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
		return roleCreate.createRole(client, adminPolicy, logger)
	}

	return roleCreate.updateRole(client, adminPolicy, role, logger)
}

// createRole creates a new Aerospike role.
func (roleCreate AerospikeRoleCreateUpdate) createRole(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
) error {
	logger.Info("Creating role", "role name", roleCreate.name)

	aerospikePrivileges, err := privilegeStringToAerospikePrivilege(roleCreate.privileges)
	if err != nil {
		return fmt.Errorf("could not create role %s: %v", roleCreate.name, err)
	}

	err = client.CreateRole(
		adminPolicy, roleCreate.name, aerospikePrivileges, roleCreate.whitelist,
		roleCreate.readQuota, roleCreate.writeQuota,
	)
	if err != nil {
		return fmt.Errorf("could not create role %s: %v", roleCreate.name, err)
	}
	logger.Info("Created role", "role name", roleCreate.name)

	return nil
}

// updateRole updates an existing Aerospike role.
func (roleCreate AerospikeRoleCreateUpdate) updateRole(
	client *as.Client, adminPolicy *as.AdminPolicy, role *as.Role,
	logger Logger,
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

		err = client.RevokePrivileges(
			adminPolicy, roleCreate.name, aerospikePrivileges,
		)

		if err != nil {
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

		err = client.GrantPrivileges(
			adminPolicy, roleCreate.name, aerospikePrivileges,
		)

		if err != nil {
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
		err = client.SetWhitelist(
			adminPolicy, roleCreate.name, roleCreate.whitelist,
		)

		if err != nil {
			return fmt.Errorf(
				"error setting whitelist for role %s: %v", roleCreate.name, err,
			)
		}

	}

	logger.Info("Updated role", "role name", roleCreate.name)
	return nil
}

// AerospikeUserCreateUpdate creates or updates an Aerospike user.
type AerospikeUserCreateUpdate struct {
	// The user's name.
	name string

	// The password to set. Required for create. Optional for update.
	password *string

	// The roles to set for the user. These roles and only these roles will be granted to the user after this operation.
	roles []string
}

// Execute creates a new Aerospike user or updates an existing one.
func (userCreate AerospikeUserCreateUpdate) Execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
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
		return userCreate.createUser(client, adminPolicy, logger)
	}

	return userCreate.updateUser(client, adminPolicy, user, logger)
}

// createUser creates a new Aerospike user.
func (userCreate AerospikeUserCreateUpdate) createUser(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
) error {
	logger.Info("Creating user", "username", userCreate.name)
	if userCreate.password == nil {
		return fmt.Errorf(
			"error creating user %s. Password not specified", userCreate.name,
		)
	}

	err := client.CreateUser(
		adminPolicy, userCreate.name, *userCreate.password, userCreate.roles,
	)
	if err != nil {
		return fmt.Errorf("could not create user %s: %v", userCreate.name, err)
	}
	logger.Info("Created user", "username", userCreate.name)

	return nil
}

// updateUser updates an existing Aerospike user.
func (userCreate AerospikeUserCreateUpdate) updateUser(
	client *as.Client, adminPolicy *as.AdminPolicy, user *as.UserRoles,
	logger Logger,
) error {
	// Update the user.
	logger.Info("Updating user", "username", userCreate.name)
	if userCreate.password != nil {
		logger.Info("Updating password for user", "username", userCreate.name)
		err := client.ChangePassword(
			adminPolicy, userCreate.name, *userCreate.password,
		)
		if err != nil {
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
		err := client.RevokeRoles(adminPolicy, userCreate.name, rolesToRevoke)

		if err != nil {
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
		err := client.GrantRoles(adminPolicy, userCreate.name, rolesToGrant)

		if err != nil {
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
	return nil
}

// AerospikeUserDrop drops an Aerospike user.
type AerospikeUserDrop struct {
	// The user's name.
	name string
}

// Execute implements dropping the user.
func (userDrop AerospikeUserDrop) Execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
) error {
	logger.Info("Dropping user", "username", userDrop.name)
	err := client.DropUser(adminPolicy, userDrop.name)

	if err != nil {
		if !strings.Contains(err.Error(), userNotFoundErr) {
			// Failure to drop for the user.
			return fmt.Errorf("error dropping user %s: %v", userDrop.name, err)
		}
	}

	logger.Info("Dropped user", "username", userDrop.name)
	return nil
}

// AerospikeRoleDrop drops an Aerospike role.
type AerospikeRoleDrop struct {
	// The role's name.
	name string
}

// Execute implements dropping the role.
func (roleDrop AerospikeRoleDrop) Execute(
	client *as.Client, adminPolicy *as.AdminPolicy, logger Logger,
) error {
	logger.Info("Dropping role", "role", roleDrop.name)
	err := client.DropRole(adminPolicy, roleDrop.name)

	if err != nil {
		if !strings.Contains(err.Error(), roleNotFoundErr) {
			// Failure to drop for the role.
			return fmt.Errorf("error dropping role %s: %v", roleDrop.name, err)
		}
	}

	logger.Info("Dropped role", "role", roleDrop.name)
	return nil
}

// SliceSubtract removes elements of slice2 from slice1 and returns the result.
func SliceSubtract(slice1 []string, slice2 []string) []string {
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
