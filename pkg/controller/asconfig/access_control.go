package asconfig

//  Aerospike access control functions provides validation and reconciliation of access control.

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"time"

	as "github.com/aerospike/aerospike-client-go"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	log "github.com/inconshreveable/log15"
)

// Logger
type Logger log.Logger

// Access control Privilege scopes.
type PrivilegeScope int

const (
	Global PrivilegeScope = iota
	NamespaceSet
)

const (
	// Maximum length for role name.
	roleNameLengthMax int = 63

	// Maximum allowed length for a username.
	userNameLengthMax int = 63

	// Maximum allowed length for a user password.
	userPasswordLengthMax int = 60

	// Error marker for user not found errors.
	userNotFoundErr = "Invalid user"

	// Error marker for role not found errors.
	roleNotFoundErr = "Invalid role"

	// The admin username.
	adminUsername = "admin"

	// The default admin user password.
	defaultAdminPAssword = "admin"
)

// Chacacters forbidden in role name.
var roleNameForbiddenChars []string = []string{";", ":"}

// Chacacters forbidden in username.
var userNameForbiddenChars []string = []string{";", ":"}

// Predefined roles names.
var predefinedRoles = map[string]struct{}{
	"user-admin":     struct{}{},
	"sys-admin":      struct{}{},
	"data-admin":     struct{}{},
	"read":           struct{}{},
	"read-write":     struct{}{},
	"read-write-udf": struct{}{},
	"write":          struct{}{},
}

// Expect at least one user with these required roles.
var requiredRoles = []string{
	"sys-admin",
	"user-admin",
}

// Privilege string allowed in the spec and associated scopes.
var privileges = map[string][]PrivilegeScope{
	"read":           []PrivilegeScope{Global, NamespaceSet},
	"write":          []PrivilegeScope{Global, NamespaceSet},
	"read-write":     []PrivilegeScope{Global, NamespaceSet},
	"read-write-udf": []PrivilegeScope{Global, NamespaceSet},
	"data-admin":     []PrivilegeScope{Global},
	"sys-admin":      []PrivilegeScope{Global},
	"user-admin":     []PrivilegeScope{Global},
}

// ValidateAerospikeAccessControlSpec validates the accessControl speciication in the clusterSpec.
//
// Asserts that the Aerospikeaccesscontrolspec
//    has correct references to other objects like namespaces
//    follows rules defined https://www.aerospike.com/docs/guide/limitations.html
//    follows rules found through server code inspection for e.g. predefined roles
//    meets operator requirements. For e.g. the necessity to have at least one sys-admin and user-admin user.
func IsAerospikeAccessControlValid(aerospikeCluster *aerospikev1alpha1.AerospikeClusterSpec) (bool, error) {
	enabled, err := isSecurityEnabled(aerospikeCluster)
	if err != nil {
		return false, err
	}

	if !enabled && aerospikeCluster.AerospikeAccessControl != nil {
		// Security is disabled however access control is specified.
		return false, fmt.Errorf("Security is disabled but access control is specified")
	}

	if !enabled {
		return true, nil
	}

	// Validate roles.
	_, err = isRoleSpecValid(aerospikeCluster.AerospikeAccessControl.Roles, aerospikeCluster.AerospikeConfig)
	if err != nil {
		return false, err
	}

	// Validate users.
	_, err = isUserSpecValid(aerospikeCluster.AerospikeAccessControl.Users, aerospikeCluster.AerospikeAccessControl.Roles)

	if err != nil {
		return false, err
	}

	return true, nil
}

// isSecurityEnabled indicates if clusterSpec has security enabled.
func isSecurityEnabled(aerospikeCluster *aerospikev1alpha1.AerospikeClusterSpec) (bool, error) {
	if len(aerospikeCluster.AerospikeConfig) == 0 {
		return false, fmt.Errorf("Missing aerospike configuration in cluster state")
	}

	enabled, err := utils.IsSecurityEnabled(aerospikeCluster.AerospikeConfig)

	if err != nil {
		return false, fmt.Errorf("Failed to get cluster security status: %v", err)
	}

	return enabled, err
}

