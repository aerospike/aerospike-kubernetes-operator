//go:build !noac

package cluster

import (
	goctx "context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe(
	"AccessControl", func() {
		ctx := goctx.TODO()

		Context(
			"AccessControl", func() {
				Context(
					"When cluster is deployed", func() {
						clusterName := fmt.Sprintf("ac-lifecycle-%d", GinkgoParallelProcess())
						clusterNamespacedName := test.GetNamespacedName(
							clusterName, namespace,
						)

						AfterEach(
							func() {
								aeroCluster := &asdbv1.AerospikeCluster{
									ObjectMeta: metav1.ObjectMeta{
										Name:      clusterNamespacedName.Name,
										Namespace: clusterNamespacedName.Namespace,
									},
								}

								Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
								Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
							},
						)

						It(
							"SecurityEnable: should enable security in running cluster",
							func() {
								var accessControl *asdbv1.AerospikeAccessControlSpec

								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}

								aerospikeConfigSpec.ConfigureSecurity(false)

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
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"data-admin",
												"read-write-udf",
												"write",
											},
										},
									},
								}

								aerospikeConfigSpec.ConfigureSecurity(true)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)

								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err != nil {
									Fail("Security should have enabled successfully")
								}
							},
						)

						It(
							"SecurityDisable: should disable security in running cluster",
							func() {
								accessControl := &asdbv1.AerospikeAccessControlSpec{
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
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"data-admin",
												"read-write-udf",
												"write",
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

								aerospikeConfigSpec.ConfigureSecurity(true)

								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)

								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								if err != nil {
									Fail("Security should be enabled")
								}

								aerospikeConfigSpec.ConfigureSecurity(false)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).To(HaveOccurred())
								Expect(err.Error()).Should(ContainSubstring("SECURITY_NOT_ENABLED"))
							},
						)

						It(
							"SecurityDisable: should disable security in partially security enabled cluster",
							func() {
								var accessControl *asdbv1.AerospikeAccessControlSpec

								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}

								aerospikeConfigSpec.ConfigureSecurity(false)

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
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"profiler",
											},
										},
									},
								}

								aerospikeConfigSpec.ConfigureSecurity(true)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)

								err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								aerospikeConfigSpec.ConfigureSecurity(false)

								// Save cluster variable as well for cleanup.
								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)

								Eventually(func(g Gomega) {
									err := testAccessControlReconcile(aeroCluster, ctx)
									g.Expect(err).To(HaveOccurred())
									g.Expect(err.Error()).To(ContainSubstring("SECURITY_NOT_ENABLED"))
								}, 5*time.Minute, 20*time.Second).Should(Succeed())
							},
						)

						It(
							"SecurityDisable: should reject access control update when security is disabled",
							func() {
								var accessControl *asdbv1.AerospikeAccessControlSpec

								aerospikeConfigSpec, err := NewAerospikeConfSpec(latestImage)
								if err != nil {
									Fail(
										fmt.Sprintf(
											"Invalid Aerospike Config Spec: %v",
											err,
										),
									)
								}

								aerospikeConfigSpec.ConfigureSecurity(true)

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
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"profiler",
											},
										},
									},
								}

								aeroCluster := getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)
								err = aerospikeClusterCreateUpdate(
									k8sClient, aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								aerospikeConfigSpec.ConfigureSecurity(false)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, accessControl,
									aerospikeConfigSpec,
								)

								err = aerospikeClusterCreateUpdate(
									k8sClient, aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								accessControl.Users = accessControl.Users[:1] // Try to drop one user.
								aeroCluster.Spec.AerospikeAccessControl = accessControl

								err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
								Expect(err).To(HaveOccurred())
								Expect(err.Error()).To(ContainSubstring(
									"aerospikeAccessControl cannot be updated when security is disabled"))

								aeroCluster.Spec.AerospikeAccessControl = nil

								err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
								Expect(err).To(HaveOccurred())
								Expect(err.Error()).To(ContainSubstring("aerospikeAccessControl cannot be removed once set"))
							},
						)

						It(
							"AccessControlLifeCycle", func() {
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
											SecretName: test.AuthSecretName,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											SecretName: test.AuthSecretName,
											Roles: []string{
												"profiler",
												"sys-admin",
											},
										},

										{
											Name:       "userToDrop",
											SecretName: test.AuthSecretName,
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

								aerospikeConfigSpec.ConfigureSecurity(true)

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
											AuthMode:   asdbv1.AerospikeAuthModeInternal,
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											AuthMode:   asdbv1.AerospikeAuthModeInternal,
											SecretName: test.AuthSecretNameForUpdate,
											Roles: []string{
												"data-admin",
												"read-write-udf",
												"write",
											},
										},
									},
								}

								aeroCluster.Spec.AerospikeAccessControl = &accessControl

								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								By("EnableQuota")

								accessControl = asdbv1.AerospikeAccessControlSpec{
									Roles: []asdbv1.AerospikeRoleSpec{
										{
											Name: "profiler",
											Privileges: []string{
												"read-write.test",
												"read-write-udf.test.users",
											},
											ReadQuota:  2,
											WriteQuota: 2,
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
											AuthMode:   asdbv1.AerospikeAuthModeInternal,
											SecretName: test.AuthSecretName,
											Roles: []string{
												"sys-admin",
												"user-admin",
											},
										},

										{
											Name:       "profileUser",
											AuthMode:   asdbv1.AerospikeAuthModeInternal,
											SecretName: test.AuthSecretName,
											Roles: []string{
												"profiler",
												"sys-admin",
											},
										},

										{
											Name:       "userToDrop",
											SecretName: test.AuthSecretName,
											Roles: []string{
												"profiler",
											},
										},
									},
								}

								aerospikeConfigSpec.ConfigureSecurity(true)
								aerospikeConfigSpec.setEnableQuotas(true)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())

								By("QuotaParamsSpecifiedButFlagIsOff")

								aerospikeConfigSpec.ConfigureSecurity(true)
								aerospikeConfigSpec.setEnableQuotas(false)

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

								By("DisableQuota")

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
											Roles: []string{
												"profiler",
												"sys-admin",
											},
										},

										{
											Name:       "userToDrop",
											SecretName: test.AuthSecretName,
											Roles: []string{
												"profiler",
											},
										},
									},
								}

								aerospikeConfigSpec.ConfigureSecurity(true)
								aerospikeConfigSpec.setEnableQuotas(false)

								aeroCluster = getAerospikeClusterSpecWithAccessControl(
									clusterNamespacedName, &accessControl,
									aerospikeConfigSpec,
								)
								err = testAccessControlReconcile(
									aeroCluster, ctx,
								)
								Expect(err).ToNot(HaveOccurred())
							},
						)
					},
				)

				Context("PKIOnly AuthMode", func() {
					clusterName := fmt.Sprintf("ac-pkionly-%d", GinkgoParallelProcess())
					clusterNamespacedName := test.GetNamespacedName(
						clusterName, namespace,
					)

					AfterEach(
						func() {
							aeroCluster := &asdbv1.AerospikeCluster{
								ObjectMeta: metav1.ObjectMeta{
									Name:      clusterNamespacedName.Name,
									Namespace: clusterNamespacedName.Namespace,
								},
							}

							Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
							Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
						},
					)
					Context("when doing invalid operations", func() {
						It("Should block PKIOnly authMode while TLS rollout is in progress", func() {
							// Create a non-TLS cluster
							aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

							By("Enable TLS first")

							aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork] = getNetworkTLSConfig()
							aeroCluster.Spec.OperatorClientCertSpec = getAdminOperatorCert()

							err := updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							// Now try to enable PKIOnly while TLS is still rolling out
							// At this point: spec has TLS, but status doesn't have TLS yet
							aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
							aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""

							err = updateClusterWithNoWait(k8sClient, ctx, aeroCluster)
							Expect(err).To(HaveOccurred())
							Expect(err.Error()).To(ContainSubstring(
								"cannot enable PKIOnly authMode while TLS rollout is in progress"))
						})
					})

					Context("when doing valid operations", func() {
						It("Should allow PKIOnly authMode with EE 8.1.0.0 or later", func() {
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
										Roles:      []string{"sys-admin", "user-admin"},
									},
								},
							}

							aeroCluster := GetPKIAuthAerospikeClusterWithAccessControl(
								clusterNamespacedName, testClusterSize, accessControl,
							)

							err := testAccessControlReconcile(aeroCluster, ctx)
							Expect(err).ToNot(HaveOccurred())
						})

						It("Should allow updating users from Internal to PKIOnly authMode", func() {
							// Create with admin user with Internal auth (password-based).
							accessControl := &asdbv1.AerospikeAccessControlSpec{
								Users: []asdbv1.AerospikeUserSpec{
									{
										Name:       "admin",
										AuthMode:   asdbv1.AerospikeAuthModeInternal,
										SecretName: test.AuthSecretName,
										Roles:      []string{"sys-admin", "user-admin"},
									},
									{
										Name:       "user01",
										AuthMode:   asdbv1.AerospikeAuthModeInternal,
										SecretName: test.AuthSecretName,
										Roles:      []string{"sys-admin", "user-admin"},
									},
								},
							}

							aeroCluster := GetPKIAuthAerospikeClusterWithAccessControl(
								clusterNamespacedName, testClusterSize, accessControl,
							)

							Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())

							// Update all users with Internal auth to PKIOnly (certificate-based).
							aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
							aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""
							aeroCluster.Spec.AerospikeAccessControl.Users[1].AuthMode = asdbv1.AerospikeAuthModePKIOnly
							aeroCluster.Spec.AerospikeAccessControl.Users[1].SecretName = ""

							Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())
						})

						It("Should allow only PKI login and reject password based login", func() {
							accessControl := &asdbv1.AerospikeAccessControlSpec{
								Users: []asdbv1.AerospikeUserSpec{
									{
										Name:       "admin",
										AuthMode:   asdbv1.AerospikeAuthModeInternal,
										SecretName: test.AuthSecretName,
										Roles:      []string{"sys-admin", "user-admin"},
									},
								},
							}

							aeroCluster := GetPKIAuthAerospikeClusterWithAccessControl(
								clusterNamespacedName, testClusterSize, accessControl,
							)
							Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())

							aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							By("Password-based auth should succeed for Internal auth mode user")
							// Password-based auth should succeed.
							policy := getClientPolicy(aeroCluster, k8sClient)

							Eventually(func() error {
								return checkClientConnection(aeroCluster, k8sClient, policy)
							}, 1*time.Minute, 5*time.Second).Should(Succeed())

							By("Updating user to PKIOnly auth mode")

							aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
							aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""
							Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())

							By("Password-based auth should fail for PKIOnly auth mode user")
							// Password-based auth should fail.
							policy = getClientPolicy(aeroCluster, k8sClient)
							policy.User = "admin"
							policy.Password = "admin123"
							policy.AuthMode = as.AuthModeInternal
							policy.FailIfNotConnected = true

							Eventually(func() error {
								return checkClientConnection(aeroCluster, k8sClient, policy)
							}, 1*time.Minute, 5*time.Second).Should(HaveOccurred())

							By("PKI auth should succeed for PKIOnly auth mode user")

							aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())
							// PKI auth should succeed.
							policy = getClientPolicy(aeroCluster, k8sClient)

							Eventually(func() error {
								return checkClientConnection(aeroCluster, k8sClient, policy)
							}, 1*time.Minute, 5*time.Second).Should(Succeed())
						})

						It("Should allow users with PKIOnly auth mode for federal image", func() {
							accessControl := &asdbv1.AerospikeAccessControlSpec{
								Users: []asdbv1.AerospikeUserSpec{
									{
										Name:     "admin",
										AuthMode: asdbv1.AerospikeAuthModePKIOnly,
										Roles:    []string{"sys-admin", "user-admin"},
									},
									{
										Name:     "user01",
										AuthMode: asdbv1.AerospikeAuthModePKIOnly,
										Roles:    []string{"sys-admin", "user-admin"},
									},
								},
							}

							aeroCluster := GetPKIAuthAerospikeClusterWithAccessControl(
								clusterNamespacedName, testClusterSize, accessControl,
							)
							aeroCluster.Spec.Image = testutil.LatestFederalImage
							Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())
						})

						It("Should allow EE security enable/disable with mTLS cluster", func() {
							securityLifecycleWithPKITest(k8sClient, ctx, clusterNamespacedName, latestImage)
						})

						It("Should allow FE security enable/disable with mTLS cluster", func() {
							securityLifecycleWithPKITest(k8sClient, ctx, clusterNamespacedName, testutil.LatestFederalImage)
						})
					})
				})
			},
		)

		Context("Using default-password-file", func() {
			clusterName := fmt.Sprintf("default-password-file-%d", GinkgoParallelProcess())

			var clusterNamespacedName = test.GetNamespacedName(
				clusterName, namespace,
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)
			It("Should use default-password-file when configured", func() {
				By("Creating cluster")

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
				racks := getDummyRackConf(1, 2)
				aeroCluster.Spec.RackConfig.Racks = racks
				aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
				// Setting incorrect secret name so that access control reconciler could not set the password for admin.
				aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = "incorrectSecretName"
				// This file is already added in the storage volume backed by the secret.
				//nolint:gosec // G101 test path literal, not real credentials
				aeroCluster.Spec.AerospikeConfig.Value["security"] = map[string]interface{}{
					"default-password-file": "/etc/aerospike/secret/password.conf",
				}

				err := k8sClient.Create(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Cluster is not ready, connect with cluster using default password")

				// Get default password from secret.
				secretNamespcedName := types.NamespacedName{
					Name:      test.AerospikeSecretName,
					Namespace: aeroCluster.Namespace,
				}
				passFileName := "password.conf"
				pass, err := getPasswordFromSecret(k8sClient, secretNamespcedName, passFileName)
				Expect(err).ToNot(HaveOccurred())

				// Cluster is not yet ready. Therefore, it should be using default password
				Eventually(func() error {
					clientPolicy := getClientPolicy(aeroCluster, k8sClient)
					clientPolicy.Password = pass
					clientPolicy.FailIfNotConnected = true

					client, cerr := getClientWithPolicy(
						pkgLog, aeroCluster, k8sClient, clientPolicy)
					if cerr != nil {
						return cerr
					}

					nodes := client.GetNodeNames()
					if len(nodes) == 0 {
						return fmt.Errorf("Not connected")
					}

					pkgLog.Info("Connected to cluster", "nodes", nodes, "pass", pass)

					return nil
				}, 5*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())

				// Set correct secret name for admin user credentials.
				aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Try scaleup")

				err = scaleUpClusterTest(
					k8sClient, ctx, clusterNamespacedName, 1,
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})
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
	racks := []asdbv1.Rack{
		{
			ID: 1,
		},
		{
			ID: 2,
		},
	}
	// create Aerospike custom resource
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			RackConfig: asdbv1.RackConfig{
				Namespaces: []string{"test"},
				Racks:      racks,
				RollingUpdateBatchSize: &intstr.IntOrString{Type: intstr.Int,
					IntVal: int32(2)},
			},
			Size: testClusterSize,
			Image: fmt.Sprintf(
				"%s:%s", baseEnterpriseImage, aerospikeConfSpec.getVersion(),
			),
			ValidationPolicy: &asdbv1.ValidationPolicySpec{
				SkipWorkDirValidate: true,
			},
			AerospikeAccessControl: accessControl,
			Storage: asdbv1.AerospikeStorageSpec{
				Volumes: []asdbv1.VolumeSpec{
					getStorageVolumeForSecret(),
				},
			},
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: ptr.To(true),
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

	defer clientP.Close()

	err = validateRoles(clientP, &aeroCluster.Spec)
	if err != nil {
		return fmt.Errorf("error validating roles: %v", err)
	}

	return validateUsers(clientP, aeroCluster)
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
	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)

	asRoles, err := clientP.QueryRoles(&adminPolicy)
	if err != nil {
		return fmt.Errorf("error querying roles: %v", err)
	}

	var currentRoleNames []string

	for _, role := range asRoles {
		if _, isPredefined := asdbv1.PredefinedRoles[role.Name]; !isPredefined {
			currentRoleNames = append(currentRoleNames, role.Name)
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

	adminPolicy := aerospikecluster.GetAdminPolicy(clusterSpec)

	asUsers, err := clientP.QueryUsers(&adminPolicy)
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

		currentRoleNames := make([]string, 0, len(asUser.Roles))

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

func securityLifecycleWithPKITest(
	k8sClient client.Client,
	ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, image string,
) {
	By("Create security disabled cluster")

	aeroCluster := CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
	aeroCluster.Spec.Image = image

	aeroCluster.Spec.AerospikeAccessControl = nil
	delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)

	Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(Succeed())

	By("Enable security with PKIOnly authMode")

	accessControl := &asdbv1.AerospikeAccessControlSpec{
		Users: []asdbv1.AerospikeUserSpec{
			{
				Name:     "admin",
				AuthMode: asdbv1.AerospikeAuthModePKIOnly,
				Roles:    []string{"sys-admin", "user-admin"},
			},
		},
	}

	aeroCluster.Spec.AerospikeAccessControl = accessControl
	aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeySecurity] = map[string]interface{}{}
	Expect(testAccessControlReconcile(aeroCluster, ctx)).To(Succeed())

	By("Disable security")
	delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)
	Expect(updateCluster(k8sClient, ctx, aeroCluster)).To(Succeed())
}
