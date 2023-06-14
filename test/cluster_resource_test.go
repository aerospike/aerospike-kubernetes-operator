package test

import (
	goctx "context"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

						By("Testing aerospike-server resource")
						validUpdateResourceTest(k8sClient, ctx, clusterNamespacedName, true, false)

						By("Testing aerospike-init resource")
						validUpdateResourceTest(k8sClient, ctx, clusterNamespacedName, false, true)

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

			},
		)

		Context(
			"When doing invalid operations", func() {

				// Test aerospike-server resource
				invalidResourceTest(ctx, true, false)

				// Test aerospike-server resource
				invalidResourceTest(ctx, false, true)
			},
		)

		Context(
			"When doing cluster recovery operations", func() {
				clusterRecoveryTest(ctx)
			},
		)
	},
)

func clusterRecoveryTest(ctx goctx.Context) {
	Context(
		"Cluster recovery", func() {
			clusterName := "cl-resource-insuff"
			clusterNamespacedName := getClusterNamespacedName(
				clusterName, namespace,
			)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
					racks := getDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig.Racks = racks
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")
					err := deployCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					_ = deleteCluster(k8sClient, ctx, aeroCluster)
				},
			)

			It("UpdateClusterWithResource: should recover after reverting back to schedulable resources", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

				err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
				Expect(err).Should(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
			)

			It("UpdateClusterWithResource: should recover failed pods first after reverting back"+
				" to schedulable resources", func() {
				aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

				err = k8sClient.Update(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("200Mi")
				aeroCluster.Spec.RackConfig.Racks[0].ID = 2
				aeroCluster.Spec.RackConfig.Racks[1].ID = 1

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
			)
		},
	)
}

func invalidResourceTest(ctx goctx.Context, checkAeroServer, checkAeroInit bool) {
	Context(
		"Deploy", func() {
			clusterName := "cluster-resource-invalid"
			clusterNamespacedName := getClusterNamespacedName(
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
				}
				if checkAeroInit {
					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = resources
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

			It("UpdateClusterWithResource: should fail for request exceeding limit", func() {
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
				}
				if checkAeroInit {
					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = resources
				}

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).Should(HaveOccurred())

				_ = deleteCluster(k8sClient, ctx, aeroCluster)
			},
			)
		},
	)
}

func validUpdateResourceTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, checkAeroServer, checkAeroInit bool,
) {
	res := &corev1.ResourceRequirements{}

	By("UpdateClusterResourceLimitMemory")

	res.Limits = corev1.ResourceList{}
	res.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer, checkAeroInit)

	By("UpdateClusterResourceLimitCpu")

	res.Limits[corev1.ResourceCPU] = resource.MustParse("300m")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer, checkAeroInit)

	By("UpdateClusterResourceRequestMemory")

	res.Requests = corev1.ResourceList{}
	res.Requests[corev1.ResourceMemory] = resource.MustParse("1Gi")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer, checkAeroInit)

	By("UpdateClusterResourceRequestCpu")

	res.Requests[corev1.ResourceCPU] = resource.MustParse("200m")
	updateResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer, checkAeroInit)

	By("RemoveResource")
	updateResource(k8sClient, ctx, clusterNamespacedName, nil, checkAeroServer, checkAeroInit)
}

func updateResource(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements, checkAeroServer, checkAeroInit bool,
) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	if checkAeroServer {
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = res
	}

	if checkAeroInit {
		aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = res
	}

	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	err = validateClusterResource(k8sClient, ctx, clusterNamespacedName, res, checkAeroServer, checkAeroInit)
	Expect(err).ToNot(HaveOccurred())
}

func validateClusterResource(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	res *corev1.ResourceRequirements, checkAeroServer, checkAeroInit bool,
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
		}

		if checkAeroInit {
			cnt = stsList.Items[stsIndex].Spec.Template.Spec.InitContainers[0]
			if !reflect.DeepEqual(&cnt.Resources, res) {
				return fmt.Errorf("resource not matching. want %v, got %v", *res, cnt.Resources)
			}
		}
	}

	return nil
}
