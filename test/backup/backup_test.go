package backup

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

var _ = Describe(
	"Backup Service Test", func() {

		var (
			backup     *asdbv1beta1.AerospikeBackup
			err        error
			backupNsNm = types.NamespacedName{
				Namespace: namespace,
				Name:      "sample-backup",
			}
		)

		AfterEach(func() {
			Expect(DeleteBackup(k8sClient, backup)).ToNot(HaveOccurred())
		})

		Context(
			"When doing Invalid operations", func() {
				It("Should fail when wrong format backup config is given", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					badConfig, gErr := getWrongBackupConfBytes(namePrefix(backupNsNm))
					Expect(gErr).ToNot(HaveOccurred())
					backup.Spec.Config.Raw = badConfig

					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when improper format name is used in config", func() {
					config := getBackupConfigInMap("wrong-prefix")

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("name should start with %s", namePrefix(backupNsNm)))
				})

				It("Should fail when more than 1 cluster is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					aeroCluster := config[common.AerospikeClusterKey].(map[string]interface{})
					aeroCluster["cluster-two"] = aeroCluster["test-cluster"]
					config[common.AerospikeClusterKey] = aeroCluster

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when on-demand backup is given at the time of creation", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: "test-routine",
						},
					}

					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing routine is given in on-demand backup", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					err = CreateBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: "non-existing-routine",
						},
					}

					err = updateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service is not present", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.BackupService.Name = "wrong-backup-service"

					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service reference is updated", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					err = CreateBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.BackupService.Name = "updated-backup-service"

					err = updateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing policy is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["backup-policy"] =
						"non-existing-policy"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing cluster is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["source-cluster"] =
						"non-existing-cluster"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing storage is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["storage"] =
						"non-existing-storage"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when a different aerospike cluster is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["source-cluster"] =
						"random-cluster"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when service field is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					config[common.ServiceKey] = map[string]interface{}{
						"http": map[string]interface{}{
							"port": 8081,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("service field cannot be specified in backup config"))
				})

				It("Should fail when backup-policies field is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					config[common.BackupPoliciesKey] = map[string]interface{}{
						"test-policy": map[string]interface{}{
							"parallel":     3,
							"remove-files": "KeepAll",
							"type":         1,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("backup-policies field cannot be specified in backup config"))
				})

				It("Should fail when storage field is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					config[common.StorageKey] = map[string]interface{}{
						"local": map[string]interface{}{
							"path": "/localStorage",
							"type": "local",
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("storage field cannot be specified in backup config"))
				})

				It("Should fail when secret-agent is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					config[common.SecretAgentsKey] = map[string]interface{}{
						"test-agent": map[string]interface{}{
							"address": "localhost",
							"port":    4000,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("secret-agent field cannot be specified in backup config"))
				})

				It("Should fail when aerospike-cluster name is updated", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					err = CreateBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					err = validateTriggeredBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					// Change prefix to generate new names
					prefix := namePrefix(backupNsNm) + "-1"
					config := getBackupConfigInMap(prefix)

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup.Spec.Config.Raw = configBytes

					err = updateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("aerospike-cluster name update is not allowed"))
				})

			},
		)

		Context("When doing Valid operations", func() {
			It("Should trigger backup when correct backup config with local storage is given", func() {
				backup, err = NewBackup(backupNsNm)
				Expect(err).ToNot(HaveOccurred())
				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

			})

			It("Should trigger backup when correct backup config with s3 storage is given", func() {
				config := getBackupConfigInMap(namePrefix(backupNsNm))
				backupRoutines := config[common.BackupRoutinesKey].(map[string]interface{})
				backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})[common.StorageKey] =
					"s3Storage"

				config[common.BackupRoutinesKey] = backupRoutines

				configBytes, mErr := json.Marshal(config)
				Expect(mErr).ToNot(HaveOccurred())

				backup = newBackupWithConfig(backupNsNm, configBytes)

				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should trigger on-demand backup when given", func() {
				backup, err = NewBackup(backupNsNm)
				Expect(err).ToNot(HaveOccurred())
				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
				Expect(err).ToNot(HaveOccurred())

				backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
					{
						ID:          "on-demand",
						RoutineName: namePrefix(backupNsNm) + "-" + "test-routine",
					},
				}

				err = updateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should unregister backup-routines when removed from backup CR", func() {
				backupConfig := getBackupConfigInMap(namePrefix(backupNsNm))
				backupRoutines := backupConfig[common.BackupRoutinesKey].(map[string]interface{})
				backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine1"] = map[string]interface{}{
					"backup-policy":      "test-policy1",
					"interval-cron":      "@daily",
					"incr-interval-cron": "@hourly",
					"namespaces":         []string{"test"},
					"source-cluster":     namePrefix(backupNsNm) + "-" + "test-cluster",
					"storage":            "local",
				}

				backupConfig[common.BackupRoutinesKey] = backupRoutines

				configBytes, err := json.Marshal(backupConfig)
				Expect(err).ToNot(HaveOccurred())

				backup = newBackupWithConfig(backupNsNm, configBytes)
				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
				Expect(err).ToNot(HaveOccurred())

				By("Removing 1 backup-routine from backup CR")
				backupConfig = getBackupConfigInMap(namePrefix(backupNsNm))

				configBytes, err = json.Marshal(backupConfig)
				Expect(err).ToNot(HaveOccurred())

				backup.Spec.Config.Raw = configBytes

				err = updateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())
			})

		})
	},
)
