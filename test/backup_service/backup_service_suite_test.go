package backupservice

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var testEnv *envtest.Environment

var k8sClient client.Client

var scheme = k8Runtime.NewScheme()

func TestBackupService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BackupService Suite")
}

var _ = BeforeSuite(
	func() {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")

		var err error
		testEnv, _, k8sClient, _, err = test.BootStrapTestEnv(scheme)
		Expect(err).NotTo(HaveOccurred())

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
