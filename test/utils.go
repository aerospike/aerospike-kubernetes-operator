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
	"strings"
	"time"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	operatorutils "github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	namespace    = "test"
	storageClass = "ssd"
	pkgLog       = ctrl.Log.WithName("test")
)

var secrets map[string][]byte

const secretDir = "../config/secrets"

const tlsSecretName = "aerospike-secret"
const authSecretName = "auth"
const authSecretNameForUpdate = "auth-update"

const multiClusterNs1 string = "test1"
const multiClusterNs2 string = "test2"

const aerospikeConfigSecret string = "aerospike-config-secret"

var aerospikeVolumeInitMethodDeleteFiles = asdbv1alpha1.AerospikeVolumeInitMethodDeleteFiles

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

	if err := createConfigSecret(k8sClient, ctx, namespace, labels); err != nil {
		return err
	}

	// Create authSecret
	pass := "admin"
	if err := createAuthSecret(k8sClient, ctx, namespace, labels, authSecretName, pass); err != nil {
		return err
	}

	// Create another authSecret. Used in access-control tests
	passUpdate := "admin321"
	if err := createAuthSecret(k8sClient, ctx, namespace, labels, authSecretNameForUpdate, passUpdate); err != nil {
		return err
	}

	// Create preReq for multiclusters
	if err := createClusterResource(k8sClient, ctx); err != nil {
		return err
	}
	if err := createClusterPreReq(k8sClient, ctx, multiClusterNs1); err != nil {
		return err
	}
	if err := createClusterPreReq(k8sClient, ctx, multiClusterNs2); err != nil {
		return err
	}

	return nil
}

func createClusterPreReq(k8sClient client.Client, ctx goctx.Context, namespace string) error {
	labels := getLabels()

	if err := createConfigSecret(k8sClient, ctx, namespace, labels); err != nil {
		return err
	}

	// Create authSecret
	pass := "admin"
	if err := createAuthSecret(k8sClient, ctx, namespace, labels, authSecretName, pass); err != nil {
		return err
	}

	return nil
}

func createStorageClass(k8sClient client.Client, ctx goctx.Context) error {
	bindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	storageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ssd",
		},
		Provisioner: "kubernetes.io/gce-pd",
		// ReclaimPolicy: &deletePolicy,
		Parameters: map[string]string{
			"type": "pd-ssd",
		},
		VolumeBindingMode: &bindingMode,
	}
	err := k8sClient.Create(ctx, storageClass)
	if err != nil {
		return err
	}
	return nil
}

func createConfigSecret(k8sClient client.Client, ctx goctx.Context, namespace string, labels map[string]string) error {
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
	if err != nil {
		return err
	}
	return nil
}

func createAuthSecret(k8sClient client.Client, ctx goctx.Context, namespace string, labels map[string]string, secretName, pass string) error {

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
	if err != nil {
		return err
	}

	return nil
}

func getLabels() map[string]string {
	return map[string]string{"app": "aerospike-cluster"}
}

func waitForAerospikeCluster(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster, replicas int, retryInterval, timeout time.Duration) error {
	var isValid bool
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Fetch the AerospikeCluster instance
		newCluster := &asdbv1alpha1.AerospikeCluster{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				pkgLog.Info("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
				return false, nil
			}
			return false, err
		}

		isValid = isClusterStateValid(aeroCluster, newCluster, replicas)
		return isValid, nil
	})
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

func isClusterStateValid(aeroCluster *asdbv1alpha1.AerospikeCluster, newCluster *asdbv1alpha1.AerospikeCluster, replicas int) bool {
	if int(newCluster.Status.Size) != replicas {
		pkgLog.Info("Cluster size is not correct")
		return false
	}

	statusToSpec, err := asdbv1alpha1.CopyStatusToSpec(newCluster.Status.AerospikeClusterStatusSpec)
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

		pkgLog.Info("Cluster pod's image %s not same as spec %s", pod.Image, aeroCluster.Spec.Image)
	}
	return true
}

func getTimeout(nodes int32) time.Duration {
	return (5 * time.Minute * time.Duration(nodes))
}

// ExecuteCommandOnPod executes a command in the specified container,
// returning stdout, stderr and error.
func ExecuteCommandOnPod(cfg *rest.Config, pod *v1.Pod, containerName string, cmd ...string) (string, string, error) {
	ClientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", "", err
	}

	req := ClientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.GetNamespace()).
		SubResource("exec").
		Param("container", containerName)
	req.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, clientgoscheme.ParameterCodec)

	var stdout, stderr bytes.Buffer

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func getPodLogs(k8sClientset *kubernetes.Clientset, ctx goctx.Context, pod *v1.Pod) string {
	podLogOpts := v1.PodLogOptions{}
	req := k8sClientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "error in opening stream"
	}
	defer podLogs.Close()

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
	bytes, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("unable to marshal src: %s", err)
	}
	err = json.Unmarshal(bytes, dst)
	if err != nil {
		return fmt.Errorf("unable to unmarshal into dst: %s", err)
	}
	return nil
}
