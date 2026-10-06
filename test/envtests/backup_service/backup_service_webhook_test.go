package backupservice

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/fixtures/backupconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeBackupService validation", func() {
	ctx := context.TODO()

	var absNsNm types.NamespacedName

	BeforeEach(func() {
		// aerospike backup service namespace name
		absNsNm = uniqueNamespacedName("backup-service")
	})

	AfterEach(func() {
		deleteBackupService(ctx, absNsNm)
	})

	Context("Deploy validation", func() {
		Context("spec.config", func() {
			Context("negative", func() {
				It("rejects the [secret] placeholder as a storage key", func() {
					config := backupconfig.BackupServiceBaseConfig()
					config[asdbv1beta1.StorageKey].(map[string]interface{})["s3"] = map[string]interface{}{
						"s3-storage": map[string]interface{}{
							"bucket":            "backups",
							"s3-region":         "us-east-1",
							"access-key-id":     "[secret]",
							"secret-access-key": "[secret]",
						},
					}

					backupService := buildBackupServiceCR(absNsNm)
					backupService.Spec.Config = runtime.RawExtension{Raw: backupconfig.MustMarshalConfig(config)}

					err := envtests.K8sClient.Create(ctx, backupService)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceWebhookErrorPrefix, `"[secret]"`, "placeholder").
						Validate(err)
				})
			})
		})
	})

	Context("Status validation", func() {
		Context("status.phase", func() {
			Context("negative", func() {
				It("rejects invalid phase value (Enum)", func() {
					backupService := buildBackupServiceCR(absNsNm)
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())

					backupService.Status.Phase = asdbv1beta1.AerospikeBackupServicePhase("InvalidPhase")
					err := envtests.K8sClient.Status().Update(ctx, backupService)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceCRDSchemaErrorPrefix, "phase",
							"Unsupported value").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("accepts valid phase values (Enum)", func() {
					backupService := buildBackupServiceCR(absNsNm)
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())

					for _, phase := range []asdbv1beta1.AerospikeBackupServicePhase{
						asdbv1beta1.AerospikeBackupServiceInProgress,
						asdbv1beta1.AerospikeBackupServiceCompleted,
						asdbv1beta1.AerospikeBackupServiceError,
					} {
						backupService.Status.Phase = phase
						Expect(envtests.K8sClient.Status().Update(ctx, backupService)).To(Succeed())
					}
				})
			})
		})
	})
})
