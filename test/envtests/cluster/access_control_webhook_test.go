/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

// Minimal TLS network config and operator cert for webhook tests (cluster package helpers are unexported).
func networkTLSConfigForTest() map[string]interface{} {
	return map[string]interface{}{
		"service": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 4333,
			"port":     3000,
		},
		"fabric": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 3011,
			"port":     3001,
		},
		"heartbeat": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 3012,
			"port":     3002,
		},
		"tls": []interface{}{
			map[string]interface{}{
				"name":      "aerospike-a-0.test-runner",
				"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
				"key-file":  "/etc/aerospike/secret/svc_key.pem",
				"ca-file":   "/etc/aerospike/secret/cacert.pem",
			},
		},
	}
}

func adminOperatorCertForTest() *asdbv1.AerospikeOperatorClientCertSpec {
	return &asdbv1.AerospikeOperatorClientCertSpec{
		AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
			SecretCertSource: &asdbv1.AerospikeSecretCertSource{
				SecretName:         "aerospike-secret",
				CaCertsFilename:    "cacert.pem",
				ClientCertFilename: "admin_chain.pem",
				ClientKeyFilename:  "admin_key.pem",
			},
		},
	}
}

// expectDeployFailsACWebhook expects admission create to fail with the given message substrings.
func expectDeployFailsACWebhook(ctx context.Context, cluster *asdbv1.AerospikeCluster, subs ...string) {
	err := envtests.K8sClient.Create(ctx, cluster)
	Expect(err).To(HaveOccurred())

	args := append([]string{"\"vaerospikecluster.kb.io\""}, subs...)
	envtests.NewStatusErrorMatcher().WithMessageSubstrings(args...).Validate(err)
}

// profilerRoleForWebhookTest matches namespace "test" from CreateDummyAerospikeCluster.
func profilerRoleForWebhookTest() []asdbv1.AerospikeRoleSpec {
	return []asdbv1.AerospikeRoleSpec{
		{
			Name:       "profiler",
			Privileges: []string{"read-write.test", "read.test"},
		},
	}
}

// validAccessControlForDeployPositive mirrors test/cluster Try ValidAccessControl; privileges use
// namespace "test" to match CreateDummyAerospikeCluster rack namespace config.
func validAccessControlForDeployPositive() *asdbv1.AerospikeAccessControlSpec {
	return &asdbv1.AerospikeAccessControlSpec{
		Roles: []asdbv1.AerospikeRoleSpec{
			{
				Name: "profiler",
				Privileges: []string{
					"read-write.test",
					"read.test",
					"sindex-admin",
					"truncate.test",
					"udf-admin",
				},
				Whitelist: []string{"8.8.0.0/16"},
			},
		},
		Users: []asdbv1.AerospikeUserSpec{
			{
				Name:       "admin",
				SecretName: test.AuthSecretName,
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
				SecretName: test.AuthSecretName,
				Roles:      []string{"profiler"},
			},
		},
	}
}

