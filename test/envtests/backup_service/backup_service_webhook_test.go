package backupservice

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
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

	Context("Update validation", func() {
		Context("spec.config merged with the ConfigMap", func() {
			// The ConfigMap holds a routine, added by an AerospikeBackup, that uses the default backup policy.
			BeforeEach(func() {
				Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm,
					backupconfig.DefaultRestoreMergedConfig())).To(Succeed())
				DeferCleanup(backupconfig.DeleteStubBackupService, ctx, envtests.K8sClient, absNsNm)
			})

			// updateBackupPolicies replaces the backup policies in the AerospikeBackupService spec.
			updateBackupPolicies := func(policies map[string]interface{}) error {
				var backupService asdbv1beta1.AerospikeBackupService
				Expect(envtests.K8sClient.Get(ctx, absNsNm, &backupService)).To(Succeed())

				config := backupconfig.BackupServiceBaseConfig()
				config[asdbv1beta1.BackupPoliciesKey] = policies
				backupService.Spec.Config = runtime.RawExtension{Raw: backupconfig.MustMarshalConfig(config)}

				return envtests.K8sClient.Update(ctx, &backupService)
			}

			Context("negative", func() {
				It("rejects removing a backup policy that a backup routine uses", func() {
					err := updateBackupPolicies(map[string]interface{}{
						"other-policy": map[string]interface{}{"parallel": 3},
					})
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceWebhookErrorPrefix,
							"aerospike backup config is not valid "+absNsNm.String(),
							"test-routine", fmt.Sprintf("%q", backupconfig.DefaultBackupPolicy)).
						Validate(err)
				})

				It("rejects an invalid backup policy", func() {
					err := updateBackupPolicies(map[string]interface{}{
						backupconfig.DefaultBackupPolicy: map[string]interface{}{
							"compression": map[string]interface{}{"level": 3},
						},
					})
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceWebhookErrorPrefix, "compression").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("accepts adding a backup policy while the routine's policy stays", func() {
					Expect(updateBackupPolicies(map[string]interface{}{
						backupconfig.DefaultBackupPolicy: map[string]interface{}{"parallel": 3},
						"other-policy":                   map[string]interface{}{"parallel": 2},
					})).To(Succeed())
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

var _ = Describe("AerospikeBackupService status patch", func() {
	ctx := context.TODO()

	It("keeps the null values of the spec config in the status config", func() {
		absNsNm := uniqueNamespacedName("status-patch")
		DeferCleanup(deleteBackupService, ctx, absNsNm)

		backupService := &asdbv1beta1.AerospikeBackupService{
			ObjectMeta: metav1.ObjectMeta{Name: absNsNm.Name, Namespace: absNsNm.Namespace},
			Spec: asdbv1beta1.AerospikeBackupServiceSpec{
				Image: backupconfig.BackupServiceImage,
				Config: runtime.RawExtension{Raw: []byte(`{"service":{"http":{"port":8081},"logger":null},` +
					`"backup-policies":{"test-policy":{"parallel":3,"retention":null}},` +
					`"storage":{"local":{"local-storage":{"path":"/tmp/localStorage"}}}}`)},
			},
		}
		Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())
		Expect(envtests.K8sClient.Get(ctx, absNsNm, backupService)).To(Succeed())

		status := backupService.Status.DeepCopy()
		status.Config = backupService.Spec.Config
		status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted

		Expect(common.PatchStatus(ctx, envtests.K8sClient, backupService, status)).To(Succeed())
		Expect(envtests.K8sClient.Get(ctx, absNsNm, backupService)).To(Succeed())
		Expect(backupService.Status.Config.Raw).To(Equal(backupService.Spec.Config.Raw))
	})
})
