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

package test

import (
	goctx "context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	admissionv1 "k8s.io/api/admission/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var testEnv *envtest.Environment

var scheme = k8Runtime.NewScheme()

var cfg *rest.Config

var k8sClient client.Client

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Setup Suite")
}

// This is used when running tests on existing cluster
// user has to install its own operator then run cleanup and then start this

var _ = BeforeSuite(
	func() {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")

		t := true
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
		}
		var err error

		cfg, err = testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		err = clientgoscheme.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = asdbv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = admissionv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		k8sClient, err = client.New(
			cfg, client.Options{Scheme: scheme},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		// Create SA for aerospike backup service
		err = createServiceAccount(k8sClient, goctx.TODO(), common.AerospikeBackupService, namespace)
		Expect(err).ToNot(HaveOccurred())

		// Setup by user function
		// test creating resource
		// IN operator namespace
		// Create aerospike-secret
		// Create auth-secret (admin)
		// Create auth-update (admin123)

		// For test1
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For test2
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For aerospike
		// Create aerospike-secret
		// Create auth-secret (admin)

		// For common
		// Create namespace test1, test2, aerospike
		// ServiceAccount: aerospike-cluster (operatorNs, test1, test2, aerospike)
		// ClusterRole: aerospike-cluster
		// ClusterRoleBinding: aerospike-cluster

		// Need to create storageClass if not created already
		err = setupByUser(k8sClient, goctx.TODO())
		Expect(err).ToNot(HaveOccurred())
	})

var _ = AfterSuite(
	func() {
		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
