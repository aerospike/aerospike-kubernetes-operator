package test

import (
	goctx "context"
	"fmt"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("SCMode", func() {
	ctx := goctx.TODO()

	clusterName := "sc-mode"
	clusterNamespacedName := getClusterNamespacedName(
		clusterName, namespace,
	)

	Context("When doing valid operation", func() {

		// Dead/Unavailable partition
		// If there are D/U p then it should get stuck and not succeed,

		// Should we allow replication factor 1 in general or in SC mode
		// Rack aware setup

		It("Should test sc cluster lifecycle in a rack enabled cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig2Ns()
			scNamespace := "test"
			racks := []asdbv1beta1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rackConf := asdbv1beta1.RackConfig{
				Namespaces: []string{scNamespace},
				Racks:      racks,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should test sc cluster lifecycle in a no rack cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig2Ns()
			scNamespace := "test"

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should test sc cluster blocked node in single namespace cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig1Ns()
			scNamespace := "test"
			racks := []asdbv1beta1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rackConf := asdbv1beta1.RackConfig{
				Namespaces: []string{scNamespace},
				Racks:      racks,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

	})
	Context("When doing invalid operation", func() {
		// Validation: can not remove more than replica node.
		//             not allow updating strong-consistency config
		It("Should not allow updating strong-consistency config", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			scFlag := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["strong-consistency"]

			var scFlagBool bool
			if scFlag != nil {
				scFlagBool = scFlag.(bool)
			}
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["strong-consistency"] = !scFlagBool

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})

		It("Should not allow different sc namespaces in different racks", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1, 2)
			racks[0].InputAerospikeConfig = &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "sc1",
							"memory-size":        1000955200,
							"replication-factor": 2,
							"strong-consistency": true,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			}
			racks[1].InputAerospikeConfig = &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "sc2",
							"memory-size":        1000955200,
							"replication-factor": 2,
							"strong-consistency": true,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			}

			aeroCluster.Spec.RackConfig.Racks = racks

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})

		It("Should not allow cluster size < replication factor for sc namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)
			racks[0].InputAerospikeConfig = &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "sc1",
							"memory-size":        1000955200,
							"replication-factor": 5,
							"strong-consistency": true,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			}

			aeroCluster.Spec.RackConfig.Racks = racks

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})
	})
})

// roster: node-id@rack-id

func validateLifecycleOperationInSCCluster(
	ctx goctx.Context, clusterNamespacedName types.NamespacedName, scNamespace string,
) {
	By("Scaleup")
	err := scaleUpClusterTest(k8sClient, ctx, clusterNamespacedName, 3)
	Expect(err).ToNot(HaveOccurred())

	validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("Set roster blockList")
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())
	aeroCluster.Spec.RosterBlockList = []string{"1A0", "2A7"}
	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())

	validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("ScaleDown")
	err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("RollingRestart")
	err = rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("Upgrade/Downgrade")
	// don't change image, it upgrades, check old version
	err = upgradeClusterTest(
		k8sClient, ctx, clusterNamespacedName, prevImage,
	)
	Expect(err).ToNot(HaveOccurred())

	validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
}

func validateRoster(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, aeroNamespace string) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	hostConns, err := newAllHostConn(logger, aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	rosterNodesMap, err := getRoster(hostConns[0], getClientPolicy(aeroCluster, k8sClient), aeroNamespace)
	Expect(err).ToNot(HaveOccurred())

	rosterStr := rosterNodesMap["roster"]
	rosterList := strings.Split(rosterStr, ",")

	// Check2 roster+blockList >= len(pods)
	if len(rosterList)+len(aeroCluster.Spec.RosterBlockList) < len(aeroCluster.Status.Pods) {
		err := fmt.Errorf("roster len not matching pods list. roster %v, blockList %v, pods %v", rosterList, aeroCluster.Spec.RosterBlockList, aeroCluster.Status.Pods)
		Expect(err).ToNot(HaveOccurred())
	}

	for _, rosterNode := range rosterList {
		nodeID := strings.Split(rosterNode, "@")[0]

		// Check1 roster should not have blocked pod
		contains := v1beta1.ContainsString(aeroCluster.Spec.RosterBlockList, nodeID)
		Expect(contains).To(BeFalse(), "roster should not have blocked node", "roster", rosterNode, "blockList", aeroCluster.Spec.RosterBlockList)

		// Check4 Scaledown: all the roster should be in pod list
		var found bool
		for _, pod := range aeroCluster.Status.Pods {
			// Remove 0 from start of nodeid (we add this dummy rack)
			if strings.EqualFold(nodeID, strings.TrimLeft(pod.Aerospike.NodeID, "0")) {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "roster node should be in pod list", "rosterNode", rosterNode, "podList", aeroCluster.Status.Pods)

	}

	// Check3 Scaleup: pod should be in roster or in blockList
	for podName, pod := range aeroCluster.Status.Pods {
		nodeID := strings.TrimLeft(pod.Aerospike.NodeID, "0")
		rackIDPtr, err := utils.GetRackIDFromPodName(podName)
		Expect(err).ToNot(HaveOccurred())
		rackID := *rackIDPtr

		nodeRoster := nodeID
		if rackID != 0 {
			nodeRoster = nodeID + "@" + fmt.Sprint(rackID)
		}

		if !v1beta1.ContainsString(aeroCluster.Spec.RosterBlockList, nodeID) &&
			!v1beta1.ContainsString(rosterList, nodeRoster) {
			err := fmt.Errorf("pod not found in roster or blockList. roster %v, blockList %v, missing pod roster %v, rackID %v", rosterList, aeroCluster.Spec.RosterBlockList, nodeRoster, rackID)
			Expect(err).ToNot(HaveOccurred())
		}
	}
}

func getRoster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("roster:namespace=%s", namespace)
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	return aerospikecluster.ParseInfoIntoMap(cmdOutput, ":", "=")
}

func getSCAerospikeConfig2Ns() *asdbv1beta1.AerospikeConfigSpec {
	return &asdbv1beta1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			"service": map[string]interface{}{
				"feature-key-file": "/etc/aerospike/secret/features.conf",
				"proto-fd-max":     defaultProtofdmax,
			},
			"security": map[string]interface{}{},
			"network":  getNetworkConfig(),
			"namespaces": []interface{}{
				map[string]interface{}{
					"name":               "test",
					"memory-size":        1000955200,
					"replication-factor": 2,
					"strong-consistency": true,
					"storage-engine": map[string]interface{}{
						"type":    "device",
						"devices": []interface{}{"/test/dev/xvdf"},
					},
				},
				map[string]interface{}{
					"name":               "bar",
					"memory-size":        1000955200,
					"replication-factor": 2,
					"storage-engine": map[string]interface{}{
						"type": "memory",
					},
				},
			},
		},
	}
}

func getSCAerospikeConfig1Ns() *asdbv1beta1.AerospikeConfigSpec {
	return &asdbv1beta1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			"service": map[string]interface{}{
				"feature-key-file": "/etc/aerospike/secret/features.conf",
				"proto-fd-max":     defaultProtofdmax,
			},
			"security": map[string]interface{}{},
			"network":  getNetworkConfig(),
			"namespaces": []interface{}{
				map[string]interface{}{
					"name":               "test",
					"memory-size":        1000955200,
					"replication-factor": 2,
					"strong-consistency": true,
					"storage-engine": map[string]interface{}{
						"type":    "device",
						"devices": []interface{}{"/test/dev/xvdf"},
					},
				},
			},
		},
	}
}
