package backupservice

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"

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
				It("Should fail when wrong format backup config is given", func() {
					badConfig, gErr := getWrongBackupServiceConfBytes()
					Expect(gErr).ToNot(HaveOccurred())
					backupService = newBackupServiceWithConfig(badConfig)

					err = DeployBackupService(k8sClient, backupService)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when wrong image is given", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = "wrong-image"

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when aerospike-clusters field is given", func() {
					configMap := getBackupServiceConfMap()
					configMap[common.AerospikeClustersKey] = map[string]interface{}{
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
					configMap[common.BackupRoutinesKey] = map[string]interface{}{
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

			It("Should restart backup service deployment pod when config is changed", func() {
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

				// Change config
				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8080}}}`)
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))

				Expect(podList.Items[0].ObjectMeta.UID).ToNot(Equal(PodUID))
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
						corev1.ResourceCPU: resource.MustParse("0.5"),
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

				Eventually(func() bool {
					svc, err = getBackupK8sServiceObj(k8sClient, name, namespace)
					if err != nil {
						return false
					}
					return svc.Status.LoadBalancer.Ingress != nil
				}, timeout, interval).Should(BeTrue())

				// Check backup service health using LB IP
				Eventually(func() bool {
					resp, err := http.Get("http://" + svc.Status.LoadBalancer.Ingress[0].IP + ":8081/health")
					if err != nil {
						pkgLog.Error(err, "Failed to get health")
						return false
					}

					defer resp.Body.Close()

					return resp.StatusCode == http.StatusOK
				}, timeout, interval).Should(BeTrue())

			})

		})
	},
)
