package asconfig

// Aerospike access control reconciliation of access control.

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	as "github.com/aerospike/aerospike-client-go"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
)

// AerospikeAdminCredentials to use for aerospike clients.
//
// Returns a tuple of admin username and password to use. If the cluster is not security
// enabled both username and password will be zero strings.
func AerospikeAdminCredentials(desiredState *aerospikev1alpha1.AerospikeClusterSpec, currentState *aerospikev1alpha1.AerospikeClusterSpec, passwordProvider AerospikeUserPasswordProvider) (string, string, error) {
	enabled, err := isSecurityEnabled(currentState)
	if err != nil {
		// Its possible this is a new cluster and current state is empty.
		enabled, err = isSecurityEnabled(desiredState)

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
		return adminUsername, defaultAdminPAssword, nil
	}

	adminUserSpec := currentState.AerospikeAccessControl.Users[adminUsername]
	password, err := passwordProvider.Get(adminUsername, &adminUserSpec)

	if err != nil {
		return "", "", err
	}

	return adminUsername, password, nil
}

// ReconcileAccessControl reconciles access control to ensure current state moves to the desired state.
func ReconcileAccessControl(desired *aerospikev1alpha1.AerospikeClusterSpec, current *aerospikev1alpha1.AerospikeClusterSpec, client *as.Client, passwordProvider AerospikeUserPasswordProvider, logger Logger) error {
	// Get admin policy based in desired state so that new timeout updates can be applied. It is safe.
	adminPolicy := getAdminPolicy(desired)

	desiredRoles := getRolesFromSpec(desired)
	currentRoles := getRolesFromSpec(current)
	err := reconcileRoles(desiredRoles, currentRoles, client, adminPolicy, logger)
	if err != nil {
		return err
	}

	desiredUsers := getUsersFromSpec(desired)
	currentUsers := getUsersFromSpec(current)
	err = reconcileUsers(desiredUsers, currentUsers, passwordProvider, client, adminPolicy, logger)
	return err
}

// getRolesFromSpec returns roles or an empty map from the spec.
func getRolesFromSpec(spec *aerospikev1alpha1.AerospikeClusterSpec) map[string]aerospikev1alpha1.AerospikeRoleSpec {
	var roles map[string]aerospikev1alpha1.AerospikeRoleSpec
	if spec.AerospikeAccessControl != nil {
		roles = spec.AerospikeAccessControl.Roles
	} else {
		roles = map[string]aerospikev1alpha1.AerospikeRoleSpec{}
	}

	return roles
}

// getUsersFromSpec returns users or an empty map from the spec.
func getUsersFromSpec(spec *aerospikev1alpha1.AerospikeClusterSpec) map[string]aerospikev1alpha1.AerospikeUserSpec {
	var users map[string]aerospikev1alpha1.AerospikeUserSpec
	if spec.AerospikeAccessControl != nil {
		users = spec.AerospikeAccessControl.Users
	} else {
		users = map[string]aerospikev1alpha1.AerospikeUserSpec{}
	}

	return users
}

// getAdminPolicy returns the AdminPolicy to use for performing access control operations.
func getAdminPolicy(clusterSpec *aerospikev1alpha1.AerospikeClusterSpec) as.AdminPolicy {
	if clusterSpec.AerospikeAccessControl == nil || clusterSpec.AerospikeAccessControl.AdminPolicy == nil {
		return *as.NewAdminPolicy()
	}

	specAdminPolicy := *clusterSpec.AerospikeAccessControl.AdminPolicy
	return as.AdminPolicy{Timeout: time.Duration(specAdminPolicy.Timeout) * time.Millisecond}
}

