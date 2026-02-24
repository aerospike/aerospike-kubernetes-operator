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

const (
	federalImage = "aerospike/aerospike-server-federal:8.1.1.0"
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
		clusterName = "access-control-webhook-cluster"
		testNs      = "default"
	)

	ctx := context.TODO()
	clusterNamespacedName := test.GetNamespacedName(clusterName, testNs)

	Context("negativeDeployClusterValidationTest", func() {
		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			_ = envtests.K8sClient.Delete(ctx, aeroCluster)
		})

		It("DeployValidation:SecurityDisabled:Should fail when security is disabled but access control is specified", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			delete(aeroCluster.Spec.AerospikeConfig.Value, asdbv1.ConfKeySecurity)

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request: security is disabled but access control is specified").
				Validate(err)
		})

		It("DeployValidation:Should fail when PKIOnly authMode is used with Enterprise image below 8.1.0.0", func() {
			aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.Image = testutil.DefaultEnterpriseImage("8.0.0.0")

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"PKIOnly authMode requires Enterprise Edition version 8.1.0.0 or later").
				Validate(err)
		})

		It("DeployValidation:Should fail when Federal Edition has mixed auth modes (not all PKI)", func() {
			aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.Image = federalImage
			aeroCluster.Spec.AerospikeAccessControl.Users = append(aeroCluster.Spec.AerospikeAccessControl.Users,
				asdbv1.AerospikeUserSpec{
					Name:       "user01",
					AuthMode:   asdbv1.AerospikeAuthModeInternal,
					SecretName: test.AuthSecretName,
					Roles:      []string{"sys-admin", "user-admin"},
				})

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"authMode for all users must be PKI with Federal Edition").
				Validate(err)
		})

		It("DeployValidation:PKIOnlyUserSecretName:Should fail when PKIOnly user has secretName set", func() {
			aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"user admin cannot set secretName when authMode is PKIOnly").
				Validate(err)
		})

		It("DeployValidation:Should fail when PKIOnly authMode is used without mTLS cluster", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModePKIOnly
			aeroCluster.Spec.AerospikeAccessControl.Users[0].SecretName = ""

			err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"PKIOnly authMode requires Aerospike cluster to be mTLS enabled").
				Validate(err)
		})
	})

	Context("UpdateValidation", func() {
		It("UpdateValidation:AuthMode:Should fail when user authMode is changed from PKI to Internal", func() {
			aeroCluster := testCluster.CreatePKIAuthEnabledCluster(clusterNamespacedName, 2)
			err := envtests.K8sClient.Create(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				_ = envtests.K8sClient.Delete(ctx, aeroCluster)
			})

			current := &asdbv1.AerospikeCluster{}
			err = envtests.K8sClient.Get(ctx, types.NamespacedName{
				Name: clusterNamespacedName.Name, Namespace: clusterNamespacedName.Namespace}, current)
			Expect(err).ToNot(HaveOccurred())

			current.Spec.AerospikeAccessControl.Users[0].AuthMode = asdbv1.AerospikeAuthModeInternal
			current.Spec.AerospikeAccessControl.Users[0].SecretName = test.AuthSecretName
			err = envtests.K8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"user admin is not allowed to update authMode from PKI to Internal").
				Validate(err)
		})

		It("UpdateValidation:TLSAndPKIOnly:Should fail when TLS and PKIOnly are enabled in a single update", func() {
			aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
			err := envtests.K8sClient.Create(ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				_ = envtests.K8sClient.Delete(ctx, aeroCluster)
			})

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

			envtests.NewStatusErrorMatcher(int32(403), metav1.StatusReasonForbidden).
				WithMessageSubstrings("admission webhook",
					"\"vaerospikecluster.kb.io\"",
					"denied the request:",
					"cannot enable TLS and PKIOnly authMode in a single update").
				Validate(err)
		})
	})
})
