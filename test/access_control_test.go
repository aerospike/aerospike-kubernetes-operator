// +build !noac

package test

import (
	goctx "context"
	"fmt"
	"math/rand"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	as "github.com/ashishshinde/aerospike-client-go/v5"
)

const (
	testClusterSize = 2
)

var aerospikeConfigWithSecurity = &asdbv1alpha1.AerospikeConfigSpec{
	Value: map[string]interface{}{
		"security": map[string]interface{}{"enable-security": true},
		"namespaces": []interface{}{
			map[string]interface{}{
				"name": "profileNs",
			},
			map[string]interface{}{
				"name": "userNs",
			},
		},
	},
}

var _ = Describe("AccessControl", func() {

	Context("AccessControl", func() {
		It("Try ValidAccessControl", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
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
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)
			Expect(err).ToNot(HaveOccurred())
			Expect(valid).To(BeTrue(), "Valid aerospike spec marked invalid")
			// if !valid {
			// 	Fail(fmt.Sprintf("Valid aerospike spec marked invalid: %v", err))
			// }
		})

		It("Try MissingRequiredUserRoles", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			// if valid || err == nil {
			// 	Fail(fmt.Sprintf("InValid aerospike spec validated")
			// }
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "required") {
				Fail(fmt.Sprintf("Error: %v should contain 'required'", err))
			}
		})

		It("Try InvalidUserRole", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
							"missingRole",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "missingRole") {
				Fail(fmt.Sprintf("Error: %v should contain 'missingRole'", err))
			}
		})

		It("Try DuplicateUser", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") || !strings.Contains(strings.ToLower(err.Error()), "aerospike") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and 'aerospike'", err))
			}
		})

		It("Try DuplicateUserRole", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
							"sys-admin",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") || !strings.Contains(strings.ToLower(err.Error()), "sys-admin") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and 'sys-admin'", err))
			}
		})

		It("Try InvalidUserSecretName", func() {
			invalidSecretNames := []string{
				"", "   ",
			}

			for _, invalidSecretName := range invalidSecretNames {
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.profileNs",
								"read-write.profileNs.set",
								"read.userNs",
							},
						},
					},
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "aerospike",
							SecretName: "someSecret",
							Roles: []string{
								"sys-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: invalidSecretName,
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
					AerospikeAccessControl: &accessControl,

					AerospikeConfig: aerospikeConfigWithSecurity,
				}

				valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

				if valid || err == nil {
					Fail("InValid aerospike spec validated")
				}
				Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

				if !strings.Contains(err.Error(), "empty secret name") {
					Fail(fmt.Sprintf("Error: %v should contain 'empty secret name'", err))
				}
			}
		})

		It("Try InvalidUserName", func() {
			name64Chars := randString(64)
			invalidUserNames := []string{
				"",
				"    ",
				name64Chars,
				"aerospike:user",
				"aerospike;user",
			}

			for _, invalidUserName := range invalidUserNames {
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.profileNs",
								"read-write.profileNs.set",
								"read.userNs",
							},
						},
					},
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "aerospike",
							SecretName: "someSecret",
							Roles: []string{
								"sys-admin",
							},
						},

						{
							Name:       invalidUserName,
							SecretName: "someOtherSecret",
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
					AerospikeAccessControl: &accessControl,

					AerospikeConfig: aerospikeConfigWithSecurity,
				}

				valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

				if valid || err == nil {
					Fail("InValid aerospike spec validated")
				}
				Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

				if !strings.Contains(strings.ToLower(err.Error()), "username") && !strings.Contains(strings.ToLower(err.Error()), "empty") {
					Fail(fmt.Sprintf("Error: %v should contain 'username' or 'empty'", err))
				}
			}
		})

		It("Try InvalidRoleName", func() {
			name64Chars := randString(64)
			invalidRoleNames := []string{
				"",
				"    ",
				name64Chars,
				"aerospike:user",
				"aerospike;user",
			}

			for _, invalidRoleName := range invalidRoleNames {
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: invalidRoleName,
							Privileges: []string{
								"read-write.profileNs",
								"read-write.profileNs.set",
								"read.userNs",
							},
						},
					},
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "aerospike",
							SecretName: "someSecret",
							Roles: []string{
								"sys-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: "someOtherSecret",
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
					AerospikeAccessControl: &accessControl,

					AerospikeConfig: aerospikeConfigWithSecurity,
				}

				valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

				if valid || err == nil {
					Fail("InValid aerospike spec validated")
				}
				Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

				if !strings.Contains(strings.ToLower(err.Error()), "role name") && !strings.Contains(strings.ToLower(err.Error()), "empty") {
					Fail(fmt.Sprintf("Error: %v should contain 'role name' or 'empty'", err))
				}
			}
		})

		It("Try DuplicateRole", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") || !strings.Contains(strings.ToLower(err.Error()), "profiler") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and 'profiler'", err))
			}
		})

		It("Try DuplicateRolePrivilege", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read-write.profileNs",
							"read-write.profileNs.set",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") || !strings.Contains(err.Error(), "read-write.profileNs") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and 'read-write.profileNs'", err))
			}
		})

		It("Try DuplicateRoleWhitelist", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
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
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") || !strings.Contains(err.Error(), "8.8.0.0/16") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and '8.8.0.0/16'", err))
			}
		})

		It("Try InvalidWhitelist", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
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
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "invalid cidr") || !strings.Contains(err.Error(), "8.8.8.8/16") {
				Fail(fmt.Sprintf("Error: %v should contain 'duplicate' and '8.8.8.8/16'", err))
			}
		})

		It("Try PredefinedRoleUpdate", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs",
							"read.userNs",
						},
						Whitelist: []string{
							"8.8.0.0/16",
						},
					},
					{
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
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "predefined") {
				Fail(fmt.Sprintf("Error: %v should contain 'predefined'", err))
			}
		})

		It("Try InvalidRoleWhitelist", func() {
			rand64Chars := randString(64)
			invalidWhitelists := []string{
				"",
				"    ",
				rand64Chars,
			}

			for _, invalidWhitelist := range invalidWhitelists {
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.profileNs",
								"read-write.profileNs.set",
								"read.userNs",
							},
							Whitelist: []string{invalidWhitelist},
						},
					},
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "aerospike",
							SecretName: "someSecret",
							Roles: []string{
								"sys-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: "someOtherSecret",
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
					AerospikeAccessControl: &accessControl,

					AerospikeConfig: aerospikeConfigWithSecurity,
				}

				valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

				if valid || err == nil {
					Fail("InValid aerospike spec validated")
				}
				Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

				if !strings.Contains(err.Error(), "invalid whitelist") && !strings.Contains(err.Error(), "empty") {
					Fail(fmt.Sprintf("Error: %v should contain 'invalid whitelist'", err))
				}
			}
		})

		It("Try MissingNamespacePrivilege", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.missingNs",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "missingNs") {
				Fail(fmt.Sprintf("Error: %v should contain 'missingNs'", err))
			}
		})

		It("Try MissingSetPrivilege", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs.",
							"read.userNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "set name") {
				Fail(fmt.Sprintf("Error: %v should contain 'missingNs'", err))
			}
		})

		It("Try InvalidPrivilege", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs.setname",
							"read.userNs",
							"non-existent",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(strings.ToLower(err.Error()), "invalid privilege") {
				Fail(fmt.Sprintf("Error: %v should contain 'invalid privilege'", err))
			}
		})

		It("Try InvalidGlobalScopeOnlyPrivilege", func() {
			accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
				Roles: []asdbv1alpha1.AerospikeRoleSpec{
					{
						Name: "profiler",
						Privileges: []string{
							"read-write.profileNs.setname",
							"read.userNs",
							// This should not be allowed.
							"sys-admin.profileNs",
						},
					},
				},
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "aerospike",
						SecretName: "someSecret",
						Roles: []string{
							"sys-admin",
						},
					},

					{
						Name:       "profileUser",
						SecretName: "someOtherSecret",
						Roles: []string{
							"profiler",
						},
					},
				},
			}

			clusterSpec := asdbv1alpha1.AerospikeClusterSpec{
				AerospikeAccessControl: &accessControl,

				AerospikeConfig: aerospikeConfigWithSecurity,
			}

			valid, err := asdbv1alpha1.IsAerospikeAccessControlValid(&clusterSpec)

			if valid || err == nil {
				Fail("InValid aerospike spec validated")
			}
			Expect(valid).To(BeFalse(), "InValid aerospike spec validated")

			if !strings.Contains(err.Error(), "namespace or set scope") {
				Fail(fmt.Sprintf("Error: %v should contain 'namespace or set scope'", err))
			}
		})

		Context("When cluster is not deployed", func() {

			ctx := goctx.Background()

			clusterName := "ac-invalid"
			clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

			It("AccessControlValidation: should fail as Security is disabled but access control is specified", func() {
				// Just a smoke test to ensure validation hook works.
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.test",
								"read-write-udf.test.users",
							},
							Whitelist: []string{
								"8.8.0.0/16",
							},
						},
						{
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
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "admin",
							SecretName: authSecretName,
							Roles: []string{
								// Missing required user admin role.
								"sys-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
								"sys-admin",
							},
						},

						{
							Name:       "userToDrop",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				aeroCluster := getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, &accessControl, false, ctx)
				err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
				if err == nil || !strings.Contains(err.Error(), "security is disabled but access control is specified") {
					Fail("AccessControlValidation should have failed")
				}
			})

			It("AccessControlValidation: should fail, missing user-admin required role", func() {
				// Just a smoke test to ensure validation hook works.
				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.test",
								"read-write-udf.test.users",
							},
							Whitelist: []string{
								"8.8.0.0/16",
							},
						},
						{
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
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "admin",
							SecretName: authSecretName,
							Roles: []string{
								// Missing required user admin role.
								"sys-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
								"sys-admin",
							},
						},

						{
							Name:       "userToDrop",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				// Save cluster variable as well for cleanup.
				aeroCluster := getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, &accessControl, true, ctx)
				err := testAccessControlReconcile(aeroCluster, ctx)
				if err == nil || !strings.Contains(err.Error(), "required roles") {
					Fail("AccessControlValidation should have failed")
				}
			})
		})

		Context("When cluster is deployed", func() {
			ctx := goctx.Background()

			It("SecurityUpdateReject: should fail, Cannot update cluster security config", func() {
				var accessControl *asdbv1alpha1.AerospikeAccessControlSpec

				clusterName := "ac-no-security"
				clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

				// Save cluster variable as well for cleanup.
				aeroCluster := getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, accessControl, false, ctx)
				err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
				Expect(err).ToNot(HaveOccurred())

				accessControl = &asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
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
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "admin",
							SecretName: authSecretNameForUpdate,
							Roles: []string{
								"sys-admin",
								"user-admin",
							},
						},

						{
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

				aeroCluster = getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, accessControl, true, ctx)
				err = testAccessControlReconcile(aeroCluster, ctx)
				if err == nil || !strings.Contains(err.Error(), "cannot update cluster security config") {
					Fail("SecurityUpdate should have failed")
				}

				if aeroCluster != nil {
					deleteCluster(k8sClient, ctx, aeroCluster)
				}
			})

			It("AccessControlLifeCycle", func() {

				clusterName := "ac-lifecycle"
				clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

				By("AccessControlCreate")

				accessControl := asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
							Name: "profiler",
							Privileges: []string{
								"read-write.test",
								"read-write-udf.test.users",
							},
						},
						{
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
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "admin",
							SecretName: authSecretName,
							Roles: []string{
								"sys-admin",
								"user-admin",
							},
						},

						{
							Name:       "profileUser",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
								"sys-admin",
							},
						},

						{
							Name:       "userToDrop",
							SecretName: authSecretName,
							Roles: []string{
								"profiler",
							},
						},
					},
				}

				aeroCluster := getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, &accessControl, true, ctx)
				err := testAccessControlReconcile(aeroCluster, ctx)
				Expect(err).ToNot(HaveOccurred())

				By("AccessControlUpdate")
				// Apply updates to drop users, drop roles, update privileges for roles and update roles for users.
				accessControl = asdbv1alpha1.AerospikeAccessControlSpec{
					Roles: []asdbv1alpha1.AerospikeRoleSpec{
						{
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
					Users: []asdbv1alpha1.AerospikeUserSpec{
						{
							Name:       "admin",
							SecretName: authSecretNameForUpdate,
							Roles: []string{
								"sys-admin",
								"user-admin",
							},
						},

						{
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

				aeroCluster = getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, &accessControl, true, ctx)
				err = testAccessControlReconcile(aeroCluster, ctx)
				Expect(err).ToNot(HaveOccurred())

				By("SecurityUpdateReject")
				aeroCluster = getAerospikeClusterSpecWithAccessControl(clusterNamespacedName, nil, false, ctx)
				err = testAccessControlReconcile(aeroCluster, ctx)
				if err == nil || !strings.Contains(err.Error(), "cannot update cluster security config") {
					Fail("SecurityUpdate should have failed")
				}

				if aeroCluster != nil {
					deleteCluster(k8sClient, ctx, aeroCluster)
				}
			})
		})
	})
})

