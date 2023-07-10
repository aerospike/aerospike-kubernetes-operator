//go:build !noac

package test

import (
	goctx "context"
	"fmt"
	"math/rand"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

const (
	testClusterSize = 2
)

var aerospikeConfigWithSecurity = &asdbv1.AerospikeConfigSpec{
	Value: map[string]interface{}{
		"security": map[string]interface{}{},
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

var aerospikeConfigWithSecurityWithQuota = &asdbv1.AerospikeConfigSpec{
	Value: map[string]interface{}{
		"security": map[string]interface{}{
			"enable-quotas": true,
		},
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

var _ = Describe(
	"AccessControl", func() {

		Context(
			"AccessControl", func() {
				It(
					"Try ValidAccessControl", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs",
										"read.userNs",
										"sindex-admin",
										"truncate.userNs",
										"udf-admin",
									},
									Whitelist: []string{
										"8.8.0.0/16",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
								{
									Name:       "admin",
									SecretName: "someSecret",
									Roles: []string{
										"sys-admin",
										"user-admin",
										"truncate",
										"sindex-admin",
										"udf-admin",
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)
						Expect(err).ToNot(HaveOccurred())
						Expect(valid).To(
							BeTrue(), "Valid aerospike spec marked invalid",
						)
						// if !valid {
						// 	Fail(fmt.Sprintf("Valid aerospike spec marked invalid: %v", err))
						// }
					},
				)

				It(
					"Try MissingRequiredUserRoles", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs",
										"read-write.profileNs.set",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						// if valid || err == nil {
						// 	Fail(fmt.Sprintf("InValid aerospike spec validated")
						// }
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(err.Error(), "required") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'required'", err,
								),
							)
						}
					},
				)

				It(
					"Try InvalidUserRole", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs",
										"read-write.profileNs.set",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(err.Error(), "missingRole") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'missingRole'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try DuplicateUser", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs",
										"read-write.profileNs.set",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "duplicate",
						) || !strings.Contains(
							strings.ToLower(err.Error()), "aerospike",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and 'aerospike'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try DuplicateUserRole", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs",
										"read-write.profileNs.set",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "duplicate",
						) || !strings.Contains(
							strings.ToLower(err.Error()), "sys-admin",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and 'sys-admin'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try InvalidUserSecretName", func() {
						invalidSecretNames := []string{
							"", "   ",
						}

						for _, invalidSecretName := range invalidSecretNames {
							accessControl := asdbv1.AerospikeAccessControlSpec{
								Roles: []asdbv1.AerospikeRoleSpec{
									{
										Name: "profiler",
										Privileges: []string{
											"read-write.profileNs",
											"read-write.profileNs.set",
											"read.userNs",
										},
									},
								},
								Users: []asdbv1.AerospikeUserSpec{
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

							clusterSpec := asdbv1.AerospikeClusterSpec{
								Image:                  latestImage,
								AerospikeAccessControl: &accessControl,
								AerospikeConfig:        aerospikeConfigWithSecurity,
							}

							valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

							if valid || err == nil {
								Fail("InValid aerospike spec validated")
							}
							Expect(valid).To(
								BeFalse(), "InValid aerospike spec validated",
							)

							if !strings.Contains(
								err.Error(), "empty secret name",
							) {
								Fail(
									fmt.Sprintf(
										"Error: %v should contain 'empty secret name'",
										err,
									),
								)
							}
						}
					},
				)

				It(
					"Try InvalidUserName", func() {
						name64Chars := randString(64)
						invalidUserNames := []string{
							"",
							"    ",
							name64Chars,
							"aerospike:user",
							"aerospike;user",
						}

						for _, invalidUserName := range invalidUserNames {
							accessControl := asdbv1.AerospikeAccessControlSpec{
								Roles: []asdbv1.AerospikeRoleSpec{
									{
										Name: "profiler",
										Privileges: []string{
											"read-write.profileNs",
											"read-write.profileNs.set",
											"read.userNs",
										},
									},
								},
								Users: []asdbv1.AerospikeUserSpec{
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

							clusterSpec := asdbv1.AerospikeClusterSpec{
								Image:                  latestImage,
								AerospikeAccessControl: &accessControl,
								AerospikeConfig:        aerospikeConfigWithSecurity,
							}

							valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

							if valid || err == nil {
								Fail("InValid aerospike spec validated")
							}
							Expect(valid).To(
								BeFalse(), "InValid aerospike spec validated",
							)

							if !strings.Contains(
								strings.ToLower(err.Error()), "username",
							) && !strings.Contains(
								strings.ToLower(err.Error()), "empty",
							) {
								Fail(
									fmt.Sprintf(
										"Error: %v should contain 'username' or 'empty'",
										err,
									),
								)
							}
						}
					},
				)

				It(
					"Try InvalidRoleName", func() {
						name64Chars := randString(64)
						invalidRoleNames := []string{
							"",
							"    ",
							name64Chars,
							"aerospike:user",
							"aerospike;user",
						}

						for _, invalidRoleName := range invalidRoleNames {
							accessControl := asdbv1.AerospikeAccessControlSpec{
								Roles: []asdbv1.AerospikeRoleSpec{
									{
										Name: invalidRoleName,
										Privileges: []string{
											"read-write.profileNs",
											"read-write.profileNs.set",
											"read.userNs",
										},
									},
								},
								Users: []asdbv1.AerospikeUserSpec{
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

							clusterSpec := asdbv1.AerospikeClusterSpec{
								Image:                  latestImage,
								AerospikeAccessControl: &accessControl,
								AerospikeConfig:        aerospikeConfigWithSecurity,
							}

							valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

							if valid || err == nil {
								Fail("InValid aerospike spec validated")
							}
							Expect(valid).To(
								BeFalse(), "InValid aerospike spec validated",
							)

							if !strings.Contains(
								strings.ToLower(err.Error()), "role name",
							) && !strings.Contains(
								strings.ToLower(err.Error()), "empty",
							) {
								Fail(
									fmt.Sprintf(
										"Error: %v should contain 'role name' or 'empty'",
										err,
									),
								)
							}
						}
					},
				)

				It(
					"Try DuplicateRole", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "duplicate",
						) || !strings.Contains(
							strings.ToLower(err.Error()), "profiler",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and 'profiler'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try DuplicateRolePrivilege", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "duplicate",
						) || !strings.Contains(
							err.Error(), "read-write.profileNs",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and 'read-write.profileNs'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try DuplicateRoleWhitelist", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "duplicate",
						) || !strings.Contains(err.Error(), "8.8.0.0/16") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and '8.8.0.0/16'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try InvalidWhitelist", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "invalid cidr",
						) || !strings.Contains(err.Error(), "8.8.8.8/16") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'duplicate' and '8.8.8.8/16'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try PredefinedRoleUpdate", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(err.Error(), "predefined") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'predefined'",
									err,
								),
							)
						}
					},
				)
				It(
					"Try InvalidRoleWhitelist", func() {
						rand64Chars := randString(64)
						invalidWhitelists := []string{
							"",
							"    ",
							rand64Chars,
						}

						for _, invalidWhitelist := range invalidWhitelists {
							accessControl := asdbv1.AerospikeAccessControlSpec{
								Roles: []asdbv1.AerospikeRoleSpec{
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
								Users: []asdbv1.AerospikeUserSpec{
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

							clusterSpec := asdbv1.AerospikeClusterSpec{
								Image:                  latestImage,
								AerospikeAccessControl: &accessControl,
								AerospikeConfig:        aerospikeConfigWithSecurity,
							}

							valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

							if valid || err == nil {
								Fail("InValid aerospike spec validated")
							}
							Expect(valid).To(
								BeFalse(), "InValid aerospike spec validated",
							)

							if !strings.Contains(
								err.Error(), "invalid whitelist",
							) && !strings.Contains(err.Error(), "empty") {
								Fail(
									fmt.Sprintf(
										"Error: %v should contain 'invalid whitelist'",
										err,
									),
								)
							}
						}
					},
				)

				It(
					"Try MissingNamespacePrivilege", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.missingNs",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(err.Error(), "missingNs") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'missingNs'", err,
								),
							)
						}
					},
				)

				It(
					"Try MissingSetPrivilege", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs.",
										"read.userNs",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(err.Error(), "set name") {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'missingNs'", err,
								),
							)
						}
					},
				)

				It(
					"Try InvalidPrivilege", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs.setname",
										"read.userNs",
										"non-existent",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "invalid privilege",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'invalid privilege'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try InvalidPost6Privilege in 5.x", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name: "profiler",
									Privileges: []string{
										"read-write.profileNs.setname",
										"read.userNs",
										"non-existent",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  "aerospike/aerospike-server-enterprise:5.7.0.8",
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							strings.ToLower(err.Error()), "invalid privilege",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'invalid privilege'",
									err,
								),
							)
						}
					},
				)

				It(
					"Try valid Post 6.0 definedRoleUpdate", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
									// Should be ok pre 6.0 since this role was not predefined then.
									Name: "truncate",
									Privileges: []string{
										"read-write.profileNs",
										"read.userNs",
									},
									Whitelist: []string{
										"8.8.0.0/16",
									},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
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

								{
									Name:       "admin",
									SecretName: authSecretName,
									Roles: []string{
										"sys-admin",
										"user-admin",
									},
								},
							},
						}

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  "aerospike/aerospike-server-enterprise:5.7.0.8",
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						Expect(err).ToNot(HaveOccurred())
						Expect(valid).To(
							BeTrue(), "Valid aerospike spec marked invalid",
						)
					},
				)

				It(
					"Try InvalidGlobalScopeOnlyPrivilege", func() {
						accessControl := asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
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
							Users: []asdbv1.AerospikeUserSpec{
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

						clusterSpec := asdbv1.AerospikeClusterSpec{
							Image:                  latestImage,
							AerospikeAccessControl: &accessControl,
							AerospikeConfig:        aerospikeConfigWithSecurity,
						}

						valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

						if valid || err == nil {
							Fail("InValid aerospike spec validated")
						}
						Expect(valid).To(
							BeFalse(), "InValid aerospike spec validated",
						)

						if !strings.Contains(
							err.Error(), "namespace or set scope",
						) {
							Fail(
								fmt.Sprintf(
									"Error: %v should contain 'namespace or set scope'",
									err,
								),
							)
						}
					},
				)

				Context(
					"When cluster is not deployed", func() {

						ctx := goctx.Background()

						clusterName := "ac-invalid"
						clusterNamespacedName := getNamespacedName(
							clusterName, namespace,
						)

						It(
							"AccessControlValidation: should fail as Security is disabled but access control is specified",
							func() {
								// Just a smoke test to ensure validation hook works.
								accessControl := asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
									Users: []asdbv1.AerospikeUserSpec{
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

								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(false); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)

								err = aerospikeClusterCreateUpdate(
									k8sClient, aeroCluster, ctx,
								)
								if err == nil || !strings.Contains(
									err.Error(),
									"security is disabled but access control is specified",
								) {
									Fail("AccessControlValidation should have failed")
								}
							},
						)

						It(
							"AccessControlValidation: should fail, missing user-admin required role",
							func() {
								// Just a smoke test to ensure validation hook works.
								accessControl := asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
									Users: []asdbv1.AerospikeUserSpec{
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
								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err == nil || !strings.Contains(
									err.Error(), "required roles",
								) {
									Fail("AccessControlValidation should have failed")
								}
							},
						)
						It(
							"Try ValidAccessControlQuota", func() {
								accessControl := asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
										{
											Name: "profiler",
											Privileges: []string{
												"read-write.profileNs",
												"read.userNs",
											},
											Whitelist: []string{
												"8.8.0.0/16",
											},
											ReadQuota:  1,
											WriteQuota: 1,
										},
									},
									Users: []asdbv1.AerospikeUserSpec{
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

								clusterSpec := asdbv1.AerospikeClusterSpec{
									Image:                  latestImage,
									AerospikeAccessControl: &accessControl,
									AerospikeConfig:        aerospikeConfigWithSecurityWithQuota,
								}

								valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)
								Expect(err).ToNot(HaveOccurred())
								Expect(valid).To(
									BeTrue(),
									"Valid aerospike spec marked invalid",
								)
							},
						)
						It(
							"Try Invalid AccessControlEnableQuotaMissing",
							func() {
								accessControl := asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
										{
											Name: "profiler",
											Privileges: []string{
												"read-write.profileNs",
												"read.userNs",
											},
											Whitelist: []string{
												"8.8.0.0/16",
											},
											ReadQuota:  1,
											WriteQuota: 1,
										},
									},
									Users: []asdbv1.AerospikeUserSpec{
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
								clusterSpec := asdbv1.AerospikeClusterSpec{
									Image:                  latestImage,
									AerospikeAccessControl: &accessControl,
									AerospikeConfig:        aerospikeConfigWithSecurity,
								}

								valid, err := asdbv1.IsAerospikeAccessControlValid(&clusterSpec)

								if valid || err == nil {
									Fail("InValid aerospike spec validated")
								}
								Expect(valid).To(
									BeFalse(),
									"InValid aerospike spec validated",
								)
								if !strings.Contains(
									strings.ToLower(err.Error()),
									"invalid aerospike.security conf. enable-quotas: not present",
								) {
									Fail(
										fmt.Sprintf(
											"Error: %v enable-quotas: not present'",
											err,
										),
									)
								}
							},
						)
					},
				)
				Context(
					"When cluster is deployed", func() {
						ctx := goctx.Background()

						It(
							"SecurityUpdateReject: should fail, Cannot update cluster security config",
							func() {
								var accessControl *asdbv1.AerospikeAccessControlSpec

								clusterName := "ac-no-security"
								clusterNamespacedName := getNamespacedName(
									clusterName, namespace,
								)
								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(false); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								// Save cluster variable as well for cleanup.
								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)
								err = aerospikeClusterCreateUpdate(
									k8sClient, aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								accessControl = &asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
									Users: []asdbv1.AerospikeUserSpec{
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

								aerospikeConfigSpec, err = NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err == nil || !strings.Contains(
									err.Error(),
									"cannot update cluster security config",
								) {
									Fail("SecurityUpdate should have failed")
								}

								if aeroCluster != nil {
									err = deleteCluster(
										k8sClient, ctx,
										aeroCluster,
									)
									Expect(err).ToNot(HaveOccurred())
								}
							},
						)

						It(
							"AccessControlLifeCycle", func() {

								clusterName := "ac-lifecycle"
								clusterNamespacedName := getNamespacedName(
									clusterName, namespace,
								)

								By("AccessControlCreate")

								accessControl := asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
									Users: []asdbv1.AerospikeUserSpec{
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
								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}

								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								By("AccessControlUpdate")
								// Apply updates to drop users, drop roles, update privileges for roles and update roles for users.
								accessControl = asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
									Users: []asdbv1.AerospikeUserSpec{
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
								aerospikeConfigSpec, err = NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								By("SecurityUpdateReject")
								aerospikeConfigSpec, err = NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(false); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}
								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, nil,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err == nil || !strings.Contains(
									err.Error(),
									"cannot update cluster security config",
								) {
									Fail("SecurityUpdate should have failed")
								}

								By("EnableQuota")

								accessControl = asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
											ReadQuota:  1,
											WriteQuota: 1,
										},
									},
									Users: []asdbv1.AerospikeUserSpec{
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

								aerospikeConfigSpec, err = NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}
								if err = aerospikeConfigSpec.setEnableQuotas(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								By("QuotaParamsSpecifiedButFlagIsOff")

								accessControl = asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
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
											ReadQuota:  1,
											WriteQuota: 1,
										},
									},
									Users: []asdbv1.AerospikeUserSpec{
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

								aerospikeConfigSpec, err = NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}
								if err = aerospikeConfigSpec.setEnableSecurity(true); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}
								if err = aerospikeConfigSpec.setEnableQuotas(false); err != nil {
									Expect(err).ToNot(HaveOccurred())
								}

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err == nil || !strings.Contains(
									err.Error(),
									"denied the request: security.enable-quotas is set to false but quota params are",
								) {
									Fail("QuotaParamsSpecifiedButFlagIsOff should have failed")
								}

								if aeroCluster != nil {
									err = deleteCluster(
										k8sClient, ctx, aeroCluster,
									)
									Expect(err).ToNot(HaveOccurred())
								}
							},
						)
					},
				)
			},
		)
	},
)

