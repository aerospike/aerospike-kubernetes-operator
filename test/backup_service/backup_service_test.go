package backupservice

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"Backup Service Test", func() {
		var (
			backupService *asdbv1beta1.AerospikeBackupService
			err           error
		)

		backupServiceName := fmt.Sprintf(name+"-%d", GinkgoParallelProcess())
		backupServiceNamespacedName := test.GetNamespacedName(backupServiceName, namespace)

		AfterEach(func() {
			Expect(DeleteBackupService(k8sClient, backupService)).ToNot(HaveOccurred())
		})

		Context(
			"When doing Invalid operations", func() {
				It("Should fail when wrong format backup service config is given", func() {
					badConfig, gErr := getWrongBackupServiceConfBytes()
					Expect(gErr).ToNot(HaveOccurred())

					backupService = newBackupServiceWithConfig(backupServiceNamespacedName, badConfig)

					err = DeployBackupService(k8sClient, backupService)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when un-supported field is given in backup service config", func() {
					configMap := getBackupServiceConfMap()
					configMap["unknown"] = "unknown"

					configBytes, mErr := json.Marshal(configMap)
					Expect(mErr).ToNot(HaveOccurred())

					backupService = newBackupServiceWithConfig(backupServiceNamespacedName, configBytes)

					err = DeployBackupService(k8sClient, backupService)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("unknown field"))
				})

				It("Should fail when wrong image is given", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = "wrong-image"

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
				})

				It("Should fail when backup service version is less than 3.0.0", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = BackupServiceVersion2Image

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("Minimum supported version is 3.0.0"))
				})

				It("Should fail when backup service image version is downgraded to 2.0", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
					Expect(err).ToNot(HaveOccurred())
					err = DeployBackupService(k8sClient, backupService)
					Expect(err).ToNot(HaveOccurred())

					By("Downgrading image version to 2.0")

					backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.Image = BackupServiceVersion2Image

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("Minimum supported version is 3.0.0"))
				})

				It("Should fail when duplicate volume names are given in secrets", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
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

					backupService = newBackupServiceWithConfig(backupServiceNamespacedName, configBytes)

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

					backupService = newBackupServiceWithConfig(backupServiceNamespacedName, configBytes)

					err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(
						ContainSubstring("backup-routines field cannot be specified in backup service config"))
				})

				It("Should fail when adding reserved label", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					backupService.Spec.PodSpec.ObjectMeta.Labels = map[string]string{
						asdbv1.AerospikeAppLabel: "test",
					}
					err = DeployBackupService(k8sClient, backupService)
					Expect(err).Should(HaveOccurred())
				})

				It("Should fail when resources.request exceeding resources.limit", func() {
					backupService, err = NewBackupService(backupServiceNamespacedName)
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
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should restart backup service deployment pod when static fields are changed in backup service "+
				"config", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				PodUID := podList.Items[0].UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				By("Change static fields")
				// Changing static field 'port' to 8080 and removing dynamic fields like backup policies and storage
				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8080}}}`)
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				Expect(podList.Items[0].ObjectMeta.UID).ToNot(Equal(PodUID))
			})

			It("Should do hot-reload when dynamic fields are changed in backup service config", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				backupService.Spec.Service = &asdbv1beta1.Service{Type: corev1.ServiceTypeLoadBalancer}
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				PodUID := podList.Items[0].UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				By("Change dynamic fields")
				// Keeping static field 'port' same and removing dynamic fields like backup policies and storage
				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8081}}}`)
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, err = getBackupServicePodList(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				Expect(podList.Items[0].ObjectMeta.UID).To(Equal(PodUID))

				config, gErr := GetAPIBackupSvcConfig(k8sClient, backupService.Name, backupService.Namespace)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(config).ToNot(BeNil())
				Expect(config[asdbv1beta1.BackupRoutinesKey]).To(BeNil())
				Expect(config[asdbv1beta1.StorageKey]).To(BeNil())
			})

			It("Should restart backup service deployment pod when pod spec is changed", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				PodUID := podList.Items[0].UID

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
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
				Expect(podList.Items).To(HaveLen(1))

				Expect(podList.Items[0].ObjectMeta.UID).ToNot(Equal(PodUID))
			})

			It("Should change K8s service type when service type is changed in CR", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				svc, sErr := getBackupK8sServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(sErr).ToNot(HaveOccurred())
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				// Change service type
				backupService.Spec.Service = &asdbv1beta1.Service{Type: corev1.ServiceTypeLoadBalancer}

				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				svc, err = getBackupK8sServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))

				_, err = GetAPIBackupSvcConfig(k8sClient, backupService.Name, backupService.Namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should add/update custom annotations and labels in the backup service deployment pods", func() {
				labels := map[string]string{"label-test-1": "test-1"}
				annotations := map[string]string{"annotation-test-1": "test-1"}

				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				backupService.Spec.PodSpec.ObjectMeta.Labels = labels
				backupService.Spec.PodSpec.ObjectMeta.Annotations = annotations
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validatePodObjectMeta(annotations, labels, backupServiceNamespacedName)

				By("Updating custom annotations and labels")

				updatedLabels := map[string]string{"label-test-2": "test-2", "label-test-3": "test-3"}
				updatedAnnotations := map[string]string{"annotation-test-2": "test-2", "annotation-test-3": "test-3"}

				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				backupService.Spec.PodSpec.ObjectMeta.Labels = updatedLabels
				backupService.Spec.PodSpec.ObjectMeta.Annotations = updatedAnnotations
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validatePodObjectMeta(updatedAnnotations, updatedLabels, backupServiceNamespacedName)
			})

			It("Should add SchedulingPolicy in the backup service deployment pods", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
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

				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				podList, gErr := getBackupServicePodList(k8sClient, backupService)
				Expect(gErr).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))
				Expect(podList.Items[0].Spec.Affinity.NodeAffinity).Should(Equal(nodeAffinity))
			})

			It("Should add resources and securityContext in the backup service container", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
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
				updateBackupServiceResources(k8sClient, backupServiceNamespacedName, res)

				By("Remove Resources")
				updateBackupServiceResources(k8sClient, backupServiceNamespacedName, nil)

				By("Validating SecurityContext")
				updateBackupServiceSecurityContext(k8sClient, backupServiceNamespacedName, sc)

				By("Remove SecurityContext")
				updateBackupServiceSecurityContext(k8sClient, backupServiceNamespacedName, nil)
			})

			It("Should deploy backup service with custom Service Account", func() {
				validateSA := func(serviceAccount string) {
					podList, gErr := getBackupServicePodList(k8sClient, backupService)
					Expect(gErr).ToNot(HaveOccurred())
					Expect(podList.Items).To(HaveLen(1))
					Expect(podList.Items[0].Spec.ServiceAccountName).Should(Equal(serviceAccount))
				}

				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validateSA(asdbv1beta1.AerospikeBackupServiceKey)

				By("Update Service Account")

				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				backupService.Spec.PodSpec.ServiceAccountName = "default"
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validateSA("default")

				By("Revert back to previous Service Account")

				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				backupService.Spec.PodSpec.ServiceAccountName = ""
				err = updateBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				validateSA(asdbv1beta1.AerospikeBackupServiceKey)
			})
		})

		Context("When doing recovery", func() {
			nodeLabelKey := fmt.Sprintf("test-key-%d", GinkgoParallelProcess())
			nodeLabelValue := "test-value"

			AfterEach(func() {
				err = test.DeleteNodeLabels(testCtx, k8sClient, []string{nodeLabelKey})
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should recover when pod is in pending state during create operation "+
				"and infra is made available later", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				affinity := &corev1.Affinity{}
				nodeSelector := &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      nodeLabelKey,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{nodeLabelValue},
								},
							},
						},
					},
				}

				nodeAffinity := &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
				}
				affinity.NodeAffinity = nodeAffinity
				backupService.Spec.PodSpec.Affinity = affinity
				err = deployBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
				Expect(err).To(HaveOccurred())

				By("Adding node labels to make infra available")

				err = test.SetNodeLabels(testCtx, k8sClient,
					map[string]string{
						nodeLabelKey: nodeLabelValue,
					})
				Expect(err).ToNot(HaveOccurred())

				err = waitForBackupService(k8sClient, backupService, timeout)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should recover when pod is in pending state during update operation "+
				"and infra is made available later", func() {
				backupService, err = NewBackupService(backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				err = DeployBackupService(k8sClient, backupService)
				Expect(err).ToNot(HaveOccurred())

				// Get backup service object
				backupService, err = getBackupServiceObj(k8sClient, backupServiceNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				By("Update Aerospike backup service port and node affinity")

				backupService.Spec.Config.Raw = []byte(`{"service":{"http":{"port":8080}}}`)

				affinity := &corev1.Affinity{}
				nodeSelector := &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      nodeLabelKey,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{nodeLabelValue},
								},
							},
						},
					},
				}

				nodeAffinity := &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
				}
				affinity.NodeAffinity = nodeAffinity
				backupService.Spec.PodSpec.Affinity = affinity

				err = updateBackupServiceWithTO(k8sClient, backupService, 1*time.Minute)
				Expect(err).To(HaveOccurred())

				By("Adding node labels to make infra available")

				err := test.SetNodeLabels(testCtx, k8sClient,
					map[string]string{
						nodeLabelKey: nodeLabelValue,
					})
				Expect(err).ToNot(HaveOccurred())

				err = waitForBackupService(k8sClient, backupService, timeout)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	},
)

