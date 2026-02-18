package cluster

import (
	goctx "context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"ClusterResource", func() {

		ctx := goctx.TODO()

		Context(
			"When doing valid operations", func() {

				clusterName := "cl-resource-lifecycle"
				clusterNamespacedName := test.GetNamespacedName(
					clusterName, namespace,
				)

				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterName,
								Namespace: namespace,
							},
						}

						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
					},
				)

				It(
					"Try ClusterWithResourceLifecycle", func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)

						By("DeployClusterWithoutResource")
						Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

						By("Testing aerospike-server resource")
						validUpdateResourceTest(k8sClient, ctx, clusterNamespacedName, true)

						By("Testing aerospike-init resource")
						validUpdateResourceTest(k8sClient, ctx, clusterNamespacedName, false)
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {

				// Test aerospike-server resource
				invalidResourceTest(ctx, true)

				// Test aerospike-init resource
				invalidResourceTest(ctx, false)
			},
		)
	},
)

func invalidResourceTest(ctx goctx.Context, checkAeroServer bool) {
	Context(
		"Deploy", func() {
			var clusterName string
			if checkAeroServer {
				clusterName = "server-resource-invalid"
			} else {
				clusterName = "init-resource-invalid"
			}

			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)
			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)

			It("DeployClusterWithResource: should fail for request exceeding limit", func() {
				// It should be greater than given in cluster namespace
				resourceMem := resource.MustParse("3Gi")
				resourceCPU := resource.MustParse("250m")
				limitMem := resource.MustParse("2Gi")
				limitCPU := resource.MustParse("200m")

				resources := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resourceCPU,
						corev1.ResourceMemory: resourceMem,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    limitCPU,
						corev1.ResourceMemory: limitMem,
					},
				}

				if checkAeroServer {
					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = resources
				} else {
					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = resources
				}

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
			})
		},
	)

	Context(
		"Update", func() {
			var clusterName string

			if checkAeroServer {
				clusterName = "server-resource-insuff"
			} else {
				clusterName = "init-resource-insuff"
			}

			clusterNamespacedName := test.GetNamespacedName(
				clusterName, namespace,
			)
			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)

			AfterEach(
				func() {
					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It("UpdateClusterWithResource: should fail for request exceeding limit", func() {
				// setup cluster to update resource
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				// It should be greater than given in cluster namespace
				resourceMem := resource.MustParse("3Gi")
				resourceCPU := resource.MustParse("250m")
				limitMem := resource.MustParse("2Gi")
				limitCPU := resource.MustParse("200m")

				resources := &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resourceCPU,
						corev1.ResourceMemory: resourceMem,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    limitCPU,
						corev1.ResourceMemory: limitMem,
					},
				}

				if checkAeroServer {
					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = resources
				} else {
					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = resources
				}

				Expect(updateCluster(k8sClient, ctx, aeroCluster)).Should(HaveOccurred())
			})
		},
	)
}

func validUpdateResourceTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, checkAeroServer bool,
) {
	res := &corev1.ResourceRequirements{}

	By("UpdateClusterResourceLimitMemory")

	res.Limits = corev1.ResourceList{}
	res.Limits[corev1.ResourceMemory] = resource.MustParse("1Gi")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer)

	By("UpdateClusterResourceLimitCpu")

	res.Limits[corev1.ResourceCPU] = resource.MustParse("400m")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer)

	By("UpdateClusterResourceRequestMemory")

	res.Requests = corev1.ResourceList{}
	res.Requests[corev1.ResourceMemory] = resource.MustParse("300Mi")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer)

	By("UpdateClusterResourceRequestCpu")

	res.Requests[corev1.ResourceCPU] = resource.MustParse("300m")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer)

	By("RemoveResource")
	updateResource(k8sClient, ctx, clusterNamespacedName, nil, checkAeroServer)
}

func updateResource(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements, checkAeroServer bool,
) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	if checkAeroServer {
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = res
	} else {
		aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = res
	}

	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	err = validateClusterResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer)
	Expect(err).ToNot(HaveOccurred())
}

func validateClusterResource(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements, checkAeroServer bool,
) error {
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

	for stsIndex := range stsList.Items {
		// aerospike container is 1st container
		var cnt corev1.Container
		if checkAeroServer {
			cnt = stsList.Items[stsIndex].Spec.Template.Spec.Containers[0]
			if !reflect.DeepEqual(&cnt.Resources, res) {
				return fmt.Errorf("resource not matching. want %v, got %v", *res, cnt.Resources)
			}
		} else {
			// Find aerospike-init container by name
			aerospikeInitContainer := test.GetContainerByName(
				stsList.Items[stsIndex].Spec.Template.Spec.InitContainers,
				asdbv1.AerospikeInitContainerName,
			)
			if aerospikeInitContainer == nil {
				return fmt.Errorf("aerospike-init container not found")
			}

			cnt = *aerospikeInitContainer
			if !reflect.DeepEqual(&cnt.Resources, res) {
				return fmt.Errorf("resource not matching. want %v, got %v", *res, cnt.Resources)
			}
		}
	}

	return nil
}
