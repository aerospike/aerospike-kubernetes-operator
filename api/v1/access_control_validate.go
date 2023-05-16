package v1

// Aerospike access control functions provides validation and reconciliation of access control.

import (
	"fmt"
	"net"
	"strings"

	"github.com/aerospike/aerospike-management-lib/asconfig"
)

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

	// AdminUsername for aerospike cluster
	AdminUsername = "admin"

	// DefaultAdminPassword si default admin user password.
	DefaultAdminPassword = "admin"
)

// roleNameForbiddenChars are characters forbidden in role name.
var roleNameForbiddenChars = []string{";", ":"}

// userNameForbiddenChars are characters forbidden in username.
var userNameForbiddenChars = []string{";", ":"}

// PredefinedRoles are all roles predefined in Aerospike server.
var PredefinedRoles = map[string]struct{}{
	"user-admin":     {},
	"sys-admin":      {},
	"data-admin":     {},
	"read":           {},
	"read-write":     {},
	"read-write-udf": {},
	"write":          {},
	"truncate":       {},
	"sindex-admin":   {},
	"udf-admin":      {},
}

// Post6PredefinedRoles are roles predefined post version 6.0 in Aerospike server.
var Post6PredefinedRoles = map[string]struct{}{
	"truncate":     {},
	"sindex-admin": {},
	"udf-admin":    {},
}

// Expect at least one user with these required roles.
var requiredRoles = []string{
	"sys-admin",
	"user-admin",
}

// Privileges are all privilege string allowed in the spec and associated scopes.
var Privileges = map[string][]PrivilegeScope{
	"read":           {Global, NamespaceSet},
	"write":          {Global, NamespaceSet},
	"read-write":     {Global, NamespaceSet},
	"read-write-udf": {Global, NamespaceSet},
	"data-admin":     {Global},
	"sys-admin":      {Global},
	"user-admin":     {Global},
	"truncate":       {Global, NamespaceSet},
	"sindex-admin":   {Global},
	"udf-admin":      {Global},
}

// Post6Privileges are post version 6.0 privilege strings allowed in the spec and associated scopes.
var Post6Privileges = map[string][]PrivilegeScope{
	"truncate":     {Global, NamespaceSet},
	"sindex-admin": {Global},
	"udf-admin":    {Global},
}

// IsAerospikeAccessControlValid validates the accessControl specification in the clusterSpec.
//
// Asserts that the AerospikeAccessControlSpec
//
//	has correct references to other objects like namespaces
//	follows rules defined https://www.aerospike.com/docs/guide/limitations.html
//	follows rules found through server code inspection for e.g. predefined roles
//	meets operator requirements. For e.g. the necessity to have at least one sys-admin and user-admin user.
func IsAerospikeAccessControlValid(aerospikeClusterSpec *AerospikeClusterSpec) (
	bool, error,
) {
	version, err := GetImageVersion(aerospikeClusterSpec.Image)
	if err != nil {
		return false, err
	}

	enabled, err := IsSecurityEnabled(version, aerospikeClusterSpec.AerospikeConfig)
	if err != nil {
		return false, err
	}

	if !enabled && aerospikeClusterSpec.AerospikeAccessControl != nil {
		// Security is disabled however access control is specified.
		return false, fmt.Errorf("security is disabled but access control is specified")
	}

	if !enabled {
		return true, nil
	}

	if aerospikeClusterSpec.AerospikeAccessControl == nil {
		return false, fmt.Errorf("security is enabled but access control is missing")
	}

	// Validate roles.
	_, err = isRoleSpecValid(
		aerospikeClusterSpec.AerospikeAccessControl.Roles,
		*aerospikeClusterSpec.AerospikeConfig, version,
	)
	if err != nil {
		return false, err
	}

	roleMap := GetRolesFromSpec(aerospikeClusterSpec)

	// Validate users.
	_, err = isUserSpecValid(
		aerospikeClusterSpec.AerospikeAccessControl.Users, roleMap,
	)

	if err != nil {
		return false, err
	}

	return true, nil
}

// GetRolesFromSpec returns roles or an empty map from the spec.
func GetRolesFromSpec(spec *AerospikeClusterSpec) map[string]AerospikeRoleSpec {
	var roles = map[string]AerospikeRoleSpec{}

	if spec.AerospikeAccessControl != nil {
		for _, roleSpec := range spec.AerospikeAccessControl.Roles {
			roles[roleSpec.Name] = roleSpec
		}
	}

	return roles
}

// GetUsersFromSpec returns users or an empty map from the spec.
func GetUsersFromSpec(spec *AerospikeClusterSpec) map[string]AerospikeUserSpec {
	var users = map[string]AerospikeUserSpec{}

	if spec.AerospikeAccessControl != nil {
		for _, userSpec := range spec.AerospikeAccessControl.Users {
			users[userSpec.Name] = userSpec
		}
	}

	return users
}

