package restore

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("AerospikeRestore CRD schema marker validation", func() {
	ctx := context.TODO()

	var (
		restoreNsNm types.NamespacedName
		absNsNm     types.NamespacedName
	)

	BeforeEach(func() {
		// restore namespace name
		restoreNsNm = uniqueNamespacedName("restore")
		// aerospike backup service namespace name
		absNsNm = uniqueNamespacedName("abs-ref")
		Expect(createStubBackupService(ctx, absNsNm)).To(Succeed())
	})

	AfterEach(func() {
		deleteRestore(ctx, restoreNsNm)
		deleteBackupService(ctx, absNsNm)
	})

	Context("Deploy validation", func() {
		Context("spec.type", func() {
			Context("negative", func() {
				It("rejects invalid restore type (Enum)", func() {
					restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.RestoreType("InvalidType"))

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "type", "Unsupported value").
						Validate(err)
				})
			})

			Context("positive", func() {
				It("accepts each valid restore type (Enum)", func() {
					for idx, restoreType := range []asdbv1beta1.RestoreType{
						asdbv1beta1.Full,
						asdbv1beta1.Incremental,
						asdbv1beta1.Timestamp,
					} {
						nsNm := uniqueNamespacedName(fmt.Sprintf("type-%d", idx))
						restore := newRestore(nsNm, absNsNm, restoreType)

						Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
						deleteRestore(ctx, nsNm)
					}
				})
			})
		})

		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name (MinLength=1)", func() {
					restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
					restore.Spec.BackupService.Name = ""

					err := envtests.K8sClient.Create(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace (MinLength=1)", func() {
					restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
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
					restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
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
		var restore *asdbv1beta1.AerospikeRestore

		BeforeEach(func() {
			restore = newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
			Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())
		})

		Context("spec.type", func() {
			Context("negative", func() {
				It("rejects invalid restore type on update (Enum)", func() {
					restore.Spec.Type = asdbv1beta1.RestoreType("Bad")

					err := envtests.K8sClient.Update(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "type", "Unsupported value").
						Validate(err)
				})
			})
		})

		Context("spec.backupService", func() {
			Context("negative", func() {
				It("rejects empty backup service name on update (MinLength=1)", func() {
					restore.Spec.BackupService.Name = ""

					err := envtests.K8sClient.Update(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "backupService", "name").
						Validate(err)
				})

				It("rejects empty backup service namespace on update (MinLength=1)", func() {
					restore.Spec.BackupService.Namespace = ""

					err := envtests.K8sClient.Update(ctx, restore)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.RestoreCRDSchemaErrorPrefix, "backupService", "namespace").
						Validate(err)
				})
			})
		})
	})

	Context("Status validation", func() {
		Context("status.phase", func() {
			BeforeEach(func() {
				restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
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

		Context("status.job-id", func() {
			Context("positive", func() {
				It("accepts integer job-id on status update (format: int64)", func() {
					restore := newRestore(restoreNsNm, absNsNm, asdbv1beta1.Full)
					Expect(envtests.K8sClient.Create(ctx, restore)).To(Succeed())

					restore.Status.Phase = asdbv1beta1.AerospikeRestoreInProgress
					restore.Status.JobID = ptr.To(int64(12345))
					Expect(envtests.K8sClient.Status().Update(ctx, restore)).To(Succeed())
				})
			})
		})
	})
})
