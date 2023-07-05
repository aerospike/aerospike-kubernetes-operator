package test

import (
	goctx "context"
	"fmt"
	"os"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var (
	customInitRegistryEnvVar  = "CUSTOM_INIT_REGISTRY"
	imagePullSecretNameEnvVar = "IMAGE_PULL_SECRET_NAME" //nolint:gosec // for testing
)

var _ = Describe(
	"PodSpec", func() {

		ctx := goctx.TODO()

		clusterName := "podspec"
		clusterNamespacedName := getNamespacedName(
			clusterName, namespace,
		)

		sidecar1 := corev1.Container{
			Name:  "nginx1",
			Image: "nginx:1.14.2",
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: 80,
				},
			},
		}

		sidecar2 := corev1.Container{
			Name:  "box",
			Image: "busybox:1.28",
			Command: []string{
				"sh", "-c", "echo The app is running! && sleep 3600",
			},
		}

		initCont1 := corev1.Container{
			Name:    "init-myservice",
			Image:   "busybox:1.28",
			Command: []string{"sh", "-c", "echo The app is running; sleep 2"},
		}

		// initCont2 := corev1.Container{
		// 	Name:    "init-mydb",
		// 	Image:   "busybox:1.28",
		// 	Command: []string{"sh", "-c",
		//	"until nslookup mydb.$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace).svc.cluster.local;
		//	do echo waiting for mydb; sleep 2; done"},
		// }

		Context(
			"When doing valid operation", func() {

				BeforeEach(
					func() {
						zones, err := getZones(ctx, k8sClient)
						Expect(err).ToNot(HaveOccurred())
						// Deploy everything in single rack
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						racks := []asdbv1.Rack{
							{ID: 1, Zone: zones[0]},
						}
						aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
							Racks: racks,
						}
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations = map[string]string{
							"annotation-test-1": "test-1"}
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = map[string]string{
							"label-test-1": "test-1"}
						err = deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)
				It("Should validate annotations and labels addition", func() {
					By("Validating Annotations")
					actual, err := getPodSpecAnnotations(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())
					valid := ValidateAttributes(actual,
						map[string]string{"annotation-test-1": "test-1"})
					Expect(valid).To(
						BeTrue(), "Unable to find annotations",
					)
					By("Validating Labels")
					actual, err = getPodSpecLabels(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())
					valid = ValidateAttributes(actual,
						map[string]string{"label-test-1": "test-1"})
					Expect(valid).To(
						BeTrue(), "Unable to find labels",
					)
				})

				It("Should validate added annotations and labels flow", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					zones, err := getZones(ctx, k8sClient)
					Expect(err).ToNot(HaveOccurred())
					zone := zones[0]
					if len(zones) > 1 {
						for i := 0; i < len(zones); i++ {
							if zones[i] != aeroCluster.Spec.RackConfig.Racks[0].Zone {
								zone = zones[i]
								break
							}
						}
					}
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations["annotation-test-2"] = "test-2"
					aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels["label-test-2"] = "test-2"
					err = addRack(
						k8sClient, ctx, clusterNamespacedName, &asdbv1.Rack{ID: 2, Zone: zone})
					Expect(err).ToNot(HaveOccurred())
					By("Validating Added Annotations")
					actual, err := getPodSpecAnnotations(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())
					valid := ValidateAttributes(actual,
						map[string]string{"annotation-test-1": "test-1", "annotation-test-2": "test-2"})
					Expect(valid).To(
						BeTrue(), "Unable to find annotations",
					)
					By("Validating Added Labels")
					actual, err = getPodSpecLabels(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())
					valid = ValidateAttributes(actual,
						map[string]string{"label-test-1": "test-1", "label-test-2": "test-2"})
					Expect(valid).To(
						BeTrue(), "Unable to find labels",
					)
				})

				It("Should validate the sidecar workflow", func() {

					By("Adding the container1")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.Sidecars = append(
						aeroCluster.Spec.PodSpec.Sidecars, sidecar1,
					)

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Adding the container2")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.Sidecars = append(
						aeroCluster.Spec.PodSpec.Sidecars, sidecar2,
					)

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Updating the container2")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.Sidecars[1].Command = []string{
						"sh", "-c", "sleep 3600",
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Removing all the containers")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
				)

				It("Should validate the initcontainer workflow", func() {

					By("Adding the container1")

					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.InitContainers = append(
						aeroCluster.Spec.PodSpec.InitContainers, initCont1,
					)

					aeroCluster.Spec.Storage.Volumes[1].InitContainers = []asdbv1.VolumeAttachment{
						{
							ContainerName: "init-myservice",
							Path:          "/workdir",
						},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// validate
					stsList, err := getSTSList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(stsList.Items)).ToNot(BeZero())

					for _, sts := range stsList.Items {
						stsInitMountPath := sts.Spec.Template.Spec.InitContainers[1].VolumeMounts[0].MountPath
						Expect(stsInitMountPath).To(Equal("/workdir"))
					}

					// By("Adding the container2")

					// aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					// Expect(err).ToNot(HaveOccurred())

					// aeroCluster.Spec.PodSpec.InitContainers = append(aeroCluster.Spec.PodSpec.InitContainers, initCont2)

					// err = updateCluster(k8sClient, ctx, aeroCluster)
					// Expect(err).ToNot(HaveOccurred())

					By("Updating the container2")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.InitContainers[0].Command = []string{
						"sh", "-c", "echo The app is running; sleep 5",
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Removing all the containers")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.InitContainers = []corev1.Container{}
					aeroCluster.Spec.Storage.Volumes[1].InitContainers = []asdbv1.VolumeAttachment{}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
				)

				// Test affinity
				// try deploying in specific hosts
				It("Should validate affinity", func() {
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					pods, err := getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					// All pods will be moved to this node
					nodeName := pods.Items[0].Spec.NodeName

					affinity := &corev1.Affinity{}
					ns := &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "kubernetes.io/hostname",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					}

					affinity.NodeAffinity = &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: ns,
					}
					aeroCluster.Spec.PodSpec.Affinity = affinity

					// All pods should move to node with nodeName
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// Verify if all the pods are moved to given node
					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					pods, err = getPodList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					for _, pod := range pods.Items {
						Expect(pod.Spec.NodeName).Should(Equal(nodeName))
					}
					// Test toleration
					// Test nodeSelector
				},
				)

				It("Should be able to update container image and other fields together", func() {

					By("Adding the container")
					aeroCluster, err := getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.Sidecars = append(
						aeroCluster.Spec.PodSpec.Sidecars, sidecar1,
					)

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Updating container image and affinity together")

					aeroCluster, err = getCluster(
						k8sClient, ctx, clusterNamespacedName,
					)
					Expect(err).ToNot(HaveOccurred())

					// Update image
					newImage := "nginx:1.21.4"
					aeroCluster.Spec.PodSpec.Sidecars[0].Image = newImage

					// Update affinity
					region, err := getRegion(ctx, k8sClient)
					Expect(err).ToNot(HaveOccurred())

					desiredAffinity := corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "topology.kubernetes.io/region",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{region},
											},
										},
									},
								},
							},
						},
					}
					aeroCluster.Spec.PodSpec.Affinity = &desiredAffinity

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// validate
					stsList, err := getSTSList(aeroCluster, k8sClient)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(stsList.Items)).ToNot(BeZero())

					var meFound bool
					for _, sts := range stsList.Items {
						actualNodeAffinity := sts.Spec.Template.Spec.Affinity.NodeAffinity
						for _, ns := range actualNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
							for _, me := range ns.MatchExpressions {
								if me.Key == "topology.kubernetes.io/region" {
									isEqual := reflect.DeepEqual(
										me,
										desiredAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.
											NodeSelectorTerms[0].MatchExpressions[0],
									)
									msg := fmt.Sprintf("node affinity actual: %v, desired: %v", actualNodeAffinity, desiredAffinity.NodeAffinity)
									Expect(isEqual).To(BeTrue(), msg)

									meFound = true
								}
							}
						}

						msg := fmt.Sprintf("node affinity actual: %v, desired: %v", actualNodeAffinity, desiredAffinity.NodeAffinity)
						Expect(meFound).To(BeTrue(), msg)

						// 1st is aerospike-server image, 2nd is 1st sidecare
						Expect(sts.Spec.Template.Spec.Containers[1].Image).To(Equal(newImage))
					}
				})

				It("Should be able to set/update aerospike-init custom registry", func() {
					operatorEnvVarRegistry := "docker.io"
					customRegistry := getEnvVar(customInitRegistryEnvVar)
					imagePullSecret := getEnvVar(imagePullSecretNameEnvVar)

					By("Updating imagePullSecret")
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.ImagePullSecrets = []corev1.LocalObjectReference{
						{
							Name: imagePullSecret,
						},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					By("Using registry in CR")
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistry = customRegistry
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					validateImageRegistry(k8sClient, ctx, aeroCluster, customRegistry)

					By("Using envVar registry")
					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					// Empty imageRegistry, should use operator envVar docker.io
					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistry = ""
					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					validateImageRegistry(k8sClient, ctx, aeroCluster, operatorEnvVarRegistry)
				})
			})
		Context(
			"When doing invalid operation", func() {
				It(
					"Should fail adding reserved label",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels = map[string]string{
							asdbv1.AerospikeAppLabel: "test"}

						err := k8sClient.Create(ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					})

				It(
					"Should fail for adding sidecar container with same name",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						aeroCluster.Spec.PodSpec.Sidecars = append(
							aeroCluster.Spec.PodSpec.Sidecars, sidecar1,
						)
						aeroCluster.Spec.PodSpec.Sidecars = append(
							aeroCluster.Spec.PodSpec.Sidecars, sidecar1,
						)

						err := k8sClient.Create(ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)

				It(
					"Should fail for adding initcontainer container with same name",
					func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						aeroCluster.Spec.PodSpec.InitContainers = append(
							aeroCluster.Spec.PodSpec.InitContainers, initCont1,
						)
						aeroCluster.Spec.PodSpec.InitContainers = append(
							aeroCluster.Spec.PodSpec.InitContainers, initCont1,
						)

						err := k8sClient.Create(ctx, aeroCluster)
						Expect(err).Should(HaveOccurred())
					},
				)
			},
		)

	},
)

func getEnvVar(envVar string) string {
	envVarVal, found := os.LookupEnv(envVar)
	Expect(found).To(BeTrue())

	return envVarVal
}

func validateImageRegistry(
	k8sClient client.Client, _ goctx.Context, aeroCluster *asdbv1.AerospikeCluster, registry string,
) {
	stsList, err := getSTSList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	for stsIndex := range stsList.Items {
		image := stsList.Items[stsIndex].Spec.Template.Spec.InitContainers[0].Image
		hasPrefix := strings.HasPrefix(image, registry)
		Expect(hasPrefix).To(BeTrue(), fmt.Sprintf("expected registry %s, found image %s", registry, image))
	}
}