// reconcileRoles reconciles roles to take them from current to desired.
func reconcileRoles(desired map[string]aerospikev1alpha1.AerospikeRoleSpec, current map[string]aerospikev1alpha1.AerospikeRoleSpec, client *as.Client, adminPolicy as.AdminPolicy, logger Logger) error {
	// Get list of existing roles from the cluster.
	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("Error querying roles: %v", err)
	}

	currentRoleNames := []string{}

	// List roles in the cluster.
	for _, role := range asRoles {
		currentRoleNames = append(currentRoleNames, role.Name)
	}

	requiredRoleNames := []string{}

	// List roles needed in the desired list.
	for roleName := range desired {
		requiredRoleNames = append(requiredRoleNames, roleName)
	}

	roleReconcileCmds := []AerospikeAccessControlReconcileCmd{}

	// Create a list of role commands to drop.
	rolesToDrop := sliceSubtract(currentRoleNames, requiredRoleNames)

	for _, roleToDrop := range rolesToDrop {
		_, ok := predefinedRoles[roleToDrop]

		if !ok {
			// Not a predefined role and can be dropped.
			roleReconcileCmds = append(roleReconcileCmds, AerospikeRoleDrop{name: roleToDrop})
		}
	}

	for roleName, roleSpec := range desired {
		roleReconcileCmds = append(roleReconcileCmds, AerospikeRoleCreateUpdate{name: roleName, privileges: roleSpec.Privileges})
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
func reconcileUsers(desired map[string]aerospikev1alpha1.AerospikeUserSpec, current map[string]aerospikev1alpha1.AerospikeUserSpec, passwordProvider AerospikeUserPasswordProvider, client *as.Client, adminPolicy as.AdminPolicy, logger Logger) error {
	// Get list of existing users from the cluster.
	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		return fmt.Errorf("Error querying users: %v", err)
	}

	currentUserNames := []string{}

	// List nodes in the cluster.
	for _, user := range asUsers {
		currentUserNames = append(currentUserNames, user.User)
	}

	requiredUserNames := []string{}

	// List users needed in the desired list.
	for userName := range desired {
		requiredUserNames = append(requiredUserNames, userName)
	}

	userReconcileCmds := []AerospikeAccessControlReconcileCmd{}

	// Create a list of user commands to drop.
	usersToDrop := sliceSubtract(currentUserNames, requiredUserNames)

	for _, userToDrop := range usersToDrop {
		userReconcileCmds = append(userReconcileCmds, AerospikeUserDrop{name: userToDrop})
	}

	// Admin user update command should be execute last to ensure admin password
	// update does not disrupt reconciliation.
	var adminUpdateCmd *AerospikeUserCreateUpdate = nil
	for userName, userSpec := range desired {
		password, err := passwordProvider.Get(userName, &userSpec)
		if err != nil {
			return err
		}

		cmd := AerospikeUserCreateUpdate{name: userName, password: &password, roles: userSpec.Roles}
		if userName == adminUsername {
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

// privilegeStringtoAerospikePrivilege converts privilegeString to an Aerospike privilege.
func privilegeStringtoAerospikePrivilege(privilegeStrings []string) ([]as.Privilege, error) {
	aerospikePrivileges := []as.Privilege{}

	for _, privilege := range privilegeStrings {
		parts := strings.Split(privilege, ".")

		_, ok := privileges[parts[0]]
		if !ok {
			// First part of the privilege is not part of defined privileges.
			return nil, fmt.Errorf("Invalid privilege %s", privilege)
		}

		nParts := len(parts)
		privilegeCode := parts[0]
		namespaceName := ""
		setName := ""
		switch nParts {
		case 2:
			namespaceName = parts[1]
			break

		case 3:
			namespaceName = parts[1]
			setName = parts[2]
			break
		}

		var code = as.Read
		switch privilegeCode {
		case "read":
			code = as.Read
			break

		case "write":
			code = as.Write
			break

		case "read-write":
			code = as.ReadWrite
			break

		case "read-write-udf":
			code = as.ReadWriteUDF
			break

		case "data-admin":
			code = as.DataAdmin
			break

		case "sys-admin":
			code = as.SysAdmin
			break

		case "user-admin":
			code = as.UserAdmin
			break

		default:
			return nil, fmt.Errorf("Unknown privilege %s", privilegeCode)

		}

		aerospikePrivilege := as.Privilege{Code: code, Namespace: namespaceName, SetName: setName}
		aerospikePrivileges = append(aerospikePrivileges, aerospikePrivilege)

	}

	return aerospikePrivileges, nil
}

// aerospikePrivilegeToPrivilegeString converts aerospikePrivilege to controller spec privilege string.
func aerospikePrivilegeToPrivilegeString(aerospikePrivileges []as.Privilege) ([]string, error) {
	privileges := []string{}
	for _, aerospikePrivilege := range aerospikePrivileges {
		var buffer bytes.Buffer

		switch aerospikePrivilege.Code {
		case as.Read:
			buffer.WriteString("read")
			break

		case as.Write:
			buffer.WriteString("write")
			break

		case as.ReadWrite:
			buffer.WriteString("read-write")
			break

		case as.ReadWriteUDF:
			buffer.WriteString("read-write-udf")
			break

		case as.DataAdmin:
			buffer.WriteString("data-admin")
			break

		case as.SysAdmin:
			buffer.WriteString("sys-admin")
			break

		case as.UserAdmin:
			buffer.WriteString("user-admin")
			break

		default:
			return nil, fmt.Errorf("Unknown privilege code %v", aerospikePrivilege.Code)
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
	// Return the password for username.
	Get(username string, userSpec *aerospikev1alpha1.AerospikeUserSpec) (string, error)
}

// AerospikeAccessControlReconcileCmd commands neeeded to reconcile a single access control entiry,
// for example a role or a user.
type AerospikeAccessControlReconcileCmd interface {
	// Execute executes the command. The implementation should be idempotent.
	Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error
}

// AerospikeRoleCreateUpdate creates or updates an Aerospike role.
// TODO: Deal with whitelist as well when go client support it.
type AerospikeRoleCreateUpdate struct {
	// The role's name.
	name string

	// The privileges to set for the role. These privileges and only these privileges will be granted to the role after this operation.
	privileges []string
}

// Execute creates a new Aerospike role or updates an existing one.
func (roleCreate AerospikeRoleCreateUpdate) Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	role, err := client.QueryRole(adminPolicy, roleCreate.name)
	isCreate := false

	if err != nil {
		if strings.Contains(err.Error(), roleNotFoundErr) {
			isCreate = true
		} else {
			// Failure to query for the role.
			return fmt.Errorf("Error querying role %s", roleCreate.name)
		}
	}

	if isCreate {
		return roleCreate.createRole(client, adminPolicy, logger)
	}

	return roleCreate.updateRole(client, adminPolicy, role, logger)
}

// createRole creates a new Aerospike role.
func (roleCreate AerospikeRoleCreateUpdate) createRole(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	logger.Info("Creating role", log.Ctx{"rolename": roleCreate.name})

	aerospikePrivileges, err := privilegeStringtoAerospikePrivilege(roleCreate.privileges)
	if err != nil {
		return fmt.Errorf("Could not create role %s: %v", roleCreate.name, err)
	}

	err = client.CreateRole(adminPolicy, roleCreate.name, aerospikePrivileges)
	if err != nil {
		return fmt.Errorf("Could not create role %s: %v", roleCreate.name, err)
	}
	logger.Info("Created role", log.Ctx{"rolename": roleCreate.name})

	return nil
}

// updateRole updates an existing Aerospike role.
func (roleCreate AerospikeRoleCreateUpdate) updateRole(client *as.Client, adminPolicy *as.AdminPolicy, role *as.Role, logger Logger) error {
	// Update the role.
	logger.Info("Updating role", log.Ctx{"rolename": roleCreate.name})

	// Find the privileges to drop.
	currentPrivileges, err := aerospikePrivilegeToPrivilegeString(role.Privileges)
	if err != nil {
		return fmt.Errorf("Could not update role %s: %v", roleCreate.name, err)
	}

	desiredPrivileges := roleCreate.privileges
	privilegesToRevoke := sliceSubtract(currentPrivileges, desiredPrivileges)
	privilegesToGrant := sliceSubtract(desiredPrivileges, currentPrivileges)

	if len(privilegesToRevoke) > 0 {
		aerospikePrivileges, err := privilegeStringtoAerospikePrivilege(privilegesToRevoke)
		if err != nil {
			return fmt.Errorf("Could not update role %s: %v", roleCreate.name, err)
		}

		err = client.RevokePrivileges(adminPolicy, roleCreate.name, aerospikePrivileges)

		if err != nil {
			return fmt.Errorf("Error revoking privileges for role %s", roleCreate.name)
		}

		logger.Info("Revoked privileges for role", log.Ctx{"rolename": roleCreate.name, "privileges": privilegesToRevoke})
	}

	if len(privilegesToGrant) > 0 {
		aerospikePrivileges, err := privilegeStringtoAerospikePrivilege(privilegesToGrant)
		if err != nil {
			return fmt.Errorf("Could not update role %s: %v", roleCreate.name, err)
		}

		err = client.GrantPrivileges(adminPolicy, roleCreate.name, aerospikePrivileges)

		if err != nil {
			return fmt.Errorf("Error granting privileges for role %s", roleCreate.name)
		}

		logger.Info("Granted privileges to role", log.Ctx{"rolename": roleCreate.name, "privileges": privilegesToGrant})
	}

	logger.Info("Updated role", log.Ctx{"rolename": roleCreate.name})
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
func (userCreate AerospikeUserCreateUpdate) Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	user, err := client.QueryUser(adminPolicy, userCreate.name)
	isCreate := false

	if err != nil {
		if strings.Contains(err.Error(), userNotFoundErr) {
			isCreate = true
		} else {
			// Failure to query for the user.
			return fmt.Errorf("Error querying user %s", userCreate.name)
		}
	}

	if isCreate {
		return userCreate.createUser(client, adminPolicy, logger)
	}

	return userCreate.updateUser(client, adminPolicy, user, logger)
}

// createUser creates a new Aerospike user.
func (userCreate AerospikeUserCreateUpdate) createUser(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	logger.Info("Creating user", log.Ctx{"username": userCreate.name})
	if userCreate.password == nil {
		return fmt.Errorf("Error creating user %s. Password not specified", userCreate.name)
	}

	err := client.CreateUser(adminPolicy, userCreate.name, *userCreate.password, userCreate.roles)
	if err != nil {
		return fmt.Errorf("Could not create user %s: %v", userCreate.name, err)
	}
	logger.Info("Created user", log.Ctx{"username": userCreate.name})

	return nil
}

// updateUser updates an existing Aerospike user.
func (userCreate AerospikeUserCreateUpdate) updateUser(client *as.Client, adminPolicy *as.AdminPolicy, user *as.UserRoles, logger Logger) error {
	// Update the user.
	logger.Info("Updating user", log.Ctx{"username": userCreate.name})
	if userCreate.password != nil {
		logger.Info("Updating password for user", log.Ctx{"username": userCreate.name})
		err := client.ChangePassword(adminPolicy, userCreate.name, *userCreate.password)
		if err != nil {
			return fmt.Errorf("Error updating password for user %s: %v", userCreate.name, err)
		}
		logger.Info("Updated password for user", log.Ctx{"username": userCreate.name})
	}

	// Find the roles to drop.
	currentRoles := user.Roles
	desiredRoles := userCreate.roles
	rolesToRevoke := sliceSubtract(currentRoles, desiredRoles)
	rolesToGrant := sliceSubtract(desiredRoles, currentRoles)

	if len(rolesToRevoke) > 0 {
		err := client.RevokeRoles(adminPolicy, userCreate.name, rolesToRevoke)

		if err != nil {
			return fmt.Errorf("Error revoking roles for user %s", userCreate.name)
		}

		logger.Info("Revoked roles for user", log.Ctx{"username": userCreate.name, "roles": rolesToRevoke})
	}

	if len(rolesToGrant) > 0 {
		err := client.GrantRoles(adminPolicy, userCreate.name, rolesToGrant)

		if err != nil {
			return fmt.Errorf("Error granting roles for user %s", userCreate.name)
		}

		logger.Info("Granted roles to user", log.Ctx{"username": userCreate.name, "roles": rolesToGrant})
	}

	logger.Info("Updated user", log.Ctx{"username": userCreate.name})
	return nil
}

// AerospikeUserDrop drops an Aerospike user.
type AerospikeUserDrop struct {
	// The user's name.
	name string
}

// Execute implements dropping the user.
func (userdrop AerospikeUserDrop) Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	logger.Info("Dropping user", log.Ctx{"username": userdrop.name})
	err := client.DropUser(adminPolicy, userdrop.name)

	if err != nil {
		if !strings.Contains(err.Error(), userNotFoundErr) {
			// Failure to drop for the user.
			return fmt.Errorf("Error dropping user %s", userdrop.name)
		}
	}

	logger.Info("Dropped user", log.Ctx{"username": userdrop.name})
	return nil
}

// AerospikeRoleDrop drops an Aerospike role.
type AerospikeRoleDrop struct {
	// The role's name.
	name string
}

// Execute implements dropping the role.
func (roledrop AerospikeRoleDrop) Execute(client *as.Client, adminPolicy *as.AdminPolicy, logger Logger) error {
	logger.Info("Dropping role", log.Ctx{"role": roledrop.name})
	err := client.DropRole(adminPolicy, roledrop.name)

	if err != nil {
		if !strings.Contains(err.Error(), roleNotFoundErr) {
			// Failure to drop for the role.
			return fmt.Errorf("Error dropping role %s", roledrop.name)
		}
	}

	logger.Info("Dropped role", log.Ctx{"role": roledrop.name})
	return nil
}

// sliceSubtract removes elements of slice2 from slice1 and returns the result.
func sliceSubtract(slice1 []string, slice2 []string) []string {
	result := []string{}
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
