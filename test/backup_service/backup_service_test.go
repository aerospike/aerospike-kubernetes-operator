package backupservice

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

var _ = Describe(
	"Backup Service Test", func() {
		var (
			backupService *asdbv1beta1.AerospikeBackupService
			err           error
		)

		AfterEach(func() {
			Expect(DeleteBackupService(k8sClient, backupService)).ToNot(HaveOccurred())
		})

		Context(
			"When doing Invalid operations", func() {
				It("Should fail when wrong format backup service config is given", func() {
					badConfig, gErr := getWrongBackupServiceConfBytes()
					Expect(gErr).ToNot(HaveOccurred())
					backupService = newBackupServiceWithConfig(badConfig)

					err = DeployBackupService(k8sClient, backupService)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when un-supported field is given in backup service config", func() {
					configMap := getBackupServiceConfMap()
					configMap["unknown"] = "unknown"

					configBytes, mErr := json.Marshal(configMap)
					Expect(mErr).ToNot(HaveOccurred())

					backupService = newBackupServiceWithConfig(configBytes)

					err = DeployBackupService(k8sClient, backupService)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("unknown field"))
				})

				It("Should fail when wrong image is given", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = "wrong-image"

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service version is less than 3.0.0", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = BackupServiceVersion2Image

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("Minimum supported version is 3.0.0"))
				})

				It("Should fail when backup service image version is downgraded to 2.0", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())
					err = DeployBackupService(k8sClient, backupService)
					Expect(err).ToNot(HaveOccurred())

					By("Downgrading image version to 2.0")
					backupService, err = getBackupServiceObj(k8sClient, name, namespace)
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = BackupServiceVersion2Image

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("Minimum supported version is 3.0.0"))
				})

				It("Should fail when duplicate volume names are given in secrets", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())
					secretCopy := backupService.Spec.SecretMounts[0]
					backupService.Spec.SecretMounts = append(backupService.Spec.SecretMounts, secretCopy)

					err = deployBackupServiceWithTO(k8sClient, backupService, 5*time.Second)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("duplicate volume name"))
				})

				It("Should fail when aerospike-clusters field is given", func() {
					configMap := getBackupServiceConfMap()
					configMap[asdbv1beta1.AerospikeClustersKey] = map[string]interface{}{
						"test-cluster": map[string]interface{}{
							"credentials": map[string]interface{}{
								"password": "admin123",
								"user":     "admin",
							},
							"seed-nodes": []map[string]interface{}{
								{
									"host-name": "aerocluster.aerospike.svc.cluster.local",
									"port":      3000,
								},
							},
						},
					}

					configBytes, mErr := json.Marshal(configMap)
					Expect(mErr).ToNot(HaveOccurred())

					backupService = newBackupServiceWithConfig(configBytes)

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(
						"aerospike-clusters field cannot be specified in backup service config"))
				})

				It("Should fail when backup-routines field is given", func() {
					configMap := getBackupServiceConfMap()
					configMap[asdbv1beta1.BackupRoutinesKey] = map[string]interface{}{
						"test-routine": map[string]interface{}{
							"backup-policy":      "test-policy",
							"interval-cron":      "@daily",
							"incr-interval-cron": "@hourly",
							"namespaces":         []string{"test"},
							"source-cluster":     "test-cluster",
							"storage":            "local",
						},
					}

					configBytes, mErr := json.Marshal(configMap)
					Expect(mErr).ToNot(HaveOccurred())

					backupService = newBackupServiceWithConfig(configBytes)

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("backup-routines field cannot be specified in backup service config"))
				})
			},
		)

		Context("When doing Valid operations", func() {
			It("Should deploy backup service components when correct backup config is given", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should restart backup service deployment pod when static fields are changed in backup service "+
				"config", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				PodUID := podList.Items[0].ObjectMeta.UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())

				By("Change static fields")
				// Changing static field 'port' to 8080 and removing dynamic fields like backup policies and storage
				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8080}}}`)
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				Expect(podList.Items[0].ObjectMeta.UID).ToNot(Equal(PodUID))
			})

			It("Should do hot-reload when dynamic fields are changed in backup service config", func() {
				backupService, err = NewBackupService()
				backupService.Spec.Service = &asdbv1beta1.Service{Type: corev1.ServiceTypeLoadBalancer}
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				PodUID := podList.Items[0].ObjectMeta.UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())

				By("Change dynamic fields")
				// Keeping static field 'port' same and removing dynamic fields like backup policies and storage
				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8081}}}`)
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				Expect(podList.Items[0].ObjectMeta.UID).To(Equal(PodUID))

				config, gErr := GetAPIBackupSvcConfig(k8sClient, backupService.Name, backupService.Namespace)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(config).ToNot(BeNil())
				Expect(config[asdbv1beta1.BackupRoutinesKey]).To(BeNil())
				Expect(config[asdbv1beta1.StorageKey]).To(BeNil())
			})

			It("Should restart backup service deployment pod when pod spec is changed", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				PodUID := podList.Items[0].ObjectMeta.UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())

				// Change Pod spec
				backupService.Spec.Resources = &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("0.2"),
					},
				}

				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				Expect(podList.Items[0].ObjectMeta.UID).ToNot(Equal(PodUID))
			})

			It("Should change K8s service type when service type is changed in CR", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				err := DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				svc, err := getBackupK8sServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())

				// Change service type
				backupService.Spec.Service = &asdbv1beta1.Service{Type: corev1.ServiceTypeLoadBalancer}

				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				svc, err = getBackupK8sServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))

				_, err = GetAPIBackupSvcConfig(k8sClient, backupService.Name, backupService.Namespace)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	},
)
