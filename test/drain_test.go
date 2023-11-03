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
				aeroCluster := createDummyAerospikeCluster(
					clusterNamespacedName, 2,
				)

				It(
					"Should delete local PVC if pod is unschedulable", func() {
						aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}
						aeroCluster.Spec.Storage.CleanLocalPVC = true
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 1*time.Minute)
						Expect(err).To(HaveOccurred())

						err = checkPVCStatus(ctx, "ns-"+clusterName+"-0-1")
						Expect(err).ToNot(HaveOccurred())

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)
			},
		)
	},
)

func checkPVCStatus(ctx context.Context, pvcName string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	pvcNamespacesName := getNamespacedName(
		pvcName, namespace,
	)

	err := k8sClient.Get(ctx, pvcNamespacesName, pvc)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		return nil
	}

	if pvc.Status.Phase == "Pending" {
		return nil
	}

	return fmt.Errorf("PVC status is not expected %s", pvc.Name)
}
