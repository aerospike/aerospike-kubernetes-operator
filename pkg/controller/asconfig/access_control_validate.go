package asconfig

// Aerospike access control functions provides validation and reconciliation of access control.

import (
	"fmt"
	"net"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	log "github.com/inconshreveable/log15"
)

// Logger type alias.
type Logger = log.Logger

// PrivilegeScope enumerates valid scopes for privileges.
type PrivilegeScope int

const (
	// Global scoped privileges.
	Global PrivilegeScope = iota

	// NamespaceSet is namespace and optional set scoped privilege.
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

// IsAerospikeAccessControlValid validates the accessControl speciication in the clusterSpec.
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

	if aerospikeCluster.AerospikeAccessControl == nil {
		return false, fmt.Errorf("Security is enabled but access control is missing")
	}

	// Validate roles.
	_, err = isRoleSpecValid(aerospikeCluster.AerospikeAccessControl.Roles, aerospikeCluster.AerospikeConfig)
	if err != nil {
		return false, err
	}

	roleMap := getRolesFromSpec(aerospikeCluster)

	// Validate users.
	_, err = isUserSpecValid(aerospikeCluster.AerospikeAccessControl.Users, roleMap)

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
func isRoleSpecValid(roles []aerospikev1alpha1.AerospikeRoleSpec, aerospikeConfig aerospikev1alpha1.Values) (bool, error) {
	seenRoles := map[string]bool{}
	for _, roleSpec := range roles {
		_, isSeen := seenRoles[roleSpec.Name]
		if isSeen {
			// Cannot have duplicate role entries.
			return false, fmt.Errorf("Duplicate entry for role: %s", roleSpec.Name)
		}
		seenRoles[roleSpec.Name] = true

		_, ok := predefinedRoles[roleSpec.Name]
		if ok {
			// Cannot modify or add predefined roles.
			return false, fmt.Errorf("Cannot create or modify predefined role: %s", roleSpec.Name)
		}

		_, err := isRoleNameValid(roleSpec.Name)

		if err != nil {
			return false, err
		}

		// Validate privileges.
		seenPrivileges := map[string]bool{}
		for _, privilege := range roleSpec.Privileges {
			_, isSeen := seenPrivileges[privilege]
			if isSeen {
				// Cannot have duplicate privilege entries.
				return false, fmt.Errorf("Duplicate privilege: %s for role: %s", privilege, roleSpec.Name)
			}
			seenPrivileges[privilege] = true

			_, err = isPrivilegeValid(privilege, aerospikeConfig)

			if err != nil {
				return false, fmt.Errorf("Role '%s' has invalid privilege: %v", roleSpec.Name, err)
			}
		}

		// Validate whitelist.
		seenNetAddresses := map[string]bool{}
		for _, netAddress := range roleSpec.Whitelist {
			_, isSeen := seenNetAddresses[netAddress]
			if isSeen {
				// Cannot have duplicate whitelist entries.
				return false, fmt.Errorf("Duplicate whitelist: %s for role: %s", netAddress, roleSpec.Name)
			}
			seenNetAddresses[netAddress] = true

			_, err = isNetAddressValid(netAddress)

			if err != nil {
				return false, fmt.Errorf("Role '%s' has invalid whitelist: %v", roleSpec.Name, err)
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
			return false, fmt.Errorf("Privilege %s cannot have namespace or set scope", privilege)
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
		set[value]++
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
func isUserSpecValid(users []aerospikev1alpha1.AerospikeUserSpec, roles map[string]aerospikev1alpha1.AerospikeRoleSpec) (bool, error) {
	requiredRolesUserFound := false
	seenUsers := map[string]bool{}
	for _, userSpec := range users {
		_, isSeen := seenUsers[userSpec.Name]
		if isSeen {
			// Cannot have duplicate user entries.
			return false, fmt.Errorf("Duplicate entry for user: %s", userSpec.Name)
		}
		seenUsers[userSpec.Name] = true

		_, err := isUserNameValid(userSpec.Name)

		if err != nil {
			return false, err
		}

		// Validate roles.
		seenRoles := map[string]bool{}
		for _, roleName := range userSpec.Roles {
			_, isSeen := seenRoles[roleName]
			if isSeen {
				// Cannot have duplicate roles.
				return false, fmt.Errorf("Duplicate role: %s for user: %s", roleName, userSpec.Name)
			}
			seenRoles[roleName] = true

			_, ok := roles[roleName]
			if !ok {
				// Check is this is a predefined role.
				_, ok = predefinedRoles[roleName]
				if !ok {
					// Neither a specified role nor a predefined role.
					return false, fmt.Errorf("User '%s' has non-existent role %s", userSpec.Name, roleName)
				}
			}
		}

		// TODO We should validate actual password here but we cannot read the secret here.
		// Will have to be done at the time of creating the user!
		if len(strings.TrimSpace(userSpec.SecretName)) == 0 {
			return false, fmt.Errorf("User %s has empty secret name", userSpec.Name)
		}

		if subset(requiredRoles, userSpec.Roles) && userSpec.Name == adminUsername {
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
