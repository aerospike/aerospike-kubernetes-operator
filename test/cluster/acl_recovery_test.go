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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	akocluster "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/cluster"
	oputils "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
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

	// Wait until failed-pod grace elapses so hasClusterFailed reports true (outside grace).
	time.Sleep(oputils.GetFailedPodGracePeriod() + 30*time.Second)

	return removeAerospikeConfigFromStatus(ctx, c, nn)
}

// waitForClusterRecovery waits until the cluster is operationally healthy after failed-create recovery.
//
// We intentionally do not use waitForAerospikeCluster: it requires reflect.DeepEqual(CopyStatusToSpec(status), spec).
// After removeAerospikeConfigFromStatus, status can stay out of sync with spec in ways DeepEqual never clears
// even when the cluster is otherwise Completed.
//
// This helper instead polls phase, size, pods (node IDs + image), API label, and selector. It also requires
// status.aerospikeAccessControl to be repopulated. Integration tests that open an Aerospike client call
// getClientPolicy, which builds a status snapshot via CopyStatusToSpec and passes it to
// AerospikeAdminCredentials together with spec—admin user/password for policy.User and policy.Password come
// from that merged view (including secret names recorded in status ACL), not from spec alone.
//
// status.aerospikeConfig may lag indefinitely after some recovery paths; do not gate readiness on it here.
func waitForClusterRecovery(
	ctx goctx.Context, c client.Client, nn types.NamespacedName,
) error {
	cl := &asdbv1.AerospikeCluster{}
	if err := c.Get(ctx, nn, cl); err != nil {
		return err
	}

	expectedSize := int(cl.Spec.Size)
	timeout := getTimeout(cl.Spec.Size) * 2

	return wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(ctx goctx.Context) (bool, error) {
		if err := c.Get(ctx, nn, cl); err != nil {
			return false, err
		}

		if asdbv1.GetBool(cl.Spec.Paused) {
			return false, nil
		}

		if int(cl.Status.Size) != expectedSize {
			return false, nil
		}

		if cl.Status.Phase != asdbv1.AerospikeClusterCompleted {
			return false, nil
		}

		if len(cl.Status.Pods) < expectedSize {
			return false, nil
		}

		nodeIDs := 0
		haveImageMatch := false

		for podName := range cl.Status.Pods {
			ps := cl.Status.Pods[podName]
			if ps.Aerospike.NodeID != "" {
				nodeIDs++
			}

			if oputils.IsImageEqual(ps.Image, cl.Spec.Image) {
				haveImageMatch = true
			}
		}

		if nodeIDs < expectedSize || !haveImageMatch {
			return false, nil
		}

		if cl.Labels[asdbv1.AerospikeAPIVersionLabel] != asdbv1.AerospikeAPIVersion {
			return false, nil
		}

		selector := labels.SelectorFromSet(oputils.LabelsForAerospikeCluster(cl.Name))
		if cl.Status.Selector != selector.String() {
			return false, nil
		}

		if cl.Status.AerospikeAccessControl == nil {
			return false, nil
		}

		return true, nil
	})
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
				Expect(waitForClusterRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())

				ac, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				statusAsSpec, err := asdbv1.CopyStatusToSpec(&ac.Status.AerospikeClusterStatusSpec)
				Expect(err).ToNot(HaveOccurred())

				pp := FromSecretPasswordProvider{k8sClient: &k8sClient, namespace: ac.Namespace}
				user, pass, err := akocluster.AerospikeAdminCredentials(&ac.Spec, statusAsSpec, pp)
				Expect(err).ToNot(HaveOccurred())
				Expect(user).NotTo(BeEmpty(),
					"admin credentials should be available from status ACL after recovery")
				Expect(pass).NotTo(BeEmpty(),
					"admin password should be resolvable from status ACL after recovery")
			})

			It("desired access-control in spec survives recovery; status ACL snapshot is cleared then rebuilt", func() {
				clusterNamespacedName = test.GetNamespacedName(
					fmt.Sprintf("fc-acl-spec-%d", GinkgoParallelProcess()), namespace,
				)

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				ac, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				specACL := lib.DeepCopy(ac.Spec.AerospikeAccessControl).(*asdbv1.AerospikeAccessControlSpec)
				Expect(specACL).NotTo(BeNil())

				Expect(ac.Status.AerospikeAccessControl).NotTo(BeNil(),
					"precondition: status should carry an access-control snapshot before recovery")

				Expect(triggerFailedCreateRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())
				Expect(waitForClusterRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())

				ac, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				Expect(reflect.DeepEqual(ac.Spec.AerospikeAccessControl, specACL)).To(BeTrue(),
					"spec aerospikeAccessControl must be unchanged by recovery")

				Expect(ac.Status.AerospikeAccessControl).NotTo(BeNil(),
					"status access-control snapshot should be repopulated after nodes resync")
			})

			It("PKI-only admin: recovery completes without relying on removed password material", func() {
				clusterNamespacedName = test.GetNamespacedName(
					fmt.Sprintf("fc-acl-pki-%d", GinkgoParallelProcess()), namespace,
				)

				accessControl := &asdbv1.AerospikeAccessControlSpec{
					Users: []asdbv1.AerospikeUserSpec{
						{
							Name:     "admin",
							AuthMode: asdbv1.AerospikeAuthModePKIOnly,
							Roles:    []string{"sys-admin", "user-admin"},
						},
					},
				}

				aeroCluster := GetPKIAuthAerospikeClusterWithAccessControl(clusterNamespacedName, 2, accessControl)
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				Expect(triggerFailedCreateRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())
				Expect(waitForClusterRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())

				ac, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				Expect(ac.Spec.AerospikeAccessControl).NotTo(BeNil())
				Expect(reflect.DeepEqual(
					*lib.DeepCopy(accessControl).(*asdbv1.AerospikeAccessControlSpec),
					*ac.Spec.AerospikeAccessControl,
				)).To(BeTrue(), "spec access control must stay PKI-only after recovery")

				Expect(ac.Status.AerospikeAccessControl).NotTo(BeNil(),
					"status access-control snapshot should be repopulated after recovery")

				var adminUser *asdbv1.AerospikeUserSpec

				for i := range ac.Status.AerospikeAccessControl.Users {
					if ac.Status.AerospikeAccessControl.Users[i].Name == asdbv1.AdminUsername {
						u := ac.Status.AerospikeAccessControl.Users[i]
						adminUser = &u

						break
					}
				}

				Expect(adminUser).NotTo(BeNil(), "admin should appear in status ACL snapshot")
				Expect(adminUser.AuthMode).To(Equal(asdbv1.AerospikeAuthModePKIOnly))
				Expect(adminUser.SecretName).To(BeEmpty(),
					"PKI-only admin in status must not reference password secret material")

				statusAsSpec, err := asdbv1.CopyStatusToSpec(&ac.Status.AerospikeClusterStatusSpec)
				Expect(err).ToNot(HaveOccurred())

				pp := FromSecretPasswordProvider{k8sClient: &k8sClient, namespace: ac.Namespace}
				user, pass, err := akocluster.AerospikeAdminCredentials(&ac.Spec, statusAsSpec, pp)
				Expect(err).ToNot(HaveOccurred())
				Expect(user).To(BeEmpty(), "client policy must not use password user for PKI-only admin")
				Expect(pass).To(BeEmpty(), "client policy must not use password for PKI-only admin")
			})

			It("multi-rack deployment recovers without access-control status blocking progress", func() {
				clusterNamespacedName = test.GetNamespacedName(
					fmt.Sprintf("fc-acl-racks-%d", GinkgoParallelProcess()), namespace,
				)

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: getDummyRackConf(1, 2)}
				aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
				randomizeServicePorts(aeroCluster, false, GinkgoParallelProcess())

				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				Expect(triggerFailedCreateRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())
				Expect(waitForClusterRecovery(ctx, k8sClient, clusterNamespacedName)).To(Succeed())

				Eventually(func(g Gomega) {
					ac, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(len(ac.Status.Pods)).To(BeNumerically(">=", 2))

					rackIDs := make(map[int]struct{})

					for podName, pod := range ac.Status.Pods {
						g.Expect(pod.Aerospike.NodeID).NotTo(BeEmpty(),
							"pod %s should report a node ID after recovery", podName)
						// status.pods[].aerospike.rackID can stay 0 while topology is still correct; rack is
						// always encoded in the StatefulSet pod name (<cluster>-<rack-id>-<pod-index>).
						rackID, _, perr := oputils.GetRackIDAndRevisionFromPodName(ac.Name, podName)
						g.Expect(perr).ToNot(HaveOccurred(), "pod name %q should encode rack id", podName)
						g.Expect(rackID).NotTo(BeZero(), "pod name %q should encode non-zero rack id", podName)
						rackIDs[rackID] = struct{}{}
					}

					g.Expect(rackIDs).To(HaveKey(1), "expected convergence on rack 1")
					g.Expect(rackIDs).To(HaveKey(2), "expected convergence on rack 2")
				}).WithTimeout(getTimeout(2)).WithPolling(retryInterval).Should(Succeed(),
					"multi-rack node IDs and pod naming should stabilize after forced recreate")
			})
		})
	})
})
