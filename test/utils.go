package test

import (
	"bytes"
	goctx "context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	operatorUtils "github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var (
	namespace    = "test"
	storageClass = "ssd"
	pkgLog       = ctrl.Log.WithName("test")
)

var secrets map[string][]byte
var cacertSecrets map[string][]byte

const secretDir = "../config/samples/secrets"               //nolint:gosec // for testing
const cacertSecretDir = "../config/samples/secrets/cacerts" //nolint:gosec // for testing

const tlsSecretName = "aerospike-secret"
const tlsCacertSecretName = "aerospike-cacert-secret" //nolint:gosec // for testing
const authSecretName = "auth-secret"
const authSecretNameForUpdate = "auth-update"

const multiClusterNs1 string = "test1"
const multiClusterNs2 string = "test2"
const aerospikeNs string = "aerospike"

const zoneKey = "topology.kubernetes.io/zone"
const regionKey = "topology.kubernetes.io/region"

// list of all the namespaces used in test-suite
var testNamespaces = []string{namespace, multiClusterNs1, multiClusterNs2, aerospikeNs}

const aerospikeConfigSecret string = "aerospike-config-secret" //nolint:gosec // for testing

var aerospikeVolumeInitMethodDeleteFiles = asdbv1.AerospikeVolumeMethodDeleteFiles

func initConfigSecret(secretDirectory string) (map[string][]byte, error) {
	initSecrets := make(map[string][]byte)

	fileInfo, err := os.ReadDir(secretDirectory)
	if err != nil {
		return nil, err
	}

	if len(fileInfo) == 0 {
		return nil, fmt.Errorf("no secret file available in %s", secretDirectory)
	}

	for _, file := range fileInfo {
		if file.IsDir() {
			// no need to check recursively
			continue
		}

		secret, err := os.ReadFile(filepath.Join(secretDirectory, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("wrong secret file %s: %v", file.Name(), err)
		}

		initSecrets[file.Name()] = secret
	}

	return initSecrets, nil
}

func setupByUser(k8sClient client.Client, ctx goctx.Context) error {
	var err error
	// Create configSecret
	if secrets, err = initConfigSecret(secretDir); err != nil {
		return fmt.Errorf("failed to init secrets: %v", err)
	}

	// Create cacertSecret
	if cacertSecrets, err = initConfigSecret(cacertSecretDir); err != nil {
		return fmt.Errorf("failed to init secrets: %v", err)
	}

	// Create preReq for namespaces used for testing
	for idx := range testNamespaces {
		if err := createClusterPreReq(k8sClient, ctx, testNamespaces[idx]); err != nil {
			return err
		}
	}

	// Create another authSecret. Used in access-control tests
	passUpdate := "admin321"
	labels := getLabels()

	if err := createAuthSecret(
		k8sClient, ctx, namespace, labels, authSecretNameForUpdate, passUpdate,
	); err != nil {
		return err
	}

	return createClusterRBAC(k8sClient, ctx)
}

func createClusterPreReq(
	k8sClient client.Client, ctx goctx.Context, namespace string,
) error {
	labels := getLabels()

	if err := createNamespace(k8sClient, ctx, namespace); err != nil {
		return err
	}

	if err := createConfigSecret(
		k8sClient, ctx, namespace, labels,
	); err != nil {
		return err
	}

	if err := createCacertSecret(
		k8sClient, ctx, namespace, labels,
	); err != nil {
		return err
	}

	// Create authSecret
	pass := "admin123"

	return createAuthSecret(
		k8sClient, ctx, namespace, labels, authSecretName, pass,
	)
}

func createCacertSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string,
) error {
	// Create configSecret
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsCacertSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: cacertSecrets,
	}

	// Remove old object
	_ = k8sClient.Delete(ctx, s)

	// use test context's create helper to create the object and add a cleanup
	// function for the new object
	err := k8sClient.Create(ctx, s)
	if err != nil {
		return err
	}

	return nil
}

func createConfigSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string,
) error {
	// Create configSecret
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: secrets,
	}

	// Remove old object
	_ = k8sClient.Delete(ctx, s)

	// use test context's create helper to create the object and add a cleanup
	// function for the new object
	return k8sClient.Create(ctx, s)
}

func createAuthSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string, secretName, pass string,
) error {
	// Create authSecret
	as := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(pass),
		},
	}
	// use test context's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, as)
	if !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func getLabels() map[string]string {
	return map[string]string{"app": "aerospike-cluster"}
}

func waitForAerospikeCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1.AerospikeCluster, replicas int,
	retryInterval, timeout time.Duration,
) error {
	var isValid bool

	err := wait.Poll(
		retryInterval, timeout, func() (done bool, err error) {
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
						"Waiting for availability of %s AerospikeCluster\n",
						aeroCluster.Name,
					)
					return false, nil
				}
				return false, err
			}

			isValid = isClusterStateValid(aeroCluster, newCluster, replicas)
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
	newCluster *asdbv1.AerospikeCluster, replicas int,
) bool {
	if int(newCluster.Status.Size) != replicas {
		pkgLog.Info("Cluster size is not correct")
		return false
	}

	// Validate status
	statusToSpec, err := asdbv1.CopyStatusToSpec(&newCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		pkgLog.Error(err, "Failed to copy spec in status", "err", err)
		return false
	}

	if !reflect.DeepEqual(statusToSpec, &newCluster.Spec) {
		pkgLog.Info("Cluster status is not matching the spec")
		return false
	}

	// Validate pods
	if len(newCluster.Status.Pods) != replicas {
		pkgLog.Info("Cluster status doesn't have pod status for all nodes. Cluster status may not have fully updated")
		return false
	}

	for podName := range newCluster.Status.Pods {
		if newCluster.Status.Pods[podName].Aerospike.NodeID == "" {
			pkgLog.Info("Cluster pod's nodeID is empty")
			return false
		}

		if operatorUtils.IsImageEqual(newCluster.Status.Pods[podName].Image, aeroCluster.Spec.Image) {
			break
		}

		pkgLog.Info(
			"Cluster pod's image %s not same as spec %s", newCluster.Status.Pods[podName].Image,
			aeroCluster.Spec.Image,
		)
	}

	return true
}

func getTimeout(nodes int32) time.Duration {
	return 3 * time.Minute * time.Duration(nodes)
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

func getRackID(pod *corev1.Pod) (int, error) {
	rack, ok := pod.ObjectMeta.Labels["aerospike.com/rack-id"]
	if !ok {
		return 0, nil
	}

	return strconv.Atoi(rack)
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
func getNetworkTLSConfig() map[string]interface{} {
	return map[string]interface{}{
		"service": map[string]interface{}{
			"tls-name": "aerospike-a-0.test-runner",
			"tls-port": 4333,
			"port":     3000,
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
			"port": 3000,
		},
		"heartbeat": map[string]interface{}{
			"port": 3001,
		},
		"fabric": map[string]interface{}{
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
			"memory-size":        1000955200,
			"replication-factor": 1,
			"storage-engine": map[string]interface{}{
				"type": "memory",
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

func (acs *AerospikeConfSpec) getVersion() string {
	return acs.version
}

func (acs *AerospikeConfSpec) setEnableSecurity(enableSecurity bool) error {
	cmpVal, err := asconfig.CompareVersions(acs.version, "5.7.0")
	if err != nil {
		return err
	}

	if cmpVal >= 0 {
		if enableSecurity {
			security := map[string]interface{}{}
			acs.security = security
		}

		return nil
	}

	acs.security = map[string]interface{}{}
	acs.security["enable-security"] = enableSecurity

	return nil
}

func (acs *AerospikeConfSpec) setEnableQuotas(enableQuotas bool) error {
	cmpVal, err := asconfig.CompareVersions(acs.version, "5.6.0")
	if err != nil {
		return err
	}

	if cmpVal >= 0 {
		if acs.security == nil {
			acs.security = map[string]interface{}{}
		}

		acs.security["enable-quotas"] = enableQuotas
	}

	return nil
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

	cmpVal, err := asconfig.CompareVersions(version, "5.7.0")
	if err != nil {
		return nil, err
	}

	if cmpVal >= 0 {
		return createAerospikeClusterPost560(
			namespace, 2, image,
		), nil
	}

	return createAerospikeClusterPost460(
		namespace, 2, image,
	), nil
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
	blkDiscardInitMethod := asdbv1.AerospikeVolumeMethodBlkdiscard
	blkDiscardWipeMethod := asdbv1.AerospikeVolumeMethodBlkdiscard

	if cloudProvider == CloudProviderAWS {
		// Blkdiscard method is not supported in AWS, so it is initialized as DD Method
		blkDiscardInitMethod = asdbv1.AerospikeVolumeMethodDD
		blkDiscardWipeMethod = asdbv1.AerospikeVolumeMethodDD
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
			{
				Name: aerospikeConfigSecret,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: tlsSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
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

func getGitRepoRootPath() (string, error) {
	path, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(path)), nil
}
