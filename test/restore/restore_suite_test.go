package restore

import (
	goctx "context"
	"fmt"
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
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/backup"
	backupservice "github.com/aerospike/aerospike-kubernetes-operator/v4/test/backup_service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
)

var testEnv *envtest.Environment

var k8sClient client.Client

var scheme = k8Runtime.NewScheme()

func TestRestore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Restore Suite")
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
		backupService, err := backupservice.NewBackupServiceWithTLSSecretMounts(
			test.GetNamespacedName("backup-service", namespace))
		Expect(err).ToNot(HaveOccurred())

		backupService.Spec.Service = &asdbv1beta1.Service{
			Type: corev1.ServiceTypeLoadBalancer,
		}

		backupServiceName = backupService.Name
		backupServiceNamespace = backupService.Namespace

		err = backupservice.DeployBackupService(k8sClient, backupService)
		Expect(err).ToNot(HaveOccurred())

		cascadeDeleteTrue := true

		By(fmt.Sprintf("Deploy source Aerospike Cluster: %s", sourceAerospikeClusterNsNm.String()))
		aeroCluster := cluster.CreateBasicTLSCluster(sourceAerospikeClusterNsNm, 2)
		aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
		aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue

		err = cluster.DeployCluster(k8sClient, testCtx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster, err = cluster.GetCluster(k8sClient, testCtx, sourceAerospikeClusterNsNm)
		Expect(err).ToNot(HaveOccurred())

		err = cluster.WriteDataToCluster(
			aeroCluster, k8sClient, []string{"test"},
		)
		Expect(err).NotTo(HaveOccurred())

		backupObj, err := backup.NewBackupWithTLS(backupNsNm)
		Expect(err).ToNot(HaveOccurred())

		// Point to current suite's backup service
		backupObj.Spec.BackupService.Name = backupServiceName
		backupObj.Spec.BackupService.Namespace = backupServiceNamespace

		err = backup.CreateBackup(k8sClient, backupObj)
		Expect(err).ToNot(HaveOccurred())

		backupDataPaths, err := backup.GetBackupDataPaths(k8sClient, backupObj)
		Expect(err).ToNot(HaveOccurred())

		pkgLog.Info(fmt.Sprintf("BackupDataPaths: %v", backupDataPaths))
		Expect(backupDataPaths).ToNot(BeEmpty())

		// Example backupDataPath = "test-sample-backup-test-routine/backup/1722353745635/data/test"
		backupDataPath = backupDataPaths[0]

		By(fmt.Sprintf("Deploy destination Aerospike Cluster: %s", destinationAerospikeClusterNsNm.String()))
		aeroCluster = cluster.CreateBasicTLSCluster(destinationAerospikeClusterNsNm, 2)
		aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
		aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue

		err = cluster.DeployCluster(k8sClient, testCtx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

var _ = AfterSuite(
	func() {
		By("Delete Aerospike Cluster")
		aeroClusters := []asdbv1.AerospikeCluster{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceAerospikeClusterNsNm.Name,
					Namespace: sourceAerospikeClusterNsNm.Namespace,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destinationAerospikeClusterNsNm.Name,
					Namespace: destinationAerospikeClusterNsNm.Namespace,
				},
			},
		}

		for idx := range aeroClusters {
			aeroCluster := aeroClusters[idx]
			Expect(cluster.DeleteCluster(k8sClient, testCtx, &aeroCluster)).ToNot(HaveOccurred())
			Expect(cluster.CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		}

		By("Delete Backup")
		backupObj := asdbv1beta1.AerospikeBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupNsNm.Name,
				Namespace: backupNsNm.Namespace,
			},
		}

		err := backup.DeleteBackup(k8sClient, &backupObj)
		Expect(err).ToNot(HaveOccurred())

		By("Delete Backup Service")
		backupService := asdbv1beta1.AerospikeBackupService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
		}

		err = backupservice.DeleteBackupService(k8sClient, &backupService)
		Expect(err).ToNot(HaveOccurred())

		By("tearing down the test environment")
		gexec.KillAndWait(5 * time.Second)
		err = testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	},
)