func testAccessControlReconcile(
	desired *asdbv1.AerospikeCluster, ctx goctx.Context,
) error {
	err := aerospikeClusterCreateUpdate(k8sClient, desired, ctx)
	if err != nil {
		return err
	}

	current := &asdbv1.AerospikeCluster{}
	err = k8sClient.Get(
		ctx,
		types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace},
		current,
	)

	if err != nil {
		return err
	}

	// Ensure desired cluster spec is applied.
	if !reflect.DeepEqual(
		desired.Spec.AerospikeAccessControl,
		current.Spec.AerospikeAccessControl,
	) {
		return fmt.Errorf(
			"cluster state not applied. Desired: %v Current: %v",
			desired.Spec.AerospikeAccessControl,
			current.Spec.AerospikeAccessControl,
		)
	}

	// Ensure the desired spec access control is correctly applied.
	return validateAccessControl(pkgLog, current)
}

func getAerospikeClusterSpecWithAccessControl(
	clusterNamespacedName types.NamespacedName,
	accessControl *asdbv1.AerospikeAccessControlSpec,
	aerospikeConfSpec *AerospikeConfSpec,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size: testClusterSize,
			Image: fmt.Sprintf(
				"%s:%s", baseImage, aerospikeConfSpec.getVersion(),
			),
			ValidationPolicy: &asdbv1.ValidationPolicySpec{
				SkipWorkDirValidate:     true,
				SkipXdrDlogFileValidate: true,
			},
			AerospikeAccessControl: accessControl,
			Storage: asdbv1.AerospikeStorageSpec{
				Volumes: []asdbv1.VolumeSpec{
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: aerospikeConfSpec.getSpec(),
			},
		},
	}
}

