package eviction

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("Pod eviction webhook", func() {
	var (
		ctx = context.Background()
		pod = &corev1.Pod{}
	)

	const (
		podName = "test-evict-pod"
	)

	BeforeEach(func() {
		pod = getDummyPodWithAerospikeLabels(podName, testutil.DefaultNamespace)
	})

	AfterEach(func() {
		By("cleaning up test pod")

		_ = envtests.K8sClient.Delete(ctx, pod)

		Eventually(func() bool {
			err := envtests.K8sClient.Get(ctx, client.ObjectKey{
				Name:      podName,
				Namespace: testutil.DefaultNamespace}, &corev1.Pod{})

			return apierrors.IsNotFound(err)
		}, 10*time.Second, 500*time.Millisecond).Should(BeTrue())
	})

	Context("When webhook is disabled", func() {
		BeforeEach(func() {
			envtests.EvictionWebhook.Enable = false
		})

		It("should allow eviction of a pod", func() {
			By("creating a pod")
			Expect(envtests.K8sClient.Create(ctx, pod)).To(Succeed())

			// Wait until pod is visible
			Eventually(func() error {
				return envtests.K8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			By("creating eviction object")

			eviction := &policyv1.Eviction{
				ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testutil.DefaultNamespace},
				DeleteOptions: &metav1.DeleteOptions{},
			}

			By("attempting eviction")

			err := envtests.ClientSet.CoreV1().Pods(testutil.DefaultNamespace).EvictV1(ctx, eviction)
			Expect(err).NotTo(HaveOccurred())

			// Optionally verify Pod deletion
			Eventually(func() bool {
				err := envtests.K8sClient.Get(ctx,
					client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, &corev1.Pod{})

				return apierrors.IsNotFound(err)
			}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
		})
	})

	Context("When webhook is enabled", func() {
		BeforeEach(func() {
			envtests.EvictionWebhook.Enable = true
		})

		AfterEach(func() {
			envtests.EvictionWebhook.Enable = false
		})

		Context("Aerospike pods", func() {
			It("should block eviction of Aerospike pod", func() {
				By("creating a pod")
				Expect(envtests.K8sClient.Create(ctx, pod)).To(Succeed())

				// Wait until pod is visible
				Eventually(func() error {
					return envtests.K8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, pod)
				}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

				By("creating eviction object")

				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testutil.DefaultNamespace},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				By("attempting eviction")

				err := envtests.ClientSet.CoreV1().Pods(testutil.DefaultNamespace).EvictV1(ctx, eviction)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("blocked by admission webhook"))

				By("verifying if eviction-blocked annotation is set")
				Eventually(func() bool {
					_ = envtests.K8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, pod)
					return pod.Annotations != nil && pod.Annotations[asdbv1.EvictionBlockedAnnotation] != ""
				}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
			})
		})

		Context("Non-Aerospike pods", func() {
			It("should allow eviction of pod without Aerospike labels", func() {
				pod.Labels = map[string]string{
					"app": "nginx",
				}

				By("creating a non-Aerospike pod")
				Expect(envtests.K8sClient.Create(ctx, pod)).To(Succeed())

				Eventually(func() error {
					return envtests.K8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, pod)
				}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

				By("attempting eviction")

				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testutil.DefaultNamespace},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				err := envtests.ClientSet.CoreV1().Pods(testutil.DefaultNamespace).EvictV1(ctx, eviction)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := envtests.K8sClient.Get(ctx,
						client.ObjectKey{Name: podName, Namespace: testutil.DefaultNamespace}, &corev1.Pod{})

					return apierrors.IsNotFound(err)
				}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
			})
		})

		Context("Edge cases", func() {
			It("should allow eviction when pod does not exist", func() {
				nonExistentPodName := "non-existent-pod"

				By("attempting to evict non-existent pod")

				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: nonExistentPodName, Namespace: testutil.DefaultNamespace},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				err := envtests.ClientSet.CoreV1().Pods(testutil.DefaultNamespace).EvictV1(ctx, eviction)
				// Should not be blocked by webhook (let API server handle NotFound)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not found"))
			})
		})
	})
})

func getDummyPodWithAerospikeLabels(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				asdbv1.AerospikeAppLabel:            asdbv1.AerospikeAppLabelValue,
				asdbv1.AerospikeCustomResourceLabel: "aerocluster",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "aerocluster-1",
				Image: "k8s.gcr.io/pause:3.9",
			}},
		},
	}
}
