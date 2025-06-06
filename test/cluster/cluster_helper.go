package cluster

import (
	goctx "context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
)

const (
	baseImage           = "aerospike/aerospike-server-enterprise"
	nextServerVersion   = "8.0.0.2_1"
	latestServerVersion = "8.0.0.2"
	invalidVersion      = "3.0.0.4"

	post6Version = "7.0.0.0"
	version6     = "6.0.0.5"

	latestSchemaVersion = "8.0.0"
)

var (
	storageClass = "ssd"
	namespace    = "test"
	pkgLog       = ctrl.Log.WithName("aerospikecluster")
)

const aerospikeConfigSecret string = "aerospike-config-secret" //nolint:gosec // for testing

const serviceTLSPort = 4333
const serviceNonTLSPort = 3000

// constants for writing data to aerospike
const (
	setName  = "test"
	key      = "key1"
	binName  = "testBin"
	binValue = "binValue"
)

var aerospikeVolumeInitMethodDeleteFiles = asdbv1.AerospikeVolumeMethodDeleteFiles

var (
	retryInterval      = time.Second * 30
	cascadeDeleteFalse = false
	cascadeDeleteTrue  = true
	logger             = logr.Discard()
	nextImage          = fmt.Sprintf("%s:%s", baseImage, nextServerVersion)
	latestImage        = fmt.Sprintf("%s:%s", baseImage, latestServerVersion)
	invalidImage       = fmt.Sprintf("%s:%s", baseImage, invalidVersion)

	// Storage wipe test
	post6Image    = fmt.Sprintf("%s:%s", baseImage, post6Version)
	version6Image = fmt.Sprintf("%s:%s", baseImage, version6)
)

func rollingRestartClusterByEnablingTLS(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change tls conf
	aeroCluster.Spec.OperatorClientCertSpec = &asdbv1.AerospikeOperatorClientCertSpec{
		AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
			SecretCertSource: &asdbv1.AerospikeSecretCertSource{
				SecretName:         test.AerospikeSecretName,
				CaCertsFilename:    "cacert.pem",
				ClientCertFilename: "svc_cluster_chain.pem",
				ClientKeyFilename:  "svc_key.pem",
			},
		},
	}
	aeroCluster.Spec.AerospikeConfig.Value["network"] = getNetworkTLSConfig()

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceTLSPort, serviceNonTLSPort})
	if err != nil {
		return err
	}

	// Port should be changed to service tls-port
	err = validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceTLSPort)
	if err != nil {
		return err
	}

	network := aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})
	serviceNetwork := network[asdbv1.ServicePortName].(map[string]interface{})
	fabricNetwork := network[asdbv1.FabricPortName].(map[string]interface{})
	heartbeartNetwork := network[asdbv1.HeartbeatPortName].(map[string]interface{})

	delete(serviceNetwork, "port")
	delete(fabricNetwork, "port")
	delete(heartbeartNetwork, "port")

	network[asdbv1.ServicePortName] = serviceNetwork
	network[asdbv1.FabricPortName] = fabricNetwork
	network[asdbv1.HeartbeatPortName] = heartbeartNetwork
	aeroCluster.Spec.AerospikeConfig.Value["network"] = network

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	return validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceTLSPort)
}

func rollingRestartClusterByDisablingTLS(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.AerospikeConfig.Value["network"] = getNetworkTLSConfig()

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	err = validateServiceUpdate(k8sClient, ctx, clusterNamespacedName, []int32{serviceTLSPort, serviceNonTLSPort})
	if err != nil {
		return err
	}

	// port should remain same i.e. service tls-port
	err = validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceTLSPort)
	if err != nil {
		return err
	}

	aeroCluster.Spec.AerospikeConfig.Value["network"] = getNetworkConfig()
	aeroCluster.Spec.OperatorClientCertSpec = nil

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Port should be updated to service non-tls port
	return validateReadinessProbe(ctx, k8sClient, aeroCluster, serviceNonTLSPort)
}

