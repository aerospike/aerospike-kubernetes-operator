package test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"PodDisruptionBudget", func() {
		ctx := context.TODO()
		aeroCluster := &asdbv1.AerospikeCluster{}
		maxUnavailable := intstr.FromInt(0)
		defaultMaxUpavailable := intstr.FromInt(1)
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
				validatePDB(ctx, aeroCluster, defaultMaxUpavailable.IntValue())
			})

			It("Validate create PDB with specified maxUnavailable", func() {
				aeroCluster.Spec.MaxUnavailable = &maxUnavailable
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, maxUnavailable.IntValue())
			})

			It("Validate update PDB", func() {
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, defaultMaxUpavailable.IntValue())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				// Update maxUnavailable
				By("Update maxUnavailable to 0")
				aeroCluster.Spec.MaxUnavailable = &maxUnavailable

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, maxUnavailable.IntValue())
			})

			It("Validate disablePDB, the Operator will not create PDB", func() {
				aeroCluster.Spec.DisablePDB = ptr.To(true)
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				// Validate PDB is not created
				_, err = getPDB(ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())
				pkgLog.Info("PDB not created as expected")
			})

			It("Validate update disablePDB, the Operator will delete and recreate PDB", func() {
				By("Create cluster with PDB enabled")
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 1)

				By("Update disablePDB to true")
				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.DisablePDB = ptr.To(true)
				aeroCluster.Spec.MaxUnavailable = nil
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				_, err = getPDB(ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())
				pkgLog.Info("PDB deleted as expected")

				By("Update disablePDB to false")
				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster.Spec.DisablePDB = ptr.To(false)
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, 1)
			})
		})

		Context("Invalid Operations", func() {
			value := intstr.FromInt(3)

			It("Should fail if maxUnavailable is greater than size", func() {
				// Cluster size is 2
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})

			It("Should fail if maxUnavailable is greater than RF", func() {
				// PDB should be < (least rf)). rf is 2 in this test
				aeroCluster.Spec.Size = 4
				value := intstr.FromInt(2)
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})
			It("Should fail if maxUnavailable is given but disablePDB is true", func() {
				aeroCluster.Spec.DisablePDB = ptr.To(true)
				value := intstr.FromInt(1)
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})
		})
	})

func validatePDB(ctx context.Context, aerocluster *asdbv1.AerospikeCluster, expectedMaxUnavailable int) {
	pdb, err := getPDB(ctx, aerocluster)
	Expect(err).ToNot(HaveOccurred())

	// Validate PDB
	pkgLog.Info("Found PDB", "pdb", pdb.Name,
		"maxUnavailable", pdb.Spec.MaxUnavailable,
		"expectedMaxUnavailable", expectedMaxUnavailable)

	Expect(pdb.Spec.MaxUnavailable.IntValue()).To(Equal(expectedMaxUnavailable))
	Expect(pdb.Status.ExpectedPods).To(Equal(aerocluster.Spec.Size))
	Expect(pdb.Status.CurrentHealthy).To(Equal(aerocluster.Spec.Size))
	Expect(pdb.Status.DisruptionsAllowed).To(Equal(int32(expectedMaxUnavailable)))
	Expect(pdb.Status.DesiredHealthy).To(Equal(aerocluster.Spec.Size - int32(expectedMaxUnavailable)))
}

func getPDB(ctx context.Context, aerocluster *asdbv1.AerospikeCluster) (*policyv1.PodDisruptionBudget, error) {
	pdb := &policyv1.PodDisruptionBudget{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: aerocluster.Namespace,
		Name:      aerocluster.Name,
	}, pdb)

	return pdb, err
}