func updateBackupServiceResources(k8sClient client.Client, backupServiceNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements) {
	backupService, err := getBackupServiceObj(k8sClient, backupServiceNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	backupService.Spec.PodSpec.ServiceContainerSpec.Resources = res
	err = updateBackupService(k8sClient, backupService)
	Expect(err).ToNot(HaveOccurred())

	err = validateBackupServiceResources(k8sClient, backupServiceNamespacedName, res)
	Expect(err).ToNot(HaveOccurred())
}

func validateBackupServiceResources(k8sClient client.Client, backupServiceNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements) error {
	deploy, err := getBackupServiceDeployment(k8sClient, backupServiceNamespacedName)
	if err != nil {
		return err
	}

	// res cannot be null in deploy spec
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}

	actual := deploy.Spec.Template.Spec.Containers[0].Resources
	if !reflect.DeepEqual(&actual, res) {
		return fmt.Errorf("resource not matching. want %v, got %v", *res, actual)
	}

	return nil
}

func updateBackupServiceSecurityContext(k8sClient client.Client, backupServiceNamespacedName types.NamespacedName,
	sc *corev1.SecurityContext) {
	backupService, err := getBackupServiceObj(k8sClient, backupServiceNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	backupService.Spec.PodSpec.ServiceContainerSpec.SecurityContext = sc
	err = updateBackupService(k8sClient, backupService)
	Expect(err).ToNot(HaveOccurred())

	err = validateBackupServiceSecurityContext(k8sClient, backupServiceNamespacedName, sc)
	Expect(err).ToNot(HaveOccurred())
}

func validateBackupServiceSecurityContext(k8sClient client.Client, backupServiceNamespacedName types.NamespacedName,
	sc *corev1.SecurityContext) error {
	deploy, err := getBackupServiceDeployment(k8sClient, backupServiceNamespacedName)
	if err != nil {
		return err
	}

	actual := deploy.Spec.Template.Spec.Containers[0].SecurityContext
	if !reflect.DeepEqual(actual, sc) {
		return fmt.Errorf("security context not matching")
	}

	return nil
}

func validatePodObjectMeta(annotations, labels map[string]string, backupServiceNamespacedName types.NamespacedName) {
	deploy, dErr := getBackupServiceDeployment(k8sClient, backupServiceNamespacedName)
	Expect(dErr).ToNot(HaveOccurred())

	By("Validating Annotations")

	actual := deploy.Spec.Template.Annotations
	valid := validateLabelsOrAnnotations(actual, annotations)
	Expect(valid).To(
		BeTrue(), "Annotations mismatch. expected %+v, found %+v", annotations, actual,
	)

	By("Validating Labels")

	actual = deploy.Spec.Template.Labels
	valid = validateLabelsOrAnnotations(actual, labels)
	Expect(valid).To(
		BeTrue(), "Labels mismatch. expected %+v, found %+v", labels, actual,
	)
}