func validateRoleQuotaParam(
	roleSpec AerospikeRoleSpec, aerospikeConfigSpec *AerospikeConfigSpec,
) error {
	if roleSpec.ReadQuota > 0 || roleSpec.WriteQuota > 0 {
		enabled, err := IsAttributeEnabled(
			aerospikeConfigSpec, "security", "enable-quotas",
		)
		if err != nil {
			return err
		}

		if !enabled {
			return fmt.Errorf(
				"security.enable-quotas is set to false but quota params are: "+
					"ReadQuota: %d  WriteQuota: %d", roleSpec.ReadQuota,
				roleSpec.WriteQuota,
			)
		}
	}

	return nil
}

// isRoleSpecValid indicates if input role spec is valid.
func isRoleSpecValid(
	roles []AerospikeRoleSpec, aerospikeConfigSpec AerospikeConfigSpec, version string,
) (bool, error) {
	seenRoles := map[string]bool{}
	for _, roleSpec := range roles {
		_, isSeen := seenRoles[roleSpec.Name]
		if isSeen {
			// Cannot have duplicate role entries.
			return false, fmt.Errorf(
				"duplicate entry for role: %s", roleSpec.Name,
			)
		}

		seenRoles[roleSpec.Name] = true

		_, ok := PredefinedRoles[roleSpec.Name]
		if ok {
			cmp, err := asconfig.CompareVersions(version, "6.0.0.0")
			if err != nil {
				return false, err
			}

			if cmp >= 0 {
				// Cannot modify or add predefined roles.
				return false, fmt.Errorf("cannot create or modify predefined role: %s", roleSpec.Name)
			} else if _, ok := Post6PredefinedRoles[roleSpec.Name]; !ok {
				// Version < 6.0 and attempt to modify a pre 6.0 role
				return false, fmt.Errorf("cannot create or modify predefined role: %s", roleSpec.Name)
			}
		}

		if _, err := isRoleNameValid(roleSpec.Name); err != nil {
			return false, err
		}

		if err := validateRoleQuotaParam(roleSpec, &aerospikeConfigSpec); err != nil {
			return false, err
		}

		// Validate privileges.
		seenPrivileges := map[string]bool{}
		for _, privilege := range roleSpec.Privileges {
			_, isSeen := seenPrivileges[privilege]

			if isSeen {
				// Cannot have duplicate privilege entries.
				return false, fmt.Errorf(
					"duplicate privilege: %s for role: %s", privilege,
					roleSpec.Name,
				)
			}

			seenPrivileges[privilege] = true

			if _, err := isPrivilegeValid(privilege, aerospikeConfigSpec, version); err != nil {
				return false, fmt.Errorf(
					"role '%s' has invalid privilege: %v", roleSpec.Name, err,
				)
			}
		}

		// Validate whitelist.
		seenNetAddresses := map[string]bool{}
		for _, netAddress := range roleSpec.Whitelist {
			_, isSeen := seenNetAddresses[netAddress]

			if isSeen {
				// Cannot have duplicate whitelist entries.
				return false, fmt.Errorf(
					"duplicate whitelist: %s for role: %s", netAddress,
					roleSpec.Name,
				)
			}

			seenNetAddresses[netAddress] = true

			if _, err := isNetAddressValid(netAddress); err != nil {
				return false, fmt.Errorf(
					"role '%s' has invalid whitelist: %v", roleSpec.Name, err,
				)
			}
		}
	}

	return true, nil
}

// Indicates if a role name is valid.
func isRoleNameValid(roleName string) (bool, error) {
	if strings.TrimSpace(roleName) == "" {
		return false, fmt.Errorf("role name cannot be empty")
	}

	if len(roleName) > roleNameLengthMax {
		return false, fmt.Errorf(
			"role name '%s' cannot have more than %d characters", roleName,
			roleNameLengthMax,
		)
	}

	for _, forbiddenChar := range roleNameForbiddenChars {
		if strings.Contains(roleName, forbiddenChar) {
			return false, fmt.Errorf(
				"role name '%s' cannot contain  %s", roleName, forbiddenChar,
			)
		}
	}

	return true, nil
}

