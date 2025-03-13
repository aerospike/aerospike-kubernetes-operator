package cluster

import (
	"bufio"
	goctx "context"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

var _ = Describe(
	"HostNetwork", func() {
		ctx := goctx.TODO()
		Context(
			"HostNetwork", func() {
				clusterName := "host-network-cluster"
				clusterNamespacedName := test.GetNamespacedName(
					clusterName, namespace,
				)
				aeroCluster := createAerospikeClusterPost640(
					clusterNamespacedName, 2, latestImage,
				)
				aeroCluster.Spec.PodSpec.HostNetwork = true
				aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(true)

				AfterEach(
					func() {
						_ = deleteCluster(k8sClient, ctx, aeroCluster)
						_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
					},
				)

				It(
					"Should not work with MultiPodPerHost enabled", func() {
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"Should verify hostNetwork flag updates", func() {
						By("Deploying cluster, Should not advertise node address when off")
						aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(false)
						aeroCluster.Spec.PodSpec.HostNetwork = false
						aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface {
						})["service"].(map[string]interface{})["port"] = serviceNonTLSPort + GinkgoParallelProcess()*10

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, false)

						By("Updating cluster, Should advertise node address when dynamically enabled")

						aeroCluster.Spec.PodSpec.HostNetwork = true
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, true)

						By("Updating cluster, Should not advertise node address when dynamically disabled")

						aeroCluster.Spec.PodSpec.HostNetwork = false
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, false)
					},
				)
			},
		)
	},
)

func checkAdvertisedAddress(
	ctx goctx.Context, aeroCluster *asdbv1.AerospikeCluster,
	expectNodIP bool,
) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	for podIndex := range podList.Items {
		if expectNodIP {
			Expect(intraClusterAdvertisesNodeIP(ctx, &podList.Items[podIndex])).To(BeTrue())
		} else {
			Expect(intraClusterAdvertisesNodeIP(ctx, &podList.Items[podIndex])).ToNot(BeTrue())
		}
	}
}

// intraClusterAdvertisesNodeIp indicates if the pod advertises k8s node IP.
func intraClusterAdvertisesNodeIP(ctx goctx.Context, pod *corev1.Pod) bool {
	podNodeIP := pod.Status.HostIP
	logs := getPodLogs(k8sClientSet, ctx, pod)
	scanner := bufio.NewScanner(strings.NewReader(logs))
	hbAdvertisesNodeID := false
	fabricAdvertisesNodeID := false

	// Account for Cleartext and TLS  endpoints.
	var hbPublishPattern = regexp.MustCompile(".*updated heartbeat published address list to {" +
		podNodeIP + ":[0-9]+," + podNodeIP + ":[0-9]+}.*")

	var fabricPublishPattern = regexp.MustCompile(".*updated fabric published address list to {" +
		podNodeIP + ":[0-9]+," + podNodeIP + ":[0-9]+}.*")

	for scanner.Scan() {
		logLine := scanner.Text()
		if hbPublishPattern.MatchString(logLine) {
			hbAdvertisesNodeID = true
		}

		if fabricPublishPattern.MatchString(logLine) {
			fabricAdvertisesNodeID = true
		}
	}

	return hbAdvertisesNodeID && fabricAdvertisesNodeID
}
