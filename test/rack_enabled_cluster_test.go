package test

import (
	goctx "context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
)

// This file needs to be changed based on setup. update zone, region, nodeName according to setup

// Local tests var
// var (
// zone1  = "us-west1-b"
// zone2  = "us-west1-b"
// region = "us-west1"
// )

// Jenkins tests var
var (
	zone1  = "us-west1-a"
	zone2  = "us-west1-a"
	region = "us-west1"
)

// Test cluster cr updation
var _ = Describe("RackLifeCycleOp", func() {
	ctx := goctx.TODO()

	Context("When doing valid operations", func() {

		clusterName := "rack-enabled"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		// This needs to be changed based on setup. update zone, region, nodeName according to setup
		racks := []asdbv1alpha1.Rack{
			{ID: 1, Zone: zone1, Region: region},
			{ID: 2, Zone: zone2, Region: region}}
		rackConf := asdbv1alpha1.RackConfig{
			Racks: racks,
		}
		aeroCluster.Spec.RackConfig = rackConf

		BeforeEach(func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should validate rack enabled cluster flow", func() {

			// Op2: scale up
			By("Scaling up the cluster")

			err := scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op3: scale down
			By("Scaling down the cluster")

			err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op4: rolling restart
			By("RollingRestart the cluster")

			err = rollingRestartClusterTest(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Op5: upgrade/downgrade
			By("Upgrade/Downgrade the cluster")

			// dont change image, it upgrade, check old version
			err = upgradeClusterTest(k8sClient, ctx, clusterNamespacedName, imageToUpgrade)
			Expect(err).ToNot(HaveOccurred())

			err = validateRackEnabledCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

		})

		It("Should validate affinity", func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			pods, err := getPodList(aeroCluster, k8sClient)
			Expect(err).ToNot(HaveOccurred())

			// All pods will be moved to this node
			nodeName := pods.Items[0].Spec.NodeName

			affinity := &corev1.Affinity{}
			ns := &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			}
			affinity.NodeAffinity = &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: ns,
			}

			rackConf := asdbv1alpha1.RackConfig{
				Racks: []asdbv1alpha1.Rack{
					{
						ID: 3,
						SchedulingPolicy: v1alpha1.SchedulingPolicy{
							Affinity: affinity,
						},
					},
				},
			}

			aeroCluster.Spec.RackConfig = rackConf

			// All pods should move to node with nodeName
			err = updateAndWait(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			// Verify if all the pods are moved to given node
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			pods, err = getPodList(aeroCluster, k8sClient)
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range pods.Items {
				Expect(pod.Spec.NodeName).Should(Equal(nodeName))
			}
			// Test toleration
			// Test nodeSelector
		})

	})
})
