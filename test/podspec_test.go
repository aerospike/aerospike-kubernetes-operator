package test

import (
	goctx "context"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Using podspec feature", func() {

	ctx := goctx.TODO()

	clusterName := "podspec"
	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

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
		Name:    "box",
		Image:   "busybox:1.28",
		Command: []string{"sh", "-c", "echo The app is running! && sleep 3600"},
	}

	var aeroCluster *v1alpha1.AerospikeCluster

	Context("When doing valid operation", func() {

		BeforeEach(func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			deleteCluster(k8sClient, ctx, aeroCluster)
		})

		It("Should validate the workflow", func() {

			By("Deploying the cluster")

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Adding the container1")

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)

			err = updateAndWait(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Adding the container2")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar2)

			err = updateAndWait(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Updating the container2")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars[1].Command = []string{"sh", "-c", "sleep 3600"}

			err = updateAndWait(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Removing all the containers")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{}

			err = updateAndWait(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("When doing invalid operation", func() {

		It("Should fail for adding container with same name", func() {
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)

			err := k8sClient.Create(ctx, aeroCluster)
			Expect(err).Should(HaveOccurred())
		})
	})
})
