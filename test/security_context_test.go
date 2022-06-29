package test

import (
	goctx "context"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe(
	"SecurityContext", func() {
		ctx := goctx.TODO()
		It(
			"Validate SecurityContext applied", func() {
				By("DeployCluster with SecurityContext")
				clusterNamespacedName := getClusterNamespacedName(
					"security-context-create", namespace,
				)
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate")
				validateSecurityContext(aeroCluster)

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It(
			"Validate SecurityContext updated", func() {
				By("DeployCluster")
				clusterNamespacedName := getClusterNamespacedName(
					"security-context-updated", namespace,
				)
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("UpdateCluster with SecurityContext")
				aeroCluster, err = getCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate")
				validateSecurityContext(aeroCluster)

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)
	},
)

func validateSecurityContext(aeroCluster *asdbv1beta1.AerospikeCluster) {
	pods, err := getPodsList(
		k8sClient, goctx.TODO(),
		getClusterNamespacedName(aeroCluster.Name, aeroCluster.Namespace),
	)
	Expect(err).ToNot(HaveOccurred())
	Expect(pods.Items).ToNot(BeEmpty())
	for _, pod := range pods.Items {
		// TODO: get pod.Spec container by name.
		Expect(pod.Spec.Containers[0].SecurityContext).To(Equal(aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext))
	}
}