func testAccessControlReconcile(desired *asdbv1alpha1.AerospikeCluster, ctx goctx.Context) error {
	err := aerospikeClusterCreateUpdate(k8sClient, desired, ctx)
	if err != nil {
		return err
	}

	current := &asdbv1alpha1.AerospikeCluster{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		return err
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(desired.Spec.AerospikeAccessControl, current.Spec.AerospikeAccessControl) {
		return fmt.Errorf("cluster state not applied. Desired: %v Current: %v", desired.Spec.AerospikeAccessControl, current.Spec.AerospikeAccessControl)
	}

	// Ensure the desired spec access control is correctly applied.
	return validateAccessControl(current)
}

func getAerospikeClusterSpecWithAccessControl(clusterNamespacedName types.NamespacedName, accessControl *asdbv1alpha1.AerospikeAccessControlSpec, enableSecurity bool, ctx goctx.Context) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	// create Aerospike custom resource
	return &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  testClusterSize,
			Image: latestClusterImage,
			ValidationPolicy: &asdbv1alpha1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			AerospikeAccessControl: accessControl,
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			PodSpec: asdbv1alpha1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
					},
					"security": map[string]interface{}{
						"enable-security": enableSecurity,
					},
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"memory-size":        1000955200,
							"replication-factor": 1,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			},
		},
	}
}

