package cluster

import (
	goctx "context"
	"fmt"
	"strconv"

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

			BeforeEach(
				func() {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
					aeroCluster.Spec.RestartStrategy = &asdbv1.RestartStrategy{
						OverrideMigrateFillDelay: &restartMigrateFillDelay,
					}
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
					// Use a non-zero configMFD so the final check distinguishes "reverted to
					// configMFD" from "cleared to 0".
					configMFD := int64(60)

					aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					svcConf := aeroCluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyService].(map[string]interface{})
					svcConf[asdbv1.ConfKeyMigrateFillDelay] = configMFD

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
					Expect(err).ToNot(HaveOccurred())

					rackID := aeroCluster.Spec.RackConfig.Racks[0].ID

					firstPodName := aeroCluster.Name + "-" + strconv.Itoa(rackID) + "-0"

					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationPodRestart, ID: "mfd-restart-1"},
					}

					err = updateCluster(k8sClient, ctx, aeroCluster)
					Expect(err).ToNot(HaveOccurred())

					// Check 3: After all pods have rejoined, MFD is restored to the
					// aerospikeConfig value — not left at the override value permanently.
					By("Check 3: MFD restored to aerospikeConfig value after all pods have rejoined")

					err = validateMigrateFillDelay(ctx, k8sClient, logger, clusterNamespacedName,
						configMFD, nil, firstPodName)
					Expect(err).ToNot(HaveOccurred())
				},
			)
		},
	)
}
