package test

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

type podID struct {
	podUID string
	asdPID string
}

var _ = Describe(
	"DynamicConfig", func() {

		ctx := goctx.Background()

		Context(
			"When doing valid operations", func() {

				clusterName := "dynamic-config-test"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)
				BeforeEach(
					func() {
						// Create a 2 node cluster
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
							"dcs": []map[string]interface{}{
								{
									"name":      "dc1",
									"auth-mode": "internal",
									"auth-user": "admin",
									"node-address-ports": []string{
										"aeroclusterdst-0-0 3000",
									},
									"auth-password-file": "/etc/aerospike/secret/password_DC1.txt",
									"namespaces": []map[string]interface{}{
										{
											"name": "test",
										},
									},
								},
							},
						}
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

				It(
					"Should update config dynamically", func() {

						By("Modify dynamic config by adding fields")
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						log := map[string]interface{}{
							"report-data-op": []string{"test"},
						}

						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 18000

						aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["log"] = log

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						pod := aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						conf, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"service", &pod)
						Expect(err).ToNot(HaveOccurred())
						cv, ok := conf["proto-fd-max"]
						Expect(ok).ToNot(BeFalse())

						Expect(cv).To(Equal(int64(18000)))

						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						reportDataOp, ok := conf["log.report-data-op[0]"].(string)
						Expect(ok).ToNot(BeFalse())

						Expect(reportDataOp).To(Equal("test"))

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

						By("Modify dynamic config by removing fields")
						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err = getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						delete(aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{}), "proto-fd-max")
						delete(aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{}), "log")

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						pod = aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"service", &pod)
						Expect(err).ToNot(HaveOccurred())
						cv, ok = conf["proto-fd-max"]
						Expect(ok).ToNot(BeFalse())

						Expect(cv).To(Equal(int64(15000)))

						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						_, ok = conf["log.report-data-op[0]"].(string)
						Expect(ok).ToNot(BeTrue())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

					},
				)

				It(
					"Should update config statically", func() {

						By("Modify static config")

						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["enable-quotas"] = true
						dc := map[string]interface{}{
							"name":      "dc2",
							"auth-mode": "internal",
							"auth-user": "admin",
							"node-address-ports": []string{
								"aeroclusterdst-0-0 3000",
							},
							"auth-password-file": "/etc/aerospike/secret/password_DC1.txt",
							"namespaces": []map[string]interface{}{
								{
									"name": "test",
								},
							},
						}

						aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"] = append(
							aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"].([]interface{}), dc)

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						pod := aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						conf, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						enableQuotas, ok := conf["enable-quotas"].(bool)
						Expect(ok).ToNot(BeFalse())

						Expect(enableQuotas).To(BeTrue())

						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"xdr", &pod)
						Expect(err).ToNot(HaveOccurred())

						Expect(conf["dcs"]).To(HaveLen(2))

						By("Verify warm restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, true)
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {

				clusterName := "dynamic-config-test"
				clusterNamespacedName := getNamespacedName(
					clusterName, namespace,
				)
				BeforeEach(
					func() {
						// Create a 2 node cluster
						aeroCluster := createDummyAerospikeCluster(
							clusterNamespacedName, 2,
						)
						aeroCluster.Spec.AerospikeConfig.Value["xdr"] = map[string]interface{}{
							"dcs": []map[string]interface{}{
								{
									"name":      "dc1",
									"auth-mode": "internal",
									"auth-user": "admin",
									"node-address-ports": []string{
										"aeroclusterdst-0-0 3000",
									},
									"auth-password-file": "/etc/aerospike/secret/password_DC1.txt",
									"namespaces": []map[string]interface{}{
										{
											"name": "test",
										},
									},
								},
							},
						}
						err := deployCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())
					},
				)

				AfterEach(
					func() {
						aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
						Expect(err).ToNot(HaveOccurred())

						_ = deleteCluster(k8sClient, ctx, aeroCluster)
					},
				)

				It(
					"Should fail dynamic config update for invalid config", func() {

						By("Modify dynamic config with incorrect value")
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// This change will lead to dynamic config update failure.
						// Assuming it will fall back to rolling restart. Which leads to pod failures.
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 9999999

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*1)
						Expect(err).To(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Recovery
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 15000

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// As pods were failed, expectation is that pods will be cold restarted.
						By("Verify cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, true)

					},
				)
			},
		)
	},
)

func validateServerRestart(ctx goctx.Context, cluster *asdbv1.AerospikeCluster, pidMap map[string]podID,
	shouldRestart bool) {
	restarted := false

	newPodPidMap, err := getPodIDs(ctx, cluster)
	Expect(err).ToNot(HaveOccurred())

	for podName, pid := range pidMap {
		if newPodPidMap[podName].podUID != pid.podUID || newPodPidMap[podName].asdPID != pid.asdPID {
			restarted = true
			break
		}
	}

	Expect(restarted).To(Equal(shouldRestart))
}

func getPodIDs(ctx context.Context, aeroCluster *asdbv1.AerospikeCluster) (map[string]podID, error) {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return nil, err
	}

	pidMap := make(map[string]podID)

	for podIndex := range podList.Items {
		pod := &podList.Items[podIndex]
		cmd := []string{
			"bash",
			"-c",
			"ps -A -o pid,cmd|grep \"asd\" | grep -v grep | grep -v tini |head -n 1 | awk '{print $1}'",
		}

		stdout, _, execErr := utils.Exec(
			utils.GetNamespacedName(pod), asdbv1.AerospikeServerContainerName, cmd, k8sClientset,
			cfg,
		)

		if execErr != nil {
			return nil, fmt.Errorf(
				"error reading ASD Pid from pod %s - %v", pod.Name, execErr,
			)
		}

		pidMap[pod.Name] = podID{
			podUID: string(pod.UID),
			asdPID: stdout,
		}
	}

	return pidMap, nil
}
