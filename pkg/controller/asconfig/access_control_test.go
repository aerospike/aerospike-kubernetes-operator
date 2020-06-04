package asconfig

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	as "github.com/aerospike/aerospike-client-go"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
)

var pkglog = log.New(log.Ctx{"module": "test_access_control"})
var logger = pkglog.New(log.Ctx{"AerospikeCluster": "test"})

var aerospikeConfigWithSecurity = map[string]interface{}{
	"security": map[string]interface{}{"enable-security": true},
	"namespace": []map[string]interface{}{
		map[string]interface{}{
			"name": "profileNs",
		},
		map[string]interface{}{
			"name": "userNs",
		},
	},
}

var aerospikeConfigWithoutSecurity = map[string]interface{}{
	"security": map[string]interface{}{"enable-security": false},
	"namespace": []map[string]interface{}{
		map[string]interface{}{
			"name": "profileNs",
		},
		map[string]interface{}{
			"name": "userNs",
		},
	},
}

func TestValidAccessControl(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"admin": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if !valid {
		t.Errorf("Valid aerospike spec marked invalid: %v", err)
	}
}

func TestMissingRequiredUserRoles(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "required") {
		t.Errorf("Error: %v should contain 'required'", err)
	}
}

func TestInvalidUserRole(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
					"missingRole",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "missingRole") {
		t.Errorf("Error: %v should contain 'missingRole'", err)
	}
}

func TestInvalidUserSecretName(t *testing.T) {
	invalidSecretNames := []string{
		"", "   ",
	}

	for _, invalidSecretName := range invalidSecretNames {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
				"profiler": aerospikev1alpha1.AerospikeRoleSpec{
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
				"aerospike": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				"profileUser": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: invalidSecretName,
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
			AerospikeAccessControl: &accessControl,

			AerospikeConfig: aerospikeConfigWithSecurity,
		}

		valid, err := IsAerospikeAccessControlValid(&clusterSpec)

		if valid || err == nil {
			t.Errorf("InValid aerospike spec validated")
		}

		if !strings.Contains(err.Error(), "empty secret name") {
			t.Errorf("Error: %v should contain 'empty secret name'", err)
		}
	}
}

func TestInvalidUserName(t *testing.T) {
	name64Chars := randString(64)
	invalidUserNames := []string{
		"",
		"    ",
		name64Chars,
		"aerospike:user",
		"aerospike;user",
	}

	for _, invalidUserName := range invalidUserNames {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
				"profiler": aerospikev1alpha1.AerospikeRoleSpec{
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
				"aerospike": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				invalidUserName: aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someOtherSecret",
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
			AerospikeAccessControl: &accessControl,

			AerospikeConfig: aerospikeConfigWithSecurity,
		}

		valid, err := IsAerospikeAccessControlValid(&clusterSpec)

		if valid || err == nil {
			t.Errorf("InValid aerospike spec validated")
		}

		if !strings.Contains(err.Error(), "Username") && !strings.Contains(err.Error(), "empty") {
			t.Errorf("Error: %v should contain 'Username' or 'empty'", err)
		}
	}
}

func TestInvalidRoleName(t *testing.T) {
	name64Chars := randString(64)
	invalidRoleNames := []string{
		"",
		"    ",
		name64Chars,
		"aerospike:user",
		"aerospike;user",
	}

	for _, invalidRoleName := range invalidRoleNames {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
				invalidRoleName: aerospikev1alpha1.AerospikeRoleSpec{
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
				"aerospike": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				"profileUser": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someOtherSecret",
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
			AerospikeAccessControl: &accessControl,

			AerospikeConfig: aerospikeConfigWithSecurity,
		}

		valid, err := IsAerospikeAccessControlValid(&clusterSpec)

		if valid || err == nil {
			t.Errorf("InValid aerospike spec validated")
		}

		if !strings.Contains(err.Error(), "Role name") && !strings.Contains(err.Error(), "empty") {
			t.Errorf("Error: %v should contain 'Role name' or 'empty'", err)
		}
	}
}

func TestPredefinedRoleUpdate(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
			"sys-admin": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "predefined") {
		t.Errorf("Error: %v should contain 'predefined'", err)
	}
}

func TestInvalidRoleWhitelist(t *testing.T) {
	rand64Chars := randString(64)
	invalidWhitelists := []string{
		"",
		"    ",
		rand64Chars,
	}

	for _, invalidWhitelist := range invalidWhitelists {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
				"profiler": aerospikev1alpha1.AerospikeRoleSpec{
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
					Whitelist: []string{invalidWhitelist},
				},
			},
			Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
				"aerospike": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				"profileUser": aerospikev1alpha1.AerospikeUserSpec{
					SecretName: "someOtherSecret",
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
			AerospikeAccessControl: &accessControl,

			AerospikeConfig: aerospikeConfigWithSecurity,
		}

		valid, err := IsAerospikeAccessControlValid(&clusterSpec)

		if valid || err == nil {
			t.Errorf("InValid aerospike spec validated")
		}

		if !strings.Contains(err.Error(), "invalid whitelist") && !strings.Contains(err.Error(), "empty") {
			t.Errorf("Error: %v should contain 'invalid whitelist'", err)
		}
	}
}

func TestMissingNamespacePrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.missingNs",
					"read.userNs",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "missingNs") {
		t.Errorf("Error: %v should contain 'missingNs'", err)
	}
}

func TestMissingSetPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs.",
					"read.userNs",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "set name") {
		t.Errorf("Error: %v should contain 'missingNs'", err)
	}
}

func TestInvalidPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs.setname",
					"read.userNs",
					"non-existent",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Invalid privilege") {
		t.Errorf("Error: %v should contain 'invalid privilege'", err)
	}
}

func TestInvalidGlobalScopeOnlyPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.profileNs.setname",
					"read.userNs",
					// This should not be allowed.
					"sys-admin.profileNs",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"aerospike": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "namespace or set scope") {
		t.Errorf("Error: %v should contain 'namespace or set scope'", err)
	}
}

func TestAccessControlCreate(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.test",
					"read-write-udf.test.users",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
			"roleToDrop": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write.test",
					"read-write-udf.test.users",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"admin": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
					"sys-admin",
				},
			},

			"userToDrop": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"profiler",
				},
			},
		},
	}

	desiredSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	currentSpec := aerospikev1alpha1.AerospikeClusterSpec{}

	// Password provider.
	pp := StaticPasswordProvider{usernameToPasswordMap: map[string]string{
		"admin":       "secretadminpassword",
		"profileUser": "secretprofileuserpassword",
		"userToDrop":  "secretThatDoesNotMatter",
	}}

	testAccessControlReconcile(&desiredSpec, &currentSpec, pp, t)

	// Desired spec is now the current spec.
	currentSpec = desiredSpec

	// Apply updates to drop users, drop roles, update privileges for roles and update roles for users.
	accessControl = aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: map[string]aerospikev1alpha1.AerospikeRoleSpec{
			"profiler": aerospikev1alpha1.AerospikeRoleSpec{
				Privileges: []string{
					"read-write-udf.test.users",
					"write",
				},
				Whitelist: []string{
					"0.0.0.0/32",
				},
			},
		},
		Users: map[string]aerospikev1alpha1.AerospikeUserSpec{
			"admin": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			"profileUser": aerospikev1alpha1.AerospikeUserSpec{
				SecretName: "someOtherSecret",
				Roles: []string{
					"data-admin",
					"read-write-udf",
					"write",
				},
			},
		},
	}

	desiredSpec = aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	testAccessControlReconcile(&desiredSpec, &currentSpec, pp, t)
}

func testAccessControlReconcile(desired *aerospikev1alpha1.AerospikeClusterSpec, current *aerospikev1alpha1.AerospikeClusterSpec, pp StaticPasswordProvider, t *testing.T) {
	// Create a client using the admin pivileges.
	clientP, err := getClient(current, desired, pp)
	if err != nil {
		t.Errorf("Error creating client: %v", err)
		return
	}

	defer (*clientP).Close()

	// Apply access control on a new cluster.
	err = ReconcileAccessControl(desired, current, clientP, pp, logger)
	if err != nil {
		t.Errorf("Error reconciling: %v", err)
		return
	}

	// Ensure the desired spec access control is correctly applied.
	validateAccessControl(desired, pp, t)
}

// validateAccessControl validates that the new acccess control have been applied correctly.
func validateAccessControl(clusterSpec *aerospikev1alpha1.AerospikeClusterSpec, pp StaticPasswordProvider, t *testing.T) {
	clientP, err := getClient(clusterSpec, clusterSpec, pp)
	if err != nil {
		t.Errorf("Error creating client: %v", err)
		return
	}

	client := *clientP
	defer client.Close()

	validateRoles(clientP, clusterSpec, t)
	validateUsers(clientP, clusterSpec, pp, t)
}

// validateRoles validates that the new roles have been applied correctly.
func validateRoles(clientP *as.Client, clusterSpec *aerospikev1alpha1.AerospikeClusterSpec, t *testing.T) {
	client := *clientP
	adminPolicy := getAdminPolicy(clusterSpec)
	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		t.Errorf("Error querying roles: %v", err)
		return
	}

	currentRoleNames := []string{}

	for _, role := range asRoles {
		_, isPredefined := predefinedRoles[role.Name]

		if !isPredefined {
			currentRoleNames = append(currentRoleNames, role.Name)
		}
	}

	expectedRoleNames := []string{}
	accessControl := clusterSpec.AerospikeAccessControl
	for roleName, _ := range accessControl.Roles {
		expectedRoleNames = append(expectedRoleNames, roleName)
	}

	if len(currentRoleNames) != len(expectedRoleNames) {
		t.Errorf("Actual roles %v do not match expected roles %v", currentRoleNames, expectedRoleNames)
		return
	}

	// Check values.
	if len(sliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
		t.Errorf("Actual roles %v do not match expected roles %v", currentRoleNames, expectedRoleNames)
		return
	}

	// Verify the privileges are correct.
	for _, asRole := range asRoles {
		_, isPredefined := predefinedRoles[asRole.Name]

		if isPredefined {
			continue
		}

		expectedRoleSpec, _ := accessControl.Roles[asRole.Name]
		expectedPrivilegeNames := expectedRoleSpec.Privileges

		currentPrivilegeNames := []string{}
		for _, privilege := range asRole.Privileges {
			temp, _ := aerospikePrivilegeToPrivilegeString([]as.Privilege{privilege})
			currentPrivilegeNames = append(currentPrivilegeNames, temp[0])
		}

		if len(currentPrivilegeNames) != len(expectedPrivilegeNames) {
			t.Errorf("For role %s actual privileges %v do not match expected privileges %v", asRole.Name, currentPrivilegeNames, expectedPrivilegeNames)
		}

		// Check values.
		if len(sliceSubtract(expectedPrivilegeNames, currentPrivilegeNames)) != 0 {
			t.Errorf("For role %s actual privileges %v do not match expected privileges %v", asRole.Name, currentPrivilegeNames, expectedPrivilegeNames)
		}
	}
}

