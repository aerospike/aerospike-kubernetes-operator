package restore

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

var _ = Describe("AerospikeRestore CRD schema marker validation", Ordered, func() {
	ctx := context.TODO()

	var (
		restoreNsNm types.NamespacedName
		absNsNm     types.NamespacedName
	)

	BeforeAll(func() {
		absNsNm = uniqueNamespacedName("shared-abs")
		Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm,
			backupconfig.DefaultRestoreMergedConfig())).To(Succeed())
	})

	AfterAll(func() {
		backupconfig.DeleteStubBackupService(ctx, envtests.K8sClient, absNsNm)
	})

	BeforeEach(func() {
		restoreNsNm = uniqueNamespacedName("restore")
	})

	AfterEach(func() {
		deleteRestore(ctx, restoreNsNm)
	})

	Context("Deploy validation", func() {
		Context("spec.type", func() {
			Context("negative", func() {
				It("rejects invalid restore type (Enum)", func() {
					restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.RestoreType("InvalidType"))

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "type", "Unsupported value").
						Validate(err)
				})
			})

			Context("positive", func() {
				DescribeTable("accepts each valid restore type (Enum)",
					func(restoreType asdbv1beta1.RestoreType) {
						nsNm := uniqueNamespacedName("restore-type")
						restore := buildRestoreCR(nsNm, absNsNm, restoreType)

						Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
						DeferCleanup(func() { deleteRestore(ctx, nsNm) })
					},
					Entry("Full", asdbv1beta1.Full),
					Entry("Incremental", asdbv1beta1.Incremental),
					Entry("Timestamp", asdbv1beta1.Timestamp),
				)
			})
		})

		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name (MinLength=1)", func() {
					restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Full)
					restore.Spec.BackupService.Name = ""

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace (MinLength=1)", func() {
					restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Full)
					restore.Spec.BackupService.Namespace = ""

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "backupService", "namespace").
						Validate(err)
				})
			})
		})

		Context("spec.config", func() {
			Context("negative", func() {
				It("rejects missing config (required)", func() {
					restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Full)
					restore.Spec.Config = runtime.RawExtension{}

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "config").
						Validate(err)
				})
			})
		})

		Context("spec.pollingPeriod", func() {
			Context("positive", func() {
				It("defaults pollingPeriod to 1m when omitted (default=1m)", func() {
					Expect(createRestoreOmittingPollingPeriod(ctx, restoreNsNm, absNsNm, asdbv1beta1.Full)).To(Succeed())

					var fetched asdbv1beta1.AerospikeRestore
					Expect(envtests.K8sClient.Get(ctx, restoreNsNm, &fetched)).To(Succeed())
					Expect(fetched.Spec.PollingPeriod.Duration.String()).To(Equal("1m0s"))
				})
			})
		})
	})

	Context("Update validation", func() {
		Context("spec.type", func() {
			Context("negative", func() {
				It("rejects any spec change", func() {
					restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Full)
					Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())

					restore.Spec.Type = asdbv1beta1.Incremental

					err := envtests.K8sClient.Update(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings("aerospikeRestore Spec is immutable").
						Validate(err)
				})
			})
		})

		Context("status.phase", func() {
			BeforeEach(func() {
				restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Full)
				Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
			})

			Context("negative", func() {
				It("rejects invalid phase value (Enum)", func() {
					restore := &asdbv1beta1.AerospikeRestore{}
					Expect(envtests.K8sClient.Get(ctx, restoreNsNm, restore)).To(Succeed())

					restore.Status.Phase = asdbv1beta1.AerospikeRestorePhase("InvalidPhase")
					err := envtests.K8sClient.Status().Update(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "phase", "Unsupported value").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("accepts valid phase values (Enum)", func() {
					restore := &asdbv1beta1.AerospikeRestore{}
					Expect(envtests.K8sClient.Get(ctx, restoreNsNm, restore)).To(Succeed())

					for _, phase := range []asdbv1beta1.AerospikeRestorePhase{
						asdbv1beta1.AerospikeRestoreInProgress,
						asdbv1beta1.AerospikeRestoreCompleted,
						asdbv1beta1.AerospikeRestoreFailed,
					} {
						restore.Status.Phase = phase
						Expect(envtests.K8sClient.Status().Update(ctx, restore)).To(Succeed())
					}
				})
			})
		})
	})
})

// Each spec here needs its own AerospikeBackupService because the storage override cases
// differ in whether the restore routine exists in the backup service config.
var _ = Describe("AerospikeRestore config webhook validation", func() {
	ctx := context.TODO()

	var (
		restoreNsNm types.NamespacedName
		absNsNm     types.NamespacedName
	)

	BeforeEach(func() {
		restoreNsNm = uniqueNamespacedName("restore-config")
		absNsNm = uniqueNamespacedName("abs-restore-config")
	})

	AfterEach(func() {
		deleteRestore(ctx, restoreNsNm)
		backupconfig.DeleteStubBackupService(ctx, envtests.K8sClient, absNsNm)
	})

	Context("timestamp restore storage override", func() {
		It("accepts source-name override for timestamp restore", func() {
			Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm,
				backupconfig.DefaultRestoreMergedConfig())).To(Succeed())

			config := minimalTimestampRestoreConfigMap()
			config[asdbv1beta1.SourceNameKey] = "local"

			restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Timestamp)
			restore.Spec.Config = runtime.RawExtension{
				Raw: backupconfig.MustMarshalConfig(config),
			}

			Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
		})

		It("accepts source-name override when routine is absent from local ABS config (BKRS-212 cross-region)", func() {
			Expect(backupconfig.CreateStubBackupService(ctx, envtests.K8sClient, absNsNm,
				backupconfig.RestoreOnlyStorageConfig())).To(Succeed())

			config := minimalTimestampRestoreConfigMap()
			config[asdbv1beta1.RoutineKey] = "remote-region-routine"
			config[asdbv1beta1.SourceNameKey] = "local"

			restore := buildRestoreCR(restoreNsNm, absNsNm, asdbv1beta1.Timestamp)
			restore.Spec.Config = runtime.RawExtension{
				Raw: backupconfig.MustMarshalConfig(config),
			}

			Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
		})
	})
})
