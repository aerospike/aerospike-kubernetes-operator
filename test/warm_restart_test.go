package test

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

var _ = Describe("WarmRestart", func() {

	ctx := goctx.TODO()

	Context("WarmRestart", func() {
		It("Should work with tini", func() {
			WarmRestart(ctx)
		})
		It("Should cold start without tini", func() {
			PodRestart(ctx)
		})

	})
})

func WarmRestart(ctx goctx.Context) {
	image := fmt.Sprintf("ashishshinde54/aerospike-server-enterprise:%s", "5.5.0.13")
	rollCluster(ctx, image, true)
}

func PodRestart(ctx goctx.Context) {
	image := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", "5.5.0.13")
	rollCluster(ctx, image, false)
}

func rollCluster(ctx goctx.Context, image string, expectWarmStart bool) {
	clusterName := "warm-restart-cluster"
	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	aeroCluster := createAerospikeClusterPost460(clusterNamespacedName, 2, image)
	err := deployCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	defer deleteCluster(k8sClient, ctx, aeroCluster)

	podToAsdPids, err := getAsdPids(ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	rollingRestartClusterTest(k8sClient, ctx, clusterNamespacedName)

	newPodToAsdPids, err := getAsdPids(ctx, aeroCluster)

	pkgLog.Info("Rolling restarated", "OldPids", podToAsdPids, "NewPids", newPodToAsdPids)
	if expectWarmStart {
		Expect(podToAsdPids).ToNot(Equal(newPodToAsdPids))
	} else {
		Expect(podToAsdPids).To(Equal(newPodToAsdPids))
	}
}

// getAsdPids returns a map from pod name to corresponding Aerospike server pid.
func getAsdPids(ctx goctx.Context, aeroCluster *asdbv1beta1.AerospikeCluster) (map[string]string, error) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return nil, err
	}

	podToAsdPids := make(map[string]string)
	for _, pod := range podList.Items {
		cmd := []string{
			"bash",
			"-c",
			"ps -A -o pid,cmd|grep 'asd' | grep -v grep | grep -v tini |head -n 1 | awk '{print $1}'",
		}

		stdout, _, err := utils.Exec(&pod, asdbv1beta1.AerospikeServerContainerName, cmd, k8sClientset, cfg)

		if err != nil {
			return nil, fmt.Errorf("Error reading ASD Pid from pod %s - %v", pod.Name, err)
		}

		podToAsdPids[pod.Name] = stdout
	}

	return podToAsdPids, nil
}
