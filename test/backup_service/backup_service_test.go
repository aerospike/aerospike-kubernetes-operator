package backupservice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
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

				It("Should fail when adding reserved label", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())
					backupService.Spec.PodSpec.ObjectMeta.Labels = map[string]string{
						asdbv1.AerospikeAppLabel: "test",
					}
					err = DeployBackupService(k8sClient, backupService)
					Expect(err).Should(HaveOccurred())
				})

				It("Should fail when resources.request exceeding resources.limit", func() {
					backupService, err = NewBackupService()
					Expect(err).ToNot(HaveOccurred())

					requestMem := resource.MustParse("3Gi")
					requestCPU := resource.MustParse("250m")
					limitMem := resource.MustParse("2Gi")
					limitCPU := resource.MustParse("200m")

					resources := &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    requestCPU,
							corev1.ResourceMemory: requestMem,
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    limitCPU,
							corev1.ResourceMemory: limitMem,
						},
					}

					backupService.Spec.PodSpec.ServiceContainerSpec.Resources = resources
					err = DeployBackupService(k8sClient, backupService)
					Expect(err).Should(HaveOccurred())
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
				backupService.Spec.PodSpec = asdbv1beta1.ServicePodSpec{
					ObjectMeta: asdbv1beta1.AerospikeObjectMeta{
						Labels:      map[string]string{"label-test-1": "test-1"},
						Annotations: map[string]string{"annotation-test-1": "test-1"},
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
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				svc, sErr := getBackupK8sServiceObj(k8sClient, name, namespace)
				Expect(sErr).ToNot(HaveOccurred())
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
					resp, gErr := http.Get("http://" + svc.Status.LoadBalancer.Ingress[0].IP + ":8081/health")
					if gErr != nil {
						pkgLog.Error(gErr, "Failed to get health")
						return false
					}

					defer resp.Body.Close()

					return resp.StatusCode == http.StatusOK
				}, timeout, interval).Should(BeTrue())

			})

			It("Should add/update custom annotations and labels in the backup service deployement pods", func() {
				labels := map[string]string{"label-test-1": "test-1"}
				annotations := map[string]string{"annotation-test-1": "test-1"}

				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				backupService.Spec.PodSpec.ObjectMeta.Labels = labels
				backupService.Spec.PodSpec.ObjectMeta.Annotations = annotations
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validatePodObjectMeta(annotations, labels)

				By("Updating custom annotations and labels")
				updatedLabels := map[string]string{"label-test-2": "test-2", "label-test-3": "test-3"}
				updatedAnnotations := map[string]string{"annotation-test-2": "test-2", "annotation-test-3": "test-3"}

				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())
				backupService.Spec.PodSpec.ObjectMeta.Labels = updatedLabels
				backupService.Spec.PodSpec.ObjectMeta.Annotations = updatedAnnotations
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validatePodObjectMeta(updatedAnnotations, updatedLabels)
			})

			It("Should add SchedulingPolicy in the backup service deployement pods", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())

				By("Validating Affinity")
				affinity := &corev1.Affinity{}
				st := []corev1.PreferredSchedulingTerm{
					{
						Weight: 1,
						Preference: corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "test-key1",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"test-value1"},
								},
							},
						},
					},
				}
				nodeAffinity := &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: st,
				}
				affinity.NodeAffinity = nodeAffinity
				backupService.Spec.PodSpec.Affinity = affinity
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				backupService, err = getBackupServiceObj(k8sClient, name, namespace)
				Expect(err).ToNot(HaveOccurred())
				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))
				Expect(podList.Items[0].Spec.Affinity.NodeAffinity).Should(Equal(nodeAffinity))
			})

			It("Should add resources and securityContext in the backup service container", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				requestMem := resource.MustParse("100Mi")
				requestCPU := resource.MustParse("20m")
				limitMem := resource.MustParse("2Gi")
				limitCPU := resource.MustParse("300m")

				res := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    requestCPU,
						corev1.ResourceMemory: requestMem,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    limitCPU,
						corev1.ResourceMemory: limitMem,
					},
				}

				sc := &corev1.SecurityContext{Privileged: new(bool)}

				By("Validating Resources")
				updateBackupServiceResources(k8sClient, res)

				By("Remove Resources")
				updateBackupServiceResources(k8sClient, nil)

				By("Validating SecurityContext")
				updateBackupServiceSecurityContext(k8sClient, sc)

				By("Remove SecurityContext")
				updateBackupServiceSecurityContext(k8sClient, nil)
			})

			It("Should deploy backup service with custom Service Account", func() {
				backupService, err = NewBackupService()
				Expect(err).ToNot(HaveOccurred())
				backupService.Spec.PodSpec.ServiceAccountName = "default"
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(1))
				Expect(podList.Items[0].Spec.ServiceAccountName).Should(Equal("default"))
			})

		})
	},
)

func updateBackupServiceResources(k8sClient client.Client, res *corev1.ResourceRequirements) {
	backupService, err := getBackupServiceObj(k8sClient, name, namespace)
	Expect(err).ToNot(HaveOccurred())

	backupService.Spec.PodSpec.ServiceContainerSpec.Resources = res
	err = updateBackupService(k8sClient, backupService)
	Expect(err).ToNot(HaveOccurred())

	err = validateBackupServiceResources(k8sClient, res)
	Expect(err).ToNot(HaveOccurred())
}

func validateBackupServiceResources(k8sClient client.Client, res *corev1.ResourceRequirements) error {
	deploy, err := getBackupServiceDeployment(k8sClient, name, namespace)
	if err != nil {
		return err
	}

	// res can not be null in deploy spec
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}

	actual := deploy.Spec.Template.Spec.Containers[0].Resources
	if !reflect.DeepEqual(&actual, res) {
		return fmt.Errorf("resource not matching. want %v, got %v", *res, actual)
	}

	return nil
}

func updateBackupServiceSecurityContext(k8sClient client.Client, sc *corev1.SecurityContext) {
	backupService, err := getBackupServiceObj(k8sClient, name, namespace)
	Expect(err).ToNot(HaveOccurred())

	backupService.Spec.PodSpec.ServiceContainerSpec.SecurityContext = sc
	err = updateBackupService(k8sClient, backupService)
	Expect(err).ToNot(HaveOccurred())

	err = validateBackupServiceSecurityContext(k8sClient, sc)
	Expect(err).ToNot(HaveOccurred())
}

func validateBackupServiceSecurityContext(k8sClient client.Client, sc *corev1.SecurityContext) error {
	deploy, err := getBackupServiceDeployment(k8sClient, name, namespace)
	if err != nil {
		return err
	}

	actual := deploy.Spec.Template.Spec.Containers[0].SecurityContext
	if !reflect.DeepEqual(actual, sc) {
		return fmt.Errorf("security context not matching")
	}

	return nil
}

func validatePodObjectMeta(annotations, labels map[string]string) {
	deploy, dErr := getBackupServiceDeployment(k8sClient, name, namespace)
	Expect(dErr).ToNot(HaveOccurred())

	By("Validating Annotations")

	actual := deploy.Spec.Template.ObjectMeta.Annotations
	valid := validateLabelsOrAnnotations(actual, annotations)
	Expect(valid).To(
		BeTrue(), "Annotations mismatch. expected %+v, found %+v", annotations, actual,
	)

	By("Validating Labels")

	actual = deploy.Spec.Template.ObjectMeta.Labels
	valid = validateLabelsOrAnnotations(actual, labels)
	Expect(valid).To(
		BeTrue(), "Labels mismatch. expected %+v, found %+v", labels, actual,
	)
}
