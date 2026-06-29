//go:build !noac

package cluster

import (
	goctx "context"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	lib "github.com/aerospike/aerospike-management-lib"
)

// KO-546 integration test: failed-create recovery with access control enabled.
// Preconditions follow SingleClusterReconciler.hasClusterFailed / recoverFailedCreate:
// all cluster pods failed (outside grace) and status.aerospikeConfig is cleared so the
// operator treats the cluster as needing wipe-and-recreate.

// removeAerospikeConfigFromStatus clears status.aerospikeConfig so IsStatusEmpty() is true and the
// operator's hasClusterFailed / recoverFailedCreate path can run after pods have failed.
func removeAerospikeConfigFromStatus(ctx goctx.Context, c client.Client, nn types.NamespacedName) error {
	var cur asdbv1.AerospikeCluster
	if err := c.Get(ctx, nn, &cur); err != nil {
		return err
	}

	if cur.Status.AerospikeConfig == nil {
		return nil
	}

	base := cur.DeepCopy()
	cur.Status.AerospikeConfig = nil

	return c.Status().Patch(ctx, &cur, client.MergeFrom(base))
}

// patchStatusAerospikeAccessControl writes a snapshot into status.aerospikeAccessControl, simulating
// stale ACL state left on the CR after nodes were wiped during failed-create recovery (KO-546).
func patchStatusAerospikeAccessControl(
	ctx goctx.Context, c client.Client, nn types.NamespacedName, acl *asdbv1.AerospikeAccessControlSpec,
) error {
	var cur asdbv1.AerospikeCluster
	if err := c.Get(ctx, nn, &cur); err != nil {
		return err
	}

	base := cur.DeepCopy()
	cur.Status.AerospikeAccessControl = lib.DeepCopy(acl).(*asdbv1.AerospikeAccessControlSpec)

	return c.Status().Patch(ctx, &cur, client.MergeFrom(base))
}

// triggerFailedCreateRecovery arms the operator failed-create recovery path: fail all pods, wait out
// the failed-pod grace period, then clear status.aerospikeConfig via removeAerospikeConfigFromStatus.
func triggerFailedCreateRecovery(ctx goctx.Context, c client.Client, nn types.NamespacedName) error {
	ac, err := getCluster(c, ctx, nn)
	if err != nil {
		return err
	}

	pl, err := getPodList(ac, c)
	if err != nil {
		return err
	}

	for i := range pl.Items {
		if err := markPodAsFailed(ctx, c, pl.Items[i].Name, ac.Namespace); err != nil {
			return err
		}
	}

	// Wait for pods to be marked as failed
	time.Sleep(10 * time.Second)

	return removeAerospikeConfigFromStatus(ctx, c, nn)
}

var _ = Describe("ACL failed-create recovery validation", func() {
	ctx := goctx.Background()

	Context("Deploy Validation", func() {
		var clusterNamespacedName types.NamespacedName

		AfterEach(func() {
			if clusterNamespacedName.Name == "" {
				return
			}

			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		})

		Context("When failed-create recovery is triggered", func() {
			It("Cluster becomes healthy again after forced recreate when access control is enabled", func() {
				clusterNamespacedName = test.GetNamespacedName(
					fmt.Sprintf("fc-acl-healthy-%d", GinkgoParallelProcess()), namespace,
				)

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				Expect(triggerFailedCreateRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())
				err := rollingRestartClusterTest(
					logger, k8sClient, ctx, clusterNamespacedName,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("recovers after bad image rollout when stale ACL remains in status and spec image is corrected", func() {
				clusterNamespacedName = test.GetNamespacedName(
					fmt.Sprintf("fc-acl-badimg-%d", GinkgoParallelProcess()), namespace,
				)

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				validImage := aeroCluster.Spec.Image
				specACL := lib.DeepCopy(aeroCluster.Spec.AerospikeAccessControl).(*asdbv1.AerospikeAccessControlSpec)

				// Multi-rack: 2 racks, 1 pod each (size=2)
				aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: getDummyRackConf(1, 2)}

				aeroCluster.Spec.Image = unavailableImage
				Expect(k8sClient.Create(ctx, aeroCluster)).To(Succeed())

				staleACL := &asdbv1.AerospikeAccessControlSpec{
					Users: []asdbv1.AerospikeUserSpec{{
						Name:       asdbv1.AdminUsername,
						SecretName: test.AuthSecretName,
						Roles:      []string{"sys-admin", "user-admin"},
						AuthMode:   asdbv1.AerospikeAuthModeInternal,
					}},
				}

				// Wait for pods to be marked as failed
				time.Sleep(10 * time.Second)

				err := patchStatusAerospikeAccessControl(ctx, k8sClient, clusterNamespacedName, staleACL)
				Expect(err).To(Succeed())

				aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				Expect(reflect.DeepEqual(aeroCluster.Spec.AerospikeAccessControl, specACL)).To(BeTrue(),
					"spec aerospikeAccessControl must be unchanged by recovery")

				Expect(aeroCluster.Status.AerospikeAccessControl).NotTo(BeNil(),
					"precondition: stale ACL snapshot must be present in status before recovery")

				aeroCluster.Spec.Image = validImage

				Expect(updateCluster(k8sClient, ctx, aeroCluster)).To(Succeed())
			})
		})
	})
})
