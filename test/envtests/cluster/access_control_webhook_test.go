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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var _ = Describe("AerospikeCluster access control validation (envtests)", func() {
	const (
		accessControlClusterName = "access-control-webhook-cluster"
	)

	ctx := context.TODO()
	clusterNamespacedName := uniqueNamespacedName(accessControlClusterName)

	Context("Deploy validation", func() {
		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})

		Context("spec.aerospikeAccessControl (validation)", func() {
			Context("negative", func() {
				It("fails when security is disabled but access control is specified", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"security is disabled but access control is specified").
						Validate(err)
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
					errPre810 := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
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

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"authMode for all users must be PKI with Federal Edition").
						Validate(err)
				})

				It("fails when PKIOnly user has secretName set", func() {
					aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
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

					err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("\"vaerospikecluster.kb.io\"",
							"PKIOnly authMode requires Aerospike cluster to be mTLS enabled").
						Validate(err)
				})
			})
		})
	})

	Context("Update validation", func() {
		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
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