// isRoleSpecValid indicates if input role spec is valid.
func isRoleSpecValid(roles map[string]aerospikev1alpha1.AerospikeRoleSpec, aerospikeConfig aerospikev1alpha1.Values) (bool, error) {
	for roleName, roleSpec := range roles {
		_, ok := predefinedRoles[roleName]
		if ok {
			// Cannot modify or add predefined roles.
			return false, fmt.Errorf("Cannot create or modify predefined role: %s", roleName)
		}

		_, err := isRoleNameValid(roleName)

		if err != nil {
			return false, err
		}

		// Validate privileges.
		for _, privilege := range roleSpec.Privileges {
			_, err = isPrivilegeValid(privilege, aerospikeConfig)

			if err != nil {
				return false, fmt.Errorf("Role '%s' has invalid privilege: %v", roleName, err)
			}
		}

		// Validate whitelist.
		for _, netAddress := range roleSpec.Whitelist {
			_, err = isNetAddressValid(netAddress)

			if err != nil {
				return false, fmt.Errorf("Role '%s' has invalid whitelist: %v", roleName, err)
			}
		}

	}

	return true, nil
}

// Indicates if a role name is valid.
func isRoleNameValid(roleName string) (bool, error) {
	if len(strings.TrimSpace(roleName)) == 0 {
		return false, fmt.Errorf("Role name cannot be empty")
	}

	if len(roleName) > roleNameLengthMax {
		return false, fmt.Errorf("Role name '%s' cannot have more than %d characters", roleName, roleNameLengthMax)
	}

	for _, forbiddenChar := range roleNameForbiddenChars {
		if strings.Index(roleName, forbiddenChar) > -1 {
			return false, fmt.Errorf("Role name '%s' cannot contain  %s", roleName, forbiddenChar)

		}
	}

	return true, nil
}

// Indicates if privilege is a valid privilege.
func isPrivilegeValid(privilege string, aerospikeConfig aerospikev1alpha1.Values) (bool, error) {
	parts := strings.Split(privilege, ".")

	_, ok := privileges[parts[0]]
	if !ok {
		// First part of the privilege is not part of defined privileges.
		return false, fmt.Errorf("Invalid privilege %s", privilege)
	}

	nParts := len(parts)

	if nParts > 3 {
		return false, fmt.Errorf("Invalid privilege %s", privilege)
	}

	if nParts > 1 {
		// This privilege should necessarily have NamespaceSet scope.
		scopes := privileges[parts[0]]
		if !scopeContains(scopes, NamespaceSet) {
			return false, fmt.Errorf("Privilege %s cannot have namespace or set scope.", privilege)
		}

		namespaceName := parts[1]

		if !utils.IsAerospikeNamespacePresent(aerospikeConfig, namespaceName) {
			return false, fmt.Errorf("For privilege %s, namespace %s not configured", privilege, namespaceName)
		}

		if nParts == 3 {
			// TODO Validate set name
			setName := parts[2]

			if len(strings.TrimSpace(setName)) == 0 {
				return false, fmt.Errorf("For privilege %s invalid set name", privilege)
			}
		}
	}

	return true, nil
}

// isNetAddressValid Indicates if networ/address sspecification is valid.
func isNetAddressValid(address string) (bool, error) {
	ip := net.ParseIP(address)

	if ip != nil {
		// This is a valid IP address.
		return true, nil
	}

	// Try parsing as CIDR
	_, _, err := net.ParseCIDR(address)

	if err != nil {
		return false, fmt.Errorf("Invalid address %s", address)
	}

	return true, nil
}

// scopeContains indicates if scopes contains the queryScope.
func scopeContains(scopes []PrivilegeScope, queryScope PrivilegeScope) bool {
	for _, scope := range scopes {
		if scope == queryScope {
			return true
		}
	}

	return false
}

// subset returns true if the first array is completely
// contained in the second array. There must be at least
// the same number of duplicate values in second as there
// are in first.
func subset(first, second []string) bool {
	set := make(map[string]int)
	for _, value := range second {
		set[value] += 1
	}

	for _, value := range first {
		if count, found := set[value]; !found {
			return false
		} else if count < 1 {
			return false
		} else {
			set[value] = count - 1
		}
	}

	return true
}

