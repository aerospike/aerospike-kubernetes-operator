package backup

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeBackup validation", func() {
	ctx := context.TODO()

	var (
		// backup namespace name
		backupNsNm types.NamespacedName
		// aerospike backup service namespace name
		absNsNm types.NamespacedName
	)

	BeforeEach(func() {
		backupNsNm = uniqueNamespacedName("backup")
		absNsNm = uniqueNamespacedName("abs-for-backup")
		Expect(createStubBackupService(ctx, absNsNm)).To(Succeed())
	})

	AfterEach(func() {
		deleteBackup(ctx, backupNsNm)
		deleteBackupService(ctx, absNsNm)
	})

	Context("Deploy validation", func() {
		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name (MinLength=1)", func() {
					backup := newBackup(backupNsNm, absNsNm)
					backup.Spec.BackupService.Name = ""

					err := envtests.K8sClient.Create(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace (MinLength=1)", func() {
					backup := newBackup(backupNsNm, absNsNm)
					backup.Spec.BackupService.Namespace = ""

					err := envtests.K8sClient.Create(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "backupService", "namespace").
						Validate(err)
				})
			})
		})

		Context("spec.onDemandBackups", func() {
			Context("negative", func() {
				It("rejects on-demand backup config on create (webhook)", func() {
					backup := newBackup(backupNsNm, absNsNm)
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: routineName(backupNsNm),
						},
					}

					err := envtests.K8sClient.Create(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupWebhookErrorPrefix,
							"onDemand backups config cannot be specified while creating backup").
						Validate(err)
				})
			})
		})
	})

	Context("Update validation", func() {
		var backup *asdbv1beta1.AerospikeBackup

		BeforeEach(func() {
			backup = newBackup(backupNsNm, absNsNm)
			Expect(envtests.K8sClient.Create(ctx, backup)).To(Succeed())
			Expect(syncBackupStatus(ctx, backup)).To(Succeed())
		})

		Context("spec.onDemandBackups", func() {
			Context("negative", func() {
				It("rejects more than one on-demand backup entry (MaxItems=1)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-1",
							RoutineName: routineName(backupNsNm),
						},
						{
							ID:          "on-demand-2",
							RoutineName: routineName(backupNsNm),
						},
					}

					err := envtests.K8sClient.Update(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "onDemandBackups").
						Validate(err)
				})

				It("rejects empty on-demand backup id (MinLength=1)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "",
							RoutineName: routineName(backupNsNm),
						},
					}

					err := envtests.K8sClient.Update(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "onDemandBackups", "id").
						Validate(err)
				})

				It("rejects invalid on-demand backup type (Enum)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-invalid-type",
							RoutineName: routineName(backupNsNm),
							Type:        asdbv1beta1.BackupType("InvalidType"),
						},
					}

					err := envtests.K8sClient.Update(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "onDemandBackups", "type",
							"Unsupported value").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("defaults on-demand backup type to Full when omitted (default=Full)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-default-type",
							RoutineName: routineName(backupNsNm),
						},
					}

					Expect(envtests.K8sClient.Update(ctx, backup)).To(Succeed())

					var updated asdbv1beta1.AerospikeBackup
					Expect(envtests.K8sClient.Get(ctx, backupNsNm, &updated)).To(Succeed())
					Expect(updated.Spec.OnDemandBackups).To(HaveLen(1))
					Expect(updated.Spec.OnDemandBackups[0].Type).To(Equal(asdbv1beta1.FullBackup))
				})
			})
		})
	})
})
