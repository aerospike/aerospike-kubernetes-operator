package backupservice

import (
	goctx "context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var testEnv *envtest.Environment

var k8sClient client.Client

var scheme = k8Runtime.NewScheme()

func TestBackupService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BackupService Suite")
}

var _ = SynchronizedBeforeSuite(
	func() []byte {
		var err error
		_, _, k8sClient, _, err = test.BootStrapTestEnv(scheme)
		Expect(err).NotTo(HaveOccurred())

		err = test.SetupByUser(k8sClient, goctx.TODO())
		Expect(err).ToNot(HaveOccurred())

		// Set up AerospikeBackupService RBAC and AWS secret
		err = test.SetupBackupServicePreReq(k8sClient, goctx.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
		// Return setupData → Passed to all nodes
		return nil
	},

	func(data []byte) {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")

		var err error
		testEnv, _, k8sClient, _, err = test.BootStrapTestEnv(scheme)
		Expect(err).NotTo(HaveOccurred())
	},
)

var _ = AfterSuite(
	func() {

		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
