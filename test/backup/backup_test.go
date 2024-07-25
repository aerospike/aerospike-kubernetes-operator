package backup

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

var _ = Describe(
	"Backup Service Test", func() {

		var (
			backup *asdbv1beta1.AerospikeBackup
			err    error
		)

		AfterEach(func() {
			Expect(deleteBackup(k8sClient, backup)).ToNot(HaveOccurred())
		})

		Context(
			"When doing Invalid operations", func() {
				It("Should fail when wrong format backup config is given", func() {
					backup, err = newBackup()
					Expect(err).ToNot(HaveOccurred())

					badConfig, gErr := getWrongBackupConfBytes()
					Expect(gErr).ToNot(HaveOccurred())
					backup.Spec.Config.Raw = badConfig

					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when more than 1 cluster is given in backup config", func() {
					config := getBackupConfigInMap()
					aeroCluster := config[common.AerospikeClusterKey].(map[string]interface{})
					aeroCluster["cluster-two"] = aeroCluster["test-cluster"]
					config[common.AerospikeClusterKey] = aeroCluster

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when on-demand backup is given at the time of creation", func() {
					backup, err = newBackup()
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: "test-routine",
						},
					}

					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing routine is given in on-demand backup", func() {
					backup, err = newBackup()
					Expect(err).ToNot(HaveOccurred())

					err = deployBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: "non-existing-routine",
						},
					}

					err = k8sClient.Update(testCtx, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service is not present", func() {
					backup, err = newBackup()
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.BackupService.Name = "wrong-backup-service"

					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service reference is updated", func() {
					backup, err = newBackup()
					Expect(err).ToNot(HaveOccurred())

					err = deployBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.BackupService.Name = "updated-backup-service"

					err = k8sClient.Update(testCtx, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing policy is referred in Backup routine", func() {
					config := getBackupConfigInMap()
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines["test-routine"].(map[string]interface{})["backup-policy"] = "non-existing-policy"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing cluster is referred in Backup routine", func() {
					config := getBackupConfigInMap()
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines["test-routine"].(map[string]interface{})["source-cluster"] = "non-existing-cluster"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing storage is referred in Backup routine", func() {
					config := getBackupConfigInMap()
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines["test-routine"].(map[string]interface{})["storage"] = "non-existing-storage"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when a different aerospike cluster is referred in Backup routine", func() {
					config := getBackupConfigInMap()
					routines := config[common.BackupRoutinesKey].(map[string]interface{})
					routines["test-routine"].(map[string]interface{})["source-cluster"] = "random-cluster"
					config[common.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when service field is given in backup config", func() {
					config := getBackupConfigInMap()
					config[common.ServiceKey] = map[string]interface{}{
						"http": map[string]interface{}{
							"port": 8081,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("service field cannot be specified in backup config"))
				})

				It("Should fail when backup-policies field is given in backup config", func() {
					config := getBackupConfigInMap()
					config[common.BackupPoliciesKey] = map[string]interface{}{
						"test-policy": map[string]interface{}{
							"parallel":     3,
							"remove-files": "KeepAll",
							"type":         1,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("backup-policies field cannot be specified in backup config"))
				})

				It("Should fail when storage field is given in backup config", func() {
					config := getBackupConfigInMap()
					config[common.StorageKey] = map[string]interface{}{
						"local": map[string]interface{}{
							"path": "/localStorage",
							"type": "local",
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("storage field cannot be specified in backup config"))
				})

				It("Should fail when secret-agent is given in backup config", func() {
					config := getBackupConfigInMap()
					config[common.SecretAgentsKey] = map[string]interface{}{
						"test-agent": map[string]interface{}{
							"address": "localhost",
							"port":    4000,
						},
					}

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(configBytes)
					err = deployBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("secret-agent field cannot be specified in backup config"))
				})

			},
		)

		Context("When doing Valid operations", func() {
			It("Should trigger backup when correct backup config is given", func() {
				backup, err = newBackup()
				Expect(err).ToNot(HaveOccurred())
				err = deployBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backupServiceName, backupServiceNamespace, backup)
				Expect(err).ToNot(HaveOccurred())

			})

			It("Should trigger on-demand backup when given", func() {
				backup, err = newBackup()
				Expect(err).ToNot(HaveOccurred())
				err = deployBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
				Expect(err).ToNot(HaveOccurred())

				backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
					{
						ID:          "on-demand",
						RoutineName: "test-routine",
					},
				}

				err = k8sClient.Update(testCtx, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backupServiceName, backupServiceNamespace, backup)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should unregister backup-routines when removed from backup CR", func() {
				backup, err = newBackup()
				Expect(err).ToNot(HaveOccurred())
				err = deployBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backupServiceName, backupServiceNamespace, backup)
				Expect(err).ToNot(HaveOccurred())

				backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
				Expect(err).ToNot(HaveOccurred())

				By("Removing 1 backup-routine from backup CR")
				backupConfig := getBackupConfigInMap()
				routines := backupConfig[common.BackupRoutinesKey].(map[string]interface{})
				delete(routines, "test-routine1")
				backupConfig[common.BackupRoutinesKey] = routines

				configBytes, mErr := json.Marshal(backupConfig)
				Expect(mErr).ToNot(HaveOccurred())

				backup.Spec.Config.Raw = configBytes

				err = k8sClient.Update(testCtx, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backupServiceName, backupServiceNamespace, backup)
				Expect(err).ToNot(HaveOccurred())
			})

		})
	},
)
