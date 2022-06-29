package test

import (
	goctx "context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe(
	"ClusterResource", func() {

		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {

				clusterName := "cl-resource-lifecycle"
				clusterNamespacedName := getClusterNamespacedName(
					clusterName, namespace,
				)

				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				It(
					"Try ClusterWithResourceLifecycle", func() {

						By("DeployClusterWithoutResource")
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						res := &corev1.ResourceRequirements{}

						By("UpdateClusterResourceLimitMemory")
						res.Limits = corev1.ResourceList{}
						res.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
						updateResource(k8sClient, ctx, clusterNamespacedName, res)

						By("UpdateClusterResourceLimitCpu")

						res.Limits[corev1.ResourceCPU] = resource.MustParse("300m")
						updateResource(k8sClient, ctx, clusterNamespacedName, res)

						By("UpdateClusterResourceRequestMemory")
						res.Requests = corev1.ResourceList{}
						res.Requests[corev1.ResourceMemory] = resource.MustParse("1Gi")
						updateResource(k8sClient, ctx, clusterNamespacedName, res)

						By("UpdateClusterResourceRequestCpu")
						res.Requests[corev1.ResourceCPU] = resource.MustParse("200m")
						updateResource(k8sClient, ctx, clusterNamespacedName, res)

						By("RemoveResource")
						updateResource(k8sClient, ctx, clusterNamespacedName, nil)

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

			},
		)

		Context(
			"When doing invalid operations", func() {

				Context(
					"Deploy", func() {
						clusterName := "cluster-resource-invalid"
						clusterNamespacedName := getClusterNamespacedName(
							clusterName, namespace,
						)
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						It(
							"DeployClusterWithResource: should fail for request exceeding limit",
							func() {
								// It should be greater than given in cluster namespace
								resourceMem := resource.MustParse("3Gi")
								resourceCPU := resource.MustParse("250m")
								limitMem := resource.MustParse("2Gi")
								limitCPU := resource.MustParse("200m")
								aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = &corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resourceCPU,
										corev1.ResourceMemory: resourceMem,
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    limitCPU,
										corev1.ResourceMemory: limitMem,
									},
								}

								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).Should(HaveOccurred())
							},
						)
					},
				)

				Context(
					"Update", func() {
						clusterName := "cl-resource-insuff"
						clusterNamespacedName := getClusterNamespacedName(
							clusterName, namespace,
						)
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						It(
							"UpdateClusterWithResource: should fail for request exceeding limit",
							func() {

								// setup cluster to update resource
								err := deployCluster(
									k8sClient, ctx, aeroCluster,
								)
								Expect(err).ToNot(HaveOccurred())

								// It should be greater than given in cluster namespace
								resourceMem := resource.MustParse("3Gi")
								resourceCPU := resource.MustParse("250m")
								limitMem := resource.MustParse("2Gi")
								limitCPU := resource.MustParse("200m")
								aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = &corev1.ResourceRequirements{
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

								_ = deleteCluster(k8sClient, ctx, aeroCluster)
							},
						)
					},
				)
			},
		)
	},
)

func updateResource(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, res *corev1.ResourceRequirements) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = res

	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	err = validateClusterResource(k8sClient, ctx, clusterNamespacedName, res)
	Expect(err).ToNot(HaveOccurred())
}

func validateClusterResource(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, res *corev1.ResourceRequirements) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	stsList, err := getSTSList(aeroCluster, k8sClient)
	if err != nil {
		return err
	}

	// Res can not be null in sts spec
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}
	for _, sts := range stsList.Items {
		// aerospike container is 1st container
		cnt := sts.Spec.Template.Spec.Containers[0]

		if !reflect.DeepEqual(&cnt.Resources, res) {
			return fmt.Errorf("resource not matching. want %v, got %v", *res, cnt.Resources)
		}
	}
	return nil
}