// validAccessControlForDeployPositiveQuota mirrors test/cluster Try ValidAccessControlQuota.
func validAccessControlForDeployPositiveQuota() *asdbv1.AerospikeAccessControlSpec {
	return &asdbv1.AerospikeAccessControlSpec{
		Roles: []asdbv1.AerospikeRoleSpec{
			{
				Name: "profiler",
				Privileges: []string{
					"read-write.test",
					"read.test",
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
				SecretName: test.AuthSecretName,
				Roles: []string{
					"sys-admin",
					"user-admin",
				},
			},
			{
				Name:       "profileUser",
				SecretName: test.AuthSecretName,
				Roles:      []string{"profiler"},
			},
		},
	}
}

var _ = Describe("AerospikeCluster access control validation (envtests)", func() {
	const (
		accessControlClusterName = "access-control-webhook-cluster"
	)

	ctx := context.TODO()
	clusterNamespacedName := uniqueNamespacedName(accessControlClusterName)

	Context("Deploy validation", func() {
		AfterEach(func() {
			deleteCluster(ctx, clusterNamespacedName)
		})

		Context("spec.aerospikeAccessControl (validation)", func() {
			Context("negative", func() {
				It("fails when security is disabled but access control is specified", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"security is disabled but access control is specified").
						Validate(err)
				})

				It("fails when admin user is missing the user-admin role", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl.Users[0].Roles = []string{
						"sys-admin",
						"read-write",
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "no admin user with required roles")
				})

				It("fails when admin user is missing with required roles", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "aerospike",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin"},
							},
							{
								Name:       "other",
								SecretName: test.AuthSecretName,
								Roles:      []string{"read"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "no admin user with required roles")
				})
			})

			Context("positive", func() {
				It("allows deploy with valid roles and users", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = validAccessControlForDeployPositive()

					valid, err := asdbv1.IsAerospikeAccessControlValid(&aeroCluster.Spec)
					Expect(err).ToNot(HaveOccurred())
					Expect(valid).To(BeTrue(), "IsAerospikeAccessControlValid should accept spec")

					err = envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})

				It("allows deploy with role quotas when security.enable-quotas is true", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeySecurity] = map[string]interface{}{
						"enable-quotas": true,
					}
					aeroCluster.Spec.AerospikeAccessControl = validAccessControlForDeployPositiveQuota()

					valid, err := asdbv1.IsAerospikeAccessControlValid(&aeroCluster.Spec)
					Expect(err).ToNot(HaveOccurred())
					Expect(valid).To(BeTrue(), "IsAerospikeAccessControlValid should accept spec with quotas")

					err = envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})

		Context("spec.aerospikeAccessControl (roles)", func() {
			Context("negative", func() {
				It("fails on duplicate custom role definitions", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{Name: "profiler", Privileges: []string{"read-write.test", "read.test"}},
							{Name: "profiler", Privileges: []string{"read-write.test", "read.test"}},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"Duplicate value: {\"name\":\"profiler\"}").
						Validate(err)
				})

				DescribeTable("fails on invalid custom role name",
					func(invalidRoleName string, msgSubs []string) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name:       invalidRoleName,
									Privileges: []string{"read-write.test", "read.test"},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
								{
									Name:       "aerospike",
									SecretName: test.AuthSecretName,
									Roles:      []string{"sys-admin"},
								},
								{
									Name:       "profileUser",
									SecretName: test.AuthSecretName,
									Roles:      []string{"profiler"},
								},
							},
						}

						expectDeployFailsACWebhook(ctx, aeroCluster, msgSubs...)
					},
					Entry("empty string", "", []string{"role name cannot be empty"}),
					Entry("whitespace only", "    ", []string{"role name cannot be empty"}),
					Entry("exceeds max length", strings.Repeat("a", 64), []string{"cannot have more than 63 characters"}),
					Entry("colon in role name", "aerospike:user", []string{"cannot contain", ":"}),
					Entry("semicolon in role name", "aerospike;user", []string{"cannot contain", ";"}),
				)

				It("fails when attempting to define a predefined role", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "sys-admin",
								Privileges: []string{"read-write.test", "read.test"},
								Whitelist:  []string{"8.8.0.0/16"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "cannot create or modify predefined role")
				})

				It("fails on duplicate privilege in a role", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name: "profiler",
								Privileges: []string{
									"read-write.test",
									"read-write.test",
									"read.test",
								},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"Duplicate value: \"read-write.test\"").
						Validate(err)
				})

				It("fails on duplicate whitelist entry for a role", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test", "read.test"},
								Whitelist:  []string{"8.8.0.0/16", "8.8.0.0/16"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"Duplicate value: \"8.8.0.0/16\"").
						Validate(err)
				})

				It("fails on invalid CIDR in role whitelist", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test", "read.test"},
								Whitelist:  []string{"8.8.8.8/16"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "invalid whitelist")
				})

				DescribeTable("fails on invalid role whitelist entry",
					func(invalidWhitelist string, msgSubs []string) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
							Roles: []asdbv1.AerospikeRoleSpec{
								{
									Name:       "profiler",
									Privileges: []string{"read-write.test", "read.test"},
									Whitelist:  []string{invalidWhitelist},
								},
							},
							Users: []asdbv1.AerospikeUserSpec{
								{
									Name:       "aerospike",
									SecretName: test.AuthSecretName,
									Roles:      []string{"sys-admin"},
								},
								{
									Name:       "profileUser",
									SecretName: test.AuthSecretName,
									Roles:      []string{"profiler"},
								},
							},
						}

						expectDeployFailsACWebhook(ctx, aeroCluster, msgSubs...)
					},
					Entry("empty string", "", []string{"invalid whitelist"}),
					Entry("whitespace only", "    ", []string{"invalid whitelist"}),
					Entry("exceeds reasonable address length", strings.Repeat("x", 64), []string{"invalid whitelist"}),
				)

				It("fails when privilege references a namespace not in config", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.missingNs", "read.test"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "missingNs", "not configured")
				})

				It("fails when namespace-scoped privilege has an empty set name", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test.", "read.test"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}
					expectDeployFailsACWebhook(ctx, aeroCluster,
						"role 'profiler' has invalid privilege",
						"read-write.test.",
						"invalid set name")
				})

				It("fails on unknown privilege string", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test", "read.test", "non-existent"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "invalid privilege", "non-existent")
				})

				It("fails when a global-only privilege uses namespace scope", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test", "read.test", "sys-admin.test"},
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "namespace or set scope", "sys-admin.test")
				})

				It("fails when role quotas are set but security enable-quotas is not configured", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: []asdbv1.AerospikeRoleSpec{
							{
								Name:       "profiler",
								Privileges: []string{"read-write.test", "read.test"},
								ReadQuota:  1,
								WriteQuota: 1,
							},
						},
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "u1",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster,
						"invalid aerospike.security conf.", "enable-quotas: not present")
				})
			})
		})

		Context("spec.aerospikeAccessControl (users)", func() {
			Context("negative", func() {
				It("fails when PKIOnly authMode is used with Enterprise image below 8.1.0.0", func() {
					aeroCluster := testCluster.CreateAerospikeClusterPost640(clusterNamespacedName, 2,
						testutil.GetEnterpriseImage(testutil.Pre810EnterpriseImage))
					aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
					aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""
					errPre810 := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(errPre810).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"PKIOnly authMode requires Enterprise Edition version 8.1.0.0 or later").
						Validate(errPre810)
				})

				It("fails if FE and auth mode of all users is not set to PKIOnly", func() {
					accessControl := &asdbv1.AerospikeAccessControlSpec{
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:     "admin",
								AuthMode: asdbv1.AerospikeAuthModePKIOnly,
								Roles:    []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "user01",
								AuthMode:   asdbv1.AerospikeAuthModeInternal,
								SecretName: test.AuthSecretName,
								Roles: []string{
									"sys-admin",
									"user-admin",
								},
							},
						},
					}

					aeroCluster := testCluster.GetPKIAuthAerospikeClusterWithAccessControl(
						clusterNamespacedName, 2, accessControl,
					)
					aeroCluster.Spec.Image = testutil.LatestFederalImage

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"authMode for all users must be PKI with Federal Edition").
						Validate(err)
				})

				It("fails when PKIOnly user has secretName set", func() {
					aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"user admin cannot set secretName when authMode is PKIOnly").
						Validate(err)
				})

				It("fails when PKIOnly authMode is used without mTLS cluster", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
					aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"PKIOnly authMode requires Aerospike cluster to be mTLS enabled").
						Validate(err)
				})

				It("fails on duplicate user entries", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "bob",
								SecretName: test.AuthSecretName,
								Roles:      []string{"read"},
							},
							{
								Name:       "bob",
								SecretName: test.AuthSecretName,
								Roles:      []string{"read"},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"Duplicate value: {\"name\":\"bob\"}").
						Validate(err)
				})

				It("fails on duplicate role assignment for a user", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl.Users[0].Roles = []string{
						"sys-admin",
						"user-admin",
						"sys-admin",
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("AerospikeCluster.asdb.aerospike.com",
							"Duplicate value: \"sys-admin\"").
						Validate(err)
				})

				It("fails when a user references a role that does not exist", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Roles: profilerRoleForWebhookTest(),
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:       "admin",
								SecretName: test.AuthSecretName,
								Roles:      []string{"sys-admin", "user-admin"},
							},
							{
								Name:       "profileUser",
								SecretName: test.AuthSecretName,
								Roles:      []string{"profiler", "missingRole"},
							},
						},
					}

					expectDeployFailsACWebhook(ctx, aeroCluster, "non-existent role", "missingRole")
				})

				DescribeTable("fails when a non-PKI user has an invalid secret name",
					func(invalidSecretName string) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
							Roles: profilerRoleForWebhookTest(),
							Users: []asdbv1.AerospikeUserSpec{
								{
									Name:       "aerospike",
									SecretName: test.AuthSecretName,
									Roles:      []string{"sys-admin"},
								},
								{
									Name:       "profileUser",
									SecretName: invalidSecretName,
									Roles:      []string{"profiler"},
								},
							},
						}

						expectDeployFailsACWebhook(ctx, aeroCluster, "empty secret name")
					},
					Entry("empty string", ""),
					Entry("whitespace only", "   "),
				)

				DescribeTable("fails on invalid username",
					func(invalidUserName string, msgSubs []string) {
						aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
							Roles: profilerRoleForWebhookTest(),
							Users: []asdbv1.AerospikeUserSpec{
								{
									Name:       "aerospike",
									SecretName: test.AuthSecretName,
									Roles:      []string{"sys-admin"},
								},
								{
									Name:       invalidUserName,
									SecretName: test.AuthSecretName,
									Roles:      []string{"profiler"},
								},
							},
						}

						expectDeployFailsACWebhook(ctx, aeroCluster, msgSubs...)
					},
					Entry("empty string", "", []string{"username cannot be empty"}),
					Entry("whitespace only", "    ", []string{"username cannot be empty"}),
					Entry("exceeds max length", strings.Repeat("u", 64), []string{"cannot have more than 63 characters"}),
					Entry("colon in username", "aerospike:user", []string{"cannot contain", ":"}),
					Entry("semicolon in username", "aerospike;user", []string{"cannot contain", ";"}),
				)
			})
		})

		Context("spec.aerospikeConfig.security.default-password-file", func() {
			Context("negative", func() {
				It("fails if volume is not present for default-password-file", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{{ID: 1}, {ID: 2}}
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeySecurity] = map[string]interface{}{
						"default-password-file": "randompath",
					}

					expectDeployFailsACWebhook(ctx, aeroCluster,
						"feature-key-file paths or tls paths or default-password-file path are not mounted",
						"create an entry for 'randompath' in 'storage.volumes'")
				})

				It("fails if volume source is not secret for default-password-file", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RackConfig.Racks = []asdbv1.Rack{{ID: 1}, {ID: 2}}
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					//nolint:gosec // G101 test path literal, not real credentials
					aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeySecurity] = map[string]interface{}{
						"default-password-file": "/etc/aerospike/defaultpass/password.conf",
					}
					aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, asdbv1.VolumeSpec{
						Name: "defaultpass",
						Source: asdbv1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/defaultpass",
						},
					})

					expectDeployFailsACWebhook(ctx, aeroCluster,
						"default-password-file path /etc/aerospike/defaultpass/password.conf",
						"volume source should be secret in storage config, volume")
				})
			})
		})
	})

	Context("Update validation", func() {
		AfterEach(func() {
			deleteCluster(ctx, clusterNamespacedName)
		})
		Context("spec.aerospikeAccessControl (users)", func() {
			Context("negative", func() {
				It("fails when user authMode is changed from PKI to Internal", func() {
					aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current := &asdbv1.AerospikeCluster{}
					err = envtests.K8sClient.Get(ctx, types.NamespacedName{
						Name: clusterNamespacedName.Name, Namespace: clusterNamespacedName.Namespace}, current)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModeInternal
					current.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"user admin is not allowed to update authMode from PKI to Internal").
						Validate(err)
				})
			})
		})

		Context("spec.aerospikeAccessControl (validation)", func() {
			Context("negative", func() {
				It("fails when TLS and PKIOnly are enabled in a single update", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					current := &asdbv1.AerospikeCluster{}
					err = envtests.K8sClient.Get(ctx, types.NamespacedName{
						Name: clusterNamespacedName.Name, Namespace: clusterNamespacedName.Namespace}, current)
					Expect(err).ToNot(HaveOccurred())

					current.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = networkTLSConfigForTest()
					current.Spec.OperatorClientCertSpec = adminOperatorCertForTest()
					current.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{
						Users: []asdbv1.AerospikeUserSpec{
							{
								Name:     "admin",
								AuthMode: asdbv1.AerospikeAuthModePKIOnly,
								Roles:    []string{"sys-admin", "user-admin"},
							},
						},
					}
					err = envtests.K8sClient.Update(ctx, current)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"cannot enable TLS and PKIOnly authMode in a single update").
						Validate(err)
				})
			})
			// Context("positive", func() {
			// 	// Add positive validation tests here
			// })
		})
	})
})
