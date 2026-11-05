package backup

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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

var _ = Describe("AerospikeBackup update with a backup service config the current rules reject",
	Ordered, ContinueOnFailure, func() {
		ctx := context.TODO()

		const finalizer = "asdb.aerospike.com/backup-finalizer"

		var backupNsNm, absNsNm types.NamespacedName

		BeforeAll(func() {
			absNsNm = uniqueNamespacedName("rejected-abs")
			backupNsNm = uniqueNamespacedName("rejected-backup")

			Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm, nil)).To(Succeed())
			DeferCleanup(backupconfig.DeleteStubBackupService, ctx, envtests.K8sClient, absNsNm)

			backup := buildBackupCR(backupNsNm, absNsNm)
			backup.Finalizers = []string{finalizer}
			Expect(envtests.K8sClient.Create(ctx, backup)).To(Succeed())
			Expect(syncBackupStatus(ctx, backup)).To(Succeed())
			DeferCleanup(func() {
				// Delete first: the webhook lets the finalizer go only while the backup is being deleted.
				deleteBackup(ctx, backupNsNm)

				current := &asdbv1beta1.AerospikeBackup{}
				if err := envtests.K8sClient.Get(ctx, backupNsNm, current); err == nil {
					current.Finalizers = nil
					_ = envtests.K8sClient.Update(ctx, current)
				}
			})

			// An S3 storage with path "/" was accepted by older backup service versions and is rejected by the current
			// rules. Writing it to the ConfigMap after the backup was created reproduces the state an upgrade leaves.
			config := backupconfig.BackupServiceBaseConfig()
			config[asdbv1beta1.StorageKey].(map[string]interface{})["s3"] = map[string]interface{}{
				"s3-storage": map[string]interface{}{
					"bucket":    "backups",
					"path":      "/",
					"s3-region": "us-east-1",
				},
			}

			configMap := &corev1.ConfigMap{}
			Expect(envtests.K8sClient.Get(ctx, absNsNm, configMap)).To(Succeed())
			configMap.Data[asdbv1beta1.BackupServiceConfigYAML] = string(backupconfig.MustMarshalConfig(config))
			Expect(envtests.K8sClient.Update(ctx, configMap)).To(Succeed())
		})

		getBackup := func() *asdbv1beta1.AerospikeBackup {
			backup := &asdbv1beta1.AerospikeBackup{}
			Expect(envtests.K8sClient.Get(ctx, backupNsNm, backup)).To(Succeed())

			return backup
		}

		expectRejected := func(err error) {
			Expect(err).To(HaveOccurred())
			envtests.NewStatusErrorMatcher().
				WithMessageSubstrings(testutil.BackupWebhookErrorPrefix, "must be relative to the storage root").
				Validate(err)
		}

		changeRoutineCron := func(backup *asdbv1beta1.AerospikeBackup) {
			config := backupconfig.BackupCRConfig(asdbv1beta1.NamePrefix(backupNsNm), backupconfig.DefaultClusterHost,
				backupconfig.BackupRoutineCrons{IntervalCron: "@daily", IncrIntervalCron: "@daily"})
			backup.Spec.Config = runtime.RawExtension{Raw: backupconfig.MustMarshalConfig(config)}
		}

		It("rejects a spec change", func() {
			backup := getBackup()
			changeRoutineCron(backup)
			expectRejected(envtests.K8sClient.Update(ctx, backup))
		})

		It("still validates an update without a spec change while the backup is not being deleted", func() {
			backup := getBackup()
			backup.Labels = map[string]string{"team": "backup"}
			expectRejected(envtests.K8sClient.Update(ctx, backup))
		})

		It("rejects a spec change while the backup is being deleted", func() {
			Expect(envtests.K8sClient.Delete(ctx, getBackup())).To(Succeed())

			backup := getBackup()
			Expect(backup.DeletionTimestamp).ToNot(BeNil())

			changeRoutineCron(backup)
			expectRejected(envtests.K8sClient.Update(ctx, backup))
		})

		It("lets the finalizer be removed so the deletion completes", func() {
			backup := getBackup()
			Expect(backup.DeletionTimestamp).ToNot(BeNil())

			backup.Finalizers = nil
			Expect(envtests.K8sClient.Update(ctx, backup)).To(Succeed())

			Eventually(func() bool {
				return apierrors.IsNotFound(envtests.K8sClient.Get(ctx, backupNsNm, &asdbv1beta1.AerospikeBackup{}))
			}).Should(BeTrue())
		})
	})
