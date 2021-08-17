package test

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	goctx "context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("HostNetwork", func() {
	ctx := goctx.TODO()
	Context("HostNetwork", func() {
		clusterName := "host-network-cluster"
		image := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", "5.5.0.13")
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)
		aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, image)
		aeroCluster.Spec.PodSpec.HostNetwork = true
		aeroCluster.Spec.MultiPodPerHost = true

		It("Should not work with MultiPodPerHost enabled", func() {
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})

		It("Should not advertise node address when off", func() {
			aeroCluster.Spec.MultiPodPerHost = false
			aeroCluster.Spec.PodSpec.HostNetwork = false
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			checkAdvertisedAddress(ctx, aeroCluster, false)
		})

		It("Should advertise node address when dynamically enabled", func() {
			aeroCluster.Spec.PodSpec.HostNetwork = true
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			checkAdvertisedAddress(ctx, aeroCluster, true)
		})

		It("Should not advertise node address when dynamically disabled", func() {
			aeroCluster.Spec.PodSpec.HostNetwork = false
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			checkAdvertisedAddress(ctx, aeroCluster, false)
		})

		It("Should destroy cluster", func() {
			deleteCluster(k8sClient, ctx, aeroCluster)
		})
	})
})

func checkAdvertisedAddress(ctx goctx.Context, aeroCluster *asdbv1beta1.AerospikeCluster, expectNodIp bool) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range podList.Items {
		if expectNodIp {
			Expect(intraClusterAdvertisesNodeIp(ctx, &pod)).To(Equal(true))
		} else {
			Expect(intraClusterAdvertisesNodeIp(ctx, &pod)).ToNot(Equal(true))
		}
	}
}

// intraClusterAdvertisesNodeIp indicates if the pod advertises k8s node IP.
func intraClusterAdvertisesNodeIp(ctx goctx.Context, pod *corev1.Pod) bool {
	podNodeIp := pod.Status.HostIP
	logs := getPodLogs(k8sClientset, ctx, pod)
	scanner := bufio.NewScanner(strings.NewReader(logs))
	hbAdvertisesNodeId := false
	fabricAdvertisesNodeId := false

	// Account for Cleartext and TLS  endpoints.
	var hbPublishPattern = regexp.MustCompile(".*updated heartbeat published address list to {" + podNodeIp + ":[0-9]+," + podNodeIp + ":[0-9]+}.*")
	var fabricPublishPattern = regexp.MustCompile(".*updated fabric published address list to {" + podNodeIp + ":[0-9]+," + podNodeIp + ":[0-9]+}.*")

	for scanner.Scan() {
		logLine := scanner.Text()
		if hbPublishPattern.MatchString(logLine) {
			hbAdvertisesNodeId = true
		}
		if fabricPublishPattern.MatchString(logLine) {
			fabricAdvertisesNodeId = true
		}
	}

	return hbAdvertisesNodeId && fabricAdvertisesNodeId
}
