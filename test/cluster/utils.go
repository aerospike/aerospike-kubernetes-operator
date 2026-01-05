package cluster

import (
	"bytes"
	goctx "context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"time"

	set "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	operatorUtils "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
)

var defaultNetworkType = flag.String("connect-through-network-type", "hostExternal",
	"Network type is used to determine an appropriate access type. Can be 'pod',"+
		" 'hostInternal' or 'hostExternal'. AS client in the test will choose access type"+
		" which matches expected network type. See details in"+
		" https://docs.aerospike.com/docs/cloud/kubernetes/operator/Cluster-configuration-settings.html#network-policy")

type CloudProvider int

const (
	CloudProviderUnknown CloudProvider = iota
	CloudProviderAWS
	CloudProviderGCP
)

const zoneKey = "topology.kubernetes.io/zone"
const regionKey = "topology.kubernetes.io/region"

var cloudProvider CloudProvider

func waitForAerospikeCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster, replicas int,
	retryInterval, timeout time.Duration, expectedPhases []asdbv1.AerospikeClusterPhase,
) error {
	var isValid bool

	err := wait.PollUntilContextTimeout(ctx,
		retryInterval, timeout, true, func(ctx goctx.Context) (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1.AerospikeCluster{}

			err = k8sClient.Get(
				ctx, types.NamespacedName{
					Name: aeroCluster.Name, Namespace: aeroCluster.Namespace,
				}, newCluster,
			)
			if err != nil {
				if errors.IsNotFound(err) {
					pkgLog.Info(
						"Waiting for availability of AerospikeCluster\n",
						"name", aeroCluster.Name,
					)

					return false, nil
				}

				return false, err
			}

			isValid = isClusterStateValid(aeroCluster, newCluster, replicas, expectedPhases)

			return isValid, nil
		},
	)
	if err != nil {
		return err
	}

	pkgLog.Info("AerospikeCluster available\n")

	// make info call
	return nil
}

func isClusterStateValid(
	aeroCluster *asdbv1.AerospikeCluster,
	newCluster *asdbv1.AerospikeCluster, replicas int, expectedPhases []asdbv1.AerospikeClusterPhase,
) bool {
	if int(newCluster.Status.Size) != replicas {
		pkgLog.Info("Cluster size is not correct", "name", aeroCluster.Name)
		return false
	}

	// Do not compare status with spec if cluster reconciliation is paused
	// `paused` flag only exists in the spec and not in the status.
	if !asdbv1.GetBool(aeroCluster.Spec.Paused) {
		// Validate status
		statusToSpec, err := asdbv1.CopyStatusToSpec(&newCluster.Status.AerospikeClusterStatusSpec)
		if err != nil {
			pkgLog.Error(err, "Failed to copy spec in status", "err", err)
			return false
		}

		if !reflect.DeepEqual(statusToSpec, &newCluster.Spec) {
			pkgLog.Info("Cluster status is not matching the spec", "name", aeroCluster.Name)
			return false
		}
	}

	// TODO: This is not valid for tests where maxUnavailablePods flag is used.
	// We can take the param in func to skip this check
	// // Validate pods
	// if len(newCluster.Status.Pods) != replicas {
	// 	pkgLog.Info("Cluster status doesn't have pod status for all nodes. Cluster status may not have fully updated")
	// 	return false
	// }

	for podName := range newCluster.Status.Pods {
		if newCluster.Status.Pods[podName].Aerospike.NodeID == "" {
			pkgLog.Info("Cluster pod's nodeID is empty", "name", aeroCluster.Name)
			return false
		}

		if operatorUtils.IsImageEqual(newCluster.Status.Pods[podName].Image, aeroCluster.Spec.Image) {
			break
		}

		pkgLog.Info(
			fmt.Sprintf("Cluster pod's image %s not same as spec %s", newCluster.Status.Pods[podName].Image,
				aeroCluster.Spec.Image,
			), "name", aeroCluster.Name,
		)

		return false
	}

	if newCluster.Labels[asdbv1.AerospikeAPIVersionLabel] != asdbv1.AerospikeAPIVersion {
		pkgLog.Info("Cluster API version label is not correct", "name", aeroCluster.Name)
		return false
	}

	// Validate phase
	phaseSet := set.NewSet(expectedPhases...)
	if !phaseSet.Contains(newCluster.Status.Phase) {
		pkgLog.Info("Cluster phase is not correct", "name", aeroCluster.Name)
		return false
	}

	// Check for status selector only in case of Completed phase
	if phaseSet.Cardinality() == 1 && phaseSet.Contains(asdbv1.AerospikeClusterCompleted) {
		selector := labels.SelectorFromSet(operatorUtils.LabelsForAerospikeCluster(newCluster.Name))

		if newCluster.Status.Selector != selector.String() {
			pkgLog.Info("Cluster status selector is not correct", "name", aeroCluster.Name)
			return false
		}
	}

	pkgLog.Info("Cluster state is validated successfully", "name", aeroCluster.Name)

	return true
}