func scaleUpClusterTestWithNSDeviceHandling(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, increaseBy int32,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size += increaseBy
	namespaceConfig := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1].(map[string]interface{})
	oldDeviceList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]
	namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = []interface{}{"/test/dev/dynamicns"}
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1] = namespaceConfig

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		// DirtyVolumes are not populated for scaled-up pods.
		if podName == aeroCluster.Name+"-0-3" {
			continue
		}

		if !contains(aeroCluster.Status.Pods[podName].DirtyVolumes, "dynamicns1") {
			return fmt.Errorf(
				"removed volume dynamicns1 missing from dirtyVolumes %v", aeroCluster.Status.Pods[podName].DirtyVolumes,
			)
		}
	}

	aeroCluster.Spec.Size += increaseBy
	namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = oldDeviceList
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1] = namespaceConfig

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		if contains(aeroCluster.Status.Pods[podName].DirtyVolumes, "dynamicns1") {
			return fmt.Errorf(
				"in-use volume dynamicns1 is present in dirtyVolumes %v", aeroCluster.Status.Pods[podName].DirtyVolumes,
			)
		}
	}

	return nil
}

func scaleUpClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, increaseBy int32,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size += increaseBy

	return updateClusterWithTO(k8sClient, ctx, aeroCluster, getTimeout(increaseBy))
}

func scaleDownClusterTestWithNSDeviceHandling(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, decreaseBy int32,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size -= decreaseBy
	namespaceConfig := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1].(map[string]interface{})
	oldDeviceList := namespaceConfig["storage-engine"].(map[string]interface{})["devices"]
	namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = []interface{}{"/test/dev/dynamicns"}
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1] = namespaceConfig

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		if !contains(aeroCluster.Status.Pods[podName].DirtyVolumes, "dynamicns1") {
			return fmt.Errorf(
				"removed volume dynamicns1 missing from dirtyVolumes %v", aeroCluster.Status.Pods[podName].DirtyVolumes,
			)
		}
	}

	aeroCluster.Spec.Size -= decreaseBy
	namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = oldDeviceList
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1] = namespaceConfig

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		if contains(aeroCluster.Status.Pods[podName].DirtyVolumes, "dynamicns1") {
			return fmt.Errorf(
				"in-use volume dynamicns1 is present in dirtyVolumes %v", aeroCluster.Status.Pods[podName].DirtyVolumes,
			)
		}
	}

	return nil
}

func scaleDownClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, decreaseBy int32,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size -= decreaseBy

	return updateClusterWithTO(k8sClient, ctx, aeroCluster, getTimeout(decreaseBy))
}

func rollingRestartClusterTest(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change config
	if _, ok := aeroCluster.Spec.AerospikeConfig.Value["service"]; !ok {
		aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{}
	}

	aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["indent-allocations"] = true

	err = updateCluster(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	// Verify that the change has been applied on the cluster.
	return validateAerospikeConfigServiceClusterUpdate(
		log, k8sClient, ctx, clusterNamespacedName, []string{"indent-allocations"},
	)
}

func rollingRestartClusterByUpdatingNamespaceStorageTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace storage-engine
	namespaceConfig := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})
	namespaceConfig["storage-engine"].(map[string]interface{})["filesize"] = 2000000000
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0] = namespaceConfig

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func rollingRestartClusterByReusingNamespaceStorageTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, dynamicNs map[string]interface{},
) error {
	err := rollingRestartClusterByAddingNamespaceDynamicallyTest(
		k8sClient, ctx, dynamicNs, clusterNamespacedName,
	)
	if err != nil {
		return err
	}

	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace storage-engine
	namespaceConfig := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1].(map[string]interface{})
	namespaceConfig["storage-engine"].(map[string]interface{})["devices"] = []interface{}{"/test/dev/dynamicns"}
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[1] = namespaceConfig

	dynamicNs1 := getNonSCNamespaceConfig("dynamicns1", "/test/dev/dynamicns1")
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = append(nsList, dynamicNs1)
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	return k8sClient.Update(ctx, aeroCluster)
}

