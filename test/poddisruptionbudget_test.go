package test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"PodDisruptionBudget", func() {
		ctx := context.TODO()
		aeroCluster := &asdbv1.AerospikeCluster{}
		maxAvailable := intstr.FromInt(2)
		clusterNamespacedName := getNamespacedName("pdb-test-cluster", namespace)

		BeforeEach(func() {
			aeroCluster = createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)
		})

		AfterEach(func() {
			Expect(deleteCluster(k8sClient, ctx, aeroCluster)).NotTo(HaveOccurred())
		})

		It("Validate create PDB with default maxUnavailable", func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validatePDB(ctx, aeroCluster, 1)
		})

		It("Validate create PDB with specified maxUnavailable", func() {
			aeroCluster.Spec.MaxUnavailable = &maxAvailable
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validatePDB(ctx, aeroCluster, 2)
		})

		It("Validate update PDB", func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validatePDB(ctx, aeroCluster, 1)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			// Update maxUnavailable
			By("Update maxUnavailable to 2")
			aeroCluster.Spec.MaxUnavailable = &maxAvailable

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validatePDB(ctx, aeroCluster, 2)
		})
	})

func validatePDB(ctx context.Context, aerocluster *asdbv1.AerospikeCluster, expectedMaxUnavailable int) {
	pdb := policyv1.PodDisruptionBudget{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: aerocluster.Namespace,
		Name:      aerocluster.Name,
	}, &pdb)
	Expect(err).ToNot(HaveOccurred())

	// Validate PDB
	Expect(pdb.Spec.MaxUnavailable.IntValue()).To(Equal(expectedMaxUnavailable))
	Expect(pdb.Status.ExpectedPods).To(Equal(aerocluster.Spec.Size))
	Expect(pdb.Status.CurrentHealthy).To(Equal(aerocluster.Spec.Size))
	Expect(pdb.Status.DisruptionsAllowed).To(Equal(int32(expectedMaxUnavailable)))
	Expect(pdb.Status.DesiredHealthy).To(Equal(aerocluster.Spec.Size - int32(expectedMaxUnavailable)))
}
