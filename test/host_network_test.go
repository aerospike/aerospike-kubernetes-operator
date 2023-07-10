package test

import (
	"bufio"
	goctx "context"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"HostNetwork", func() {
		ctx := goctx.TODO()
		Context(
			"HostNetwork", func() {
				clusterName := "host-network-cluster"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)
				aeroCluster := createAerospikeClusterPost560(
					clusterNamespacedName, 2, latestImage,
				)
				aeroCluster.Spec.PodSpec.HostNetwork = true
				aeroCluster.Spec.PodSpec.MultiPodPerHost = true

				It(
					"Should not work with MultiPodPerHost enabled", func() {
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).To(HaveOccurred())
					},
				)

				It(
					"Should verify hostNetwork flag updates", func() {
						By("Deploying cluster, Should not advertise node address when off")
						aeroCluster.Spec.PodSpec.MultiPodPerHost = false
						aeroCluster.Spec.PodSpec.HostNetwork = false

						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, false)

						By("Updating cluster, Should advertise node address when dynamically enabled")
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.HostNetwork = true
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, true)

						By("Updating cluster, Should not advertise node address when dynamically disabled")
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.PodSpec.HostNetwork = false
						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
						checkAdvertisedAddress(ctx, aeroCluster, false)

						By("Deleting cluster")
						err = deleteCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
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
			Expect(intraClusterAdvertisesNodeIP(ctx, &podList.Items[podIndex])).To(Equal(true))
		} else {
			Expect(intraClusterAdvertisesNodeIP(ctx, &podList.Items[podIndex])).ToNot(Equal(true))
		}
	}
}

// intraClusterAdvertisesNodeIp indicates if the pod advertises k8s node IP.
func intraClusterAdvertisesNodeIP(ctx goctx.Context, pod *corev1.Pod) bool {
	podNodeIP := pod.Status.HostIP
	logs := getPodLogs(k8sClientset, ctx, pod)
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
