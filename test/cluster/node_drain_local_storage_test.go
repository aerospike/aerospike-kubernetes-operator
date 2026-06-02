package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

// NodeDrainSimulationLocalStorage exercises cordon + pod evictions (the same eviction path as
// kubectl drain) while the cluster uses local PV storage classes.
var _ = Describe(
	"NodeDrainSimulationLocalStorage", func() {
		ctx := goctx.Background()

		var cordonedNode string

		AfterEach(func() {
			if cordonedNode != "" {
				Expect(uncordonNode(ctx, cordonedNode)).To(Succeed(), "uncordon node after test")
				cordonedNode = ""
			}
		})

		Context("When cluster uses local storage classes", func() {
			It("Should survive cordon and eviction of pods on that node (drain simulation)", func() {
				clusterName := fmt.Sprintf("node-drain-local-%d", GinkgoParallelProcess())
				clusterNamespacedName := test.GetNamespacedName(clusterName, namespace)

				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.Storage.LocalStorageClasses = []string{storageClass}

				defer func() {
					cl := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterName,
							Namespace: namespace,
						},
					}
					Expect(DeleteCluster(k8sClient, ctx, cl)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, namespace, clusterName)).ToNot(HaveOccurred())
				}()

				By("Deploy Aerospike cluster with local storage classes")
				Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

				cur, err := getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				podList, err := getClusterPodList(k8sClient, ctx, cur)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).NotTo(BeEmpty())

				targetNode := podList.Items[0].Spec.NodeName
				Expect(targetNode).NotTo(BeEmpty(), "pod should be scheduled on a node")

				var podsOnNode []corev1.Pod

				for i := range podList.Items {
					p := podList.Items[i]
					if p.Spec.NodeName == targetNode {
						podsOnNode = append(podsOnNode, p)
					}
				}

				Expect(podsOnNode).NotTo(BeEmpty(), "expected at least one Aerospike pod on target node")

				oldPvcInfoPerPod, err := extractClusterPVC(ctx, k8sClient, cur)
				Expect(err).ToNot(HaveOccurred())

				originalUIDByName := make(map[string]string, len(podsOnNode))
				for i := range podsOnNode {
					originalUIDByName[podsOnNode[i].Name] = string(podsOnNode[i].UID)
				}

				By("Cordoning node (kubectl drain first step)")
				Expect(cordonNode(ctx, targetNode)).To(Succeed())
				cordonedNode = targetNode

				By("Evicting each Aerospike pod on the cordoned node (kubectl drain eviction path)")

				for i := range podsOnNode {
					pod := &podsOnNode[i]

					err = evictPod(ctx, pod)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("blocked by admission webhook"))
				}

				By("Verifying eviction-blocked annotation on affected pods")

				for podName := range originalUIDByName {
					pod := &corev1.Pod{}
					err = k8sClient.Get(ctx, test.GetNamespacedName(podName, namespace), pod)
					Expect(err).ToNot(HaveOccurred())
					Expect(pod.Annotations).NotTo(BeNil())
					Expect(pod.Annotations[asdbv1.EvictionBlockedAnnotation]).NotTo(BeEmpty())
				}

				By("Waiting for pods on the cordoned node to restart and become ready")

				for podName, uid := range originalUIDByName {
					Expect(waitForPodRestart(ctx, podName, namespace, uid)).To(Succeed())
				}

				By("Verifying cluster is healthy after simulated drain")

				cur, err = getCluster(k8sClient, ctx, clusterNamespacedName)
				Expect(err).ToNot(HaveOccurred())

				err = waitForAerospikeCluster(
					k8sClient, ctx, cur, int(cur.Spec.Size),
					retryInterval, getTimeout(cur.Spec.Size),
					[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
				)
				Expect(err).ToNot(HaveOccurred())

				By("Validating local PVCs were recreated only for pods restarted on the cordoned node")

				oldPvcInfoRestartedPods := make(map[string]map[string]types.UID, len(originalUIDByName))
				for podName := range originalUIDByName {
					oldPvcInfoRestartedPods[podName] = oldPvcInfoPerPod[podName]
				}

				validateClusterPVCDeletion(ctx, oldPvcInfoRestartedPods)
			})
		})
	},
)

func cordonNode(ctx goctx.Context, nodeName string) error {
	patch := []byte(`{"spec":{"unschedulable":true}}`)

	_, err := k8sClientSet.CoreV1().Nodes().Patch(
		ctx, nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	)

	return err
}

func uncordonNode(ctx goctx.Context, nodeName string) error {
	patch := []byte(`{"spec":{"unschedulable":false}}`)

	_, err := k8sClientSet.CoreV1().Nodes().Patch(
		ctx, nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	)

	return err
}