func getTimeout(nodes int32) time.Duration {
	return 5 * time.Minute * time.Duration(nodes)
}

func getPodLogs(
	k8sClientset *kubernetes.Clientset, ctx goctx.Context, pod *corev1.Pod,
) string {
	podLogOpts := corev1.PodLogOptions{}
	req := k8sClientset.CoreV1().Pods(pod.Namespace).GetLogs(
		pod.Name, &podLogOpts,
	)

	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "error in opening stream"
	}

	defer func(podLogs io.ReadCloser) {
		_ = podLogs.Close()
	}(podLogs)

	buf := new(bytes.Buffer)

	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "error in copy information from podLogs to buf"
	}

	str := buf.String()

	return str
}

// Copy makes a deep copy from src into dst.
func Copy(dst, src interface{}) error {
	if dst == nil {
		return fmt.Errorf("dst cannot be nil")
	}

	if src == nil {
		return fmt.Errorf("src cannot be nil")
	}

	jsonBytes, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("unable to marshal src: %s", err)
	}

	err = json.Unmarshal(jsonBytes, dst)
	if err != nil {
		return fmt.Errorf("unable to unmarshal into dst: %s", err)
	}

	return nil
}

type AerospikeConfSpec struct {
	version    string
	network    map[string]interface{}
	service    map[string]interface{}
	security   map[string]interface{}
	namespaces []interface{}
}

func (acs *AerospikeConfSpec) getVersion() string {
	return acs.version
}

func (acs *AerospikeConfSpec) configureSecurity(enableSecurity bool) {
	if enableSecurity {
		security := map[string]interface{}{}
		acs.security = security
	} else {
		acs.security = nil
	}
}

func (acs *AerospikeConfSpec) setEnableQuotas(enableQuotas bool) {
	if acs.security == nil {
		acs.security = map[string]interface{}{}
	}

	acs.security["enable-quotas"] = enableQuotas
}

func (acs *AerospikeConfSpec) getSpec() map[string]interface{} {
	spec := map[string]interface{}{
		"service":    acs.service,
		"network":    acs.network,
		"namespaces": acs.namespaces,
	}
	if acs.security != nil {
		spec["security"] = acs.security
	}

	return spec
}

func getOperatorCert() *asdbv1.AerospikeOperatorClientCertSpec {
	return &asdbv1.AerospikeOperatorClientCertSpec{
		TLSClientName: "aerospike-a-0.test-runner",
		AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
			SecretCertSource: &asdbv1.AerospikeSecretCertSource{
				SecretName:         "aerospike-secret",
				CaCertsFilename:    "cacert.pem",
				ClientCertFilename: "svc_cluster_chain.pem",
				ClientKeyFilename:  "svc_key.pem",
			},
		},
	}
}

func getAdminOperatorCert() *asdbv1.AerospikeOperatorClientCertSpec {
	return &asdbv1.AerospikeOperatorClientCertSpec{
		AerospikeOperatorCertSource: asdbv1.AerospikeOperatorCertSource{
			SecretCertSource: &asdbv1.AerospikeSecretCertSource{
				SecretName:         "aerospike-secret",
				CaCertsFilename:    "cacert.pem",
				ClientCertFilename: "admin_chain.pem",
				ClientKeyFilename:  "admin_key.pem",
			},
		},
	}
}

