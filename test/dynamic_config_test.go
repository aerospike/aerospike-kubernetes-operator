package test

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
	ctrl "sigs.k8s.io/controller-runtime"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/configschema"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/info"
)

type podID struct {
	podUID string
	asdPID string
}

var configWithMaxDefaultVal = mapset.NewSet("info-max-ms", "flush-max-ms")

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

		Context(
			"When doing complete dynamic config change", func() {

				clusterName := "aerocluster"
				clusterNamespacedName := getNamespacedName(
					clusterName, "test",
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

						aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
							getSCNamespaceConfigWithSet("test", "/test/dev/xvdf"),
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

						pods, err := getPodList(aeroCluster, k8sClient)
						Expect(err).ToNot(HaveOccurred())

						podPIDMap, err := getPodIDs(ctx, aeroCluster)
						Expect(err).ToNot(HaveOccurred())

						pod := aeroCluster.Status.Pods[pods.Items[0].Name]

						host, err := createHost(&pod)
						Expect(err).ToNot(HaveOccurred())

						asinfo := info.NewAsInfo(
							logger, host, getClientPolicy(aeroCluster, k8sClient),
						)

						dynamic, err := asconfig.GetDynamic("7.0.0")
						Expect(err).ToNot(HaveOccurred())

						serverConf, err := asconfig.GenerateConf(logger, asinfo, false)
						Expect(err).ToNot(HaveOccurred())

						server, err := asconfig.NewMapAsConfig(logger, serverConf.Conf)
						Expect(err).ToNot(HaveOccurred())

						flatServer := server.GetFlatMap()

						spec, err := asconfig.NewMapAsConfig(logger, aeroCluster.Spec.AerospikeConfig.Value)
						Expect(err).ToNot(HaveOccurred())

						flatSpec := spec.GetFlatMap()

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
						err = validateXDRContextDynamically(ctx, flatServer, flatSpec, aeroCluster, dynamic)
						Expect(err).ToNot(HaveOccurred())

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

func updateValue(val interface{}) interface{} {
	switch val2 := val.(type) {
	case []string:

	case string:

	case bool:

	case int:
		return val2 + 1
	case uint64:
		return val2 + 1
	case int64:
		return val2 + 1
	case float64:
		return val2 + 1

	case lib.Stats:

	default:
		return nil
	}

	return nil
}

func validateServiceContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec
	ignoredConf := mapset.NewSet("cluster-name")

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "service" {
			continue
		}

		tokens := strings.Split(confKey, ".")

		if dyn := dynamic.Contains(asconfig.GetFlatKey(tokens)); dyn && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v := updateValue(val)
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

		if dyn := dynamic.Contains(asconfig.GetFlatKey(tokens)); dyn && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v := updateValue(val)
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
	ignoredConf := mapset.NewSet("rack-id", "default-ttl")

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "namespaces" {
			continue
		}

		tokens := strings.Split(confKey, ".")
		if dyn := dynamic.Contains(asconfig.GetFlatKey(tokens)); dyn && !ignoredConf.Contains(asconfig.BaseKey(confKey)) {
			v := updateValue(val)
			if v != nil {
				if configWithMaxDefaultVal.Contains(asconfig.BaseKey(confKey)) {
					v = v.(int64) - 1
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
		if dyn := dynamic.Contains(asconfig.GetFlatKey(tokens)); dyn {
			v := updateValue(val)
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

func validateXDRContextDynamically(
	ctx goctx.Context, flatServer, flatSpec *asconfig.Conf,
	aeroCluster *asdbv1.AerospikeCluster, dynamic mapset.Set[string],
) error {
	newSpec := *flatSpec

	for confKey, val := range *flatServer {
		if asconfig.ContextKey(confKey) != "xdr" {
			continue
		}

		tokens := strings.Split(confKey, ".")
		if dyn := dynamic.Contains(asconfig.GetFlatKey(tokens)); dyn {
			v := updateValue(val)
			if v != nil {
				switch asconfig.BaseKey(confKey) {
				case "max-throughput":
					v = v.(int64) + 99
				case "transaction-queue-limit":
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
