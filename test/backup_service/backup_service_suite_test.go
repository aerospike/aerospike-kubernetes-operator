package backupservice

import (
	"bytes"
	goctx "context"
	"encoding/gob"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
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
		var (
			err error
			cfg *rest.Config
		)

		testEnv, cfg, err = test.StartTestEnvironment()
		Expect(err).NotTo(HaveOccurred())

		k8sClient, _, err = test.InitialiseClients(scheme, cfg)
		Expect(err).NotTo(HaveOccurred())

		// Set up all necessary Secrets, RBAC roles, and ServiceAccounts for the test environment
		err = test.SetupByUser(k8sClient, goctx.TODO())
		Expect(err).ToNot(HaveOccurred())

		// Set up AerospikeBackupService RBAC and AWS secret
		err = test.SetupBackupServicePreReq(k8sClient, goctx.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())

		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		Expect(enc.Encode(cfg)).To(Succeed())
		return buf.Bytes()
	},

	func(data []byte) {
		// this runs once per process, we grab the existing rest.Config here
		var (
			err    error
			config rest.Config
		)

		dec := gob.NewDecoder(bytes.NewReader(data))
		Expect(dec.Decode(&config)).To(Succeed())

		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")
		k8sClient, _, err = test.InitialiseClients(scheme, &config)
		Expect(err).NotTo(HaveOccurred())
	},
)

var _ = SynchronizedAfterSuite(func() {
	// runs on *all* processes
}, func() {
	// runs *only* on process #1
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
