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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/go-version"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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

func createClusterPreReq(
	k8sClient client.Client, ctx goctx.Context, namespace string,
) error {
	labels := getLabels()

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
	if err != nil {
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
	if err != nil {
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
				if apierrors.IsNotFound(err) {
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
	version     *version.Version
	constraints *version.Constraints
	service     map[string]interface{}
	security    map[string]interface{}
	namespaces  []interface{}
}

func NewAerospikeConfSpec(v *version.Version) (*AerospikeConfSpec, error) {
	service := map[string]interface{}{
		"feature-key-file": "/etc/aerospike/secret/features.conf",
	}
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

	security := map[string]interface{}{}

	constraints, err := version.NewConstraint(">= 5.6")
	if err != nil {
		return nil, err
	}

	return &AerospikeConfSpec{
		version:     v,
		constraints: &constraints,
		service:     service,
		namespaces:  namespaces,
		security:    security,
	}, nil
}

func (acs *AerospikeConfSpec) getVersion() string {
	return acs.version.String()
}

func (acs *AerospikeConfSpec) setEnableSecurity(enableSecurity bool) {
	acs.security["enable-security"] = enableSecurity
}

func (acs *AerospikeConfSpec) setEnableQuotas(enableQuotas bool) {
	if acs.constraints.Check(acs.version) {
		acs.security["enable-quotas"] = enableQuotas
	}
}

func (acs *AerospikeConfSpec) getSpec() map[string]interface{} {
	return map[string]interface{}{
		"service":    acs.service,
		"security":   acs.security,
		"namespaces": acs.namespaces,
	}
}
