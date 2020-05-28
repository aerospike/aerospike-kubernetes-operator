package admission

import (
	"fmt"
	"net"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
)

// Privilege scopes.
type PrivilegeScope int

const (
	Global PrivilegeScope = iota
	NamespaceSet
)

const (
	// Maximum length for role string.
	roleNameLengthMax int = 63

	// Maximum allowed length for a user name.
	userNameLengthMax int = 63

	// Maximum allowed length for a user password.
	userPasswordLengthMax int = 60
)

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

// Privileges.
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
func IsAerospikeAccessControlValid(aerospikeCluster aerospikev1alpha1.AerospikeClusterSpec) (bool, error) {
	enabled, err := isSecurityEnabled(aerospikeCluster)
	if err != nil {
		return false, nil
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
func isSecurityEnabled(aerospikeCluster aerospikev1alpha1.AerospikeClusterSpec) (bool, error) {
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
			return false, fmt.Errorf("Cannot create or mdify redefined role: %s", roleName)
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
	if len(roleName) > roleNameLengthMax {
		return false, fmt.Errorf("Role name '%s' cannot have more than %d characters", roleName, roleNameLengthMax)
	}

	// TODO Length seems to be the only constraint to check. Find out more checks.
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

	if nParts > 2 {
		return false, fmt.Errorf("Invalid privilege %s", privilege)
	}

	if nParts > 1 {
		// This privilege should necessarily have NamespaceSet scope.
		scopes := privileges[parts[0]]
		if !scopeContains(scopes, NamespaceSet) {
			return false, fmt.Errorf("Privilege %s cannot have namespace or set scope.")
		}

		namespaceName := parts[1]

		if !utils.IsAerospikeNamespacePresent(aerospikeConfig, namespaceName) {
			return false, fmt.Errorf("For privilege %s, namespace %s not configured", privilege, namespaceName)
		}

		if nParts == 2 {
			// TODO Validate set name
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
				return false, fmt.Errorf("User '%s' has non-existent role %s", userName, roleName)
			}
		}

		// TODO Should validate password here but we cannot read the secret here.
		// Will have to be done at the time of creating the user!

		if subset(requiredRoles, userSpec.Roles) {
			// We found a user that has the required roles.
			requiredRolesUserFound = true
		}
	}

	if !requiredRolesUserFound {
		return false, fmt.Errorf("No user with required roles: %v found", requiredRoles)
	}

	return true, nil
}

// isUserNameValid Indicates if a user name is valid.
func isUserNameValid(userName string) (bool, error) {
	if len(userName) > userNameLengthMax {
		return false, fmt.Errorf("User name '%s' cannot have more than %d characters", userName, userNameLengthMax)
	}

	// TODO Length seems to be the only constraint to check. Find out more checks.
	return true, nil
}
