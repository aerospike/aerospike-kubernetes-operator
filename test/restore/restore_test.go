package restore

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

var _ = Describe(
	"Restore Test", func() {

		var (
			restore     *asdbv1beta1.AerospikeRestore
			err         error
			restoreNsNm = types.NamespacedName{
				Namespace: namespace,
				Name:      "sample-restore",
			}
		)

		AfterEach(func() {
			Expect(deleteRestore(k8sClient, restore)).ToNot(HaveOccurred())
		})

		Context(
			"When doing Invalid operations", func() {
				It("Should fail when wrong format restore config is given", func() {
					config := getRestoreConfigInMap(backupDataPath)

					// change the format from a single element to slice
					config["destination"] = []interface{}{config["destination"]}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Full, configBytes)
					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when un-supported field is given in restore config", func() {
					config := getRestoreConfigInMap(backupDataPath)
					config["unknown"] = "unknown"

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Full, configBytes)
					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("unknown field"))
				})

				It("Should fail when spec is updated", func() {
					restore, err = newRestore(restoreNsNm, asdbv1beta1.Full)
					Expect(err).ToNot(HaveOccurred())

					err = createRestore(k8sClient, restore)
					Expect(err).ToNot(HaveOccurred())

					restore, err = getRestoreObj(k8sClient, restoreNsNm)
					Expect(err).ToNot(HaveOccurred())

					restore.Spec.Type = asdbv1beta1.Incremental

					err = k8sClient.Update(testCtx, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("aerospikeRestore Spec is immutable"))
				})

				It("Should fail restore when wrong backup path is given", func() {
					config := getRestoreConfigInMap("wrong-backup-path")

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Full, configBytes)

					err = createRestoreWithTO(k8sClient, restore, 30*time.Second)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when routine/time is not given for Timestamp restore type", func() {
					// getRestoreConfigInMap returns restore config without a routine, time and with source type
					restoreConfig := getRestoreConfigInMap(backupDataPath)
					delete(restoreConfig, asdbv1beta1.SourceKey)
					delete(restoreConfig, asdbv1beta1.BackupDataPathKey)

					configBytes, mErr := json.Marshal(restoreConfig)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Timestamp, configBytes)

					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("empty field validation error: \"time\" required"))
				})

				It("Should fail when source field is given for Timestamp restore type", func() {
					restore, err = newRestore(restoreNsNm, asdbv1beta1.Timestamp)
					Expect(err).ToNot(HaveOccurred())

					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("source field is not allowed in restore config"))
				})

				It("Should fail when routine field is given for Full/Incremental restore type", func() {
					restoreConfig := getRestoreConfigInMap(backupDataPath)
					restoreConfig[asdbv1beta1.RoutineKey] = "test-routine"

					configBytes, mErr := json.Marshal(restoreConfig)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Full, configBytes)

					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("routine field is not allowed in restore config"))
				})

				It("Should fail when time field is given for Full/Incremental restore type", func() {
					restoreConfig := getRestoreConfigInMap(backupDataPath)
					restoreConfig[asdbv1beta1.TimeKey] = 1722408895094

					configBytes, mErr := json.Marshal(restoreConfig)
					Expect(mErr).ToNot(HaveOccurred())

					restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Full, configBytes)

					err = createRestore(k8sClient, restore)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("time field is not allowed in restore config"))
				})
			})

		Context(
			"When doing valid operations", func() {
				It(
					"Should complete restore for Full restore type", func() {
						restore, err = newRestore(restoreNsNm, asdbv1beta1.Full)
						Expect(err).ToNot(HaveOccurred())

						err = createRestore(k8sClient, restore)
						Expect(err).ToNot(HaveOccurred())

						err = validateRestoredData(k8sClient)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should complete restore for Full restore type and with TLS configured", func() {
						restore, err = newRestoreWithTLS(restoreNsNm, asdbv1beta1.Full)
						Expect(err).ToNot(HaveOccurred())

						err = createRestore(k8sClient, restore)
						Expect(err).ToNot(HaveOccurred())

						err = validateRestoredData(k8sClient)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should complete restore for Incremental restore type", func() {
						restore, err = newRestore(restoreNsNm, asdbv1beta1.Incremental)
						Expect(err).ToNot(HaveOccurred())

						err = createRestore(k8sClient, restore)
						Expect(err).ToNot(HaveOccurred())

						err = validateRestoredData(k8sClient)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should complete restore for Timestamp restore type", func() {
						configBytes, err := getTimeStampRestoreConfigBytes(getRestoreConfigInMap(backupDataPath))
						Expect(err).ToNot(HaveOccurred())

						restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Timestamp, configBytes)

						err = createRestore(k8sClient, restore)
						Expect(err).ToNot(HaveOccurred())

						err = validateRestoredData(k8sClient)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(
					"Should complete restore for Timestamp restore type and with TLS configured", func() {
						configBytes, err := getTimeStampRestoreConfigBytes(getRestoreConfigWithTLSInMap(backupDataPath))
						Expect(err).ToNot(HaveOccurred())

						restore = newRestoreWithConfig(restoreNsNm, asdbv1beta1.Timestamp, configBytes)

						err = createRestore(k8sClient, restore)
						Expect(err).ToNot(HaveOccurred())

						err = validateRestoredData(k8sClient)
						Expect(err).ToNot(HaveOccurred())
					},
				)
			})
	})

func getTimeStampRestoreConfigBytes(restoreConfig map[string]interface{}) (configBytes []byte, err error) {
	delete(restoreConfig, asdbv1beta1.SourceKey)
	delete(restoreConfig, asdbv1beta1.BackupDataPathKey)

	parts := strings.Split(backupDataPath, "/")
	time := parts[len(parts)-3]
	timeInt, err := strconv.Atoi(time)
	Expect(err).ToNot(HaveOccurred())

	// increase time by 1 millisecond to consider the latest backup under time bound
	restoreConfig[asdbv1beta1.TimeKey] = int64(timeInt) + 1
	restoreConfig[asdbv1beta1.RoutineKey] = parts[len(parts)-5]

	return getRestoreConfBytes(restoreConfig)
}
