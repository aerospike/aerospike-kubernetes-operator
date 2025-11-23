package envtests

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
)

var _ = Describe("Pod eviction webhook", func() {
	var (
		ctx = context.Background()
	)

	const (
		podName = "test-evict-pod"
		testNs  = "default"
	)

	AfterEach(func() {
		By("cleaning up test pod")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNs}}

		_ = k8sClient.Delete(ctx, pod)

		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, &corev1.Pod{})
			return apierrors.IsNotFound(err)
		}, 10*time.Second, 500*time.Millisecond).Should(BeTrue())
	})

	Context("When webhook is disabled", func() {
		BeforeEach(func() {
			evictionWebhook.Enable = false
		})

		It("should allow eviction of a pod", func() {
			// Create a simple pod
			pod := getDummyPodWithAerospikeLabels(podName, testNs)

			By("creating a pod")
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait until pod is visible
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			By("creating eviction object")
			eviction := &policyv1.Eviction{
				ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testNs},
				DeleteOptions: &metav1.DeleteOptions{},
			}

			By("attempting eviction")
			err := clientSet.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
			Expect(err).NotTo(HaveOccurred())

			// Optionally verify Pod deletion
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, &corev1.Pod{})
				return apierrors.IsNotFound(err)
			}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
		})

	})

	Context("When webhook is enabled", func() {
		BeforeEach(func() {
			evictionWebhook.Enable = true
		})

		Context("Aerospike pods", func() {
			It("should block eviction of Aerospike pod", func() {
				evictionWebhook.Enable = true
				// Create a simple pod
				pod := getDummyPodWithAerospikeLabels(podName, testNs)

				By("creating a pod")
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				// Wait until pod is visible
				Eventually(func() error {
					return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
				}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

				By("creating eviction object")
				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testNs},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				By("attempting eviction")
				err := clientSet.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("blocked by admission webhook"))

				By("verifying annotation is set")
				Eventually(func() bool {
					_ = k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
					return pod.Annotations != nil && pod.Annotations[asdbv1.EvictionBlockedAnnotation] != ""
				}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
			})
		})

		Context("Non-Aerospike pods", func() {
			It("should allow eviction of pod without Aerospike labels", func() {
				pod := getDummyPodWithAerospikeLabels(podName, testNs)
				pod.Labels = map[string]string{
					"app": "nginx",
				}

				By("creating a non-Aerospike pod")
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				Eventually(func() error {
					return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
				}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

				By("attempting eviction")
				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: podName, Namespace: testNs},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				err := clientSet.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, &corev1.Pod{})
					return apierrors.IsNotFound(err)
				}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
			})
		})

		Context("Edge cases", func() {
			It("should allow eviction when pod does not exist", func() {
				nonExistentPodName := "non-existent-pod"

				By("attempting to evict non-existent pod")
				eviction := &policyv1.Eviction{
					ObjectMeta:    metav1.ObjectMeta{Name: nonExistentPodName, Namespace: testNs},
					DeleteOptions: &metav1.DeleteOptions{},
				}

				err := clientSet.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
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