func rollingRestartClusterByAddingNamespaceDynamicallyTest(
	k8sClient client.Client, ctx goctx.Context,
	dynamicNs map[string]interface{},
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace list
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = append(nsList, dynamicNs)
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func rollingRestartClusterByRemovingNamespaceDynamicallyTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace list
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = nsList[:len(nsList)-1]
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateServiceUpdate(k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, ports []int32) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	serviceNamespacesNames := make([]types.NamespacedName, 0)
	for podName := range aeroCluster.Status.Pods {
		serviceNamespacesNames = append(serviceNamespacesNames,
			types.NamespacedName{Name: podName, Namespace: clusterNamespacedName.Namespace})
	}

	serviceNamespacesNames = append(serviceNamespacesNames, clusterNamespacedName)

	for _, serviceNamespacesName := range serviceNamespacesNames {
		service := &corev1.Service{}

		err = k8sClient.Get(ctx, serviceNamespacesName, service)
		if err != nil {
			return err
		}

		portSet := sets.NewInt32(ports...)

		for _, p := range service.Spec.Ports {
			if portSet.Has(p.Port) {
				portSet.Delete(p.Port)
			}
		}

		if portSet.Len() > 0 {
			return fmt.Errorf("service %s port not configured correctly", serviceNamespacesName.Name)
		}
	}

	return nil
}

func validateAerospikeConfigServiceClusterUpdate(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, updatedKeys []string,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		pod := aeroCluster.Status.Pods[podName]
		// TODO:
		// We may need to check for all keys in aerospikeConfig in rack
		// but we know that we are changing for service only for now
		host, err := createHost(&pod)
		if err != nil {
			return err
		}

		asinfo := info.NewAsInfo(
			log, host, getClientPolicy(aeroCluster, k8sClient),
		)

		confs, err := getAsConfig(asinfo, "service")
		if err != nil {
			return err
		}

		svcConfs := confs["service"].(lib.Stats)

		inputSvcConf := aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})
		for _, k := range updatedKeys {
			v, ok := inputSvcConf[k]
			if !ok {
				return fmt.Errorf(
					"config %s missing in aerospikeConfig %v", k, svcConfs,
				)
			}

			cv, ok := svcConfs[k]
			if !ok {
				return fmt.Errorf(
					"config %s missing in aerospike config asinfo %v", k,
					svcConfs,
				)
			}

			strV := fmt.Sprintf("%v", v)
			strCv := fmt.Sprintf("%v", cv)

			if strV != strCv {
				return fmt.Errorf(
					"config %s mismatch with config. got %v:%T, want %v:%T, aerospikeConfig %v",
					k, cv, cv, v, v, svcConfs,
				)
			}
		}
	}

	return nil
}

func validateMigrateFillDelay(
	ctx goctx.Context, k8sClient client.Client, log logr.Logger,
	clusterNamespacedName types.NamespacedName, expectedMigFillDelay int64,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Use any pod for confirmation. Using first pod for the confirmation
	firstPodName := aeroCluster.Name + "-" + strconv.Itoa(aeroCluster.Spec.RackConfig.Racks[0].ID) + "-" + strconv.Itoa(0)

	firstPod, exists := aeroCluster.Status.Pods[firstPodName]
	if !exists {
		return fmt.Errorf("pod %s missing from the status", firstPodName)
	}

	host, err := createHost(&firstPod)
	if err != nil {
		return err
	}

	asinfo := info.NewAsInfo(log, host, getClientPolicy(aeroCluster, k8sClient))
	err = wait.PollUntilContextTimeout(ctx,
		retryInterval, getTimeout(1), true, func(goctx.Context) (done bool, err error) {
			confs, err := getAsConfig(asinfo, "service")
			if err != nil {
				return false, err
			}

			svcConfs := confs["service"].(lib.Stats)

			current, exists := svcConfs["migrate-fill-delay"]
			if !exists {
				return false, fmt.Errorf("migrate-fill-delay missing from the Aerospike Service config")
			}

			if current.(int64) != expectedMigFillDelay {
				pkgLog.Info("Waiting for migrate-fill-delay to be", "value", expectedMigFillDelay)
				return false, nil
			}

			pkgLog.Info("Found expected migrate-fill-delay", "value", expectedMigFillDelay)

			return true, nil
		},
	)

	return err
}

