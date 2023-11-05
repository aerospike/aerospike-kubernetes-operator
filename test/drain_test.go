package test

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"NodeDrain", func() {
		ctx := goctx.TODO()
		Context(
			"Clean local PVC", func() {
				clusterName := "fake-local-volume-cluster"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)

				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterNamespacedName.Name,
								Namespace: clusterNamespacedName.Namespace,
							},
						}
						err := deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				It(

					"Should delete local PVC if pod is unschedulable", func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						aeroCluster.Spec.Storage.CleanLocalPVC = true
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).To(HaveOccurred())

						pvcDeleted := false
						pvcDeleted, err = isPVCDeleted(ctx, "ns-"+clusterName+"-0-1")
						Expect(err).ToNot(HaveOccurred())
						Expect(pvcDeleted).To(Equal(true))
					},
				)
				It(
					"Should not delete local PVC if cleanLocalPVC flag is not set", func() {
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).To(HaveOccurred())

						pvcDeleted := false
						pvcDeleted, err = isPVCDeleted(ctx, "ns-"+clusterName+"-0-1")
						Expect(err).ToNot(HaveOccurred())
						Expect(pvcDeleted).To(Equal(false))
					},
				)
			},
		)
	},
)

func isPVCDeleted(ctx context.Context, pvcName string) (bool, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	pvcNamespacesName := getNamespacedName(
		pvcName, namespace,
	)

	err := k8sClient.Get(ctx, pvcNamespacesName, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	//nolint:exhaustive // rest of the cases handled by default
	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		return true, nil
	case corev1.ClaimBound:
		return false, nil
	default:
		return false, fmt.Errorf("PVC status is not expected %s", pvc.Name)
	}
}