// isUserSpecValid indicates if input user specification is valid.
func isUserSpecValid(users map[string]aerospikev1alpha1.AerospikeUserSpec, roles map[string]aerospikev1alpha1.AerospikeRoleSpec) (bool, error) {
	requiredRolesUserFound := false
	for userName, userSpec := range users {
		_, err := isUserNameValid(userName)

		if err != nil {
			return false, err
		}

		// Validate roles.
		for _, roleName := range userSpec.Roles {
			_, ok := roles[roleName]
			if !ok {
				// Check is this is a predefined role.
				_, ok = predefinedRoles[roleName]
				if !ok {
					// Neither a specified role nor a predefined role.
					return false, fmt.Errorf("User '%s' has non-existent role %s", userName, roleName)
				}
			}
		}

		// TODO Should validate actual password here but we cannot read the secret here.
		// Will have to be done at the time of creating the user!
		if len(strings.TrimSpace(userSpec.SecretName)) == 0 {
			return false, fmt.Errorf("User %s has empty secret name", userName)
		}

		if subset(requiredRoles, userSpec.Roles) && userName == adminUsername {
			// We found admin user that has the required roles.
			requiredRolesUserFound = true
		}
	}

	if !requiredRolesUserFound {
		return false, fmt.Errorf("No admin user with required roles: %v found", requiredRoles)
	}

	return true, nil
}

// isUserNameValid Indicates if a user name is valid.
func isUserNameValid(userName string) (bool, error) {
	if len(strings.TrimSpace(userName)) == 0 {
		return false, fmt.Errorf("Username cannot be empty")
	}

	if len(userName) > userNameLengthMax {
		return false, fmt.Errorf("Username '%s' cannot have more than %d characters", userName, userNameLengthMax)
	}

	for _, forbiddenChar := range userNameForbiddenChars {
		if strings.Index(userName, forbiddenChar) > -1 {
			return false, fmt.Errorf("Username '%s' cannot contain  %s", userName, forbiddenChar)

		}
	}

	return true, nil
}

// AerospikeAdminCredentials to use for aerospike clients.
//
// Returns a tuple of admin username and password to use. If the cluster is not security
// enabled both username and password will be zero strings.
func AerospikeAdminCredentials(currentState *aerospikev1alpha1.AerospikeClusterSpec, desiredState *aerospikev1alpha1.AerospikeClusterSpec, passwordProvider AerospikeUserPasswordProvider) (string, string, error) {
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
	for roleName, _ := range desired {
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
	for userName, _ := range desired {
		requiredUserNames = append(requiredUserNames, userName)
	}

	userReconcileCmds := []AerospikeAccessControlReconcileCmd{}

	// Create a list of user commands to drop.
	usersToDrop := sliceSubtract(currentUserNames, requiredUserNames)

	for _, userToDrop := range usersToDrop {
		userReconcileCmds = append(userReconcileCmds, AerospikeUserDrop{name: userToDrop})
	}

	for userName, userSpec := range desired {
		password, err := passwordProvider.Get(userName, &userSpec)
		if err != nil {
			return err
		}

		userReconcileCmds = append(userReconcileCmds, AerospikeUserCreateUpdate{name: userName, password: &password, roles: userSpec.Roles})
	}

	// Execute all commands.
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

// sliceSubtract removes slice2 from s1 and returns the result.
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
	} else {
		return roleCreate.updateRole(client, adminPolicy, role, logger)
	}
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
			return fmt.Errorf("Error revoking privileges for role %s.", roleCreate.name)
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
			return fmt.Errorf("Error granting privileges for role %s.", roleCreate.name)
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
	} else {
		return userCreate.updateUser(client, adminPolicy, user, logger)
	}
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
			return fmt.Errorf("Error revoking roles for user %s.", userCreate.name)
		}

		logger.Info("Revoked roles for user", log.Ctx{"username": userCreate.name, "roles": rolesToRevoke})
	}

	if len(rolesToGrant) > 0 {
		err := client.GrantRoles(adminPolicy, userCreate.name, rolesToGrant)

		if err != nil {
			return fmt.Errorf("Error granting roles for user %s.", userCreate.name)
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
