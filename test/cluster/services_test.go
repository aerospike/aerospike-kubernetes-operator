package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
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
		aeroCluster := &asdbv1.AerospikeCluster{}
		AfterEach(
			func() {
				Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
			},
		)
		It(
			"Validate create LB", func() {
				By("DeployCluster with LB")
				clusterNamespacedName := test.GetNamespacedName(
					"load-balancer-create", namespace,
				)
				aeroCluster = createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validate LB service exists")
				validateLoadBalancerExists(aeroCluster)
			},
		)

		It(
			"Validate update LB", func() {
				By("DeployCluster")
				clusterNamespacedName := test.GetNamespacedName(
					"load-balancer-invalid", namespace,
				)
				aeroCluster = createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("UpdateCluster with LB")

				aeroCluster.Spec.SeedsFinderServices.LoadBalancer.ExternalTrafficPolicy = "Cluster"
				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			},
		)

		It(
			"Validate create LB in an existing cluster", func() {
				By("DeployCluster without LB")
				clusterNamespacedName := test.GetNamespacedName(
					"load-balancer-update", namespace,
				)
				aeroCluster = createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = nil
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				service := &corev1.Service{}
				err := k8sClient.Get(
					goctx.TODO(), loadBalancerName(aeroCluster), service,
				)
				Expect(err).To(HaveOccurred())

				By("UpdateCluster with LB")

				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate LB service exists")
				validateLoadBalancerExists(aeroCluster)
			},
		)

		It(
			"Validate delete LB", func() {
				By("DeployCluster with LB")
				clusterNamespacedName := test.GetNamespacedName(
					"load-balancer-delete", namespace,
				)
				aeroCluster = createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = createLoadBalancer()
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validate LB service exists")
				validateLoadBalancerExists(aeroCluster)

				By("Delete LB from the CR")
				aeroCluster.Spec.SeedsFinderServices.LoadBalancer = nil
				err := updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate LB service is deleted")
				validateLoadBalancerSvcDeleted(aeroCluster)
			},
		)

		It(
			"Validate create headless service with default metadata", func() {
				By("Deploying cluster with headless service")
				clusterNamespacedName := test.GetNamespacedName("headless-service-create", namespace)
				aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating headless service exists with correct metadata")
				svc := &corev1.Service{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				}, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.GetAnnotations()["service.alpha.kubernetes.io/tolerate-unready-endpoints"]).To(Equal("true"))
				Expect(svc.GetLabels()[asdbv1.AerospikeCustomResourceLabel]).To(Equal(clusterNamespacedName.Name))
				Expect(svc.GetLabels()[asdbv1.AerospikeAppLabel]).To(Equal(asdbv1.AerospikeAppLabelValue))
			})

		It(
			"Validate create and update headless service with correct metadata", func() {
				By("Deploying cluster with headless service")
				clusterNamespacedName := test.GetNamespacedName("headless-service-update", namespace)
				aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.HeadlessService.Metadata.Annotations = map[string]string{
					"test-annotation": "test-annotation-value",
				}
				aeroCluster.Spec.HeadlessService.Metadata.Labels = map[string]string{
					"test-label": "test-label-value",
				}

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating headless service exists with correct metadata")
				svc := &corev1.Service{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				}, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.GetAnnotations()["test-annotation"]).To(Equal("test-annotation-value"))
				Expect(svc.GetAnnotations()["service.alpha.kubernetes.io/tolerate-unready-endpoints"]).To(Equal("true"))
				Expect(svc.GetLabels()["test-label"]).To(Equal("test-label-value"))
				Expect(svc.GetLabels()[asdbv1.AerospikeCustomResourceLabel]).To(Equal(clusterNamespacedName.Name))
				Expect(svc.GetLabels()[asdbv1.AerospikeAppLabel]).To(Equal(asdbv1.AerospikeAppLabelValue))

				By("Updating headless service metadata")
				aeroCluster.Spec.HeadlessService.Metadata.Annotations["new-annotation"] = "new-annotation-value"
				aeroCluster.Spec.HeadlessService.Metadata.Labels["new-label"] = "new-label-value"
				delete(aeroCluster.Spec.HeadlessService.Metadata.Annotations, "test-annotation")
				delete(aeroCluster.Spec.HeadlessService.Metadata.Labels, "test-label")

				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating headless service metadata was updated")
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				}, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(svc.GetAnnotations()).ToNot(HaveKey("test-annotation"))
				Expect(svc.GetLabels()).ToNot(HaveKey("test-label"))
				Expect(svc.GetAnnotations()["new-annotation"]).To(Equal("new-annotation-value"))
				Expect(svc.GetAnnotations()["service.alpha.kubernetes.io/tolerate-unready-endpoints"]).To(Equal("true"))
				Expect(svc.GetLabels()["new-label"]).To(Equal("new-label-value"))
				Expect(svc.GetLabels()[asdbv1.AerospikeCustomResourceLabel]).To(Equal(clusterNamespacedName.Name))
				Expect(svc.GetLabels()[asdbv1.AerospikeAppLabel]).To(Equal(asdbv1.AerospikeAppLabelValue))
			})

		It(
			"Validate create and update pod service with correct metadata", func() {
				By("Deploying cluster with pod service")
				clusterNamespacedName := test.GetNamespacedName("pod-service-test", namespace)
				aeroCluster = createDummyAerospikeCluster(clusterNamespacedName, 2)

				aeroCluster.Spec.PodService.Metadata.Annotations = map[string]string{
					"test-annotation": "test-annotation-value",
				}
				aeroCluster.Spec.PodService.Metadata.Labels = map[string]string{
					"test-label": "test-label-value",
				}

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating pod service exists with correct metadata")
				for i := 0; i < 2; i++ {
					svc := &corev1.Service{}
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf("%s-0-%d", clusterNamespacedName.Name, i),
						Namespace: clusterNamespacedName.Namespace,
					}, svc)
					Expect(err).ToNot(HaveOccurred())
					Expect(svc.GetAnnotations()["test-annotation"]).To(Equal("test-annotation-value"))
					Expect(svc.GetLabels()["test-label"]).To(Equal("test-label-value"))
				}

				By("Updating pod service metadata")
				aeroCluster.Spec.PodService.Metadata.Annotations["new-annotation"] = "new-annotation-value"
				aeroCluster.Spec.PodService.Metadata.Labels["new-label"] = "new-label-value"
				delete(aeroCluster.Spec.PodService.Metadata.Annotations, "test-annotation")
				delete(aeroCluster.Spec.PodService.Metadata.Labels, "test-label")

				Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				By("Validating pod service metadata was updated")
				for i := 0; i < 2; i++ {
					svc := &corev1.Service{}
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      fmt.Sprintf("%s-0-%d", clusterNamespacedName.Name, i),
						Namespace: clusterNamespacedName.Namespace,
					}, svc)
					Expect(err).ToNot(HaveOccurred())
					Expect(svc.GetAnnotations()).ToNot(HaveKey("test-annotation"))
					Expect(svc.GetLabels()).ToNot(HaveKey("test-label"))
					Expect(svc.GetAnnotations()["new-annotation"]).To(Equal("new-annotation-value"))
					Expect(svc.GetLabels()["new-label"]).To(Equal("new-label-value"))
				}
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

func validateLoadBalancerSvcDeleted(aeroCluster *asdbv1.AerospikeCluster) {
	Eventually(func() error {
		service := &corev1.Service{}

		err := k8sClient.Get(goctx.TODO(), loadBalancerName(aeroCluster), service)
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("service still exists: %v", err)
	}, 5*time.Minute, 10*time.Second).Should(Succeed())
}
