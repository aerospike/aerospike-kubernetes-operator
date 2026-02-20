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

package envtests

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	admissionv1 "k8s.io/api/admission/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	webhookv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/webhook/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/configschema"
	"github.com/aerospike/aerospike-management-lib/asconfig"

	// +kubebuilder:scaffold:imports

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	evictionwebhook "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/webhook/eviction"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var testEnv *envtest.Environment

var k8sClient client.Client

var clientSet *kubernetes.Clientset

var cfg *rest.Config

var scheme = k8Runtime.NewScheme()

var cancel context.CancelFunc

var evictionWebhook *evictionwebhook.EvictionWebhook

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Env tests Suite")
}

var _ = BeforeSuite(
	func() {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		var (
			err error
		)

		// Load the SchemaMap
		schemaMap, err := configschema.NewSchemaMap()
		Expect(err).NotTo(HaveOccurred(), "Failed to load SchemaMap for tests")

		// Initialize the global asconfig state
		// We use a discard logger or the GinkgoWriter to keep test output clean
		testLog := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
		asconfig.InitFromMap(testLog, schemaMap)

		By("Bootstrapping test environment")

		t := false
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
			CRDDirectoryPaths: []string{
				"../../config/crd/bases",
			},
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				Paths: []string{"../../config/webhook"},
			},
		}

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

		By("Creating Kubernetes client (waiting for CRDs)")
		Eventually(func() error {
			k8sClient, err = client.New(
				cfg, client.Options{Scheme: scheme},
			)
			return err
		}, time.Second*10, time.Millisecond*250).Should(Succeed())
		Expect(k8sClient).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		clientSet, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(clientSet).NotTo(BeNil())

		// Start the webhook server using controller-runtime manager
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme,
			WebhookServer: &webhook.DefaultServer{
				Options: webhook.Options{
					Host:    testEnv.WebhookInstallOptions.LocalServingHost,
					Port:    testEnv.WebhookInstallOptions.LocalServingPort,
					CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Setup eviction webhook
		evictionWebhook = evictionwebhook.SetupEvictionWebhookWithManager(mgr)

		// Register AerospikeCluster validating webhook directly
		// it should register to mutating and validation webhook both.
		err = webhookv1.SetupAerospikeClusterWebhookWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())

		ctx, c := context.WithCancel(context.Background())
		cancel = c
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		// Wait for webhook server to be ready
		By("waiting for the webhook server to start")
		time.Sleep(3 * time.Second)
	})

var _ = AfterSuite(
	func() {
		By("tearing down the test environment")
		cancel()
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
