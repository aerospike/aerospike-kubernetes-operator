// +build !noac

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	asConfig "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	as "github.com/ashishshinde/aerospike-client-go"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
)

const (
	testClusterSize = 2
)

var aerospikeConfigWithSecurity = map[string]interface{}{
	"security": map[string]interface{}{"enable-security": true},
	"namespace": []interface{}{
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
	"namespace": []interface{}{
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
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"8.8.0.0/16",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "admin",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if !valid {
		t.Errorf("Valid aerospike spec marked invalid: %v", err)
	}
}

func TestMissingRequiredUserRoles(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "required") {
		t.Errorf("Error: %v should contain 'required'", err)
	}
}

func TestInvalidUserRole(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "missingRole") {
		t.Errorf("Error: %v should contain 'missingRole'", err)
	}
}

func TestDuplicateUser(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Duplicate") || !strings.Contains(err.Error(), "aerospike") {
		t.Errorf("Error: %v should contain 'Duplicate' and 'aerospike'", err)
	}
}

func TestDuplicateUserRole(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"sys-admin",
				},
			},
		},
	}

	clusterSpec := aerospikev1alpha1.AerospikeClusterSpec{
		AerospikeAccessControl: &accessControl,

		AerospikeConfig: aerospikeConfigWithSecurity,
	}

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Duplicate") || !strings.Contains(err.Error(), "sys-admin") {
		t.Errorf("Error: %v should contain 'Duplicate' and 'sys-admin'", err)
	}
}

func TestInvalidUserSecretName(t *testing.T) {
	invalidSecretNames := []string{
		"", "   ",
	}

	for _, invalidSecretName := range invalidSecretNames {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "aerospike",
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
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

		valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

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
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "aerospike",
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       invalidUserName,
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

		valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

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
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: invalidRoleName,
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "aerospike",
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
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

		valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

		if valid || err == nil {
			t.Errorf("InValid aerospike spec validated")
		}

		if !strings.Contains(err.Error(), "Role name") && !strings.Contains(err.Error(), "empty") {
			t.Errorf("Error: %v should contain 'Role name' or 'empty'", err)
		}
	}
}

func TestDuplicateRole(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Duplicate") || !strings.Contains(err.Error(), "profiler") {
		t.Errorf("Error: %v should contain 'Duplicate' and 'profiler'", err)
	}
}

func TestDuplicateRolePrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Duplicate") || !strings.Contains(err.Error(), "read-write.profileNs") {
		t.Errorf("Error: %v should contain 'Duplicate' and 'read-write.profileNs'", err)
	}
}

func TestDuplicateRoleWhitelist(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
				Whitelist: []string{
					"8.8.0.0/16",
					"8.8.0.0/16",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Duplicate") || !strings.Contains(err.Error(), "8.8.0.0/16") {
		t.Errorf("Error: %v should contain 'Duplicate' and '8.8.0.0/16'", err)
	}
}

func TestInvalidWhitelist(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read-write.profileNs.set",
					"read.userNs",
				},
				Whitelist: []string{
					"8.8.8.8/16",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Invalid CIDR") || !strings.Contains(err.Error(), "8.8.8.8/16") {
		t.Errorf("Error: %v should contain 'Duplicate' and '8.8.8.8/16'", err)
	}
}

func TestPredefinedRoleUpdate(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"8.8.0.0/16",
				},
			},
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "sys-admin",
				Privileges: []string{
					"read-write.profileNs",
					"read.userNs",
				},
				Whitelist: []string{
					"8.8.0.0/16",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

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
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.profileNs",
						"read-write.profileNs.set",
						"read.userNs",
					},
					Whitelist: []string{invalidWhitelist},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "aerospike",
					SecretName: "someSecret",
					Roles: []string{
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
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

		valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

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
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.missingNs",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "missingNs") {
		t.Errorf("Error: %v should contain 'missingNs'", err)
	}
}

func TestMissingSetPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs.",
					"read.userNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "set name") {
		t.Errorf("Error: %v should contain 'missingNs'", err)
	}
}

func TestInvalidPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs.setname",
					"read.userNs",
					"non-existent",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "Invalid privilege") {
		t.Errorf("Error: %v should contain 'invalid privilege'", err)
	}
}