// validate readiness port
func validateReadinessProbe(ctx goctx.Context, k8sClient client.Client, aeroCluster *asdbv1.AerospikeCluster,
	requiredPort int) error {
	for podName := range aeroCluster.Status.Pods {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: aeroCluster.Namespace,
			Name:      podName,
		}, pod); err != nil {
			return err
		}

		for idx := range pod.Spec.Containers {
			container := &pod.Spec.Containers[idx]

			if container.Name != asdbv1.AerospikeServerContainerName {
				continue
			}

			if !reflect.DeepEqual(container.ReadinessProbe.TCPSocket.Port, intstr.FromInt(requiredPort)) {
				return fmt.Errorf("readiness tcp port mismatch, expected: %v, found: %v",
					intstr.FromInt(requiredPort), container.ReadinessProbe.TCPSocket.Port)
			}
		}
	}

	return nil
}

func validateDirtyVolumes(
	ctx goctx.Context, k8sClient client.Client,
	clusterNamespacedName types.NamespacedName, expectedVolumes []string,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for podName := range aeroCluster.Status.Pods {
		if !reflect.DeepEqual(aeroCluster.Status.Pods[podName].DirtyVolumes, expectedVolumes) {
			return fmt.Errorf(
				"dirtyVolumes mismatch, expected: %v, found %v", expectedVolumes,
				aeroCluster.Status.Pods[podName].DirtyVolumes,
			)
		}
	}

	return nil
}

func upgradeClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, image string,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}
	// Change config
	if err := UpdateClusterImage(aeroCluster, image); err != nil {
		return err
	}

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func getCluster(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*asdbv1.AerospikeCluster, error) {
	aeroCluster := &asdbv1.AerospikeCluster{}

	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

// GetCluster is the public variant of getCluster
// Remove this when getCluster will be made public
func GetCluster(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*asdbv1.AerospikeCluster, error) {
	aeroCluster := &asdbv1.AerospikeCluster{}

	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

func getClusterIfExists(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*asdbv1.AerospikeCluster, error) {
	aeroCluster := &asdbv1.AerospikeCluster{}

	if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("error getting cluster: %v", err)
	}

	return aeroCluster, nil
}

func DeleteCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
) error {
	if err := k8sClient.Delete(ctx, aeroCluster); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// Wait for all pod to get deleted.
	// TODO: Maybe add these checks in cluster delete itself.
	// time.Sleep(time.Second * 12)

	clusterNamespacedName := test.GetNamespacedName(
		aeroCluster.Name, aeroCluster.Namespace,
	)

	for {
		existing, err := getClusterIfExists(
			k8sClient, ctx, clusterNamespacedName,
		)
		if err != nil {
			return err
		}
		// Pods still may exist in terminating state for some time even if CR is deleted. Keeping them breaks some
		// tests which use the same cluster name and run one after another. Thus, waiting pods to disappear.
		allClustersPods, err := getClusterPodList(
			k8sClient, ctx, aeroCluster,
		)
		if err != nil {
			return err
		}

		if existing == nil && len(allClustersPods.Items) == 0 {
			break
		}

		time.Sleep(time.Second)
	}

	// Wait for all removed PVCs to be terminated.
	for {
		newPVCList, err := getAeroClusterPVCList(aeroCluster, k8sClient)
		if err != nil {
			return fmt.Errorf("error getting PVCs: %v", err)
		}

		pending := false

		for pvcIndex := range newPVCList {
			if utils.IsPVCTerminating(&newPVCList[pvcIndex]) {
				pending = true
				break
			}
		}

		if !pending {
			break
		}

		time.Sleep(time.Second)
	}

	return nil
}

func DeployCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
) error {
	return deployClusterWithTO(
		k8sClient, ctx, aeroCluster, retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func deployClusterWithTO(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
	retryInterval, timeout time.Duration,
) error {
	// Use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, aeroCluster)
	if err != nil {
		return err
	}
	// Wait for aerocluster to reach the desired cluster size.
	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		timeout, []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)
}

func updateSTS(
	k8sClient client.Client, ctx goctx.Context,
	sts *appsv1.StatefulSet,
) error {
	err := k8sClient.Update(ctx, sts)
	return err
}

func updateCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
) error {
	return updateClusterWithTO(k8sClient, ctx, aeroCluster, getTimeout(aeroCluster.Spec.Size))
}

func updateClusterWithExpectedPhases(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster, expectedPhases []asdbv1.AerospikeClusterPhase,
) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size), expectedPhases,
	)
}

func updateClusterWithTO(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster, timeout time.Duration,
) error {
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := getCluster(k8sClient, ctx, utils.GetNamespacedName(aeroCluster))
		if err != nil {
			return err
		}

		current.Spec = *lib.DeepCopy(&aeroCluster.Spec).(*asdbv1.AerospikeClusterSpec)
		current.Labels = aeroCluster.Labels
		current.Annotations = aeroCluster.Annotations

		return k8sClient.Update(ctx, current)
	}); err != nil {
		return err
	}

	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		timeout, []asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)
}

func updateClusterWithNoWait(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := getCluster(k8sClient, ctx, utils.GetNamespacedName(aeroCluster))
		if err != nil {
			return err
		}

		current.Spec = *lib.DeepCopy(&aeroCluster.Spec).(*asdbv1.AerospikeClusterSpec)
		current.Labels = aeroCluster.Labels
		current.Annotations = aeroCluster.Annotations

		return k8sClient.Update(ctx, current)
	})
}

func getClusterPodList(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster,
) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := k8sClient.List(ctx, podList, listOps); err != nil {
		return nil, err
	}

	return podList, nil
}

// feature-key file needed
func createAerospikeClusterPost570(
	clusterNamespacedName types.NamespacedName, size int32, image string,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  size,
			Image: image,
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: test.AuthSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},

			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: ptr.To(true),
			},
			OperatorClientCertSpec: &asdbv1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1.AerospikeSecretCertSource{
						SecretName:         test.AerospikeSecretName,
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{

					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
					},
					"security": map[string]interface{}{},
					"network":  getNetworkTLSConfig(),
					"namespaces": []interface{}{
						getNonSCNamespaceConfigPre700("test", "/test/dev/xvdf"),
					},
				},
			},
		},
	}
	aeroCluster.Spec.Storage = getBasicStorageSpecObject()
	aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
	aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue

	return aeroCluster
}

func createAerospikeClusterPost640(
	clusterNamespacedName types.NamespacedName, size int32, image string,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := createAerospikeClusterPost570(clusterNamespacedName, size, image)
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		getNonSCNamespaceConfig("test", "/test/dev/xvdf"),
	}

	return aeroCluster
}
func createDummyRackAwareWithStorageAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeClusterWithoutStorage(clusterNamespacedName, size)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []asdbv1.Rack{
		{
			ID: 1,
		},
	}
	inputStorage := getBasicStorageSpecObject()
	racks[0].InputStorage = &inputStorage
	rackConf := asdbv1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf

	return aeroCluster
}

//nolint:unparam // generic function
func createDummyRackAwareAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, size)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []asdbv1.Rack{{ID: 1}}
	rackConf := asdbv1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf

	return aeroCluster
}

var defaultProtofdmax int64 = 15000

func createDummyAerospikeClusterWithoutStorage(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	return createDummyAerospikeClusterWithRFAndStorage(clusterNamespacedName, size, 1, nil)
}

