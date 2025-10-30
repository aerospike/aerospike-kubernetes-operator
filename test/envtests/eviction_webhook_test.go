package envtests

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

var _ = Describe("Pod eviction webhook", func() {
	var (
		ctx       context.Context
		clientset *kubernetes.Clientset
	)

	const (
		podName = "test-evict-pod"
		testNs  = "default"
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should allow eviction of a pod", func() {
		os.Setenv("WATCH_NAMESPACE", "default")
		// Create a simple pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: testNs,
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

		By("creating a pod")
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// Wait until pod is visible
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
		}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

		By("creating eviction object")
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: testNs,
			},
			DeleteOptions: &metav1.DeleteOptions{},
		}

		By("calling eviction API")
		err := clientset.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
		Expect(err).NotTo(HaveOccurred())

		// Optionally verify Pod deletion
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, &corev1.Pod{})
			return client.IgnoreNotFound(err) == nil
		}, 5*time.Second, 500*time.Millisecond).Should(BeTrue())
	})

	It("should not allow eviction of a pod", func() {
		os.Setenv("WATCH_NAMESPACE", "default")
		os.Setenv("ENABLE_SAFE_POD_EVICTION", "true")
		// Create a simple pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: testNs,
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

		By("creating a pod")
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// Wait until pod is visible
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: testNs}, pod)
		}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

		By("creating eviction object")
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: testNs,
			},
			DeleteOptions: &metav1.DeleteOptions{},
		}

		By("calling eviction API")
		err := clientset.CoreV1().Pods(testNs).EvictV1(ctx, eviction)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("blocked by admission webhook"))
	})
})
