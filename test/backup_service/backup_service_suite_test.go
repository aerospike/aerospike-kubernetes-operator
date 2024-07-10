package backupservice

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

var cfg *rest.Config

var testEnv *envtest.Environment

var k8sClient client.Client

var scheme = k8Runtime.NewScheme()

var testCtx = context.TODO()

var pkgLog = ctrl.Log.WithName("backupservice")

const (
	name      = "backup-service"
	namespace = "test"
)

func TestBackupService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BackupService Suite")
}

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

		err = asdbv1beta1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = admissionv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		k8sClient, err = client.New(
			cfg, client.Options{Scheme: scheme},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		sa := corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.AerospikeBackupService,
				Namespace: namespace,
			},
		}

		err = k8sClient.Create(testCtx, &sa)
		if err != nil && !errors.IsAlreadyExists(err) {
			Fail(err.Error())
		}
	})

var _ = AfterSuite(
	func() {
		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