func createDummyAerospikeClusterWithRF(
	clusterNamespacedName types.NamespacedName, size int32, rf int,
) *asdbv1.AerospikeCluster {
	storage := getBasicStorageSpecObject()
	return createDummyAerospikeClusterWithRFAndStorage(clusterNamespacedName, size, rf, &storage)
}

func createDummyAerospikeClusterWithRFAndStorage(
	clusterNamespacedName types.NamespacedName, size int32, rf int, storage *asdbv1.AerospikeStorageSpec,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1.AerospikeCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "asdb.aerospike.com/v1",
			Kind:       "AerospikeCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  size,
			Image: latestImage,
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: test.AuthSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
							"read-write",
						},
					},
				},
			},

			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost:            ptr.To(true),
				AerospikeInitContainerSpec: &asdbv1.AerospikeInitContainerSpec{},
			},

			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"proto-fd-max":     defaultProtofdmax,
					},
					"security": map[string]interface{}{},
					"network":  getNetworkConfig(),
					"namespaces": []interface{}{
						getNonSCNamespaceConfigWithRF("test", "/test/dev/xvdf", rf),
					},
				},
			},
		},
	}
	if storage != nil {
		aeroCluster.Spec.Storage = *storage
	}

	return aeroCluster
}

func createNonSCDummyAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	aerospikeCluster := createDummyAerospikeCluster(clusterNamespacedName, size)
	aerospikeCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		getNonSCNamespaceConfig("test", "/test/dev/xvdf"),
	}

	return aerospikeCluster
}

func createDummyAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1.AerospikeCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "asdb.aerospike.com/v1",
			Kind:       "AerospikeCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  size,
			Image: latestImage,
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: test.AuthSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
							"read-write",
						},
					},
				},
			},

			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost:            ptr.To(true),
				AerospikeInitContainerSpec: &asdbv1.AerospikeInitContainerSpec{},
			},

			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"proto-fd-max":     defaultProtofdmax,
						"auto-pin":         "none",
					},
					"security": map[string]interface{}{},
					"network":  getNetworkConfig(),
					"namespaces": []interface{}{
						getSCNamespaceConfig("test", "/test/dev/xvdf"),
					},
				},
			},
		},
	}
	aeroCluster.Spec.Storage = getBasicStorageSpecObject()

	return aeroCluster
}

// CreateDummyAerospikeCluster func is a public variant of createDummyAerospikeCluster
// Remove this when createDummyAerospikeCluster will be made public
func CreateDummyAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	return createDummyAerospikeCluster(clusterNamespacedName, size)
}

func UpdateClusterImage(
	aerocluster *asdbv1.AerospikeCluster, image string,
) error {
	outgoingVersion, err := asdbv1.GetImageVersion(aerocluster.Spec.Image)
	if err != nil {
		return err
	}

	incomingVersion, err := asdbv1.GetImageVersion(image)
	if err != nil {
		return err
	}

	ov, err := lib.CompareVersions(outgoingVersion, "7.0.0")
	if err != nil {
		return err
	}

	nv, err := lib.CompareVersions(incomingVersion, "7.0.0")
	if err != nil {
		return err
	}

	switch {
	case nv >= 0 && ov >= 0, nv < 0 && ov < 0:
		aerocluster.Spec.Image = image

	case nv >= 0 && ov < 0:
		aerocluster.Spec.Image = image

		namespaces := aerocluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
		for idx := range namespaces {
			ns := namespaces[idx].(map[string]interface{})
			delete(ns, "memory-size")

			storageEngine := ns["storage-engine"].(map[string]interface{})
			if storageEngine["type"] == "memory" {
				storageEngine["data-size"] = 1073741824
				ns["storage-engine"] = storageEngine
			}

			namespaces[idx] = ns
		}

	default:
		aerocluster.Spec.Image = image

		namespaces := aerocluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
		for idx := range namespaces {
			ns := namespaces[idx].(map[string]interface{})
			ns["memory-size"] = 1073741824

			storageEngine := ns["storage-engine"].(map[string]interface{})
			if storageEngine["type"] == "memory" {
				delete(storageEngine, "data-size")
				ns["storage-engine"] = storageEngine
			}

			namespaces[idx] = ns
		}
	}

	return nil
}