// validateAccessControl validates that the new access control have been applied correctly.
func validateAccessControl(aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	clientP, err := getClient(aeroCluster, k8sClient)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	client := *clientP
	defer client.Close()

	err = validateRoles(clientP, &aeroCluster.Spec)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	pp := getPasswordProvider(aeroCluster, k8sClient)
	err = validateUsers(clientP, aeroCluster, pp)
	return err
}

func getRole(roles []asdbv1alpha1.AerospikeRoleSpec, roleName string) *asdbv1alpha1.AerospikeRoleSpec {
	for _, role := range roles {
		if role.Name == roleName {
			return &role
		}
	}

	return nil
}

func getUser(users []asdbv1alpha1.AerospikeUserSpec, userName string) *asdbv1alpha1.AerospikeUserSpec {
	for _, user := range users {
		if user.Name == userName {
			return &user
		}
	}

	return nil
}

// validateRoles validates that the new roles have been applied correctly.
func validateRoles(clientP *as.Client, clusterSpec *asdbv1alpha1.AerospikeClusterSpec) error {
	client := *clientP
	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)
	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying roles: %v", err)
	}

	currentRoleNames := []string{}

	for _, role := range asRoles {
		_, isPredefined := asdbv1alpha1.PredefinedRoles[role.Name]

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
	if len(aerospikecluster.SliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
		return fmt.Errorf("Actual roles %v do not match expected roles %v", currentRoleNames, expectedRoleNames)
	}

	// Verify the privileges and whitelists are correct.
	for _, asRole := range asRoles {
		_, isPredefined := asdbv1alpha1.PredefinedRoles[asRole.Name]

		if isPredefined {
			continue
		}

		expectedRoleSpec := *getRole(accessControl.Roles, asRole.Name)
		expectedPrivilegeNames := expectedRoleSpec.Privileges

		currentPrivilegeNames := []string{}
		for _, privilege := range asRole.Privileges {
			temp, _ := aerospikecluster.AerospikePrivilegeToPrivilegeString([]as.Privilege{privilege})
			currentPrivilegeNames = append(currentPrivilegeNames, temp[0])
		}

		if len(currentPrivilegeNames) != len(expectedPrivilegeNames) {
			return fmt.Errorf("For role %s actual privileges %v do not match expected privileges %v", asRole.Name, currentPrivilegeNames, expectedPrivilegeNames)
		}

		// Check values.
		if len(aerospikecluster.SliceSubtract(expectedPrivilegeNames, currentPrivilegeNames)) != 0 {
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
func validateUsers(clientP *as.Client, aeroCluster *asdbv1alpha1.AerospikeCluster, pp aerospikecluster.AerospikeUserPasswordProvider) error {
	clusterSpec := &aeroCluster.Spec
	client := *clientP

	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)
	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying users: %v", err)
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
	if len(aerospikecluster.SliceSubtract(expectedUserNames, currentUserNames)) != 0 {
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

		userClient, err := getClientForUser(asUser.User, password, aeroCluster, k8sClient)
		if err != nil {
			return fmt.Errorf("For user %s cannot get client. Possible auth error :%v", asUser.User, err)
		}
		(*userClient).Close()

		expectedRoleNames := expectedUserSpec.Roles
		currentRoleNames := []string{}
		currentRoleNames = append(currentRoleNames, asUser.Roles...)

		if len(currentRoleNames) != len(expectedRoleNames) {
			return fmt.Errorf("For user %s actual roles %v do not match expected roles %v", asUser.User, currentRoleNames, expectedRoleNames)
		}

		// Check values.
		if len(aerospikecluster.SliceSubtract(expectedRoleNames, currentRoleNames)) != 0 {
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
