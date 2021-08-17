package test

import (
	goctx "context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ClusterResource", func() {

	ctx := goctx.TODO()

	Context("When doing valid operations", func() {

		clusterName := "cl-resource-lifecycle"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

		It("Try ClusterWithResourceLifecycle", func() {

			By("DeployClusterWithResource")

			// It should be greater than given in cluster namespace
			mem := resource.MustParse("2Gi")
			cpu := resource.MustParse("200m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			}
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateResource(k8sClient, ctx, aeroCluster)

			By("UpdateClusterWithResource")

			aeroCluster = &asdbv1beta1.AerospikeCluster{}
			err = k8sClient.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			// It should be greater than given in cluster namespace
			mem = resource.MustParse("1Gi")
			cpu = resource.MustParse("250m")
			aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			}
			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateResource(k8sClient, ctx, aeroCluster)

			deleteCluster(k8sClient, ctx, aeroCluster)
		})

	})

	Context("When doing invalid operations", func() {

		Context("Deploy", func() {
			clusterName := "cluster-resource-invalid"
			clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			It("NoResourceRequest: should fail for nil resource.request", func() {
				aeroCluster.Spec.Resources = &corev1.ResourceRequirements{}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})

			It("DeployClusterWithResource: should fail for request exceeding limit", func() {
				// It should be greater than given in cluster namespace
				resourceMem := resource.MustParse("3Gi")
				resourceCPU := resource.MustParse("250m")
				limitMem := resource.MustParse("2Gi")
				limitCPU := resource.MustParse("200m")
				aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resourceCPU,
						corev1.ResourceMemory: resourceMem,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    limitCPU,
						corev1.ResourceMemory: limitMem,
					},
				}

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())
			})
		})

		Context("Update", func() {
			clusterName := "cl-resource-insuff"
			clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)

			It("UpdateClusterWithResource: should fail for request exceeding limit", func() {

				// setup cluster to update resource
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// It should be greater than given in cluster namespace
				resourceMem := resource.MustParse("3Gi")
				resourceCPU := resource.MustParse("250m")
				limitMem := resource.MustParse("2Gi")
				limitCPU := resource.MustParse("200m")
				aeroCluster.Spec.Resources = &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resourceCPU,
						corev1.ResourceMemory: resourceMem,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    limitCPU,
						corev1.ResourceMemory: limitMem,
					},
				}
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())

				deleteCluster(k8sClient, ctx, aeroCluster)
			})
		})
	})
})
