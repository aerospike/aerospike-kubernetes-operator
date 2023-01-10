package test

import (
	goctx "context"
	"fmt"
	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var (
	unavailableImage = fmt.Sprintf("%s:%s", baseImage, "6.0.0.99")
	availableImage1  = fmt.Sprintf("%s:%s", baseImage, "6.0.0.1")
	availableImage2  = fmt.Sprintf("%s:%s", baseImage, "6.0.0.2")
)

func percent(val string) *intstr.IntOrString {
	v := intstr.FromString(val)
	return &v
}

func count(val int) *intstr.IntOrString {
	v := intstr.FromInt(val)
	return &v
}

var _ = Describe("BatchRestart", func() {
	ctx := goctx.TODO()

	Context("When doing valid operations", func() {
		clusterName := "batch-restart"
		clusterNamespacedName := getClusterNamespacedName(
			clusterName, namespace,
		)
		Context("BatchRollingRestart", func() {
			BatchRollingRestart(ctx, clusterNamespacedName)
		})
		Context("BatchUpgrade", func() {
			clusterName := "batch-upgrade"
			clusterNamespacedName := getClusterNamespacedName(
				clusterName, namespace,
			)
			BatchUpgrade(ctx, clusterNamespacedName)
		})
	})

	Context("When doing invalid operations", func() {
		clusterName := "batch-restart"
		clusterNamespacedName := getClusterNamespacedName(
			clusterName, namespace,
		)
		BeforeEach(
			func() {
				aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
				racks := getDummyRackConf(1, 2)
				aeroCluster.Spec.RackConfig.Racks = racks
				aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
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

		It("Should fail if number of racks is less than 2 and RollingUpdateBatchSize PCT or RollingUpdateBatchSize Count is given", func() {
			// During deployment
			// During update. User should not be allowed to remove rack if above condition is met.
			By("Using RollingUpdateBatchSize PCT")
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.RackConfig.Racks = nil
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
			err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			By("Using RollingUpdateBatchSize Count")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.RackConfig.Racks = nil
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(1)
			err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})
		It("Should fail if there are any non rack-enabled namespaces", func() {
			By("Using RollingUpdateBatchSize PCT")
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.RackConfig.Namespaces = nil
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
			err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			By("Using RollingUpdateBatchSize Count")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.RackConfig.Namespaces = nil
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(1)
			err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When doing namespace related operations", func() {
		clusterName := "batch-restart"
		clusterNamespacedName := getClusterNamespacedName(
			clusterName, namespace,
		)
		It("Should fail if replication-factor is 1", func() {
			By("Using RollingUpdateBatchSize PCT")
			aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 1)
			racks := getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())

			By("Using RollingUpdateBatchSize Count")
			aeroCluster = createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 1)
			racks = getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(10)
			err = deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})
		It("Should fail if namespace is configured in single rack", func() {
			aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
			racks := getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test", "bar"}
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
			aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &v1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "bar",
							"memory-size":        1000955200,
							"replication-factor": 2,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			}

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})
		It("Should pass if namespace is configured in 1+ racks", func() {
			aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
			racks := getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test", "bar"}
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
			config := &v1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "bar",
							"memory-size":        1000955200,
							"replication-factor": 2,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			}
			aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = config
			aeroCluster.Spec.RackConfig.Racks[1].InputAerospikeConfig = config

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	// TODO: Should we ensure that racks are not less than replication-factor?
	// Or just keep printing warning during validation?, Can we disable the feature in this case?
	// TODO: What if racks according to namespace replication-factor are not maintained
})

func BatchRollingRestart(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 8, 2)
			racks := getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
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
	// Restart 1 node at a time
	It("Should restart one pod at a time", func() {
		By("Using default RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("1Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Using RollingUpdateBatchSize PCT which is not enough eg. 1%")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("1%")
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = nil
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Test steps
	// 1: update cluster to demand huge resources. It will unschedule batch of pods
	// 2: verify if more than 1 pods are in unscheduled state. In default mode, there can not be more than 1 unscheduled pods
	// 3: update cluster to demand limited resources. It will schedule old unschedulable pods

	// Restart full rack at a time
	It("Should restart full rack in one go", func() {

		By("Using RollingUpdateBatchSize PCT as 100")
		// Unschedule batch of pods
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		// schedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("1Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Using RollingUpdateBatchSize Count greater than pods in rack")
		// Unschedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(10)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		// Schedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(10)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("2Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Restart batch of nodes
	It("Should do BatchRollingRestart", func() {
		By("Use RollingUpdateBatchSize PCT")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("90%")
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("90%")
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("1Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("2Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// User should be able to change RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count when restart is going on
	It("Should allow multiple changes in RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count", func() {
		By("Update RollingUpdateBatchSize Count")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("1Gi")
		err = k8sClient.Update(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(time.Second * 1)

		By("Again Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(1)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = nil
		err = k8sClient.Update(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(time.Second * 1)

		By("Again Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = schedulableResource("1Gi")
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Should be able to deal with failed nodes
}

func BatchUpgrade(ctx goctx.Context, clusterNamespacedName types.NamespacedName) {
	BeforeEach(
		func() {
			aeroCluster := createDummyAerospikeClusterWithRF(clusterNamespacedName, 8, 2)
			racks := getDummyRackConf(1, 2)
			aeroCluster.Spec.RackConfig.Racks = racks
			aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
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
	// Restart 1 node at a time
	It("Should upgrade one pod at a time", func() {
		By("Using default RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Using RollingUpdateBatchSize PCT which is not enough eg. 1%")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("1%")
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Test steps
	// 1: update cluster to demand huge resources. It will unschedule batch of pods
	// 2: verify if more than 1 pods are in unscheduled state. In default mode, there can not be more than 1 unscheduled pods
	// 3: update cluster to demand limited resources. It will schedule old unschedulable pods

	// Restart full rack at a time
	It("Should upgrade full rack in one go", func() {

		By("Using RollingUpdateBatchSize PCT as 100")
		// Unschedule batch of pods
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
		aeroCluster.Spec.Image = unavailableImage
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		// schedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("100%")
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Using RollingUpdateBatchSize Count greater than pods in rack")
		// Unschedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(10)
		aeroCluster.Spec.Image = unavailableImage
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		// Schedule batch of pods
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(10)
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Restart batch of nodes
	It("Should do BatchUpgrade", func() {
		By("Use RollingUpdateBatchSize PCT")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("90%")
		aeroCluster.Spec.Image = unavailableImage
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = percent("90%")
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		By("Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.Image = unavailableImage
		err = updateClusterForBatchRestart(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// User should be able to change RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count when restart is going on
	It("Should allow multiple changes in RollingUpdateBatchSize PCT/RollingUpdateBatchSize Count", func() {
		By("Update RollingUpdateBatchSize Count")
		aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.Image = availableImage1
		err = k8sClient.Update(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(time.Second * 1)

		By("Again Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(1)
		aeroCluster.Spec.Image = availableImage2
		err = k8sClient.Update(ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(time.Second * 1)

		By("Again Update RollingUpdateBatchSize Count")
		aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
		Expect(err).ToNot(HaveOccurred())
		aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = count(3)
		aeroCluster.Spec.Image = availableImage1
		err = updateAndWait(k8sClient, ctx, aeroCluster)
		Expect(err).ToNot(HaveOccurred())
	})

	// Should be able to deal with failed nodes
}

func isBatchRestart(aeroCluster *v1beta1.AerospikeCluster) bool {
	// Wait for starting the pod restart process
	for {
		readyPods := getReadyPods(aeroCluster)
		unreadyPods := int(aeroCluster.Spec.Size) - len(readyPods)
		if unreadyPods > 0 {
			break
		}
	}

	//Operator should restart batch of pods which will make multiple pods unready
	for i := 0; i < 100; i++ {
		readyPods := getReadyPods(aeroCluster)
		unreadyPods := int(aeroCluster.Spec.Size) - len(readyPods)
		fmt.Printf("unreadyPods %d\n", unreadyPods)
		if unreadyPods > 1 {
			return true
		}
	}

	return false
}

func getReadyPods(aeroCluster *v1beta1.AerospikeCluster) []string {
	podList, err := getPodList(aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	var readyPods []string
	for _, pod := range podList.Items {
		if utils.IsPodRunningAndReady(&pod) {
			readyPods = append(readyPods, pod.Name)
		}
	}
	return readyPods
}

func updateClusterForBatchRestart(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *v1beta1.AerospikeCluster,
) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	if !isBatchRestart(aeroCluster) {
		return fmt.Errorf("looks like pods are not restarting in batch")
	}
	return nil
}

func unschedulableResource() *corev1.ResourceRequirements {
	resourceMem := resource.MustParse("3000Gi")

	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourceMem,
		},
	}
}

func schedulableResource(mem string) *corev1.ResourceRequirements {
	resourceMem := resource.MustParse(mem)

	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourceMem,
		},
	}
}