// feature-key file needed
func createBasicTLSCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  size,
			Image: latestImage,
			Storage: asdbv1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				Volumes: []asdbv1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					getStorageVolumeForSecret(),
				},
			},
			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: test.AuthSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},

			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: ptr.To(true),
			},

			OperatorClientCertSpec: &asdbv1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1.AerospikeSecretCertSource{
						SecretName:         test.AerospikeSecretName,
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{

					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
					},
					"security": map[string]interface{}{},
					"network":  getNetworkTLSConfig(),
				},
			},
		},
	}

	return aeroCluster
}

func createSSDStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = &multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/test/dev/xvdf",
				},
			},
		}...,
	)

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		getNonSCNamespaceConfigWithRF("test", "/test/dev/xvdf", int(repFact)),
	}

	return aeroCluster
}

func createHDDAndDataInMemStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = &multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/data",
				},
			},
		}...,
	)

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type":     "memory",
				"files":    []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize": 2000955200,
			},
		},
	}

	return aeroCluster
}

func createDataInMemWithoutPersistentStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = &multiPodPerHost
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type":      "memory",
				"data-size": 1073741824,
			},
		},
	}

	return aeroCluster
}

func aerospikeClusterCreateUpdateWithTO(
	k8sClient client.Client, desired *asdbv1.AerospikeCluster,
	ctx goctx.Context, retryInterval, timeout time.Duration,
) error {
	current := &asdbv1.AerospikeCluster{}
	if err := k8sClient.Get(
		ctx,
		types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace},
		current,
	); err != nil {
		// Deploy the cluster.
		// t.Logf("Deploying cluster at %v", time.Now().Format(time.RFC850))
		return deployClusterWithTO(
			k8sClient, ctx, desired, retryInterval, timeout,
		)
	}

	// Apply the update.
	if desired.Spec.AerospikeAccessControl != nil {
		current.Spec.AerospikeAccessControl = &asdbv1.AerospikeAccessControlSpec{}
		current.Spec = *lib.DeepCopy(&desired.Spec).(*asdbv1.AerospikeClusterSpec)
	} else {
		current.Spec.AerospikeAccessControl = nil
	}

	current.Spec.AerospikeConfig.Value = desired.Spec.AerospikeConfig.DeepCopy().Value

	if err := k8sClient.Update(ctx, current); err != nil {
		return err
	}

	return waitForAerospikeCluster(
		k8sClient, ctx, desired, int(desired.Spec.Size), retryInterval, timeout,
		[]asdbv1.AerospikeClusterPhase{asdbv1.AerospikeClusterCompleted},
	)
}

func aerospikeClusterCreateUpdate(
	k8sClient client.Client, desired *asdbv1.AerospikeCluster,
	ctx goctx.Context,
) error {
	return aerospikeClusterCreateUpdateWithTO(
		k8sClient, desired, ctx, retryInterval, getTimeout(desired.Spec.Size),
	)
}

func getBasicStorageSpecObject() asdbv1.AerospikeStorageSpec {
	storage := asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cascadeDeleteFalse,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
			InputCascadeDelete: &cascadeDeleteFalse,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/test/dev/xvdf",
				},
			},
			{
				Name: "workdir",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike",
				},
			},
			getStorageVolumeForSecret(),
		},
	}

	return storage
}

func getStorageVolumeForAerospike(name, path string) asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: name,
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   corev1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: path,
		},
	}
}

func getStorageVolumeForSecret() asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: aerospikeConfigSecret,
		Source: asdbv1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: test.AerospikeSecretName,
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
			Path: "/etc/aerospike/secret",
		},
	}
}

func getSCNamespaceConfig(name, path string) map[string]interface{} {
	return map[string]interface{}{
		"name":               name,
		"replication-factor": 2,
		"strong-consistency": true,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{path},
		},
	}
}

