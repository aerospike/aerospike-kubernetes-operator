package test

import (
	goctx "context"
	"fmt"

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

						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 18000
						log := map[string]interface{}{
							"report-data-op": []string{"test"},
						}

						aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["log"] = log
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

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						logs := aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["log"]
						Expect(aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"]).
							To(Equal(float64(18000)))
						Expect(logs.(map[string]interface{})["report-data-op"].([]interface{})[0]).To(Equal("test"))
						Expect(aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"]).To(HaveLen(2))

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
						aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"] =
							aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"].([]interface{})[:1]

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						Expect(aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"]).To(BeNil())
						Expect(aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["log"]).To(BeNil())
						Expect(aeroCluster.Spec.AerospikeConfig.Value["xdr"].(map[string]interface{})["dcs"]).To(HaveLen(1))

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

						aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["enable-quotas"] = false

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						Expect(aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["enable-quotas"]).
							To(Equal(false))

						By("Verify warm restarts in Pods")
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