// Indicates if privilege is a valid privilege.
func isPrivilegeValid(
	privilege string, aerospikeConfigSpec AerospikeConfigSpec, version string,
) (bool, error) {
	parts := strings.Split(privilege, ".")

	_, ok := Privileges[parts[0]]
	if !ok {
		return false, fmt.Errorf("invalid privilege %s", privilege)
	}

	// Check if new privileges are used in an older version.
	cmp, err := asconfig.CompareVersions(version, "6.0.0.0")
	if err != nil {
		return false, err
	}

	if cmp < 0 {
		if _, ok := Post6Privileges[parts[0]]; ok {
			// Version < 6.0 using post 6.0 privilege.
			return false, fmt.Errorf("invalid privilege %s", privilege)
		}
	}

	nParts := len(parts)

	if nParts > 3 {
		return false, fmt.Errorf("invalid privilege %s", privilege)
	}

	if nParts > 1 {
		// This privilege should necessarily have NamespaceSet scope.
		scopes := Privileges[parts[0]]

		if !scopeContains(scopes, NamespaceSet) {
			return false, fmt.Errorf(
				"privilege %s cannot have namespace or set scope", privilege,
			)
		}

		namespaceName := parts[1]

		if !IsAerospikeNamespacePresent(aerospikeConfigSpec, namespaceName) {
			return false, fmt.Errorf(
				"for privilege %s, namespace %s not configured", privilege,
				namespaceName,
			)
		}

		if nParts == 3 {
			// TODO Validate set name
			setName := parts[2]

			if strings.TrimSpace(setName) == "" {
				return false, fmt.Errorf(
					"for privilege %s invalid set name", privilege,
				)
			}
		}
	}

	return true, nil
}

// isNetAddressValid Indicates if network/address specification is valid.
func isNetAddressValid(address string) (bool, error) {
	ip := net.ParseIP(address)

	if ip != nil {
		// This is a valid IP address.
		return true, nil
	}

	// Try parsing as CIDR
	_, ipNet, err := net.ParseCIDR(address)
	if err != nil {
		return false, fmt.Errorf("invalid address %s", address)
	}

	if ipNet.String() != address {
		return false, fmt.Errorf(
			"invalid CIDR %s - suggested CIDR %v", address, ipNet,
		)
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
		count, found := set[value]

		switch {
		case !found:
			return false
		case count < 1:
			return false
		default:
			set[value] = count - 1
		}
	}

	return true
}

// isUserSpecValid indicates if input user specification is valid.
func isUserSpecValid(
	users []AerospikeUserSpec, roles map[string]AerospikeRoleSpec,
) (bool, error) {
	requiredRolesUserFound := false
	seenUsers := map[string]bool{}

	for _, userSpec := range users {
		_, isSeen := seenUsers[userSpec.Name]

		if isSeen {
			// Cannot have duplicate user entries.
			return false, fmt.Errorf(
				"duplicate entry for user: %s", userSpec.Name,
			)
		}

		seenUsers[userSpec.Name] = true

		if _, err := isUserNameValid(userSpec.Name); err != nil {
			return false, err
		}

		// Validate roles.
		seenRoles := map[string]bool{}

		for _, roleName := range userSpec.Roles {
			_, isSeen := seenRoles[roleName]

			if isSeen {
				// Cannot have duplicate roles.
				return false, fmt.Errorf(
					"duplicate role: %s for user: %s", roleName, userSpec.Name,
				)
			}

			seenRoles[roleName] = true

			if _, ok := roles[roleName]; !ok {
				// Check is this is a predefined role.
				_, ok = PredefinedRoles[roleName]

				if !ok {
					// Neither a specified role nor a predefined role.
					return false, fmt.Errorf(
						"user '%s' has non-existent role %s", userSpec.Name,
						roleName,
					)
				}
			}
		}

		// TODO We should validate actual password here but we cannot read the secret here.
		// Will have to be done at the time of creating the user!
		if strings.TrimSpace(userSpec.SecretName) == "" {
			return false, fmt.Errorf(
				"user %s has empty secret name", userSpec.Name,
			)
		}

		if subset(
			requiredRoles, userSpec.Roles,
		) && userSpec.Name == AdminUsername {
			// We found admin user that has the required roles.
			requiredRolesUserFound = true
		}
	}

	if !requiredRolesUserFound {
		return false, fmt.Errorf(
			"no admin user with required roles: %v found", requiredRoles,
		)
	}

	return true, nil
}

// isUserNameValid Indicates if a username is valid.
func isUserNameValid(userName string) (bool, error) {
	if strings.TrimSpace(userName) == "" {
		return false, fmt.Errorf("username cannot be empty")
	}

	if len(userName) > userNameLengthMax {
		return false, fmt.Errorf(
			"username '%s' cannot have more than %d characters", userName,
			userNameLengthMax,
		)
	}

	for _, forbiddenChar := range userNameForbiddenChars {
		if strings.Contains(userName, forbiddenChar) {
			return false, fmt.Errorf(
				"username '%s' cannot contain  %s", userName, forbiddenChar,
			)
		}
	}

	return true, nil
}
