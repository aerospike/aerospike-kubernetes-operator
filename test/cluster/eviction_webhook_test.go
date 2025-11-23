package cluster

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"PodEviction", func() {

		ctx := goctx.Background()
		clusterName := fmt.Sprintf("pod-eviction-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

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

		Context(
			"When doing valid operations", func() {
				Context("Aerospike pods", func() {
					BeforeEach(
						func() {
							By("Creating Aerospike cluster")
							// Create a 2 node cluster
							aeroCluster := createDummyAerospikeCluster(
								clusterNamespacedName, 2,
							)
							aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
							Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						},
					)

					It(
						"Should handle pod eviction and validate cold restart with annotation tracking", func() {
							aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
							Expect(err).ToNot(HaveOccurred())

							podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							firstPod := &podList.Items[0]
							firstOriginalPodUID := string(firstPod.UID)

							secondPod := &podList.Items[1]
							secondOriginalPodUID := string(secondPod.UID)

							By("Triggering pod eviction")
							err = evictPod(ctx, firstPod)
							Expect(err).To(HaveOccurred())
							Expect(err.Error()).To(ContainSubstring(
								"Eviction of Aerospike pod %s/%s is blocked by admission webhook", firstPod.Namespace, firstPod.Name))

							err = evictPod(ctx, secondPod)
							Expect(err).To(HaveOccurred())
							Expect(err.Error()).To(ContainSubstring(
								"Eviction of Aerospike pod %s/%s is blocked by admission webhook", secondPod.Namespace, secondPod.Name))

							By("Verifying pod gets annotation for restart tracking")
							podList, err = getClusterPodList(k8sClient, ctx, aeroCluster)
							Expect(err).ToNot(HaveOccurred())

							for i := range podList.Items {
								pod := &podList.Items[i]
								Expect(pod.Annotations).NotTo(BeNil())
								Expect(pod.Annotations[asdbv1.EvictionBlockedAnnotation]).NotTo(BeEmpty())
							}

							By("Waiting for pod to restart and become ready")
							err = waitForPodRestart(ctx, firstPod.Name, firstPod.Namespace, firstOriginalPodUID)
							Expect(err).ToNot(HaveOccurred())

							err = waitForPodRestart(ctx, secondPod.Name, secondPod.Namespace, secondOriginalPodUID)
							Expect(err).ToNot(HaveOccurred())

							By("Verifying cluster is healthy after restart")
							err = waitForAerospikeCluster(
								k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size),
								retryInterval, getTimeout(aeroCluster.Spec.Size),
								[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
							)
							Expect(err).ToNot(HaveOccurred())

							validateClusterPVCDeletion(ctx, oldPvcInfoPerPod)
						},
					)
				})

				Context("Non-Aerospike pods", func() {
					It(
						"Should successfully evict nginx pod without admission webhook blocking", func() {
							By("Creating nginx sts")
							nginxNamespacedName := test.GetNamespacedName(
								strings.ToLower("nginx"), namespace,
							)
							sts := createNginxStatefulSet(nginxNamespacedName, 1)
							Expect(k8sClient.Create(ctx, sts)).ToNot(HaveOccurred())

							defer func() {
								err := k8sClient.Delete(ctx, sts)
								Expect(err).ToNot(HaveOccurred(), "failed to clean up StatefulSet")
							}()

							podName := "nginx-0"
							started := waitForPod(types.NamespacedName{Namespace: namespace, Name: podName})
							Expect(started).To(BeTrue(), "Nginx pod did not start in time")

							By("Getting nginx pod")
							pod := &corev1.Pod{}
							err := k8sClient.Get(ctx, test.GetNamespacedName(podName, namespace), pod)
							Expect(err).ToNot(HaveOccurred(), "failed to get nginx pod")
							originalPodUID := string(pod.UID)

							By("Evicting nginx pod (should succeed)")
							err = evictPod(ctx, pod)
							Expect(err).ToNot(HaveOccurred())

							By("Waiting for nginx pod to restart")
							err = waitForPodRestart(ctx, pod.Name, pod.Namespace, originalPodUID)
							Expect(err).ToNot(HaveOccurred())
						},
					)
				})
			},
		)
	},
)

func evictPod(ctx goctx.Context, pod *corev1.Pod) error {
	// Evict the pod using Kubernetes Eviction API
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}

	return k8sClientSet.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
}

func waitForPodRestart(ctx goctx.Context, podName, namespace, originalPodUID string) error {
	return wait.PollUntilContextTimeout(ctx,
		5*time.Second, 5*time.Minute, true, func(goctx.Context) (done bool, err error) {
			pod := &corev1.Pod{}

			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      podName,
				Namespace: namespace,
			}, pod)
			if err != nil {
				return false, err
			}

			// Check if pod has restarted (new UID) and is ready
			if string(pod.UID) != originalPodUID && utils.IsPodRunningAndReady(pod) {
				return true, nil
			}

			return false, nil
		},
	)
}

func createNginxStatefulSet(namespacedName types.NamespacedName, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			Labels: map[string]string{
				"app": "nginx-sts-test",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx-sts-test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx-sts-test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.21",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
									Name:          "http",
								},
							},
						},
					},
				},
			},
		},
	}
}
