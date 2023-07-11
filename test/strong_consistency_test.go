package test

import (
	goctx "context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

const (
	sc1Name     = "sc1"
	scNamespace = "test"
)

var _ = Describe("SCMode", func() {
	ctx := goctx.TODO()

	Context("When doing valid operation", func() {
		clusterName := "sc-mode"
		clusterNamespacedName := getNamespacedName(
			clusterName, namespace,
		)
		AfterEach(func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		})

		// Dead/Unavailable partition
		// If there are D/U p then it should get stuck and not succeed,

		// Should we allow replication factor 1 in general or in SC mode
		// Rack aware setup

		It("Should test combination of sc and non-sc namespace cluster lifecycle in a rack enabled cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAndNonSCAerospikeConfig()
			scNamespace := scNamespace
			racks := []asdbv1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rackConf := asdbv1.RackConfig{
				Namespaces: []string{scNamespace},
				Racks:      racks,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)
		})

		It("Should test sc cluster lifecycle in a no rack cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAndNonSCAerospikeConfig()
			scNamespace := scNamespace

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)
		})

		It("Should test single sc namespace cluster", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()
			scNamespace := scNamespace
			racks := []asdbv1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rackConf := asdbv1.RackConfig{
				Namespaces: []string{scNamespace},
				Racks:      racks,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)
		})

		It("Should allow adding and removing SC namespace", func() {
			By("Deploy")
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()

			addedSCNs := "newscns"
			path := "/test/dev/" + addedSCNs
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(addedSCNs, path))

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("Add new SC namespace")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			SCConf := getSCNamespaceConfig(addedSCNs, path)
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"] =
				append(aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{}), SCConf)

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
			validateRoster(k8sClient, ctx, clusterNamespacedName, addedSCNs)

			By("Add new non-SC namespace")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			addedNs := "newns"
			conf := map[string]interface{}{
				"name":               addedNs,
				"memory-size":        1000955200,
				"replication-factor": 2,
				"storage-engine": map[string]interface{}{
					"type": "memory",
				},
			}
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"] =
				append(aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{}), conf)

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
			validateRoster(k8sClient, ctx, clusterNamespacedName, addedSCNs)

			By("Remove added namespaces")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
		})

		It("Should allow batch restart in the SC setup", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 6)
			aeroCluster.Spec.AerospikeConfig = getSCAndNonSCAerospikeConfig()
			nonSCNamespace := "bar"
			racks := []asdbv1.Rack{
				{ID: 1},
				{ID: 2},
			}
			rollingUpdateBatchSize := intstr.FromInt(2)
			rackConf := asdbv1.RackConfig{
				Namespaces:             []string{scNamespace, nonSCNamespace},
				Racks:                  racks,
				RollingUpdateBatchSize: &rollingUpdateBatchSize,
			}
			aeroCluster.Spec.RackConfig = rackConf

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("RollingRestart")
			err = rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("Upgrade/Downgrade")
			// don't change image, it upgrades
			err = upgradeClusterTest(
				k8sClient, ctx, clusterNamespacedName, prevImage,
			)
			Expect(err).ToNot(HaveOccurred())

			validateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
		})
	})

	Context("When doing invalid operation", func() {
		clusterName := "sc-mode-invalid"
		clusterNamespacedName := getNamespacedName(
			clusterName, namespace,
		)

		AfterEach(func() {
			aeroCluster, _ := getCluster(k8sClient, ctx, clusterNamespacedName)
			if aeroCluster != nil {
				err := deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		// Validation: can not remove more than replica node.
		//             not allow updating strong-consistency config
		It("Should not allow updating strong-consistency config", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			namespaceConfig :=
				aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
			scFlag := namespaceConfig["strong-consistency"]

			var scFlagBool bool
			if scFlag != nil {
				scFlagBool = scFlag.(bool)
			}
			namespaceConfig["strong-consistency"] = !scFlagBool
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})

		It("Should not allow different sc namespaces in different racks", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1, 2)

			sc1Path := "/test/dev/" + sc1Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc1Name, sc1Path))

			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						getSCNamespaceConfig(sc1Name, sc1Path),
					},
				},
			}

			sc2Name := "sc2"
			sc2Path := "/test/dev/" + sc2Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc2Name, sc2Path))

			racks[1].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						getSCNamespaceConfig(sc2Name, sc2Path),
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

			sc1Path := "/test/dev/" + sc1Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc1Name, sc1Path))

			conf := getSCNamespaceConfig(sc1Name, sc1Path)
			conf["replication-factor"] = 5
			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{conf},
				},
			}

			aeroCluster.Spec.RackConfig.Racks = racks

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).To(HaveOccurred())
		})

		It("Should not allow in-memory sc namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)

			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               sc1Name,
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
	// Keep IDs in lowercase
	aeroCluster.Spec.RosterNodeBlockList = []string{"1A0", "2A7"}
	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())
	// 1 - 1, 1,
	// 2 - 1,
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

