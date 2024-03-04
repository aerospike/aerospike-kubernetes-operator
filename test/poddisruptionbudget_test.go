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
		maxAvailable := intstr.FromInt(0)
		clusterNamespacedName := getNamespacedName("pdb-test-cluster", namespace)

		BeforeEach(func() {
			aeroCluster = createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)
		})

		AfterEach(func() {
			Expect(deleteCluster(k8sClient, ctx, aeroCluster)).NotTo(HaveOccurred())
		})

		Context("Valid Operations", func() {
			It("Validate create PDB with default maxUnavailable", func() {
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 1)
			})

			It("Validate create PDB with specified maxUnavailable", func() {
				aeroCluster.Spec.MaxUnavailable = &maxAvailable
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 0)
			})

			It("Validate update PDB", func() {
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 1)

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				// Update maxUnavailable
				By("Update maxUnavailable to 0")
				aeroCluster.Spec.MaxUnavailable = &maxAvailable

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 0)
			})
		})

		Context("Invalid Operations", func() {
			value := intstr.FromInt(3)

			It("Should fail if maxUnavailable is greater than size", func() {
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})

			It("Should fail if maxUnavailable is greater than RF", func() {
				aeroCluster.Spec.Size = 3
				value := intstr.FromInt(3)
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})
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
