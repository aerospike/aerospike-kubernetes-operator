package cluster

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

const aerospikePath = "/opt/hostpath"

var _ = Describe(
	"DynamicRack", func() {
		ctx := goctx.TODO()
		clusterName := fmt.Sprintf("dynamic-rack-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

		Context(
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

						err = waitForDaemonSetPodsRunning(
							k8sClient, ctx, namespace, "hostpath-writer", retryInterval,
							time.Minute*2,
						)
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

								aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2, aerospikePath, "/tmp")
								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
								aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}

								Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

								aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
								Expect(err).ToNot(HaveOccurred())

								podObject := &v1.Pod{}
								Eventually(
									func() bool {
										err = k8sClient.Get(
											ctx, types.NamespacedName{
												Name:      aeroCluster.Name + "-1-0",
												Namespace: clusterNamespacedName.Namespace,
											}, podObject,
										)

										return isHostPathReadOnly(podObject.Status.ContainerStatuses, aerospikePath)
									}, time.Minute, time.Second,
								).Should(BeTrue())

								pod := aeroCluster.Status.Pods[aeroCluster.Name+"-1-0"]

								err = validateDynamicRackID(ctx, k8sClient, &pod, clusterNamespacedName, "2")
								Expect(err).ToNot(HaveOccurred())
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

								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
								hostPathVolume := asdbv1.VolumeSpec{
									Name: "hostpath",
									Source: asdbv1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{
											Path: "/tmp",
										},
									},
									Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
										Path: aerospikePath,
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

								err = validateDynamicRackID(ctx, k8sClient, &pod, clusterNamespacedName, "2")
								Expect(err).ToNot(HaveOccurred())

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

								err = validateDynamicRackID(ctx, k8sClient, &pod, clusterNamespacedName, "1")
								Expect(err).ToNot(HaveOccurred())

								By("Updating cluster by enabling dynamic rack")
								oldPodIDs, err = getPodIDs(ctx, aeroCluster)
								Expect(err).ToNot(HaveOccurred())

								aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
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

								err = validateDynamicRackID(ctx, k8sClient, &pod, clusterNamespacedName, "2")
								Expect(err).ToNot(HaveOccurred())
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
							aerospikePath, "/dev/null")
						aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, asdbv1.Rack{ID: 2})
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if RollingUpdateBatchSize is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							aerospikePath, "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
						aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if ScaleDownBatchSize is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							aerospikePath, "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
						aeroCluster.Spec.RackConfig.ScaleDownBatchSize = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if MaxIgnorablePods is given along with dynamic rack", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							aerospikePath, "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}
						aeroCluster.Spec.RackConfig.MaxIgnorablePods = &intstr.IntOrString{IntVal: 2}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source file path is not absolute", func() {
						aeroCluster := createDummyAerospikeClusterWithHostPathVolume(clusterNamespacedName, 2,
							aerospikePath, "/dev/null")
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source volume is not mounted", func() {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source volume is not hostpath type", func() {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						emptyDirVolume := asdbv1.VolumeSpec{
							Name: "empty",
							Source: asdbv1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: "/opt/empty",
							},
						}

						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, emptyDirVolume)
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: "/opt/empty/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)

				It(
					"Should fail if dynamic rackID source volume is mounted with readOnly false", func() {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						hostpathVolume := asdbv1.VolumeSpec{
							Name: "hostpath",
							Source: asdbv1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/dev/null",
								},
							},
							Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
								Path: aerospikePath,
								AttachmentOptions: asdbv1.AttachmentOptions{
									MountOptions: asdbv1.MountOptions{
										ReadOnly: ptr.To(false),
									},
								},
							},
						}

						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, hostpathVolume)
						aeroCluster.Spec.RackConfig.RackIDSource = &asdbv1.RackIDSource{FilePath: aerospikePath + "/rackid"}

						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
					},
				)
			},
		)
	},
)

func isHostPathReadOnly(containerStatuses []v1.ContainerStatus, mountPath string) bool {
	if len(containerStatuses) == 0 {
		return false
	}

	for idx := range containerStatuses[0].VolumeMounts {
		if containerStatuses[0].VolumeMounts[idx].MountPath == mountPath {
			return containerStatuses[0].VolumeMounts[idx].ReadOnly
		}
	}

	return false
}

func waitForDaemonSetPodsRunning(k8sClient client.Client, ctx goctx.Context, namespace, dsName string,
	retryInterval, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx,
		retryInterval, timeout, true, func(ctx goctx.Context) (done bool, err error) {
			ds := &appsv1.DaemonSet{}
			if err := k8sClient.Get(
				ctx, types.NamespacedName{
					Name: dsName, Namespace: namespace,
				}, ds,
			); err != nil {
				return false, err
			}

			// DaemonSet selector
			selector := labels.Set(ds.Spec.Selector.MatchLabels).AsSelector()

			podList := &v1.PodList{}
			listOps := &client.ListOptions{
				Namespace: namespace, LabelSelector: selector,
			}

			if err := k8sClient.List(goctx.TODO(), podList, listOps); err != nil {
				return false, err
			}

			var runningAndReady int32

			for idx := range podList.Items {
				if utils.IsPodRunningAndReady(&podList.Items[idx]) {
					runningAndReady++
				}
			}

			if runningAndReady == ds.Status.DesiredNumberScheduled {
				return true, nil
			}

			return false, nil
		},
	)
}

func validateDynamicRackID(
	ctx context.Context, k8sClient client.Client, pod *asdbv1.AerospikePodStatus, namespacedName types.NamespacedName,
	expectedValue string,
) error {
	info, err := requestInfoFromNode(logger, k8sClient, ctx, namespacedName, "namespace/test", pod)
	if err != nil {
		return err
	}

	confs := strings.Split(info["namespace/test"], ";")
	for _, conf := range confs {
		if strings.Contains(conf, "rack-id") {
			keyValue := strings.Split(conf, "=")
			if keyValue[1] != expectedValue {
				return fmt.Errorf("expected rack-id %s, but got %s", expectedValue, keyValue[1])
			}

			return nil
		}
	}

	return nil
}
