package test

import (
	"bytes"
	goctx "context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	operatorutils "github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	namespace    = "test"
	storageClass = "ssd"
	pkgLog       = ctrl.Log.WithName("test")
)

var secrets map[string][]byte

const secretDir = "../config/samples/secrets"

const tlsSecretName = "aerospike-secret"
const authSecretName = "auth"
const authSecretNameForUpdate = "auth-update"

const multiClusterNs1 string = "test1"
const multiClusterNs2 string = "test2"

const aerospikeConfigSecret string = "aerospike-config-secret"

var aerospikeVolumeInitMethodDeleteFiles = asdbv1beta1.AerospikeVolumeInitMethodDeleteFiles

func initConfigSecret(secretDir string) error {
	secrets = make(map[string][]byte)

	fileInfo, err := ioutil.ReadDir(secretDir)
	if err != nil {
		return err
	}

	if len(fileInfo) == 0 {
		return fmt.Errorf("no secret file available in %s", secretDir)
	}

	for _, file := range fileInfo {
		if file.IsDir() {
			// no need to check recursively
			continue
		}

		secret, err := ioutil.ReadFile(filepath.Join(secretDir, file.Name()))
		if err != nil {
			return fmt.Errorf("wrong secret file %s: %v", file.Name(), err)
		}

		secrets[file.Name()] = secret
	}

	return nil
}

func setupByUser(k8sClient client.Client, ctx goctx.Context) error {

	labels := getLabels()

	// Create configSecret
	if err := initConfigSecret(secretDir); err != nil {
		return fmt.Errorf("failed to init secrets: %v", err)
	}

	if err := createConfigSecret(
		k8sClient, ctx, namespace, labels,
	); err != nil {
		return err
	}

	// Create authSecret
	pass := "admin"
	if err := createAuthSecret(
		k8sClient, ctx, namespace, labels, authSecretName, pass,
	); err != nil {
		return err
	}

	// Create another authSecret. Used in access-control tests
	passUpdate := "admin321"
	if err := createAuthSecret(
		k8sClient, ctx, namespace, labels, authSecretNameForUpdate, passUpdate,
	); err != nil {
		return err
	}

	// Create preReq for multi-clusters
	if err := createClusterPreReq(k8sClient, ctx, multiClusterNs1); err != nil {
		return err
	}
	if err := createClusterPreReq(k8sClient, ctx, multiClusterNs2); err != nil {
		return err
	}
	if err := createClusterRBAC(k8sClient, ctx); err != nil {
		return err
	}
	return nil
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

	// Create authSecret
	pass := "admin"
	if err := createAuthSecret(
		k8sClient, ctx, namespace, labels, authSecretName, pass,
	); err != nil {
		return err
	}

	return nil
}

func createConfigSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string,
) error {
	// Create configSecret
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: v1.SecretTypeOpaque,
		Data: secrets,
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, s)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createAuthSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string, secretName, pass string,
) error {

	// Create authSecret
	as := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(pass),
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
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
	aeroCluster *asdbv1beta1.AerospikeCluster, replicas int,
	retryInterval, timeout time.Duration,
) error {
	var isValid bool
	err := wait.Poll(
		retryInterval, timeout, func() (done bool, err error) {
			// Fetch the AerospikeCluster instance
			newCluster := &asdbv1beta1.AerospikeCluster{}
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
	if !isValid {
		return fmt.Errorf("cluster state not matching with desired state")
	}
	pkgLog.Info("AerospikeCluster available\n")

	// make info call
	return nil
}

func isClusterStateValid(
	aeroCluster *asdbv1beta1.AerospikeCluster,
	newCluster *asdbv1beta1.AerospikeCluster, replicas int,
) bool {
	if int(newCluster.Status.Size) != replicas {
		pkgLog.Info("Cluster size is not correct")
		return false
	}

	statusToSpec, err := asdbv1beta1.CopyStatusToSpec(newCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		pkgLog.Error(err, "Failed to copy spec in status", "err", err)
		return false
	}
	if !reflect.DeepEqual(statusToSpec, &newCluster.Spec) {
		pkgLog.Info("Cluster status is not matching the spec")
		return false
	}

	if len(newCluster.Status.Pods) != replicas {
		pkgLog.Info("Cluster status doesn't have pod status for all nodes. Cluster status may not have fully updated")
		return false
	}

	for _, pod := range newCluster.Status.Pods {
		if pod.Aerospike.NodeID == "" {
			pkgLog.Info("Cluster pod's nodeID is empty")
			return false
		}
		if operatorutils.IsImageEqual(pod.Image, aeroCluster.Spec.Image) {
			break
		}

		pkgLog.Info(
			"Cluster pod's image %s not same as spec %s", pod.Image,
			aeroCluster.Spec.Image,
		)
	}
	return true
}

func getTimeout(nodes int32) time.Duration {
	return 10 * time.Minute * time.Duration(nodes)
}

func getPodLogs(
	k8sClientset *kubernetes.Clientset, ctx goctx.Context, pod *v1.Pod,
) string {
	podLogOpts := v1.PodLogOptions{}
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

func getRackID(pod *v1.Pod) (int, error) {
	rack, ok := pod.ObjectMeta.Labels["aerospike.com/rack-id"]
	if !ok {
		return 0, nil
	}

	return strconv.Atoi(rack)
}

// Copy makes a deep copy from src into dst.
func Copy(dst interface{}, src interface{}) error {
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

func getOperatorCert() *asdbv1beta1.AerospikeOperatorClientCertSpec {
	return &asdbv1beta1.AerospikeOperatorClientCertSpec{
		TLSClientName: "aerospike-a-0.test-runner",
		AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
			SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
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

	ver, err := asdbv1beta1.GetImageVersion(image)
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
) (*asdbv1beta1.AerospikeCluster, error) {
	version, err := asdbv1beta1.GetImageVersion(image)
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
	} else {
		return createAerospikeClusterPost460(
			namespace, 2, image,
		), nil
	}
}

func getDynamicClusterNamespace() map[string]interface{} {
	return map[string]interface{}{
		"name":               "dynamicns",
		"memory-size":        1000955200,
		"replication-factor": 2,
		"storage-engine": map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{"/test/dev/dynamicns"},
		},
	}
}

func getDynamicNameSpaceVolume() *asdbv1beta1.VolumeSpec {
	return &asdbv1beta1.VolumeSpec{
		Name: "dynamicns",
		Source: asdbv1beta1.VolumeSource{
			PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
				Size:         resource.MustParse("1Gi"),
				StorageClass: storageClass,
				VolumeMode:   v1.PersistentVolumeBlock,
			},
		},
		Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
			Path: "/test/dev/dynamicns",
		},
	}
}

func getAerospikeStorageConfig(
	containerName string, inputCascadeDelete bool, cloudProvider CloudProvider) *asdbv1beta1.AerospikeStorageSpec {

	// Create pods and strorge devices write data to the devices.
	// - deletes cluster without cascade delete of volumes.
	// - recreate and check if volumes are reinitialized correctly.
	fileDeleteInitMethod := asdbv1beta1.AerospikeVolumeInitMethodDeleteFiles
	// TODO
	ddInitMethod := asdbv1beta1.AerospikeVolumeInitMethodDD
	blkDiscardInitMethod := asdbv1beta1.AerospikeVolumeInitMethodBlkdiscard
	if cloudProvider == CloudProviderAWS {
		// Blkdiscard methood is not supported in AWS so it is initialized as DD Method
		blkDiscardInitMethod = asdbv1beta1.AerospikeVolumeInitMethodDD
	}

	return &asdbv1beta1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &inputCascadeDelete,
		},
		Volumes: []asdbv1beta1.VolumeSpec{
			{
				Name: "file-noinit",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-noinit",
				},
			},
			{
				Name: "file-init",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &fileDeleteInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/filesystem-init",
				},
			},
			{
				Name: "device-noinit",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-noinit",
				},
			},
			{
				Name: "device-dd",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-dd",
				},
			},
			{
				Name: "device-blkdiscard",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &blkDiscardInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/blockdevice-init-blkdiscard",
				},
			},
			{
				Name: "file-noinit-1",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Sidecars: []asdbv1beta1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/filesystem-noinit",
					},
				},
			},
			// {
			// 	Name: "file-init-1",
			// 	AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			// 		InputInitMethod: &fileDeleteInitMethod,
			// 	},
			// 	Source: asdbv1beta1.VolumeSource{
			// 		PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
			// 			Size:         resource.MustParse("1Gi"),
			// 			StorageClass: storageClass,
			// 			VolumeMode:   corev1.PersistentVolumeFilesystem,
			// 		},
			// 	},
			// 	Sidecars: []asdbv1beta1.VolumeAttachment{
			// 		{
			// 			ContainerName: containerName,
			// 			Path:          "/opt/aerospike/filesystem-init",
			// 		},
			// 	},
			// },
			// {
			// 	Name: "device-noinit-1",
			// 	Source: asdbv1beta1.VolumeSource{
			// 		PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
			// 			Size:         resource.MustParse("1Gi"),
			// 			StorageClass: storageClass,
			// 			VolumeMode:   corev1.PersistentVolumeBlock,
			// 		},
			// 	},
			// 	Sidecars: []asdbv1beta1.VolumeAttachment{
			// 		{
			// 			ContainerName: containerName,
			// 			Path:          "/opt/aerospike/blockdevice-noinit",
			// 		},
			// 	},
			// },
			{
				Name: "device-dd-1",
				AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod: &ddInitMethod,
				},
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Sidecars: []asdbv1beta1.VolumeAttachment{
					{
						ContainerName: containerName,
						Path:          "/opt/aerospike/blockdevice-init-dd",
					},
				},
			},
			// {
			// 	Name: "device-blkdiscard-1",
			// 	AerospikePersistentVolumePolicySpec: asdbv1beta1.AerospikePersistentVolumePolicySpec{
			// 		InputInitMethod: &blkDiscardInitMethod,
			// 	},
			// 	Source: asdbv1beta1.VolumeSource{
			// 		PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
			// 			Size:         resource.MustParse("1Gi"),
			// 			StorageClass: storageClass,
			// 			VolumeMode:   corev1.PersistentVolumeBlock,
			// 		},
			// 	},
			// 	Sidecars: []asdbv1beta1.VolumeAttachment{
			// 		{
			// 			ContainerName: containerName,
			// 			Path:          "/opt/aerospike/blockdevice-init-blkdiscard",
			// 		},
			// 	},
			// },
			{
				Name: aerospikeConfigSecret,
				Source: asdbv1beta1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: tlsSecretName,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}