func getNetworkTLSConfig() map[string]interface{} {
	return map[string]interface{}{
		"service": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": serviceTLSPort,
			"port":     serviceNonTLSPort,
		},
		"fabric": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 3011,
			"port":     3001,
		},
		"heartbeat": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 3012,
			"port":     3002,
		},

		"tls": []interface{}{
			map[string]interface{}{
				"name":      "aerospike-a-0.test-runner",
				"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
				"key-file":  "/etc/aerospike/secret/svc_key.pem",
				"ca-file":   "/etc/aerospike/secret/cacert.pem",
			},
		},
	}
}

func getNetworkConfig() map[string]interface{} {
	return map[string]interface{}{
		"service": map[string]interface{}{
			"port": serviceNonTLSPort,
		},
		"fabric": map[string]interface{}{
			"port": 3001,
		},
		"heartbeat": map[string]interface{}{
			"port": 3002,
		},
	}
}

func NewAerospikeConfSpec(image string) (*AerospikeConfSpec, error) {
	ver, err := asdbv1.GetImageVersion(image)
	if err != nil {
		return nil, err
	}

	service := map[string]interface{}{
		"feature-key-file": "/etc/aerospike/secret/features.conf",
	}
	network := getNetworkConfig()
	namespaces := []interface{}{
		map[string]interface{}{
			"name":               "test",
			"replication-factor": 2,
			"storage-engine": map[string]interface{}{
				"type":      "memory",
				"data-size": 1073741824,
			},
		},
	}

	return &AerospikeConfSpec{
		version:    ver,
		service:    service,
		network:    network,
		namespaces: namespaces,
		security:   nil,
	}, nil
}

func ValidateAttributes(
	actual []map[string]string, expected map[string]string,
) bool {
	for key, val := range expected {
		for i := 0; i < len(actual); i++ {
			m := actual[i]

			v, ok := m[key]
			if ok && v == val {
				return true
			}
		}
	}

	return false
}

func getAeroClusterConfig(
	namespace types.NamespacedName, image string,
) (*asdbv1.AerospikeCluster, error) {
	version, err := asdbv1.GetImageVersion(image)
	if err != nil {
		return nil, err
	}

	cmpVal1, err := lib.CompareVersions(version, "6.0.0")
	if err != nil {
		return nil, err
	}

	cmpVal2, err := lib.CompareVersions(version, "7.0.0")
	if err != nil {
		return nil, err
	}

	switch {
	case cmpVal2 >= 0:
		return createAerospikeClusterPost640(
			namespace, 2, image,
		), nil

	case cmpVal1 >= 0:
		return createAerospikeClusterPost570(
			namespace, 2, image,
		), nil

	default:
		return nil, fmt.Errorf("invalid image version %s", version)
	}
}

func getAerospikeStorageConfig(
	containerName string, inputCascadeDelete bool,
	storageSize string,
	cloudProvider CloudProvider,
) *asdbv1.AerospikeStorageSpec {
	// Create pods and storage devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteInitMethod := asdbv1.AerospikeVolumeMethodDeleteFiles
	ddInitMethod := asdbv1.AerospikeVolumeMethodDD
	headerCleanupInitMethod := asdbv1.AerospikeVolumeMethodHeaderCleanup
	blkDiscardInitMethod := asdbv1.AerospikeVolumeMethodBlkdiscard
	blkDiscardWithHeaderCleanupInitMethod := asdbv1.AerospikeVolumeMethodBlkdiscardWithHeaderCleanup
	blkDiscardWipeMethod := asdbv1.AerospikeVolumeMethodBlkdiscard

	if cloudProvider == CloudProviderAWS {
		// Blkdiscard method is not supported in AWS, so it is initialized as DD Method
		blkDiscardInitMethod = asdbv1.AerospikeVolumeMethodDD
		blkDiscardWipeMethod = asdbv1.AerospikeVolumeMethodDD
		blkDiscardWithHeaderCleanupInitMethod = asdbv1.AerospikeVolumeMethodDD
	}

	return &asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "file-noinit",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-noinit",
				},
			},
			{
				Name: "file-init",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-init",
				},
			},
			{
				Name: "device-noinit",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-noinit",
				},
			},
			{
				Name: "device-dd",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-dd",
				},
			},
			{
				Name: "device-header-cleanup",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &headerCleanupInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-header-cleanup",
				},
			},
			{
				Name: "device-blkdiscard",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardInitMethod,
					InputWipeMethod: &blkDiscardWipeMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-blkdiscard",
				},
			},
			{
				Name: "device-blkdiscard-with-header-cleanup",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardWithHeaderCleanupInitMethod,
					InputWipeMethod: &blkDiscardWipeMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-blkdiscard-with-header-cleanup",
				},
			},
			{
				Name: "file-noinit-1",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Sidecars: []asdbv1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/filesystem-noinit",
					},
				},
			},
			{
				Name: "device-dd-1",
				AerospikePersistentVolumePolicySpec: asdbv1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse(storageSize),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Sidecars: []asdbv1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/blockdevice-init-dd",
					},
				},
			},
			getStorageVolumeForSecret(),
		},
	}
}

