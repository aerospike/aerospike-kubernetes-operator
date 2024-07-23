package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

const (
	timeout   = 2 * time.Minute
	interval  = 2 * time.Second
	name      = "sample-backup"
	namespace = "test"
)

var testCtx = context.TODO()

var backupServiceName, backupServiceNamespace string

var pkgLog = ctrl.Log.WithName("backup")

var aerospikeNsNm = types.NamespacedName{
	Name:      "aerocluster",
	Namespace: namespace,
}

func newBackup() (*asdbv1beta1.AerospikeBackup, error) {
	configBytes, err := getBackupConfBytes()
	if err != nil {
		return nil, err
	}

	return &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
			Config: runtime.RawExtension{
				Raw: configBytes,
			},
		},
	}, nil
}

func newBackupWithConfig(conf []byte) *asdbv1beta1.AerospikeBackup {
	return &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
			Config: runtime.RawExtension{
				Raw: conf,
			},
		},
	}
}

func getBackupObj(cl client.Client, name, namespace string) (*asdbv1beta1.AerospikeBackup, error) {
	var backup asdbv1beta1.AerospikeBackup

	if err := cl.Get(testCtx, types.NamespacedName{Name: name, Namespace: namespace}, &backup); err != nil {
		return nil, err
	}

	return &backup, nil
}

func deployBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
	if err := cl.Create(testCtx, backup); err != nil {
		return err
	}

	return waitForBackup(cl, backup, timeout)
}

func deleteBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
	if err := cl.Delete(testCtx, backup); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	return nil
}

func waitForBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup,
	timeout time.Duration) error {
	namespaceName := types.NamespacedName{
		Name: backup.Name, Namespace: backup.Namespace,
	}

	return wait.PollUntilContextTimeout(
		testCtx, 1*time.Second,
		timeout, true, func(ctx context.Context) (bool, error) {
			if err := cl.Get(ctx, namespaceName, backup); err != nil {
				return false, nil
			}

			status := asdbv1beta1.AerospikeBackupStatus{}
			status.BackupService = backup.Spec.BackupService
			status.Config = backup.Spec.Config
			status.OnDemand = backup.Spec.OnDemand

			if !reflect.DeepEqual(status, backup.Status) {
				pkgLog.Info("Backup status not updated yet")
				return false, nil
			}
			return true, nil
		})
}

func getBackupConfBytes() ([]byte, error) {
	backupConfig := getBackupConfigInMap()

	configBytes, err := json.Marshal(backupConfig)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

func getBackupConfigInMap() map[string]interface{} {
	return map[string]interface{}{
		common.AerospikeClusterKey: map[string]interface{}{
			"test-cluster": map[string]interface{}{
				"credentials": map[string]interface{}{
					"password": "admin123",
					"user":     "admin",
				},
				"seed-nodes": []map[string]interface{}{
					{
						"host-name": fmt.Sprintf("%s.%s.svc.cluster.local",
							aerospikeNsNm.Name, aerospikeNsNm.Namespace,
						),
						"port": 3000,
					},
				},
			},
		},
		common.BackupRoutinesKey: map[string]interface{}{
			"test-routine": map[string]interface{}{
				"backup-policy":      "test-policy",
				"interval-cron":      "@daily",
				"incr-interval-cron": "@hourly",
				"namespaces":         []string{"test"},
				"source-cluster":     "test-cluster",
				"storage":            "local",
			},
		},
	}
}

func getWrongBackupConfBytes() ([]byte, error) {
	backupConfig := getBackupConfigInMap()

	// change the format from map to list
	backupConfig[common.BackupRoutinesKey] = []interface{}{
		backupConfig[common.BackupRoutinesKey],
	}

	configBytes, err := json.Marshal(backupConfig)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

// validateTriggeredBackup validates if the backup is triggered by checking the current config of backup-service
func validateTriggeredBackup(k8sClient client.Client, backupServiceName, backupServiceNamespace string,
	backup *asdbv1beta1.AerospikeBackup) error {
	var backupK8sService corev1.Service

	// Wait for Service LB IP to be populated
	if err := wait.PollUntilContextTimeout(testCtx, interval, timeout, true,
		func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(testCtx,
				types.NamespacedName{Name: backupServiceName, Namespace: backupServiceNamespace},
				&backupK8sService); err != nil {
				return false, err
			}

			if backupK8sService.Status.LoadBalancer.Ingress == nil {
				return false, nil
			}

			return true, nil
		}); err != nil {
		return err
	}

	var body []byte

	// Wait for Backup service to be ready
	if err := wait.PollUntilContextTimeout(testCtx, interval, timeout, true,
		func(ctx context.Context) (bool, error) {
			resp, err := http.Get("http://" + backupK8sService.Status.LoadBalancer.Ingress[0].IP + ":8081/v1/config")
			if err != nil {
				pkgLog.Error(err, "Failed to get backup service config")
				return false, nil
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return false, fmt.Errorf("backup service config fetch failed with status code: %d",
					resp.StatusCode)
			}

			// Validate the config
			body, err = io.ReadAll(resp.Body)
			if err != nil {
				return false, err
			}

			return true, nil
		}); err != nil {
		return err
	}

	var config model.Config

	if err := yaml.Unmarshal(body, &config); err != nil {
		return err
	}

	desiredConfigMap := make(map[string]interface{})

	if err := yaml.Unmarshal(backup.Spec.Config.Raw, &desiredConfigMap); err != nil {
		return err
	}

	if _, ok := desiredConfigMap[common.AerospikeClusterKey]; !ok {
		return fmt.Errorf("aerospike-cluster key not found in backup config")
	}

	if _, ok := desiredConfigMap[common.BackupRoutinesKey]; !ok {
		return fmt.Errorf("backup-routines key not found in backup config")
	}

	newCluster := desiredConfigMap[common.AerospikeClusterKey].(map[string]interface{})

	for name := range newCluster {
		if _, ok := config.AerospikeClusters[name]; !ok {
			return fmt.Errorf("cluster %s not found in backup config", name)
		}
	}

	pkgLog.Info("Backup cluster info is found in backup config")

	routines := desiredConfigMap[common.BackupRoutinesKey].(map[string]interface{})

	for name := range routines {
		if _, ok := config.BackupRoutines[name]; !ok {
			return fmt.Errorf("routine %s not found in backup config", name)
		}
	}

	pkgLog.Info("Backup routines info is found in backup config")

	return nil
}
