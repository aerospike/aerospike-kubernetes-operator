package backup

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
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
					Expect(err.Error()).To(
						ContainSubstring("name should start with %s", namePrefix(backupNsNm)))
				})

				It("Should fail when un-supported field is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["unknown"] = "unknown"
					config[asdbv1beta1.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("unknown field"))
				})

				It("Should fail when more than 1 cluster is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					aeroCluster := config[asdbv1beta1.AerospikeClusterKey].(map[string]interface{})
					aeroCluster["cluster-two"] = aeroCluster["test-cluster"]
					config[asdbv1beta1.AerospikeClusterKey] = aeroCluster

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("only one aerospike cluster is allowed in backup config"))
				})

				It("Should fail when on-demand backup is given at the time of creation", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: namePrefix(backupNsNm) + "-" + "test-routine",
						},
					}

					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("onDemand backups config cannot be specified while creating backup"))
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
					Expect(err.Error()).To(
						ContainSubstring("invalid onDemand config, backup routine non-existing-routine not found"))
				})

				It("Should fail when backup service is not present", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.BackupService.Name = "wrong-backup-service"

					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("not found"))
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
					Expect(err.Error()).To(
						ContainSubstring("backup service cannot be updated"))
				})

				It("Should fail when non-existing policy is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["backup-policy"] =
						"non-existing-policy"
					config[asdbv1beta1.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing cluster is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})[asdbv1beta1.SourceClusterKey] =
						"non-existing-cluster"
					config[asdbv1beta1.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when non-existing storage is referred in Backup routine", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					routines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
					routines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})["storage"] =
						"non-existing-storage"
					config[asdbv1beta1.BackupRoutinesKey] = routines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup = newBackupWithConfig(backupNsNm, configBytes)
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when empty backup config is given", func() {
					backup = newBackupWithEmptyConfig(backupNsNm)
					backup.Spec.Config.Raw = []byte("{}")
					err = CreateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when service field is given in backup config", func() {
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					config[asdbv1beta1.ServiceKey] = map[string]interface{}{
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
					config[asdbv1beta1.BackupPoliciesKey] = map[string]interface{}{
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
					config[asdbv1beta1.StorageKey] = map[string]interface{}{
						"local": map[string]interface{}{
							"local-storage": map[string]interface{}{
								"path": "/tmp/localStorage",
							},
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
					config[asdbv1beta1.SecretAgentsKey] = map[string]interface{}{
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
					Expect(err.Error()).To(
						ContainSubstring("aerospike-cluster name cannot be updated"))
				})

				It("Should fail when on-demand backup is added along with backup-config update", func() {
					backup, err = NewBackup(backupNsNm)
					Expect(err).ToNot(HaveOccurred())

					err = CreateBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					err = validateTriggeredBackup(k8sClient, backup)
					Expect(err).ToNot(HaveOccurred())

					backup, err = getBackupObj(k8sClient, backup.Name, backup.Namespace)
					Expect(err).ToNot(HaveOccurred())

					backup.Spec.OnDemandBackups = []asdbv1beta1.OnDemandBackupSpec{
						{
							ID:          "on-demand",
							RoutineName: namePrefix(backupNsNm) + "-" + "test-routine",
						},
					}

					// change storage to change overall backup config
					config := getBackupConfigInMap(namePrefix(backupNsNm))
					backupRoutines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
					backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})[asdbv1beta1.StorageKey] =
						"s3Storage"

					config[asdbv1beta1.BackupRoutinesKey] = backupRoutines

					configBytes, mErr := json.Marshal(config)
					Expect(mErr).ToNot(HaveOccurred())

					backup.Spec.Config.Raw = configBytes

					err = updateBackup(k8sClient, backup)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(
						"can not add/update onDemand backup along with backup config change"))
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

			It("Should trigger backup when correct backup config with TLS and local storage are given", func() {
				backup, err = NewBackupWithTLS(backupNsNm)
				Expect(err).ToNot(HaveOccurred())
				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

			})

			It("Should trigger backup when correct backup config with s3 storage is given", func() {
				config := getBackupConfigInMap(namePrefix(backupNsNm))
				backupRoutines := config[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
				backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine"].(map[string]interface{})[asdbv1beta1.StorageKey] =
					"s3Storage"

				config[asdbv1beta1.BackupRoutinesKey] = backupRoutines

				configBytes, mErr := json.Marshal(config)
				Expect(mErr).ToNot(HaveOccurred())

				backup = newBackupWithConfig(backupNsNm, configBytes)

				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should delete dangling routines from the Backup service configMap", func() {
				backup, err = NewBackup(backupNsNm)
				Expect(err).ToNot(HaveOccurred())

				err = CreateBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				err = validateTriggeredBackup(k8sClient, backup)
				Expect(err).ToNot(HaveOccurred())

				By("Get Backup service configmap to update new dangling backup routine")
				var cm corev1.ConfigMap

				err = k8sClient.Get(testCtx,
					types.NamespacedName{Name: backupServiceName, Namespace: backupServiceNamespace},
					&cm)
				Expect(err).ToNot(HaveOccurred())

				// Add a routine to the configMap
				data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]
				backupSvcConfig := make(map[string]interface{})

				err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
				Expect(err).ToNot(HaveOccurred())

				backupRoutines := backupSvcConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
				// Add a new routine with a different name
				newRoutineName := namePrefix(backupNsNm) + "-" + "test-routine1"
				backupRoutines[newRoutineName] =
					backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine"]

				backupSvcConfig[asdbv1beta1.BackupRoutinesKey] = backupRoutines

				newData, mErr := yaml.Marshal(backupSvcConfig)
				Expect(mErr).ToNot(HaveOccurred())

				cm.Data[asdbv1beta1.BackupServiceConfigYAML] = string(newData)

				err = k8sClient.Update(testCtx, &cm)
				Expect(err).ToNot(HaveOccurred())

				By("Update backup CR to add on-demand backup")
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

				By("Validate the routine is removed from the Backup service configMap")
				err = k8sClient.Get(testCtx,
					types.NamespacedName{Name: backupServiceName, Namespace: backupServiceNamespace},
					&cm)
				Expect(err).ToNot(HaveOccurred())

				data = cm.Data[asdbv1beta1.BackupServiceConfigYAML]
				backupSvcConfig = make(map[string]interface{})

				err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
				Expect(err).ToNot(HaveOccurred())

				backupRoutines = backupSvcConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
				_, ok := backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine1"]
				Expect(ok).To(BeFalse())
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
				backupRoutines := backupConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})
				backupRoutines[namePrefix(backupNsNm)+"-"+"test-routine1"] = map[string]interface{}{
					"backup-policy":      "test-policy1",
					"interval-cron":      "@daily",
					"incr-interval-cron": "@hourly",
					"namespaces":         []string{"test"},
					"source-cluster":     namePrefix(backupNsNm) + "-" + "test-cluster",
					"storage":            "local",
				}

				backupConfig[asdbv1beta1.BackupRoutinesKey] = backupRoutines

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
