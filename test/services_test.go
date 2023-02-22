package test

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	lib "github.com/aerospike/aerospike-management-lib"
)

var (
	loadBalancersPerCloud = map[CloudProvider]asdbv1beta1.LoadBalancerSpec{
		CloudProviderGCP: {
			Annotations: map[string]string{
				"cloud.google.com/load-balancer-type": "Internal",
			},
		},
		CloudProviderAWS: {
			Annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-type":                              "nlb",
				"service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled": "true",
				"service.beta.kubernetes.io/aws-load-balancer-internal":                          "true",
				"service.beta.kubernetes.io/aws-load-balancer-backend-protocol":                  "(ssl|tcp)",
			},
			LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
		},
	}
)

var _ = Describe(
	"ClusterService", func() {
		ctx := goctx.TODO()
		It(
			"Validate LB created", func() {
				By("DeployCluster with LB")
				clusterNamespacedName := getClusterNamespacedName(
					"load-balancer-create", namespace,
				)
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate")
				validateLoadBalancerExists(aeroCluster)

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It(
			"Validate LB can not be updated", func() {
				By("DeployCluster")
				clusterNamespacedName := getClusterNamespacedName(
					"load-balancer-invalid", namespace,
				)
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("UpdateCluster with LB")
				aeroCluster, err = getCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer.ExternalTrafficPolicy = "Cluster"
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It(
			"Validate LB created in existing cluster", func() {
				By("DeployCluster without LB")
				clusterNamespacedName := getClusterNamespacedName(
					"load-balancer-update", namespace,
				)
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = nil
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				service := &corev1.Service{}
				err = k8sClient.Get(
					goctx.TODO(), loadBalancerName(aeroCluster), service,
				)
				Expect(err).To(HaveOccurred())

				By("UpdateCluster with LB")
				aeroCluster, err = getCluster(
					k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate")
				validateLoadBalancerExists(aeroCluster)

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)
	},
)

func createLoadBalancer() *asdbv1beta1.LoadBalancerSpec {
	lb, validCloud := loadBalancersPerCloud[cloudProvider]
	Expect(validCloud).To(
		BeTrue(), fmt.Sprintf(
			"Can't find LoadBalancer specification for cloud provider \"%d\"",
			cloudProvider,
		),
	)

	result := &asdbv1beta1.LoadBalancerSpec{}
	lib.DeepCopy(result, lb)

	return result
}

func loadBalancerName(aeroCluster *asdbv1beta1.AerospikeCluster) types.NamespacedName {
	return types.NamespacedName{
		Name: aeroCluster.Name + "-lb", Namespace: aeroCluster.Namespace,
	}
}

func validateLoadBalancerExists(aeroCluster *asdbv1beta1.AerospikeCluster) {
	service := &corev1.Service{}
	err := k8sClient.Get(goctx.TODO(), loadBalancerName(aeroCluster), service)
	Expect(err).ToNot(HaveOccurred())
	Expect(service.Spec.Type).To(BeEquivalentTo("LoadBalancer"))
}
