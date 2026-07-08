package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// The operator used to panic when the StatefulSet Create call
// inside createSTS failed, because the cleanup path unconditionally passed the resulting
// nil *appsv1.StatefulSet into deleteSTS. This is reproduced end-to-end by blocking
// StatefulSet creation at the apiserver with a zero-count ResourceQuota, deployed in the
// dedicated "aerospike" namespace (unused by any other test in this suite, so the quota
// cannot interfere with concurrently running specs). The cluster's own StatefulSet Get
// always returns NotFound in this scenario (one is never created), so every reconcile
// attempt re-enters the exact createEmptyRack/createSTS failure path being guarded against.
var _ = Describe(
	"STSCreateFailure", func() {
		ctx := goctx.TODO()

		Context(
			"When StatefulSet creation is blocked at admission", func() {
				clusterName := "ko-586-sts-create-failure"
				clusterNamespacedName := test.GetNamespacedName(clusterName, test.AerospikeNs)

				quota := &corev1.ResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ko-586-block-statefulsets",
						Namespace: test.AerospikeNs,
					},
					Spec: corev1.ResourceQuotaSpec{
						Hard: corev1.ResourceList{
							corev1.ResourceName("count/statefulsets.apps"): resource.MustParse("0"),
						},
					},
				}

				AfterEach(
					func() {
						aeroCluster := &asdbv1.AerospikeCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      clusterName,
								Namespace: test.AerospikeNs,
							},
						}

						Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
						Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
						Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, quota))).ToNot(HaveOccurred())
					},
				)

				It(
					"Should surface the failure via CR status instead of crashing the operator", func() {
						By("Blocking StatefulSet creation in the aerospike namespace")
						Expect(k8sClient.Create(ctx, quota)).ToNot(HaveOccurred())

						By("Waiting for the quota to be observed by the apiserver's quota controller")

						Expect(waitForResourceQuotaSync(ctx, quota)).ToNot(HaveOccurred())

						By("Recording operator controller-manager pod restart counts")

						initialRestarts, err := getControllerManagerRestartCounts()
						Expect(err).ToNot(HaveOccurred())

						By("Creating a cluster whose StatefulSet can never be created")

						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 1)
						Expect(k8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())

						By("Waiting for the cluster to land in Error phase")
						Expect(
							waitForClusterPhase(ctx, clusterNamespacedName, asdbv1.AerospikeClusterError, 2*time.Minute),
						).ToNot(HaveOccurred())

						By("Confirming the operator did not crash/restart while repeatedly hitting the failure")
						Consistently(
							getControllerManagerRestartCounts, 30*time.Second, 5*time.Second,
						).Should(Equal(initialRestarts))
					},
				)
			},
		)
	},
)

// getControllerManagerRestartCounts returns the total container restart count for each
// operator controller-manager pod, keyed by pod name. The operator's own Deployment always
// runs in the "test" namespace regardless of which namespaces it watches for AerospikeCluster
// CRs (see test/deploy-test-operator.sh, which installs the OLM bundle with --namespace=test).
func getControllerManagerRestartCounts() (map[string]int32, error) {
	podList := &corev1.PodList{}
	listOps := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{"control-plane": "controller-manager"}),
	}

	if err := k8sClient.List(goctx.TODO(), podList, listOps); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no controller-manager pods found in namespace %s", namespace)
	}

	restarts := make(map[string]int32, len(podList.Items))

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		var total int32
		for containerIdx := range pod.Status.ContainerStatuses {
			total += pod.Status.ContainerStatuses[containerIdx].RestartCount
		}

		restarts[pod.Name] = total
	}

	return restarts, nil
}

// waitForClusterPhase polls the AerospikeCluster CR until its status reaches expectedPhase.
// Unlike waitForAerospikeCluster, this does not require the cluster's spec/status to converge
// (Status.Size and Status.AerospikeConfig never get populated when StatefulSet creation is
// permanently blocked), so it checks only the phase field.
func waitForClusterPhase(
	ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	expectedPhase asdbv1.AerospikeClusterPhase, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(
		ctx, 5*time.Second, timeout, true, func(ctx goctx.Context) (bool, error) {
			aeroCluster := &asdbv1.AerospikeCluster{}
			if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			return aeroCluster.Status.Phase == expectedPhase, nil
		},
	)
}

// waitForResourceQuotaSync polls until the quota controller has populated Status.Hard for
// quota, confirming the apiserver's quota admission plugin has started enforcing it. This
// closes the small race window between creating the ResourceQuota and it actually being
// observed, so the subsequent AerospikeCluster create is guaranteed to be blocked.
func waitForResourceQuotaSync(ctx goctx.Context, quota *corev1.ResourceQuota) error {
	quotaNamespacedName := types.NamespacedName{Name: quota.Name, Namespace: quota.Namespace}

	return wait.PollUntilContextTimeout(
		ctx, 200*time.Millisecond, 15*time.Second, true, func(ctx goctx.Context) (bool, error) {
			current := &corev1.ResourceQuota{}
			if err := k8sClient.Get(ctx, quotaNamespacedName, current); err != nil {
				return false, err
			}

			for name, hard := range quota.Spec.Hard {
				observed, ok := current.Status.Hard[name]
				if !ok || !observed.Equal(hard) {
					return false, nil
				}
			}

			return true, nil
		},
	)
}
