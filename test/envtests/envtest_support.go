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

// Package envtests: envtest_support.go exports test env setup and
//
//	K8sClient for use by subpackages (e.g. test/envtests/cluster).
//
// Test-only symbols are in this file (no _test suffix) so they are
// visible when the package is imported by other test packages.
package envtests

import (
	"context"
	"strings"
	"time"

	//nolint:staticcheck //ST1001: dot imports are standard practice for Ginkgo DSL
	. "github.com/onsi/ginkgo/v2"
	//nolint:staticcheck // ST1001: dot imports are standard practice for Gomega assertions
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	evictionwebhook "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/webhook/eviction"
)

// K8sClient is the Kubernetes client for envtests. Exported for use by subpackages (e.g. envtests/cluster).
var K8sClient client.Client

// SetupTestEnv starts the envtest environment and webhook server. Idempotent.
// basePath is the path from the test package directory to the repo root
// (e.g. "../../" for envtests, "../../../" for envtests/cluster).
func SetupTestEnv(basePath string) {
	if K8sClient != nil {
		return
	}

	if basePath == "" {
		basePath = "../../"
	}

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error

	schemaMap, err := configschema.NewSchemaMap()
	Expect(err).NotTo(HaveOccurred(), "Failed to load SchemaMap for tests")

	testLog := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	asconfig.InitFromMap(testLog, schemaMap)

	By("Bootstrapping test environment")

	t := false
	testEnv = &envtest.Environment{
		UseExistingCluster: &t,
		CRDDirectoryPaths: []string{
			basePath + "config/crd/bases",
		},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{basePath + "config/webhook"},
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

	By("Creating Kubernetes client (waiting for CRDs)")
	Eventually(func() error {
		K8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		return err
	}, time.Second*10, time.Millisecond*250).Should(Succeed())
	Expect(K8sClient).NotTo(BeNil())
	k8sClient = K8sClient

	clientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(clientSet).NotTo(BeNil())

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

	evictionWebhook = evictionwebhook.SetupEvictionWebhookWithManager(mgr)
	err = webhookv1.SetupAerospikeClusterWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	ctx, c := context.WithCancel(context.Background())
	cancel = c

	go func() {
		defer GinkgoRecover()

		Expect(mgr.Start(ctx)).To(Succeed())
	}()

	By("waiting for the webhook server to start")
	time.Sleep(3 * time.Second)
}

// TeardownTestEnv stops the envtest environment. Idempotent.
func TeardownTestEnv() {
	if testEnv == nil {
		return
	}

	By("tearing down the test environment")
	cancel()
	gexec.KillAndWait(5 * time.Second)

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())

	testEnv = nil
	K8sClient = nil
	k8sClient = nil
}

// Package-level vars used by envtests and set by SetupTestEnv.
var (
	testEnv         *envtest.Environment
	k8sClient       client.Client
	clientSet       *kubernetes.Clientset
	cfg             *rest.Config
	scheme          = k8Runtime.NewScheme()
	cancel          context.CancelFunc
	evictionWebhook *evictionwebhook.EvictionWebhook
)

// StatusErrorMatcher validates a Kubernetes API StatusError (e.g. from admission webhooks).
type StatusErrorMatcher struct {
	reason            metav1.StatusReason
	messageSubstrings []string
	causes            []metav1.StatusCause
	code              int32
}

// NewStatusErrorMatcher returns a matcher expecting the given status code and reason.
func NewStatusErrorMatcher(code int32, reason metav1.StatusReason) *StatusErrorMatcher {
	return &StatusErrorMatcher{code: code, reason: reason}
}

// WithMessageSubstrings adds required substrings that must appear in the status message.
func (m *StatusErrorMatcher) WithMessageSubstrings(ss ...string) *StatusErrorMatcher {
	m.messageSubstrings = append(m.messageSubstrings, ss...)
	return m
}

// WithCauses adds expected status causes to match.
func (m *StatusErrorMatcher) WithCauses(causes ...metav1.StatusCause) *StatusErrorMatcher {
	m.causes = append(m.causes, causes...)
	return m
}

// Validate asserts that err is a StatusError matching code, reason, message substrings, and causes.
func (m *StatusErrorMatcher) Validate(err error) {
	statusErr, ok := err.(*apierrors.StatusError)
	Expect(ok).To(BeTrue(), "expected a *errors.StatusError, got %T", err)
	Expect(statusErr.ErrStatus.Code).To(Equal(m.code))
	Expect(statusErr.ErrStatus.Reason).To(Equal(m.reason))

	msg := statusErr.ErrStatus.Message
	for _, sub := range m.messageSubstrings {
		Expect(strings.Contains(msg, sub)).To(BeTrue(), "message %q should contain %q", msg, sub)
	}

	if len(m.causes) > 0 {
		Expect(statusErr.ErrStatus.Details).NotTo(BeNil())
		Expect(statusErr.ErrStatus.Details.Causes).To(HaveLen(len(m.causes)))

		for i, c := range m.causes {
			Expect(statusErr.ErrStatus.Details.Causes[i].Type).To(Equal(c.Type))
			Expect(statusErr.ErrStatus.Details.Causes[i].Message).To(Equal(c.Message))
			Expect(statusErr.ErrStatus.Details.Causes[i].Field).To(Equal(c.Field))
		}
	}
}
