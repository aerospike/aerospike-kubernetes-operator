package cluster

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var _ = Describe(
	"PodDisruptionBudget", func() {
		ctx := context.TODO()
		maxUnavailable := intstr.FromInt32(0)
		defaultMaxUnavailable := intstr.FromInt32(1)
		clusterName := fmt.Sprintf("pdb-test-cluster-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			Expect(deleteCluster(k8sClient, ctx, aeroCluster)).NotTo(HaveOccurred())
			Expect(deletePDB(ctx, aeroCluster)).NotTo(HaveOccurred())
			_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
		})

		Context("Valid Operations", func() {
			It("Validate create PDB with default maxUnavailable", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())
			})

			It("Validate create PDB with specified maxUnavailable", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				aeroCluster.Spec.MaxUnavailable = &maxUnavailable
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, maxUnavailable.IntValue())
			})

			It("Validate update PDB", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())

				// Update maxUnavailable
				By("Update maxUnavailable to 0")
				aeroCluster.Spec.MaxUnavailable = &maxUnavailable

				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, maxUnavailable.IntValue())
			})

			It("Validate disablePDB, the Operator will not create PDB", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

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
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				By("Create cluster with PDB enabled")
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())

				By("Update disablePDB to true")
				aeroCluster.Spec.DisablePDB = ptr.To(true)
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				Expect(aeroCluster.Spec.MaxUnavailable).To(BeNil())

				_, err = getPDB(ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())
				pkgLog.Info("PDB deleted as expected")

				By("Update disablePDB to false")
				aeroCluster.Spec.DisablePDB = ptr.To(false)
				err = updateCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
				validatePDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())
			})

			It("Validate that non-operator created PDB is not created", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				By("Create PDB")
				err := createPDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())
				Expect(err).ToNot(HaveOccurred())

				By("Create cluster. It should fail as PDB is already created")
				// Create cluster should fail as PDB is not created by operator
				err = deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, shortRetry)
				Expect(err).To(HaveOccurred())

				By("Delete PDB")
				err = deletePDB(ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Wait for cluster to be created. It should pass as PDB is deleted")
				// Create cluster should pass as PDB is deleted
				err = waitForAerospikeCluster(
					k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
					getTimeout(aeroCluster.Spec.Size), []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
				)
				Expect(err).ToNot(HaveOccurred())

				validatePDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())
			})

			It("Validate that cluster is deployed with disabledPDB even if non-operator created PDB is present", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				err := createPDB(ctx, aeroCluster, defaultMaxUnavailable.IntValue())
				Expect(err).ToNot(HaveOccurred())

				// Create cluster with disabledPDB
				aeroCluster.Spec.DisablePDB = ptr.To(true)
				err = deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("Invalid Operations", func() {
			value := intstr.FromInt32(3)

			It("Should fail if maxUnavailable is greater than size", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				// Cluster size is 2
				aeroCluster.Spec.MaxUnavailable = &value
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).To(HaveOccurred())
			})

			It("Should fail if maxUnavailable is greater than RF", func() {
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				// PDB should be < (least rf). rf is 2 in this test
				aeroCluster.Spec.Size = 4
				value := intstr.FromInt32(2)
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

func createPDB(ctx context.Context, aerocluster *asdbv1.AerospikeCluster, maxUnavailable int) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aerocluster.Name,
			Namespace: aerocluster.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: int32(maxUnavailable)},
		},
	}

	return k8sClient.Create(ctx, pdb)
}

func deletePDB(ctx context.Context, aerocluster *asdbv1.AerospikeCluster) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aerocluster.Name,
			Namespace: aerocluster.Namespace,
		},
	}

	err := k8sClient.Delete(ctx, pdb)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}
