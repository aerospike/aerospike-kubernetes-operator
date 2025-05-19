package cluster

import (
	goctx "context"
	"fmt"
	"strings"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = FDescribe(
	"DynamicRack", func() {
		ctx := goctx.TODO()
		clusterName := fmt.Sprintf("dynamic-rack-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)
		FContext(
			"When doing valid operations", Ordered, func() {
				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterName,
								Namespace: namespace,
							},
						}

						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
					},
				)

				BeforeAll(
					func() {
						dir := v1.HostPathDirectory
						ds := &appsv1.DaemonSet{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "hostpath-writer",
								Namespace: namespace,
								Labels: map[string]string{
									"app": "hostpath-writer",
								},
							},
							Spec: appsv1.DaemonSetSpec{
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "hostpath-writer"},
								},
								Template: v1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Labels: map[string]string{"app": "hostpath-writer"}, // must match selector
									},
									Spec: v1.PodSpec{
										Containers: []v1.Container{
											{
												Name:    "writer",
												Image:   "busybox",
												Command: []string{"sh", "-c", `echo "2" > /tmp/rackid && sleep 3600`},
												VolumeMounts: []v1.VolumeMount{
													{
														Name:      "tmp-mount",
														MountPath: "/tmp",
													},
												},
												Lifecycle: &v1.Lifecycle{
													PreStop: &v1.LifecycleHandler{
														Exec: &v1.ExecAction{
															Command: []string{
																"/bin/sh", "-c", "rm /tmp/rackid; sleep 5",
															},
														},
													},
												},
											},
										},
										Volumes: []v1.Volume{
											{
												Name: "tmp-mount",
												VolumeSource: v1.VolumeSource{
													HostPath: &v1.HostPathVolumeSource{
														Path: "/tmp",
														Type: &dir,
													},
												},
											},
										},
									},
								},
							},
						}

						err := k8sClient.Create(ctx, ds)
						Expect(err).NotTo(HaveOccurred())
					},
				)

				AfterAll(
					func() {
						ds := &appsv1.DaemonSet{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "hostpath-writer",
								Namespace: namespace,
							},
						}

						err := k8sClient.Delete(ctx, ds)
						Expect(err).NotTo(HaveOccurred())
					})

				Context(
					"When deploying cluster", func() {
						It(
							"Should deploy with dynamic rack", func() {
								aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2, "/opt/hostpath", "/tmp")
								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
								aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								pod := aeroCluster.Status.Pods[aeroCluster.Name+"-1-0"]

								info, err := requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", &pod)
								Expect(err).ToNot(HaveOccurred())

								confs := strings.Split(info["namespace/test"], ";")
								for _, conf := range confs {
									if strings.Contains(conf, "rack-id") {
										keyValue := strings.Split(conf, "=")
										Expect(keyValue[1]).To(Equal("2"))
									}
								}
							},
						)
					},
				)

				Context(
					"When updating existing cluster", func() {
						It(
							"Should deploy with dynamic rack", func() {
								By("Deploying cluster with single rack")
								aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
								aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								oldPodIDs, err := getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								By("Updating cluster with dynamic rack and add hostpath volume")

								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
								hostPathVolume := asdbv1.VolumeSpec{
									Name: "hostpath",
									Source: asdbv1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{
											Path: "/tmp",
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: "/opt/hostpath",
									},
								}

								aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, hostPathVolume)

								Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								operationTypeMap := map[string]asdbv1.OperationKind{
									aeroCluster.Name + "-1-0": asdbv1.OperationPodRestart,
									aeroCluster.Name + "-1-1": asdbv1.OperationPodRestart,
								}

								aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
								Expect(err).ToNot(HaveOccurred())

								pod := aeroCluster.Status.Pods[aeroCluster.Name+"-1-0"]

								info, err := requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", &pod)
								Expect(err).ToNot(HaveOccurred())

								confs := strings.Split(info["namespace/test"], ";")
								for _, conf := range confs {
									if strings.Contains(conf, "rack-id") {
										keyValue := strings.Split(conf, "=")
										Expect(keyValue[1]).To(Equal("2"))
									}
								}

								By("Updating cluster by disabling dynamic rack")
								oldPodIDs, err = getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig.RackIDSource = nil
								Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								operationTypeMap = map[string]asdbv1.OperationKind{
									aeroCluster.Name + "-1-0": asdbv1.OperationWarmRestart,
									aeroCluster.Name + "-1-1": asdbv1.OperationWarmRestart,
								}

								aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
								Expect(err).ToNot(HaveOccurred())

								pod = aeroCluster.Status.Pods[aeroCluster.Name+"-1-0"]

								info, err = requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", &pod)
								Expect(err).ToNot(HaveOccurred())

								confs = strings.Split(info["namespace/test"], ";")
								for _, conf := range confs {
									if strings.Contains(conf, "rack-id") {
										keyValue := strings.Split(conf, "=")
										Expect(keyValue[1]).To(Equal("1"))
									}
								}

								By("Updating cluster by enabling dynamic rack")
								oldPodIDs, err = getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
								Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								operationTypeMap = map[string]asdbv1.OperationKind{
									aeroCluster.Name + "-1-0": asdbv1.OperationWarmRestart,
									aeroCluster.Name + "-1-1": asdbv1.OperationWarmRestart,
								}

								aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								err = validateOperationTypes(ctx, aeroCluster, oldPodIDs, operationTypeMap)
								Expect(err).ToNot(HaveOccurred())

								pod = aeroCluster.Status.Pods[aeroCluster.Name+"-1-0"]

								info, err = requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", &pod)
								Expect(err).ToNot(HaveOccurred())

								confs = strings.Split(info["namespace/test"], ";")
								for _, conf := range confs {
									if strings.Contains(conf, "rack-id") {
										keyValue := strings.Split(conf, "=")
										Expect(keyValue[1]).To(Equal("2"))
									}
								}
							},
						)
					},
				)
			},
		)
		Context(
			"When doing invalid operations", func() {

				It(
					"Should fail if multiple racks are given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							"/opt/hostpath", "/dev/null")
						aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, asdbv1.Rack{ID: 2})
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if RollingUpdateBatchSize is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							"/opt/hostpath", "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
						aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if ScaleDownBatchSize is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							"/opt/hostpath", "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
						aeroCluster.Spec.RackConfig.ScaleDownBatchSize = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if MaxIgnorablePods is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							"/opt/hostpath", "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}
						aeroCluster.Spec.RackConfig.MaxIgnorablePods = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source file path is not absolute", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							"/opt/hostpath", "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source volume is not mounted", func() {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/hostpath/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source volume is not hostpath type", func() {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						emptydirVolume := asdbv1.VolumeSpec{
							Name: "empty",
							Source: asdbv1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/opt/empty",
							},
						}

						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, emptydirVolume)
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/empty/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)
			},
		)
	},
)
