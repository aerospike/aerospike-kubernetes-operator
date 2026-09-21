package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"RestartMigrateFillDelay", func() {
		ctx := goctx.TODO()

		Context(
			"RestartMigrateFillDelay", func() {
				RestartMigrateFillDelayTest(ctx)
			},
		)
	},
)

func RestartMigrateFillDelayTest(ctx goctx.Context) {
	Context(
		"When RestartMigrateFillDelay is configured", func() {
			clusterNamespacedName := test.GetNamespacedName(
				fmt.Sprintf("restart-mfd-cluster-%d", GinkgoParallelProcess()), namespace,
			)
			restartMigrateFillDelay := int64(120)
			configMFD := int64(60)

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RestartStrategy = &asdbv1.RestartStrategy{
						OverrideMigrateFillDelay: &restartMigrateFillDelay,
					}

					svcConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
					svcConf[asdbv1.ConfKeyMigrateFillDelay] = configMFD

					Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				},
			)

			AfterEach(
				func() {
					aeroCluster := &asdbv1.AerospikeCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      clusterNamespacedName.Name,
							Namespace: clusterNamespacedName.Namespace,
						},
					}

					Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
					Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
				},
			)

			It(
				"Should set OverrideMigrateFillDelay before pod restart and restore aerospikeConfig value after",
				func() {
					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					By("Updating pod metadata to trigger rolling restart")

					aeroCluster.Spec.PodSpec.AerospikeObjectMeta = asdbv1.AerospikeObjectMeta{
						Labels: map[string]string{
							"test-label": "test-value",
						},
					}

					updateAndValidateIntermediateMFD(ctx, k8sClient, aeroCluster, configMFD)
				},
			)
		},
	)
}
