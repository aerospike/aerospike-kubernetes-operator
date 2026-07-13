package backupservice

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
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
