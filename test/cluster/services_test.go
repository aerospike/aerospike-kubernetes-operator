package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
)

var (
	loadBalancersPerCloud = map[CloudProvider]asdbv1.LoadBalancerSpec{
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
				clusterNamespacedName := getNamespacedName(
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
			"Validate LB can be updated", func() {
				By("DeployCluster")
				clusterNamespacedName := getNamespacedName(
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
				Expect(err).ToNot(HaveOccurred())

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It(
			"Validate LB created in existing cluster", func() {
				By("DeployCluster without LB")
				clusterNamespacedName := getNamespacedName(
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

		It("Validate headless service is created and updated with correct metadata", func() {
			By("Deploying cluster with headless service")
			clusterNamespacedName := getNamespacedName("headless-service-test", namespace)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			// Add annotations and labels to headless service
			aeroCluster.Spec.HeadlessService = asdbv1.ServiceSpec{
				Metadata: asdbv1.AerospikeObjectMeta{
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
			}

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validating headless service exists with correct metadata")
			svc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      clusterNamespacedName.Name,
				Namespace: clusterNamespacedName.Namespace,
			}, svc)
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.Annotations["test-annotation"]).To(Equal("test-value"))
			Expect(svc.Annotations["service.alpha.kubernetes.io/tolerate-unready-endpoints"]).To(Equal("true"))
			Expect(svc.Labels["test-label"]).To(Equal("test-value"))
			Expect(svc.Labels["aerospike.com/cr"]).To(Equal("headless-service-test"))
			Expect(svc.Labels["app"]).To(Equal("aerospike-cluster"))

			By("Updating headless service metadata")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.HeadlessService.Metadata.Annotations["new-annotation"] = "new-annotation-value"
			aeroCluster.Spec.HeadlessService.Metadata.Labels["new-label"] = "new-label-value"
			delete(aeroCluster.Spec.HeadlessService.Metadata.Annotations, "test-annotation")
			delete(aeroCluster.Spec.HeadlessService.Metadata.Labels, "test-label")

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validating headless service metadata was updated")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      clusterNamespacedName.Name,
				Namespace: clusterNamespacedName.Namespace,
			}, svc)
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.Annotations).ToNot(HaveKey("test-annotation"))
			Expect(svc.Labels).ToNot(HaveKey("test-label"))
			Expect(svc.Annotations["new-annotation"]).To(Equal("new-annotation-value"))
			Expect(svc.Annotations["service.alpha.kubernetes.io/tolerate-unready-endpoints"]).To(Equal("true"))
			Expect(svc.Labels["new-label"]).To(Equal("new-label-value"))
			Expect(svc.Labels["aerospike.com/cr"]).To(Equal("headless-service-test"))
			Expect(svc.Labels["app"]).To(Equal("aerospike-cluster"))

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Validate pod service is created and updated with correct metadata", func() {
			By("Deploying cluster with pod service")
			clusterNamespacedName := getNamespacedName("pod-service-test", namespace)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			// Add annotations and labels to pod service
			aeroCluster.Spec.PodService = asdbv1.ServiceSpec{
				Metadata: asdbv1.AerospikeObjectMeta{
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
			}

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validating pod service exists with correct metadata")
			for i := 0; i < 3; i++ {
				svc := &corev1.Service{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-0-%d", clusterNamespacedName.Name, i),
					Namespace: clusterNamespacedName.Namespace,
				}, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.Annotations["test-annotation"]).To(Equal("test-value"))
				Expect(svc.Labels["test-label"]).To(Equal("test-value"))
			}

			By("Updating pod service metadata")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodService.Metadata.Annotations["new-annotation"] = "new-annotation-value"
			aeroCluster.Spec.PodService.Metadata.Labels["new-label"] = "new-label-value"
			delete(aeroCluster.Spec.PodService.Metadata.Annotations, "test-annotation")
			delete(aeroCluster.Spec.PodService.Metadata.Labels, "test-label")

			// Pod service is not updated on cluster update, only when pods are scaled or restarted
			err = rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			By("Validating pod service metadata was updated")
			for i := 0; i < 3; i++ {
				svc := &corev1.Service{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-0-%d", clusterNamespacedName.Name, i),
					Namespace: clusterNamespacedName.Namespace,
				}, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.Annotations).ToNot(HaveKey("test-annotation"))
				Expect(svc.Labels).ToNot(HaveKey("test-label"))
				Expect(svc.Annotations["new-annotation"]).To(Equal("new-annotation-value"))
				Expect(svc.Labels["new-label"]).To(Equal("new-label-value"))
			}

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	},
)

func createLoadBalancer() *asdbv1.LoadBalancerSpec {
	lb, validCloud := loadBalancersPerCloud[cloudProvider]
	Expect(validCloud).To(
		BeTrue(), fmt.Sprintf(
			"Can't find LoadBalancer specification for cloud provider \"%d\"",
			cloudProvider,
		),
	)

	return lib.DeepCopy(&lb).(*asdbv1.LoadBalancerSpec)
}

func loadBalancerName(aeroCluster *asdbv1.AerospikeCluster) types.NamespacedName {
	return types.NamespacedName{
		Name: aeroCluster.Name + "-lb", Namespace: aeroCluster.Namespace,
	}
}

func validateLoadBalancerExists(aeroCluster *asdbv1.AerospikeCluster) {
	service := &corev1.Service{}
	err := k8sClient.Get(goctx.TODO(), loadBalancerName(aeroCluster), service)
	Expect(err).ToNot(HaveOccurred())
	Expect(service.Spec.Type).To(BeEquivalentTo("LoadBalancer"))
}