func validateRoster(k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, aeroNamespace string) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	hostConns, err := newAllHostConn(logger, aeroCluster, k8sClient)
	Expect(err).ToNot(HaveOccurred())

	rosterNodesMap, err := getRoster(hostConns[0], getClientPolicy(aeroCluster, k8sClient), aeroNamespace)
	Expect(err).ToNot(HaveOccurred())

	// Roster is in uppercase, whereas nodeID is in lower case in server. Keep it in mind when comparing list
	rosterStr := rosterNodesMap["roster"]
	rosterList := strings.Split(rosterStr, ",")

	// Check2 roster+blockList >= len(pods)
	if len(rosterList)+len(aeroCluster.Spec.RosterNodeBlockList) < len(aeroCluster.Status.Pods) {
		err := fmt.Errorf("roster len not matching pods list. "+
			"roster %v, blockList %v, pods %v", rosterList, aeroCluster.Spec.RosterNodeBlockList, aeroCluster.Status.Pods)
		Expect(err).ToNot(HaveOccurred())
	}

	rosterNodeBlockListSet := sets.NewString(aeroCluster.Spec.RosterNodeBlockList...)
	podNodeIDSet := sets.NewString()

	for podName := range aeroCluster.Status.Pods {
		// Remove 0 from start of nodeID (we add this dummy rack)
		podNodeIDSet.Insert(strings.ToUpper(strings.TrimLeft(aeroCluster.Status.Pods[podName].Aerospike.NodeID, "0")))
	}

	for _, rosterNode := range rosterList {
		nodeID := strings.Split(rosterNode, "@")[0]
		// Check1 roster should not have blocked pod
		Expect(rosterNodeBlockListSet.Has(nodeID)).To(
			BeFalse(), fmt.Sprintf("roster should not have blocked node. roster %v, blockList %v",
				rosterNode, aeroCluster.Spec.RosterNodeBlockList))
		// Check4 Scaledown: all the roster should be in pod list
		Expect(podNodeIDSet.Has(nodeID)).To(
			BeTrue(), fmt.Sprintf("roster node should be in pod list. roster %v, podNodeIDs %v", rosterNode, podNodeIDSet))
	}

	rosterListSet := sets.NewString(rosterList...)
	// Check3 Scaleup: pod should be in roster or in blockList
	for podName := range aeroCluster.Status.Pods {
		nodeID := strings.ToUpper(strings.TrimLeft(aeroCluster.Status.Pods[podName].Aerospike.NodeID, "0"))
		rackIDPtr, err := utils.GetRackIDFromPodName(podName)
		Expect(err).ToNot(HaveOccurred())

		rackID := *rackIDPtr

		nodeRoster := nodeID
		if rackID != 0 {
			nodeRoster = nodeID + "@" + fmt.Sprint(rackID)
		}

		if !rosterNodeBlockListSet.Has(nodeID) &&
			!rosterListSet.Has(nodeRoster) {
			err := fmt.Errorf("pod not found in roster or blockList. roster %v,"+
				" blockList %v, missing pod roster %v, rackID %v",
				rosterList, aeroCluster.Spec.RosterNodeBlockList, nodeRoster, rackID)
			Expect(err).ToNot(HaveOccurred())
		}
	}
}

func getRoster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy,
	namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("roster:namespace=%s", namespace)

	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	return deployment.ParseInfoIntoMap(cmdOutput, ":", "=")
}

func getSCAndNonSCAerospikeConfig() *asdbv1.AerospikeConfigSpec {
	conf := getSCAerospikeConfig()
	nonSCConf := map[string]interface{}{
		"name":               "bar",
		"memory-size":        1000955200,
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type": "memory",
		},
	}
	conf.Value["namespaces"] = append(conf.Value["namespaces"].([]interface{}), nonSCConf)

	return conf
}

func getSCAerospikeConfig() *asdbv1.AerospikeConfigSpec {
	return &asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			"service": map[string]interface{}{
				"feature-key-file": "/etc/aerospike/secret/features.conf",
				"proto-fd-max":     defaultProtofdmax,
			},
			"security": map[string]interface{}{},
			"network":  getNetworkConfig(),
			"namespaces": []interface{}{
				getSCNamespaceConfig(scNamespace, "/test/dev/xvdf"),
			},
		},
	}
}