func TestInvalidGlobalScopeOnlyPrivilege(t *testing.T) {
	accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
		Roles: []aerospikev1alpha1.AerospikeRoleSpec{
			aerospikev1alpha1.AerospikeRoleSpec{
				Name: "profiler",
				Privileges: []string{
					"read-write.profileNs.setname",
					"read.userNs",
					// This should not be allowed.
					"sys-admin.profileNs",
				},
			},
		},
		Users: []aerospikev1alpha1.AerospikeUserSpec{
			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "aerospike",
				SecretName: "someSecret",
				Roles: []string{
					"sys-admin",
				},
			},

			aerospikev1alpha1.AerospikeUserSpec{
				Name:       "profileUser",
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

	valid, err := asConfig.IsAerospikeAccessControlValid(&clusterSpec)

	if valid || err == nil {
		t.Errorf("InValid aerospike spec validated")
	}

	if !strings.Contains(err.Error(), "namespace or set scope") {
		t.Errorf("Error: %v should contain 'namespace or set scope'", err)
	}
}

func TestNoSecurityIntegration(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Errorf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	var aeroCluster *aerospikev1alpha1.AerospikeCluster = nil

	t.Run("AccessControlValidation", func(t *testing.T) {
		// Just a smoke test to ensure validation hook works.
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "roleToDrop",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "admin",
					SecretName: authSecretName,
					Roles: []string{
						// Missing required user admin role.
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "userToDrop",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		aeroCluster = getAerospikeClusterSpecWithAccessControl(&accessControl, false, ctx)
		err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
		if err == nil || !strings.Contains(err.Error(), "Security is disabled but access control is specified") {
			t.Error(err)
		}
	})

	t.Run("NoAccessControlCreate", func(t *testing.T) {
		var accessControl *aerospikev1alpha1.AerospikeAccessControlSpec = nil

		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAccessControl(accessControl, false, ctx)
		err := aerospikeClusterCreateUpdate(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("SecurityUpdateReject", func(t *testing.T) {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write-udf.test.users",
						"write",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "admin",
					SecretName: authSecretNameForUpdate,
					Roles: []string{
						"sys-admin",
						"user-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
					SecretName: authSecretNameForUpdate,
					Roles: []string{
						"data-admin",
						"read-write-udf",
						"write",
					},
				},
			},
		}

		aeroCluster = getAerospikeClusterSpecWithAccessControl(&accessControl, true, ctx)
		err := testAccessControlReconcile(aeroCluster, ctx, t)
		if err == nil || !strings.Contains(err.Error(), "Cannot update cluster security config") {
			t.Error(err)
		}
	})

	if aeroCluster != nil {
		deleteCluster(t, f, ctx, aeroCluster)
	}
}

func TestAccessControlIntegration(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Errorf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	var aeroCluster *aerospikev1alpha1.AerospikeCluster = nil

	t.Run("AccessControlValidation", func(t *testing.T) {
		// Just a smoke test to ensure validation hook works.
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "roleToDrop",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "admin",
					SecretName: authSecretName,
					Roles: []string{
						// Missing required user admin role.
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "userToDrop",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAccessControl(&accessControl, true, ctx)
		err := testAccessControlReconcile(aeroCluster, ctx, t)
		if err == nil || !strings.Contains(err.Error(), "required roles") {
			t.Error(err)
		}
	})

	t.Run("AccessControlCreate", func(t *testing.T) {
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
				},
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "roleToDrop",
					Privileges: []string{
						"read-write.test",
						"read-write-udf.test.users",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "admin",
					SecretName: authSecretName,
					Roles: []string{
						"sys-admin",
						"user-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
						"sys-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "userToDrop",
					SecretName: authSecretName,
					Roles: []string{
						"profiler",
					},
				},
			},
		}

		aeroCluster := getAerospikeClusterSpecWithAccessControl(&accessControl, true, ctx)
		err := testAccessControlReconcile(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("AccessControlUpdate", func(t *testing.T) {
		// Apply updates to drop users, drop roles, update privileges for roles and update roles for users.
		accessControl := aerospikev1alpha1.AerospikeAccessControlSpec{
			Roles: []aerospikev1alpha1.AerospikeRoleSpec{
				aerospikev1alpha1.AerospikeRoleSpec{
					Name: "profiler",
					Privileges: []string{
						"read-write-udf.test.users",
						"write",
					},
					Whitelist: []string{
						"8.8.0.0/16",
					},
				},
			},
			Users: []aerospikev1alpha1.AerospikeUserSpec{
				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "admin",
					SecretName: authSecretNameForUpdate,
					Roles: []string{
						"sys-admin",
						"user-admin",
					},
				},

				aerospikev1alpha1.AerospikeUserSpec{
					Name:       "profileUser",
					SecretName: authSecretNameForUpdate,
					Roles: []string{
						"data-admin",
						"read-write-udf",
						"write",
					},
				},
			},
		}

		aeroCluster := getAerospikeClusterSpecWithAccessControl(&accessControl, true, ctx)
		err := testAccessControlReconcile(aeroCluster, ctx, t)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("SecurityUpdateReject", func(t *testing.T) {
		aeroCluster := getAerospikeClusterSpecWithAccessControl(nil, false, ctx)
		err := testAccessControlReconcile(aeroCluster, ctx, t)
		if err == nil || !strings.Contains(err.Error(), "Cannot update cluster security config") {
			t.Error(err)
		}
	})

	if aeroCluster != nil {
		deleteCluster(t, f, ctx, aeroCluster)
	}
}

func getAerospikeClusterSpecWithAccessControl(accessControl *aerospikev1alpha1.AerospikeAccessControlSpec, enableSecurity bool, ctx *framework.TestCtx) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	kubeNs, _ := ctx.GetNamespace()
	// create Aerospike custom resource
	return &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "accesscontroltest",
			Namespace: kubeNs,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  testClusterSize,
			Build: latestClusterBuild,
			ValidationPolicy: &aerospikev1alpha1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			AerospikeAccessControl: accessControl,
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			MultiPodPerHost: true,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: aerospikev1alpha1.Values{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
				},
				"security": map[string]interface{}{
					"enable-security": enableSecurity,
				},
				"namespace": []interface{}{
					map[string]interface{}{
						"name":               "test",
						"memory-size":        1000955200,
						"replication-factor": 1,
						"storage-engine":     "memory",
					},
				},
			},
		},
	}
}

func testAccessControlReconcile(desired *aerospikev1alpha1.AerospikeCluster, ctx *framework.TestCtx, t *testing.T) error {
	err := aerospikeClusterCreateUpdate(desired, ctx, t)
	if err != nil {
		return err
	}

	current := &aerospikev1alpha1.AerospikeCluster{}
	err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		return err
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(desired.Spec.AerospikeAccessControl, current.Spec.AerospikeAccessControl) {
		return fmt.Errorf("Cluster state not applied. Desired: %v Current: %v", desired.Spec.AerospikeAccessControl, current.Spec.AerospikeAccessControl)
	}

	// Ensure the desired spec access control is correctly applied.
	return validateAccessControl(current)
}

// validateAccessControl validates that the new access control have been applied correctly.
func validateAccessControl(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	clientP, err := getClient(aeroCluster, &framework.Global.Client.Client)
	if err != nil {
		return fmt.Errorf("Error creating client: %v", err)
	}

	client := *clientP
	defer client.Close()

	err = validateRoles(clientP, &aeroCluster.Spec)
	if err != nil {
		return fmt.Errorf("Error creating client: %v", err)
	}

	pp := getPasswordProvider(aeroCluster, &framework.Global.Client.Client)
	err = validateUsers(clientP, aeroCluster, pp)
	return err
}

func getRole(roles []aerospikev1alpha1.AerospikeRoleSpec, roleName string) *aerospikev1alpha1.AerospikeRoleSpec {
	for _, role := range roles {
		if role.Name == roleName {
			return &role
		}
	}

	return nil
}

func getUser(users []aerospikev1alpha1.AerospikeUserSpec, userName string) *aerospikev1alpha1.AerospikeUserSpec {
	for _, user := range users {
		if user.Name == userName {
			return &user
		}
	}

	return nil
}

// validateRoles validates that the new roles have been applied correctly.
func validateRoles(clientP *as.Client, clusterSpec *aerospikev1alpha1.AerospikeClusterSpec) error {
	client := *clientP
	adminPolicy := asConfig.GetAdminPolicy(clusterSpec)
	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("Error querying roles: %v", err)
	}

	currentRoleNames := []string{}

	for _, role := range asRoles {
		_, isPredefined := asConfig.PredefinedRoles[role.Name]

		if !isPredefined {
			currentRoleNames = append(currentRoleNames, role.Name)
		}
	}

	expectedRoleNames := []string{}
	accessControl := clusterSpec.AerospikeAccessControl
	for _, role := range accessControl.Roles {
		expectedRoleNames = append(expectedRoleNames, role.Name)
	}

	if len(currentRoleNames) != len(expectedRoleNames) {
		return fmt.Errorf("Actual roles %v do not match expected roles %v", currentRoleNames, expectedRoleNames)
	}

	// Check values.
	if len(asConfig.SliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
		return fmt.Errorf("Actual roles %v do not match expected roles %v", currentRoleNames, expectedRoleNames)
	}

	// Verify the privileges and whitelists are correct.
	for _, asRole := range asRoles {
		_, isPredefined := asConfig.PredefinedRoles[asRole.Name]

		if isPredefined {
			continue
		}

		expectedRoleSpec := *getRole(accessControl.Roles, asRole.Name)
		expectedPrivilegeNames := expectedRoleSpec.Privileges

		currentPrivilegeNames := []string{}
		for _, privilege := range asRole.Privileges {
			temp, _ := asConfig.AerospikePrivilegeToPrivilegeString([]as.Privilege{privilege})
			currentPrivilegeNames = append(currentPrivilegeNames, temp[0])
		}

		if len(currentPrivilegeNames) != len(expectedPrivilegeNames) {
			return fmt.Errorf("For role %s actual privileges %v do not match expected privileges %v", asRole.Name, currentPrivilegeNames, expectedPrivilegeNames)
		}

		// Check values.
		if len(asConfig.SliceSubtract(expectedPrivilegeNames, currentPrivilegeNames)) != 0 {
			return fmt.Errorf("For role %s actual privileges %v do not match expected privileges %v", asRole.Name, currentPrivilegeNames, expectedPrivilegeNames)
		}

		// Validate whitelists.
		if !reflect.DeepEqual(expectedRoleSpec.Whitelist, asRole.Whitelist) {
			return fmt.Errorf("For role %s actual whitelist %v does not match expected whitelist %v", asRole.Name, asRole.Whitelist, expectedRoleSpec.Whitelist)
		}
	}

	return nil
}

// validateUsers validates that the new users have been applied correctly.
func validateUsers(clientP *as.Client, aeroCluster *aerospikev1alpha1.AerospikeCluster, pp asConfig.AerospikeUserPasswordProvider) error {
	clusterSpec := &aeroCluster.Spec
	client := *clientP

	adminPolicy := asConfig.GetAdminPolicy(clusterSpec)
	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		return fmt.Errorf("Error querying users: %v", err)
	}

	currentUserNames := []string{}

	for _, user := range asUsers {
		currentUserNames = append(currentUserNames, user.User)
	}

	expectedUserNames := []string{}
	accessControl := clusterSpec.AerospikeAccessControl
	for _, user := range accessControl.Users {
		expectedUserNames = append(expectedUserNames, user.Name)
	}

	if len(currentUserNames) != len(expectedUserNames) {
		return fmt.Errorf("Actual users %v do not match expected users %v", currentUserNames, expectedUserNames)
	}

	// Check values.
	if len(asConfig.SliceSubtract(expectedUserNames, currentUserNames)) != 0 {
		return fmt.Errorf("Actual users %v do not match expected users %v", currentUserNames, expectedUserNames)
	}

	// Verify the roles are correct.
	for _, asUser := range asUsers {
		expectedUserSpec := *getUser(accessControl.Users, asUser.User)
		// Validate that the new user password is applied
		password, err := pp.Get(asUser.User, &expectedUserSpec)

		if err != nil {
			return fmt.Errorf("For user %s cannot get password %v", asUser.User, err)
		}

		userClient, err := getClientForUser(asUser.User, password, aeroCluster, &framework.Global.Client.Client)
		if err != nil {
			return fmt.Errorf("For user %s cannot get client. Possible auth error :%v", asUser.User, err)
		}
		(*userClient).Close()

		expectedRoleNames := expectedUserSpec.Roles
		currentRoleNames := []string{}
		for _, roleName := range asUser.Roles {
			currentRoleNames = append(currentRoleNames, roleName)
		}

		if len(currentRoleNames) != len(expectedRoleNames) {
			return fmt.Errorf("For user %s actual roles %v do not match expected roles %v", asUser.User, currentRoleNames, expectedRoleNames)
		}

		// Check values.
		if len(asConfig.SliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
			return fmt.Errorf("For user %s actual roles %v do not match expected roles %v", asUser.User, currentRoleNames, expectedRoleNames)
		}
	}
	return nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
