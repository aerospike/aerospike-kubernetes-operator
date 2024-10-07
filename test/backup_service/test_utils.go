package backupservice

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	app "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
)

const BackupServiceImage = "aerospike/aerospike-backup-service:2.0.0"

const (
	timeout   = 2 * time.Minute
	interval  = 2 * time.Second
	name      = "backup-service"
	namespace = "test"
)

var testCtx = context.TODO()

var pkgLog = ctrl.Log.WithName("aerospikebackupservice")

func NewBackupService() (*asdbv1beta1.AerospikeBackupService, error) {
	configBytes, err := getBackupServiceConfBytes()
	if err != nil {
		return nil, err
	}

	backupService := newBackupServiceWithEmptyConfig()
	backupService.Spec.Config = runtime.RawExtension{
		Raw: configBytes,
	}

	return backupService, nil
}

func newBackupServiceWithConfig(config []byte) *asdbv1beta1.AerospikeBackupService {
	backupService := newBackupServiceWithEmptyConfig()
	backupService.Spec.Config = runtime.RawExtension{
		Raw: config,
	}

	return backupService
}

func newBackupServiceWithEmptyConfig() *asdbv1beta1.AerospikeBackupService {
	return &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupServiceSpec{
			Image: BackupServiceImage,
			SecretMounts: []asdbv1beta1.SecretMount{
				{
					SecretName: test.AWSSecretName,
					VolumeMount: corev1.VolumeMount{
						Name:      test.AWSSecretName,
						MountPath: "/root/.aws/credentials",
						SubPath:   "credentials",
					},
				},
			},
		},
	}
}

func getBackupServiceObj(cl client.Client, name, namespace string) (*asdbv1beta1.AerospikeBackupService,
	error) {
	var backupService asdbv1beta1.AerospikeBackupService

	if err := cl.Get(testCtx, types.NamespacedName{Name: name, Namespace: namespace}, &backupService); err != nil {
		return nil, err
	}

	return &backupService, nil
}

func getBackupK8sServiceObj(cl client.Client, name, namespace string) (*corev1.Service, error) {
	var svc corev1.Service

	if err := cl.Get(testCtx, types.NamespacedName{Name: name, Namespace: namespace}, &svc); err != nil {
		return nil, err
	}

	return &svc, nil
}
func DeployBackupService(cl client.Client, backupService *asdbv1beta1.AerospikeBackupService) error {
	if err := cl.Create(testCtx, backupService); err != nil {
		return err
	}

	return waitForBackupService(cl, backupService, timeout)
}

func deployBackupServiceWithTO(cl client.Client, backupService *asdbv1beta1.AerospikeBackupService,
	timeout time.Duration) error {
	if err := cl.Create(testCtx, backupService); err != nil {
		return err
	}

	return waitForBackupService(cl, backupService, timeout)
}

func updateBackupService(cl client.Client, backupService *asdbv1beta1.AerospikeBackupService) error {
	if err := cl.Update(testCtx, backupService); err != nil {
		return err
	}

	return waitForBackupService(cl, backupService, timeout)
}

func waitForBackupService(cl client.Client, backupService *asdbv1beta1.AerospikeBackupService,
	timeout time.Duration) error {
	namespaceName := types.NamespacedName{
		Name: backupService.Name, Namespace: backupService.Namespace,
	}

	if err := wait.PollUntilContextTimeout(
		testCtx, 1*time.Second,
		timeout, true, func(ctx context.Context) (bool, error) {
			if err := cl.Get(ctx, namespaceName, backupService); err != nil {
				return false, nil
			}

			if backupService.Status.Phase != asdbv1beta1.AerospikeBackupServiceCompleted {
				pkgLog.Info(fmt.Sprintf("BackupService is in %s phase", backupService.Status.Phase))
				return false, nil
			}

			podList, err := getBackupServicePodList(cl, backupService)
			if err != nil {
				return false, nil
			}

			if len(podList.Items) != 1 {
				return false, nil
			}

			return true, nil
		}); err != nil {
		return err
	}

	var cm corev1.ConfigMap

	if err := cl.Get(testCtx, namespaceName, &cm); err != nil {
		return err
	}

	pkgLog.Info("ConfigMap is present")

	var deploy app.Deployment

	if err := cl.Get(testCtx, namespaceName, &deploy); err != nil {
		return err
	}

	pkgLog.Info("Deployment is present")

	var svc corev1.Service

	if err := cl.Get(testCtx, namespaceName, &svc); err != nil {
		return err
	}

	pkgLog.Info("Service is present")

	return nil
}

func getBackupServiceConfBytes() ([]byte, error) {
	config := getBackupServiceConfMap()

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

func getWrongBackupServiceConfBytes() ([]byte, error) {
	config := getBackupServiceConfMap()

	tempList := make([]interface{}, 0, len(config[common.BackupPoliciesKey].(map[string]interface{})))

	for _, policy := range config[common.BackupPoliciesKey].(map[string]interface{}) {
		tempList = append(tempList, policy)
	}

	// change the format from map to list
	config[common.BackupPoliciesKey] = tempList

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

func getBackupServiceConfMap() map[string]interface{} {
	return map[string]interface{}{
		common.ServiceKey: map[string]interface{}{
			"http": map[string]interface{}{
				"port": 8081,
			},
		},
		common.BackupPoliciesKey: map[string]interface{}{
			"test-policy": map[string]interface{}{
				"parallel":     3,
				"remove-files": "KeepAll",
			},
			"test-policy1": map[string]interface{}{
				"parallel":     3,
				"remove-files": "KeepAll",
			},
		},
		common.StorageKey: map[string]interface{}{
			"local": map[string]interface{}{
				"path": "/localStorage",
				"type": "local",
			},
			"s3Storage": map[string]interface{}{
				"type":       "aws-s3",
				"path":       "s3://aerospike-kubernetes-operator-test",
				"s3-region":  "us-east-1",
				"s3-profile": "default",
			},
		},
	}
}

func getBackupServicePodList(cl client.Client, backupService *asdbv1beta1.AerospikeBackupService) (*corev1.PodList,
	error) {
	var podList corev1.PodList

	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(backupService.Name))
	listOps := &client.ListOptions{
		Namespace: backupService.Namespace, LabelSelector: labelSelector,
	}

	if err := cl.List(context.TODO(), &podList, listOps); err != nil {
		return nil, err
	}

	return &podList, nil
}

func DeleteBackupService(
	k8sClient client.Client,
	backService *asdbv1beta1.AerospikeBackupService,
) error {
	deletePolicy := metav1.DeletePropagationForeground

	// Add Delete propagation policy to delete the dependent resources first
	if err := k8sClient.Delete(testCtx, backService,
		&client.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// Wait for all the dependent resources to be garbage collected by k8s
	for {
		_, err := getBackupServiceObj(k8sClient, backService.Name, backService.Namespace)

		if err != nil {
			if k8serrors.IsNotFound(err) {
				break
			}

			return err
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}