//nolint:unparam // generic function
func contains(elems []string, v string) bool {
	for _, s := range elems {
		if v == s {
			return true
		}
	}

	return false
}

func getASInfo(log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, podName, network string) (*info.AsInfo, error) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	pod := aeroCluster.Status.Pods[podName]

	host, err := createHost(&pod, network)
	if err != nil {
		return nil, err
	}

	return info.NewAsInfo(
		log, host, getClientPolicy(aeroCluster, k8sClient),
	), nil
}

func getAerospikeConfigFromNode(log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, configContext, podName string) (lib.Stats, error) {
	asinfo, err := getASInfo(log, k8sClient, ctx, clusterNamespacedName, podName, "service")
	if err != nil {
		return nil, fmt.Errorf("failed to get ASInfo: %w", err)
	}

	confs, err := getAsConfig(asinfo, configContext)
	if err != nil {
		return nil, err
	}

	return confs[configContext].(lib.Stats), nil
}

func requestInfoFromNode(log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, cmd, podName string) (map[string]string, error) {
	asinfo, err := getASInfo(log, k8sClient, ctx, clusterNamespacedName, podName, "service")
	if err != nil {
		return nil, fmt.Errorf("failed to get ASInfo: %w", err)
	}

	confs, err := asinfo.RequestInfo(cmd)
	if err != nil {
		return nil, err
	}

	return confs, nil
}

func getPasswordFromSecret(k8sClient client.Client,
	secretNamespcedName types.NamespacedName, passFileName string,
) (string, error) {
	secret := &corev1.Secret{}

	err := k8sClient.Get(goctx.TODO(), secretNamespcedName, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %v", secretNamespcedName, err)
	}

	passBytes, ok := secret.Data[passFileName]
	if !ok {
		return "", fmt.Errorf(
			"failed to get password file in secret %s, fileName %s",
			secretNamespcedName, passFileName,
		)
	}

	return string(passBytes), nil
}

func getAerospikeClient(aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client) (*as.Client, error) {
	policy := getClientPolicy(aeroCluster, k8sClient)
	policy.FailIfNotConnected = false
	policy.Timeout = time.Minute * 2
	policy.UseServicesAlternate = true
	policy.ConnectionQueueSize = 100
	policy.LimitConnectionsToQueueSize = true

	hostList := make([]*as.Host, 0, len(aeroCluster.Status.Pods))

	for podName := range aeroCluster.Status.Pods {
		pod := aeroCluster.Status.Pods[podName]

		host, err := createHost(&pod, "service")
		if err != nil {
			return nil, err
		}

		hostList = append(hostList, host)
	}

	asClient, err := as.NewClientWithPolicyAndHost(policy, hostList...)
	if asClient == nil {
		return nil, fmt.Errorf(
			"failed to create aerospike cluster asClient: %v", err,
		)
	}

	_, _ = asClient.WarmUp(-1)

	// Wait for 5 minutes for cluster to connect
	for j := 0; j < 150; j++ {
		if isConnected := asClient.IsConnected(); isConnected {
			break
		}

		time.Sleep(time.Second * 2)
	}

	return asClient, nil
}

func getPodList(
	aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(operatorUtils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(goctx.TODO(), podList, listOps); err != nil {
		return nil, err
	}

	return podList, nil
}

func deletePVC(k8sClient client.Client, pvcNamespacedName types.NamespacedName) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8sClient.Get(goctx.TODO(), pvcNamespacedName, pvc); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if operatorUtils.IsPVCTerminating(pvc) {
		return nil
	}

	if err := k8sClient.Delete(goctx.TODO(), pvc); err != nil {
		return fmt.Errorf("could not delete pvc %s: %w", pvc.Name, err)
	}

	return nil
}

