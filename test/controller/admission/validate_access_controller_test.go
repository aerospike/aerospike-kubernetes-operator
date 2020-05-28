package admission

import (
	"math/rand"
	"strings"
	"testing"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	validator "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/admission"
)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

		valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

		valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

		valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

		valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

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

	valid, err := validator.IsAerospikeAccessControlValid(clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "namespace or set scope") {
		t.Errorf("Error: %v should contain 'namespace or set scope'", err)
	}
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