// validateAccessControl validates that the new access control have been applied correctly.
func validateAccessControl(
	log logr.Logger, aeroCluster *asdbv1.AerospikeCluster,
) error {
	clientP, err := getClient(log, aeroCluster, k8sClient)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	client := *clientP
	defer client.Close()

	err = validateRoles(clientP, &aeroCluster.Spec)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	err = validateUsers(clientP, aeroCluster)

	return err
}

func getRole(
	roles []asdbv1.AerospikeRoleSpec, roleName string,
) *asdbv1.AerospikeRoleSpec {
	for _, role := range roles {
		if role.Name == roleName {
			return &role
		}
	}

	return nil
}

func getUser(
	users []asdbv1.AerospikeUserSpec, userName string,
) *asdbv1.AerospikeUserSpec {
	for _, user := range users {
		if user.Name == userName {
			return &user
		}
	}

	return nil
}

// validateRoles validates that the new roles have been applied correctly.
func validateRoles(
	clientP *as.Client, clusterSpec *asdbv1.AerospikeClusterSpec,
) error {
	client := *clientP
	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)

	asRoles, err := client.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying roles: %v", err)
	}

	var currentRoleNames []string

	for _, role := range asRoles {
		if _, isPredefined := asdbv1.PredefinedRoles[role.Name]; !isPredefined {
			if _, isPredefined = asdbv1.Post6PredefinedRoles[role.Name]; !isPredefined {
				currentRoleNames = append(currentRoleNames, role.Name)
			}
		}
	}

	accessControl := clusterSpec.AerospikeAccessControl
	expectedRoleNames := make([]string, 0, len(accessControl.Roles))

	for roleIndex := range accessControl.Roles {
		expectedRoleNames = append(expectedRoleNames, accessControl.Roles[roleIndex].Name)
	}

	if len(currentRoleNames) != len(expectedRoleNames) {
		return fmt.Errorf(
			"actual roles %v do not match expected roles %v", currentRoleNames,
			expectedRoleNames,
		)
	}

	// Check values.
	if len(
		aerospikecluster.SliceSubtract(
			expectedRoleNames, currentRoleNames,
		),
	) != 0 {
		return fmt.Errorf(
			"actual roles %v do not match expected roles %v", currentRoleNames,
			expectedRoleNames,
		)
	}

	// Verify the privileges and whitelists are correct.
	for _, asRole := range asRoles {
		if _, isPredefined := asdbv1.PredefinedRoles[asRole.Name]; isPredefined {
			continue
		}

		if _, isPredefined := asdbv1.Post6PredefinedRoles[asRole.Name]; isPredefined {
			continue
		}

		expectedRoleSpec := *getRole(accessControl.Roles, asRole.Name)
		expectedPrivilegeNames := expectedRoleSpec.Privileges

		var currentPrivilegeNames []string

		for _, privilege := range asRole.Privileges {
			temp, _ := aerospikecluster.AerospikePrivilegeToPrivilegeString([]as.Privilege{privilege})
			currentPrivilegeNames = append(currentPrivilegeNames, temp[0])
		}

		if len(currentPrivilegeNames) != len(expectedPrivilegeNames) {
			return fmt.Errorf(
				"for role %s actual privileges %v do not match expected"+
					" privileges %v",
				asRole.Name, currentPrivilegeNames, expectedPrivilegeNames,
			)
		}

		// Check values.
		if len(
			aerospikecluster.SliceSubtract(
				expectedPrivilegeNames, currentPrivilegeNames,
			),
		) != 0 {
			return fmt.Errorf(
				"for role %s actual privileges %v do not match expected"+
					" privileges %v",
				asRole.Name, currentPrivilegeNames, expectedPrivilegeNames,
			)
		}

		// Validate Write Quota
		if expectedRoleSpec.WriteQuota != asRole.WriteQuota {
			return fmt.Errorf(
				"for role %s actual write-qouta %d does not match expected write-quota %d",
				asRole.Name, asRole.WriteQuota, expectedRoleSpec.WriteQuota,
			)
		}

		// Validate Read Quota
		if expectedRoleSpec.ReadQuota != asRole.ReadQuota {
			return fmt.Errorf(
				"for role %s actual read-quota %v does not match expected read-quota %v",
				asRole.Name, asRole.ReadQuota, expectedRoleSpec.ReadQuota,
			)
		}

		// Validate whitelists.
		if !reflect.DeepEqual(expectedRoleSpec.Whitelist, asRole.Whitelist) {
			return fmt.Errorf(
				"for role %s actual whitelist %v does not match expected"+
					" whitelist %v",
				asRole.Name, asRole.Whitelist, expectedRoleSpec.Whitelist,
			)
		}
	}

	return nil
}

