package backup

import (
	goctx "context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8Runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	backupservice "github.com/aerospike/aerospike-kubernetes-operator/v4/test/backup_service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
)

var testEnv *envtest.Environment

var k8sClient client.Client

var scheme = k8Runtime.NewScheme()

func TestBackup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Backup Suite")
}

var _ = BeforeSuite(
	func() {
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("Bootstrapping test environment")
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

		By("Deploy Backup Service")
		backupServiceNamespacedName := test.GetNamespacedName("backup-service", namespace)
		backupService, err := backupservice.NewBackupServiceWithTLSSecretMounts(backupServiceNamespacedName)
		Expect(err).ToNot(HaveOccurred())

		backupService.Spec.Service = &asdbv1beta1.Service{
			Type: corev1.ServiceTypeLoadBalancer,
		}

		backupServiceName = backupService.Name
		backupServiceNamespace = backupService.Namespace

		err = backupservice.DeployBackupService(k8sClient, backupService)
		Expect(err).ToNot(HaveOccurred())

		By("Deploy Aerospike Cluster")
		cascadeDeleteTrue := true
		aeroCluster := cluster.CreateBasicTLSCluster(aerospikeNsNm, 2)
		aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
		aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue

		err = cluster.DeployCluster(k8sClient, testCtx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

var _ = AfterSuite(
	func() {
		By("Delete Aerospike Cluster")
		aeroCluster := asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aerospikeNsNm.Name,
				Namespace: aerospikeNsNm.Namespace,
			},
		}

		Expect(cluster.DeleteCluster(k8sClient, testCtx, &aeroCluster)).ToNot(HaveOccurred())
		Expect(cluster.CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())

		By("Delete Backup Service")
		backupService := asdbv1beta1.AerospikeBackupService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
		}

		err := backupservice.DeleteBackupService(k8sClient, &backupService)
		Expect(err).ToNot(HaveOccurred())

		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err = testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