func getSCNamespaceConfigWithSet(name, path string) map[string]interface{} {
	return map[string]interface{}{
		"name":               name,
		"replication-factor": 2,
		"strong-consistency": true,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{path},
		},
		"sets": []map[string]interface{}{
			{
				"name": "testset",
			},
		},
	}
}

func getNonSCInMemoryNamespaceConfig(name string) map[string]interface{} {
	return map[string]interface{}{
		"name":               name,
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type":      "memory",
			"data-size": 1073741824,
		},
	}
}

func getNonSCInMemoryNamespaceConfigPre700(name string) map[string]interface{} {
	return map[string]interface{}{
		"name":               name,
		"replication-factor": 2,
		"memory-size":        1073741824,
		"storage-engine": map[string]interface{}{
			"type": "memory",
		},
	}
}

func getNonSCNamespaceConfig(name, path string) map[string]interface{} {
	return getNonSCNamespaceConfigWithRF(name, path, 2)
}

// getNonSCNamespaceConfigPre700 returns a namespace config for Aerospike version < 7.0.0
func getNonSCNamespaceConfigPre700(name, path string) map[string]interface{} {
	config := getNonSCNamespaceConfigWithRF(name, path, 2)
	config["memory-size"] = 2000955200

	return config
}

func getNonSCNamespaceConfigWithRF(name, path string, rf int) map[string]interface{} {
	return map[string]interface{}{
		"name":               name,
		"replication-factor": rf,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{path},
		},
	}
}

func getNonRootPodSpec() asdbv1.AerospikePodSpec {
	var id int64 = 1001

	changePolicy := corev1.PodFSGroupChangePolicy("Always")

	return asdbv1.AerospikePodSpec{
		HostNetwork:     false,
		MultiPodPerHost: ptr.To(true),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsUser:           &id,
			RunAsGroup:          &id,
			FSGroup:             &id,
			FSGroupChangePolicy: &changePolicy,
		},
	}
}

func getAeroClusterPVCList(
	aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client,
) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(goctx.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}

	return pvcList.Items, nil
}

func WriteDataToCluster(
	aeroCluster *asdbv1.AerospikeCluster,
	k8sClient client.Client,
	namespaces []string,
) error {
	asClient, err := getAerospikeClient(aeroCluster, k8sClient)
	if err != nil {
		return err
	}

	defer asClient.Close()

	pkgLog.Info(
		"Loading record", "nodes", asClient.GetNodeNames(),
	)

	wp := as.NewWritePolicy(0, 0)

	for _, ns := range namespaces {
		newKey, err := as.NewKey(ns, setName, key)
		if err != nil {
			return err
		}

		if err := asClient.Put(
			wp, newKey, as.BinMap{
				binName: binValue,
			},
		); err != nil {
			return err
		}
	}

	return nil
}

func CheckDataInCluster(
	aeroCluster *asdbv1.AerospikeCluster,
	k8sClient client.Client,
	namespaces []string,
) (map[string]bool, error) {
	data := make(map[string]bool)

	asClient, err := getAerospikeClient(aeroCluster, k8sClient)
	if err != nil {
		return nil, err
	}

	defer asClient.Close()

	pkgLog.Info(
		"Loading record", "nodes", asClient.GetNodeNames(),
	)

	for _, ns := range namespaces {
		newKey, err := as.NewKey(ns, setName, key)
		if err != nil {
			return nil, err
		}

		record, err := asClient.Get(nil, newKey)
		if err != nil {
			return nil, nil
		}

		if bin, exists := record.Bins[binName]; exists {
			value, ok := bin.(string)

			if !ok {
				return nil, fmt.Errorf(
					"Bin-Name: %s - conversion to bin value failed", binName,
				)
			}

			if value == binValue {
				data[ns] = true
			} else {
				return nil, fmt.Errorf(
					"bin: %s exsists but the value is changed", binName,
				)
			}
		} else {
			data[ns] = false
		}
	}

	return data, nil
}