// validateUsers validates that the new users have been applied correctly.
func validateUsers(
	clientP *as.Client, aeroCluster *asdbv1.AerospikeCluster,
) error {
	clusterSpec := &aeroCluster.Spec
	client := *clientP

	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)

	asUsers, err := client.QueryUsers(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying users: %v", err)
	}

	currentUserNames := make([]string, 0, len(asUsers))

	for userIndex := range asUsers {
		currentUserNames = append(currentUserNames, asUsers[userIndex].User)
	}

	accessControl := clusterSpec.AerospikeAccessControl
	expectedUserNames := make([]string, 0, len(accessControl.Users))

	for userIndex := range accessControl.Users {
		expectedUserNames = append(expectedUserNames, accessControl.Users[userIndex].Name)
	}

	if len(currentUserNames) != len(expectedUserNames) {
		return fmt.Errorf(
			"actual users %v do not match expected users %v", currentUserNames,
			expectedUserNames,
		)
	}

	// Check values.
	if len(
		aerospikecluster.SliceSubtract(
			expectedUserNames, currentUserNames,
		),
	) != 0 {
		return fmt.Errorf(
			"actual users %v do not match expected users %v", currentUserNames,
			expectedUserNames,
		)
	}

	// Verify the roles are correct.
	for _, asUser := range asUsers {
		expectedUserSpec := *getUser(accessControl.Users, asUser.User)

		userClient, err := getClient(
			pkgLog, aeroCluster, k8sClient,
		)
		if err != nil {
			return fmt.Errorf(
				"for user %s cannot get client. Possible auth error :%v",
				asUser.User, err,
			)
		}

		(*userClient).Close()

		expectedRoleNames := expectedUserSpec.Roles

		var currentRoleNames []string

		currentRoleNames = append(currentRoleNames, asUser.Roles...)

		if len(currentRoleNames) != len(expectedRoleNames) {
			return fmt.Errorf(
				"for user %s actual roles %v do not match expected roles %v",
				asUser.User, currentRoleNames, expectedRoleNames,
			)
		}

		// Check values.
		if len(
			aerospikecluster.SliceSubtract(
				expectedRoleNames, currentRoleNames,
			),
		) != 0 {
			return fmt.Errorf(
				"for user %s actual roles %v do not match expected roles %v",
				asUser.User, currentRoleNames, expectedRoleNames,
			)
		}
	}

	return nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))] //nolint:gosec // for testing
	}

	return string(b)
}
