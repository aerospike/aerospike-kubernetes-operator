package backup

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/fixtures/backupconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeBackup validation", Ordered, func() {
	ctx := context.TODO()

	var (
		// backup namespace name
		backupNsNm types.NamespacedName
		// aerospike backup service namespace name
		absNsNm types.NamespacedName
	)

	BeforeAll(func() {
		absNsNm = uniqueNamespacedName("shared-abs")
		Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm, nil)).To(Succeed())
	})

	AfterAll(func() {
		backupconfig.DeleteStubBackupService(ctx, envtests.K8sClient, absNsNm)
	})

	BeforeEach(func() {
		backupNsNm = uniqueNamespacedName("backup")
	})

	AfterEach(func() {
		deleteBackup(ctx, backupNsNm)
	})

	Context("Deploy validation", func() {
		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name (MinLength=1)", func() {
					backup := buildBackupCR(backupNsNm, absNsNm)
					backup.Spec.BackupService.Name = ""

					err := envtests.K8sClient.Create(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace (MinLength=1)", func() {
					backup := buildBackupCR(backupNsNm, absNsNm)
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
				It("rejects on-demand backup config on create", func() {
					backup := buildBackupCR(backupNsNm, absNsNm)
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
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
			backup = buildBackupCR(backupNsNm, absNsNm)
			Expect(envtests.K8sClient.Create(ctx, backup)).To(Succeed())
			Expect(syncBackupStatus(ctx, backup)).To(Succeed())
		})

		Context("spec.onDemandBackups", func() {
			Context("negative", func() {
				It("rejects more than one on-demand backup entry (MaxItems=1)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-1",
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
						},
						{
							ID:          "on-demand-2",
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
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
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
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
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
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
				It("accepts on-demand backup with type Incremental on update (Enum)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-incremental",
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
							Type:        asdbv1beta1.IncrementalBackup,
						},
					}

					Expect(envtests.K8sClient.Update(ctx, backup)).To(Succeed())

					var updated asdbv1beta1.AerospikeBackup
					Expect(envtests.K8sClient.Get(ctx, backupNsNm, &updated)).To(Succeed())
					Expect(updated.Spec.OnDemandBackups).To(HaveLen(1))
					Expect(updated.Spec.OnDemandBackups[0].Type).To(Equal(asdbv1beta1.IncrementalBackup))
				})

				It("defaults on-demand backup type to Full when omitted (default=Full)", func() {
					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand-default-type",
							RoutineName: backupconfig.BuildRoutineNameForBackup(backupNsNm),
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

		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name on update (MinLength=1)", func() {
					backup.Spec.BackupService.Name = ""
					err := envtests.K8sClient.Update(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace on update (MinLength=1)", func() {
					backup.Spec.BackupService.Namespace = ""
					err := envtests.K8sClient.Update(ctx, backup)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupCRDSchemaErrorPrefix, "backupService", "namespace").
						Validate(err)
				})
			})
		})
	})
})
