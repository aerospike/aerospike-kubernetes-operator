package cluster

import (
	goctx "context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

const (
	sc1Name     = "sc1"
	scNamespace = "test"
)

var _ = Describe("SCMode", func() {
	ctx := goctx.TODO()

	Context("When doing valid operation", func() {
		clusterName := fmt.Sprintf("sc-mode-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

		AfterEach(func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
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

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)
		})

		It("Should test sc cluster lifecycle in a no rack cluster", func() {
			By("Deploy")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAndNonSCAerospikeConfig()
			scNamespace := scNamespace

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

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

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			validateLifecycleOperationInSCCluster(ctx, clusterNamespacedName, scNamespace)
		})

		It("Should test blocking rack from roster", func() {
			By("Deploy")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
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

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("Block rack 1 from roster")

			expectedRoster := "2A1@2,2A0@2"
			aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			hostConns, err := newAllHostConn(logger, aeroCluster, k8sClient)
			Expect(err).ToNot(HaveOccurred())

			isEqual, currentRoster, err := compareRoster(hostConns[0], expectedRoster, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(isEqual).To(BeTrue(), fmt.Sprintf(
				"expected %v to equal %v", currentRoster, expectedRoster,
			))

			By("Unblock rack 1 from roster")

			expectedRoster = "2A1@2,2A0@2,1A1@1,1A0@1"
			aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(false)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			isEqual, currentRoster, err = compareRoster(hostConns[0], expectedRoster, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(isEqual).To(BeTrue(), fmt.Sprintf(
				"expected %v to equal %v", currentRoster, expectedRoster,
			))

			By("Block rack 1 from roster with failed pods")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = unschedulableResource()
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = &intstr.IntOrString{IntVal: 2}

			err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 30*time.Second)
			Expect(err).Should(HaveOccurred())

			expectedRoster = "2A1@2,2A0@2"
			aeroCluster.Spec.RackConfig.Racks[0].ForceBlockFromRoster = ptr.To(true)
			aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = nil

			err = updateClusterWithTO(k8sClient, ctx, aeroCluster, 30*time.Second)
			Expect(err).Should(HaveOccurred())

			Eventually(func() error {
				isEqual, currentRoster, err = compareRoster(hostConns[2], expectedRoster, aeroCluster)
				if err != nil {
					return err
				}

				if !isEqual {
					return fmt.Errorf("roster not matching. expected %s, got %s", expectedRoster, currentRoster)
				}

				return nil
			}, 5*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())
		})

		It("Should allow adding and removing SC namespace", func() {
			By("Deploy")

			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()

			addedSCNs := "newscns"
			path := "/test/dev/" + addedSCNs
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(addedSCNs, path))

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("Add new SC namespace")

			SCConf := getSCNamespaceConfig(addedSCNs, path)
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"] =
				append(aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{}), SCConf)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, addedSCNs)

			By("Add new non-SC namespace")

			addedNs := "newns"
			conf := map[string]interface{}{
				"name":               addedNs,
				"replication-factor": 2,
				"storage-engine": map[string]interface{}{
					"type":      "memory",
					"data-size": 1073741824,
				},
			}
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"] =
				append(aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{}), conf)

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, addedSCNs)

			By("Remove added namespaces")

			aeroCluster.Spec.AerospikeConfig = getSCAerospikeConfig()

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
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

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("RollingRestart")

			err := rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

			By("Upgrade/Downgrade")
			// don't change image, it upgrades
			err = upgradeClusterTest(
				k8sClient, ctx, clusterNamespacedName, nextImage,
			)
			Expect(err).ToNot(HaveOccurred())

			ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
		})

		It("Should allow MRT fields in SC namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)

			sc1Path := "/test/dev/" + sc1Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc1Name, sc1Path))

			conf := getSCNamespaceConfig(sc1Name, sc1Path)
			conf["mrt-duration"] = 15

			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{conf},
				},
			}
			aeroCluster.Spec.RackConfig.Racks = racks

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
		})
	})

	Context("When doing invalid operation", func() {
		clusterName := fmt.Sprintf("sc-mode-invalid-%d", GinkgoParallelProcess())
		clusterNamespacedName := test.GetNamespacedName(
			clusterName, namespace,
		)

		AfterEach(
			func() {
				aeroCluster := &asdbv1.AerospikeCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: namespace,
					},
				}

				Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
				Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
			},
		)

		// Validation: can not remove more than replica node.
		//             not allow updating strong-consistency config
		It("Should not allow updating strong-consistency config", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			namespaceConfig :=
				aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
			scFlag := namespaceConfig["strong-consistency"]

			var scFlagBool bool
			if scFlag != nil {
				scFlagBool = scFlag.(bool)
			}

			namespaceConfig["strong-consistency"] = !scFlagBool
			aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
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

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
		})

		It("Should not allow cluster size < replication factor for sc namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)

			sc1Path := "/test/dev/" + sc1Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc1Name, sc1Path))

			conf := getSCNamespaceConfig(sc1Name, sc1Path)
			conf[asdbv1.ConfKeyReplicationFactor] = 5
			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{conf},
				},
			}

			aeroCluster.Spec.RackConfig.Racks = racks

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
		})

		It("Should not allow in-memory sc namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)

			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               sc1Name,
							"replication-factor": 2,
							"strong-consistency": true,
							"storage-engine": map[string]interface{}{
								"type":      "memory",
								"data-size": 1073741824,
							},
						},
					},
				},
			}

			aeroCluster.Spec.RackConfig.Racks = racks
			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
		})

		It("Should not allow MRT fields in non-SC namespace", func() {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 3)
			racks := getDummyRackConf(1)

			sc1Path := "/test/dev/" + sc1Name
			aeroCluster.Spec.Storage.Volumes = append(
				aeroCluster.Spec.Storage.Volumes, getStorageVolumeForAerospike(sc1Name, sc1Path))

			conf := getSCNamespaceConfig(sc1Name, sc1Path)
			conf["strong-consistency"] = false
			conf["mrt-duration"] = 10

			racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"namespaces": []interface{}{conf},
				},
			}
			aeroCluster.Spec.RackConfig.Racks = racks

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).To(HaveOccurred())
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

	ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("Set roster blockList")

	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())
	// Keep IDs in lowercase
	aeroCluster.Spec.RosterNodeBlockList = []string{"1A0", "2A7"}
	err = updateCluster(k8sClient, ctx, aeroCluster)
	Expect(err).ToNot(HaveOccurred())
	// 1 - 1, 1,
	// 2 - 1,
	ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("ScaleDown")

	err = scaleDownClusterTest(k8sClient, ctx, clusterNamespacedName, 2)
	Expect(err).ToNot(HaveOccurred())

	ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("RollingRestart")

	err = rollingRestartClusterTest(logger, k8sClient, ctx, clusterNamespacedName)
	Expect(err).ToNot(HaveOccurred())

	ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)

	By("Upgrade/Downgrade")
	// don't change image, it upgrades, check old version
	err = upgradeClusterTest(
		k8sClient, ctx, clusterNamespacedName, nextImage,
	)
	Expect(err).ToNot(HaveOccurred())

	ValidateRoster(k8sClient, ctx, clusterNamespacedName, scNamespace)
}

func getSCAndNonSCAerospikeConfig() *asdbv1.AerospikeConfigSpec {
	conf := getSCAerospikeConfig()
	nonSCConf := map[string]interface{}{
		"name":               "bar",
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type":      "memory",
			"data-size": 1073741824,
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

func compareRoster(hostConn *deployment.HostConn, expectedRoster string,
	aeroCluster *asdbv1.AerospikeCluster) (isEqual bool, currentRoster string, err error) {
	rosterNodesMap, err := getRoster(hostConn, getClientPolicy(aeroCluster, k8sClient), aeroCluster.Namespace)
	if err != nil {
		return false, "", err
	}

	// Roster is in uppercase, whereas nodeID is in lower case in server. Keep it in mind when comparing list
	currentRoster = rosterNodesMap["roster"]

	return currentRoster == expectedRoster, currentRoster, nil
}