// validateUsers validates that the new users have been applied correctly.
func validateUsers(clientP *as.Client, clusterSpec *aerospikev1alpha1.AerospikeClusterSpec, pp StaticPasswordProvider, t *testing.T) {
	client := *clientP

	adminPolicy := getAdminPolicy(clusterSpec)
	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		t.Errorf("Error querying users: %v", err)
		return
	}

	currentUserNames := []string{}

	for _, user := range asUsers {
		currentUserNames = append(currentUserNames, user.User)
	}

	expectedUserNames := []string{}
	accessControl := clusterSpec.AerospikeAccessControl
	for userName, _ := range accessControl.Users {
		expectedUserNames = append(expectedUserNames, userName)
	}

	if len(currentUserNames) != len(expectedUserNames) {
		t.Errorf("Actual users %v do not match expected users %v", currentUserNames, expectedUserNames)
		return
	}

	// Check values.
	if len(sliceSubtract(expectedUserNames, currentUserNames)) != 0 {
		t.Errorf("Actual users %v do not match expected users %v", currentUserNames, expectedUserNames)
		return
	}

	// Verify the roles are correct.
	for _, asUser := range asUsers {
		expectedUserSpec, _ := accessControl.Users[asUser.User]
		// Validate that the new user password is applied
		password, err := pp.Get(asUser.User, &expectedUserSpec)

		if err != nil {
			t.Errorf("For user %s cannot get password %v", asUser.User, err)
			return
		}

		userClient, err := getClientForUser(asUser.User, password)
		if err != nil {
			t.Errorf("For user %s cannot get client. Possible auth error :%v", asUser.User, err)
			return
		}
		(*userClient).Close()

		expectedRoleNames := expectedUserSpec.Roles
		currentRoleNames := []string{}
		for _, roleName := range asUser.Roles {
			currentRoleNames = append(currentRoleNames, roleName)
		}

		if len(currentRoleNames) != len(expectedRoleNames) {
			t.Errorf("For user %s actual roles %v do not match expected roles %v", asUser.User, currentRoleNames, expectedRoleNames)
			return
		}

		// Check values.
		if len(sliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
			t.Errorf("For user %s actual roles %v do not match expected roles %v", asUser.User, currentRoleNames, expectedRoleNames)
			return
		}
	}
}

type StaticPasswordProvider struct {
	// Map from username to password.
	usernameToPasswordMap map[string]string
}

func (pp StaticPasswordProvider) Get(username string, userSpec *aerospikev1alpha1.AerospikeUserSpec) (string, error) {
	password, ok := pp.usernameToPasswordMap[username]

	if !ok {
		return "", fmt.Errorf("Could not get password for user %s", username)
	}

	return password, nil
}

func getClient(currentSpec *aerospikev1alpha1.AerospikeClusterSpec, desiredSpec *aerospikev1alpha1.AerospikeClusterSpec, pp StaticPasswordProvider) (*as.Client, error) {
	username, password, err := AerospikeAdminCredentials(desiredSpec, currentSpec, &pp)

	if err != nil {
		return nil, err
	}

	return getClientForUser(username, password)
}

func getClientForUser(username string, password string) (*as.Client, error) {
	env := os.Getenv("AEROSPIKE_HOSTS")

	if env == "" {
		return nil, fmt.Errorf("Aerospike cluster seeds not configured")
	}

	hostStrings := strings.Split(env, ",")

	var hosts []*as.Host
	for _, hostString := range hostStrings {
		parts := strings.Split(hostString, ":")
		port, _ := strconv.ParseInt(parts[1], 10, 0)
		hosts = append(hosts, &as.Host{
			Name: parts[0],
			Port: int(port),
		})
	}

	cp := as.NewClientPolicy()
	cp.User = username
	cp.Password = password
	cp.Timeout = 3 * time.Second

	client, err := as.NewClientWithPolicyAndHost(cp, hosts...)

	if err != nil {
		return nil, err
	}

	return client, nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
