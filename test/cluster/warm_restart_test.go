package cluster

import (
	goCtx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const tempTestDir = "/tmp/test"
const testFile = tempTestDir + "/test"

var _ = Describe(
	"WarmRestart", func() {

		ctx := goCtx.TODO()

		Context(
			"WarmRestart", func() {
				It(
					"Should work with tini", func() {
						WarmRestart(ctx)
					},
				)
			},
		)
	},
)

func WarmRestart(ctx goCtx.Context) {
	rollCluster(ctx, latestImage)
}

func rollCluster(ctx goCtx.Context, image string) {
	clusterName := "warm-restart-cluster"
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	aeroCluster, err := getAeroClusterConfig(
		clusterNamespacedName, image,
	)
	Expect(err).ToNot(HaveOccurred())

	// Add a volume of type empty dir to figure if pod restarted.
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, asdbv1.VolumeSpec{
			Name: "test-dir",
			Source: asdbv1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{Path: tempTestDir},
		},
	)
	err = deployCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	defer func(
		k8sClient client.Client, ctx goCtx.Context,
		aeroCluster *asdbv1.AerospikeCluster,
	) {
		_ = deleteCluster(k8sClient, ctx, aeroCluster)
	}(k8sClient, ctx, aeroCluster)

	// Create a file in the empty dir as a marker.
	err = createMarkerFile(ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	err = rollingRestartClusterTest(
		logger, k8sClient, ctx, clusterNamespacedName,
	)
	Expect(err).ToNot(HaveOccurred())

	podToMarkerPresent, err := isMarkerPresent(ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	pkgLog.Info("Rolling restarted", "Markers", podToMarkerPresent)

	for _, marker := range podToMarkerPresent {
		Expect(marker).To(Equal(true))
	}
}

// createMarkerFile create a file on ephemeral storage to detect pod restart.
func createMarkerFile(
	ctx goCtx.Context, aeroCluster *asdbv1.AerospikeCluster,
) error {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	for podIndex := range podList.Items {
		pod := &podList.Items[podIndex]
		cmd := []string{
			"bash",
			"-c",
			"touch " + testFile,
		}

		_, _, err := utils.Exec(
			utils.GetNamespacedName(pod), asdbv1.AerospikeServerContainerName, cmd, k8sClientSet,
			cfg,
		)

		if err != nil {
			return fmt.Errorf(
				"error reading ASD Pid from pod %s - %v", pod.Name, err,
			)
		}
	}

	return nil
}

// isMarkerPresent indicates if the test file is present on all pods.
func isMarkerPresent(
	ctx goCtx.Context, aeroCluster *asdbv1.AerospikeCluster,
) (map[string]bool, error) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return nil, err
	}

	podToMarkerPresent := make(map[string]bool)

	for podIndex := range podList.Items {
		pod := &podList.Items[podIndex]
		cmd := []string{
			"bash",
			"-c",
			"ls " + testFile,
		}

		_, _, err := utils.Exec(
			utils.GetNamespacedName(pod), asdbv1.AerospikeServerContainerName, cmd, k8sClientSet,
			cfg,
		)

		podToMarkerPresent[podList.Items[podIndex].Name] = err == nil
	}

	return podToMarkerPresent, nil
}
