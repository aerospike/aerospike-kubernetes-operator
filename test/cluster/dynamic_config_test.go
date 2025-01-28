package cluster

import (
	goctx "context"
	"fmt"
	"os"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/configschema"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/info"
)

type podID struct {
	podUID string
	asdPID string
}

const clName = "dynamic-config-test"

var (
	configWithMaxDefaultVal = mapset.NewSet("info-max-ms", "flush-max-ms")
	configWithPow2Val       = mapset.NewSet("flush-size", "transaction-queue-limit")
	configWithMul100Val     = mapset.NewSet("max-throughput")
)

var _ = Describe(
	"DynamicConfig", func() {

		ctx := goctx.Background()
		var clusterNamespacedName = getNamespacedName(
			clName, namespace,
		)

		Context(
			"When doing valid operations", func() {

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
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
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

						By("Fetch and verify dynamic configs")

						pod := aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						// Fetch and verify service section config
						conf, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"service", &pod)
						Expect(err).ToNot(HaveOccurred())

						cv, ok := conf["proto-fd-max"]
						Expect(ok).ToNot(BeFalse())

						Expect(cv).To(Equal(int64(18000)))

						// Fetch and verify security section config
						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						reportDataOp, ok := conf["log.report-data-op[0]"].(string)
						Expect(ok).ToNot(BeFalse())

						Expect(reportDataOp).To(Equal("test"))

						// Fetch and verify xdr section config
						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"xdr", &pod)
						Expect(err).ToNot(HaveOccurred())

						Expect(conf["dcs"]).To(HaveLen(2))

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

						By("Fetch and verify dynamic configs")

						pod = aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						// Fetch and verify service section config
						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"service", &pod)
						Expect(err).ToNot(HaveOccurred())
						cv, ok = conf["proto-fd-max"]
						Expect(ok).ToNot(BeFalse())

						Expect(cv).To(Equal(int64(15000)))

						// Fetch and verify security section config
						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						_, ok = conf["log.report-data-op[0]"].(string)
						Expect(ok).ToNot(BeTrue())

						// Fetch and verify xdr section config
						conf, err = getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"xdr", &pod)
						Expect(err).ToNot(HaveOccurred())

						Expect(conf["dcs"]).To(HaveLen(1))

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

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Fetch and verify static configs")

						pod := aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						conf, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"security", &pod)
						Expect(err).ToNot(HaveOccurred())

						enableQuotas, ok := conf["enable-quotas"].(bool)
						Expect(ok).ToNot(BeFalse())

						Expect(enableQuotas).To(BeTrue())

						By("Verify warm restarts in Pods")

						validateServerRestart(ctx, aeroCluster, podPIDMap, true)
					},
				)

				It(
					"Should update config statically by disabling dynamic update feature", func() {

						By("Modify dynamic config statically")

						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Disable dynamic config update and set config
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(false)
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 19000

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Fetch and verify static configs")

						pod := aeroCluster.Status.Pods["dynamic-config-test-0-0"]

						conf, err := getAerospikeConfigFromNode(logger, k8sClient, ctx, clusterNamespacedName,
							"service", &pod)
						Expect(err).ToNot(HaveOccurred())

						cv, ok := conf["proto-fd-max"]
						Expect(ok).ToNot(BeFalse())

						Expect(cv).To(Equal(int64(19000)))

						By("Verify warm restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, true)
					},
				)
			},
		)

		Context(
			"When doing invalid operations", func() {

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
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)
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
					"Should fail dynamic config update fully for invalid config", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Failure:
						// Update invalid config value
						// This change will lead to dynamic config update failure.
						// In case of full failure, will not fall back to rolling restart
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 9999999

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*1)
						Expect(err).To(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Recovery:
						// Update valid config value
						// This change will lead to dynamic config update success.
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 15000

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// As there was no full config update failure,
						// expectation is that pods will not be restarted.
						By("Verify cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)
					},
				)

				It(
					"Should fail dynamic config update partially for invalid config", func() {
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						// Failure:
						// Update multiple invalid config values
						// This change will lead to dynamic config update failure.
						// Assuming it will fall back to rolling restart in case of partial failure.
						// Which leads to pod failures.
						aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 9999999

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

						err = updateClusterWithTO(k8sClient, ctx, aeroCluster, time.Minute*1)
						Expect(err).To(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						// Recovery:
						// Update valid config value
						// This change will lead to static config update success.
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

		Context(
			"When doing complete dynamic config change", func() {

				BeforeEach(
					func() {
						// Create a 2 node cluster
						aeroCluster := getAerospikeClusterSpecWithLDAP(
							clusterNamespacedName,
						)
						aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes,
							asdbv1.VolumeSpec{
								Name: "bar",
								Source: asdbv1.VolumeSource{
									PersistentVolume: &asdbv1.PersistentVolumeSpec{
										Size:         resource.MustParse("1Gi"),
										StorageClass: storageClass,
										VolumeMode:   v1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
									Path: "/test/dev/xvdf1",
								},
							},
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

						aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
							getSCNamespaceConfigWithSet("test", "/test/dev/xvdf"),
							getNonSCNamespaceConfig("bar", "/test/dev/xvdf1"),
						}

						aeroCluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})["log"] = map[string]interface{}{
							"report-data-op-role": []string{"read"},
							"report-data-op-user": []string{"admin2"},
						}

						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)

						admin2 := asdbv1.AerospikeUserSpec{
							Name:       "admin2",
							SecretName: test.AuthSecretName,
							Roles: []string{
								"sys-admin",
								"user-admin",
								"read-write",
							},
						}

						aeroCluster.Spec.AerospikeAccessControl.Users = append(aeroCluster.Spec.AerospikeAccessControl.Users, admin2)

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

						By("Modify all dynamic config fields")
						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						schemaMap, err := configschema.NewSchemaMap()
						if err != nil {
							os.Exit(1)
						}

						schemaMapLogger := ctrl.Log.WithName("schema-map")
						asconfig.InitFromMap(schemaMapLogger, schemaMap)

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						dynamic, err := asconfig.GetDynamic(latestSchemaVersion)
						Expect(err).ToNot(HaveOccurred())

						flatServer, flatSpec, err := getAerospikeConfigFromNodeAndSpec(aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Verify Service Context configs dynamically")
						err = validateServiceContextDynamically(ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

						By("Verify Network Context configs dynamically")
						err = validateNetworkContextDynamically(ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

						By("Verify Namespace Context configs dynamically")
						err = validateNamespaceContextDynamically(ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

						By("Verify Security Context configs dynamically")
						err = validateSecurityContextDynamically(ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)

						By("Verify XDR Context configs dynamically")
						err = validateXDRContextDynamically(clusterNamespacedName,
							ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

						aeroCluster, err = getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						By("Verify no warm/cold restarts in Pods")
						validateServerRestart(ctx, aeroCluster, podPIDMap, false)
					},
				)
			},
		)

		Context(
			"When changing fields those need recluster", func() {
				BeforeEach(
					func() {
						// Create a 4 node cluster
						aeroCluster := createNonSCDummyAerospikeCluster(
							clusterNamespacedName, 4,
						)
						aeroCluster.Spec.Storage.Volumes = append(
							aeroCluster.Spec.Storage.Volumes,
							asdbv1.VolumeSpec{
								Name: "ns1",
								Source: asdbv1.VolumeSource{
									PersistentVolume: &asdbv1.PersistentVolumeSpec{
										Size:         resource.MustParse("1Gi"),
										StorageClass: storageClass,
										VolumeMode:   v1.PersistentVolumeBlock,
									},
								},
								Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
									Path: "/test/dev/xvdf1",
								},
							},
						)
						aeroCluster.Spec.EnableDynamicConfigUpdate = ptr.To(true)

						aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks,
							asdbv1.Rack{
								ID: 1,
							},
							asdbv1.Rack{
								ID: 2,
							})
						aeroCluster.Spec.RackConfig.Namespaces = []string{
							"test",
							"test1",
						}
						nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
						nsList = append(nsList, getSCNamespaceConfig("test1", "/test/dev/xvdf1"))
						aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList
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
					"Should update active-rack dynamically", func() {

						By("Modify dynamic config by adding fields")

						aeroCluster, err := getCluster(
							k8sClient, ctx, clusterNamespacedName,
						)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
						nsList[0].(map[string]interface{})["active-rack"] = 1
						nsList[1].(map[string]interface{})["active-rack"] = 2

						err = updateCluster(k8sClient, ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						By("Fetch and verify dynamic configs")

						pod := aeroCluster.Status.Pods["dynamic-config-test-1-0"]

						info, err := requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test", &pod)
						Expect(err).ToNot(HaveOccurred())

						confs := strings.Split(info["namespace/test"], ";")
						for _, conf := range confs {
							if strings.Contains(conf, "effective_active_rack") {
								keyValue := strings.Split(conf, "=")
								Expect(keyValue[1]).To(Equal("1"))
							}
						}

						info, err = requestInfoFromNode(logger, k8sClient, ctx, clusterNamespacedName, "namespace/test1", &pod)
						Expect(err).ToNot(HaveOccurred())

						confs = strings.Split(info["namespace/test1"], ";")
						for _, conf := range confs {
							if strings.Contains(conf, "effective_active_rack") {
								keyValue := strings.Split(conf, "=")
								Expect(keyValue[1]).To(Equal("2"))
							}
						}

						By("Verify no warm/cold restarts in Pods")

						validateServerRestart(ctx, aeroCluster, podPIDMap, false)
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
			utils.GetNamespacedName(pod), asdbv1.AerospikeServerContainerName, cmd, k8sClientSet,
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

func updateValue(val interface{}, confKey string) (interface{}, error) {
	switch val2 := val.(type) {
	case []string:
		if v, ok := sliceConfTypeVal[confKey]; ok {
			return v, nil
		}
	case string:
		if v, ok := stringConfTypeVal[confKey]; ok {
			return v, nil
		}
	case bool:
		return !val2, nil
	case int:
		return val2 + 1, nil
	case uint64:
		return val2 + 1, nil
	case int64:
		return val2 + 1, nil
	case float64:
		return val2 + 1, nil

	case lib.Stats:
		pkgLog.Info("got lib.stats type key", confKey)
	default:
		return nil, fmt.Errorf("format not supported")
	}

	return nil, nil
}

var stringConfTypeVal = map[string]string{
	"network.heartbeat.protocol":              "v3",
	"namespaces._.storage-engine.compression": "snappy",
}

var sliceConfTypeVal = map[string][]string{
	"security.log.report-data-op":        {"test"},
	"security.log.report-data-op-role":   {"sys-admin"},
	"security.log.report-data-op-user":   {"admin"},
	"xdr.dcs._.namespaces._.ship-sets":   {"testset"},
	"xdr.dcs._.namespaces._.ignore-sets": {"testset"},
	"xdr.dcs._.namespaces._.ignore-bins": {"testbin"},
}

func validateServiceContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("cluster-name", "microsecond-histograms", "advertise-ipv6")

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "service" {
			continue
		}

		tokens := strings.Split(confKey, ".")

		if dynamic.Contains(asconfig.GetFlatKey(tokens)) && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				if configWithMaxDefaultVal.Contains(asconfig.BaseKey(confKey)) {
					v = v.(uint64) - 1
				}

				if asconfig.BaseKey(confKey) == "proto-fd-idle-ms" {
					v = 70000
				}

				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["service"] = lib.DeepCopy(newMap["service"])

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateNetworkContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("connect-timeout-ms")

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "network" {
			continue
		}

		tokens := strings.Split(confKey, ".")

		if dynamic.Contains(asconfig.GetFlatKey(tokens)) && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["network"] = lib.DeepCopy(newMap["network"])

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateNamespaceContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("rack-id", "default-ttl", "disable-write-dup-res",
		"disallow-expunge", "conflict-resolution-policy", "compression-acceleration", "compression-level",
		"strong-consistency-allow-expunge")

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "namespaces" {
			continue
		}

		tokens := strings.Split(confKey, ".")
		if dynamic.Contains(asconfig.GetFlatKey(tokens)) && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				switch {
				case configWithMaxDefaultVal.Contains(asconfig.BaseKey(confKey)):
					v = v.(int64) - 1
				case configWithPow2Val.Contains(asconfig.BaseKey(confKey)):
					v = (v.(int64) - 1) * 2
				}

				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = lib.DeepCopy(newMap["namespaces"])

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateSecurityContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "security" {
			continue
		}

		tokens := strings.Split(confKey, ".")
		if dynamic.Contains(asconfig.GetFlatKey(tokens)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["security"] = lib.DeepCopy(newMap["security"])

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateXDRContextDynamically(clusterNamespacedName types.NamespacedName,
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	xdrNSFields := make(asconfig.Conf)
	dcFields := make(asconfig.Conf)

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "xdr" {
			continue
		}

		tokens := strings.Split(confKey, ".")
		if dynamic.Contains(asconfig.GetFlatKey(tokens)) {
			if len(tokens) < 3 || tokens[len(tokens)-3] == "namespaces" {
				xdrNSFields[confKey] = val
			} else {
				dcFields[confKey] = val
			}
		}
	}

	if err := validateXDRDCFieldsDynamically(ctx, &dcFields, flatSpec, aeroCluster); err != nil {
		return err
	}

	aeroCluster, err := getCluster(
		k8sClient, ctx, clusterNamespacedName,
	)
	if err != nil {
		return err
	}

	return validateXDRNSFieldsDynamically(ctx, &xdrNSFields, flatSpec, aeroCluster)
}

func validateXDRNSFieldsDynamically(ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("ship-bin-luts")

	for confKey, val := range *flatServer {
		tokens := strings.Split(confKey, ".")
		if !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				switch {
				case configWithMul100Val.Contains(asconfig.BaseKey(confKey)):
					v = v.(int64) + 99
				case configWithPow2Val.Contains(asconfig.BaseKey(confKey)):
					v = (v.(int64) - 1) * 2
				}

				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["xdr"] = lib.DeepCopy(newMap["xdr"])

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateXDRDCFieldsDynamically(ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("connector")

	for confKey, val := range *flatServer {
		tokens := strings.Split(confKey, ".")
		if !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v, err := updateValue(val, asconfig.GetFlatKey(tokens))
			if err != nil {
				return err
			}

			if v != nil {
				newSpec[confKey] = v
			}
		}
	}

	newConf := asconfig.New(logger, &newSpec)
	newMap := *newConf.ToMap()

	aeroCluster.Spec.AerospikeConfig.Value["xdr"] = lib.DeepCopy(newMap["xdr"])
	dcs := aeroCluster.Spec.AerospikeConfig.Value["xdr"].(lib.Stats)["dcs"].([]lib.Stats)
	delete(dcs[0], "namespaces")
	delete(dcs[0], "node-address-ports")
	aeroCluster.Spec.AerospikeConfig.Value["xdr"].(lib.Stats)["dcs"] = dcs

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func getAerospikeConfigFromNodeAndSpec(aeroCluster *asdbv1.AerospikeCluster) (flatServer, flatSpec *asconfig.Conf,
	err error) {
	pods, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil, nil, err
	}

	pod := aeroCluster.Status.Pods[pods.Items[0].Name]

	host, err := createHost(&pod)
	if err != nil {
		return nil, nil, err
	}

	asinfo := info.NewAsInfo(
		logger, host, getClientPolicy(aeroCluster, k8sClient),
	)

	serverConf, err := asconfig.GenerateConf(logger, asinfo, false)
	if err != nil {
		return nil, nil, err
	}

	server, err := asconfig.NewMapAsConfig(logger, serverConf.Conf)
	if err != nil {
		return nil, nil, err
	}

	spec, err := asconfig.NewMapAsConfig(logger, aeroCluster.Spec.AerospikeConfig.Value)
	if err != nil {
		return nil, nil, err
	}

	return server.GetFlatMap(), spec.GetFlatMap(), nil
}