func CleanupPVC(k8sClient client.Client, ns, clName string) error {
	if clName == "" {
		if err := k8sClient.DeleteAllOf(goctx.TODO(), &corev1.PersistentVolumeClaim{}, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("could not delete pvcs: %w", err)
		}
	} else {
		clLabels := operatorUtils.LabelsForAerospikeCluster(clName)
		if err := k8sClient.DeleteAllOf(goctx.TODO(), &corev1.PersistentVolumeClaim{}, client.InNamespace(ns),
			client.MatchingLabels(clLabels)); err != nil {
			return fmt.Errorf("could not delete pvcs: %w", err)
		}
	}

	return nil
}

func getSTSList(
	aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client,
) (*appsv1.StatefulSetList, error) {
	stsList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(operatorUtils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(goctx.TODO(), stsList, listOps); err != nil {
		return nil, err
	}

	return stsList, nil
}

func getServiceForPod(
	pod *corev1.Pod, k8sClient client.Client,
) (*corev1.Service, error) {
	service := &corev1.Service{}

	err := k8sClient.Get(
		goctx.TODO(),
		types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get service for pod %s: %v", pod.Name, err,
		)
	}

	return service, nil
}

func getCloudProvider(
	ctx goctx.Context, k8sClient client.Client,
) (CloudProvider, error) {
	labelKeys := map[string]struct{}{}

	nodes, err := test.GetNodeList(ctx, k8sClient)
	if err != nil {
		return CloudProviderUnknown, err
	}

	for idx := range nodes.Items {
		for labelKey := range nodes.Items[idx].Labels {
			if strings.Contains(labelKey, "cloud.google.com") {
				return CloudProviderGCP, nil
			}

			if strings.Contains(labelKey, "eks.amazonaws.com") {
				return CloudProviderAWS, nil
			}

			labelKeys[labelKey] = struct{}{}
		}

		provider := determineByProviderID(&nodes.Items[idx])
		if provider != CloudProviderUnknown {
			return provider, nil
		}
	}

	labelKeysSlice := make([]string, 0, len(labelKeys))

	for labelKey := range labelKeys {
		labelKeysSlice = append(labelKeysSlice, labelKey)
	}

	return CloudProviderUnknown, fmt.Errorf(
		"can't determin cloud platform by node's labels: %v", labelKeysSlice,
	)
}

func determineByProviderID(node *corev1.Node) CloudProvider {
	if strings.Contains(node.Spec.ProviderID, "gce") {
		return CloudProviderGCP
	} else if strings.Contains(node.Spec.ProviderID, "aws") {
		return CloudProviderAWS
	}
	// TODO add cloud provider detection for Azure
	return CloudProviderUnknown
}

func getZones(ctx goctx.Context, k8sClient client.Client) ([]string, error) {
	unqZones := map[string]int{}

	nodes, err := test.GetNodeList(ctx, k8sClient)
	if err != nil {
		return nil, err
	}

	for idx := range nodes.Items {
		unqZones[nodes.Items[idx].Labels[zoneKey]] = 1
	}

	zones := make([]string, 0, len(unqZones))

	for zone := range unqZones {
		zones = append(zones, zone)
	}

	return zones, nil
}

func getRegion(ctx goctx.Context, k8sClient client.Client) (string, error) {
	nodes, err := test.GetNodeList(ctx, k8sClient)
	if err != nil {
		return "", err
	}

	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("node list empty: %v", nodes.Items)
	}

	return nodes.Items[0].Labels[regionKey], nil
}

func getGitRepoRootPath() (string, error) {
	path, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(path)), nil
}

func randomizeServicePorts(
	aeroCluster *asdbv1.AerospikeCluster,
	tlsEnabled bool, processID int,
) {
	if tlsEnabled {
		aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface {
		})["service"].(map[string]interface{})["tls-port"] = serviceTLSPort + processID*10
	}

	aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface {
	})["service"].(map[string]interface{})["port"] = serviceNonTLSPort + processID*10
}
