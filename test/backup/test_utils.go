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
	namespace = "test"
)

var testCtx = context.TODO()

var backupServiceName, backupServiceNamespace string

var pkgLog = ctrl.Log.WithName("backup")

var aerospikeNsNm = types.NamespacedName{
	Name:      "aerocluster",
	Namespace: namespace,
}

func newBackup(backupNsNm types.NamespacedName) (*asdbv1beta1.AerospikeBackup, error) {
	configBytes, err := getBackupConfBytes(namePrefix(backupNsNm))
	if err != nil {
		return nil, err
	}

	backup := newBackupWithEmptyConfig(backupNsNm)

	backup.Spec.Config = runtime.RawExtension{
		Raw: configBytes,
	}

	return backup, nil
}

func newBackupWithConfig(backupNsNm types.NamespacedName, conf []byte) *asdbv1beta1.AerospikeBackup {
	backup := newBackupWithEmptyConfig(backupNsNm)

	backup.Spec.Config = runtime.RawExtension{
		Raw: conf,
	}

	return backup
}

func newBackupWithEmptyConfig(backupNsNm types.NamespacedName) *asdbv1beta1.AerospikeBackup {
	return &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupNsNm.Name,
			Namespace: backupNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
		},
	}
}

func getBackupConfBytes(prefix string) ([]byte, error) {
	backupConfig := getBackupConfigInMap(prefix)

	configBytes, err := json.Marshal(backupConfig)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

func getBackupConfigInMap(prefix string) map[string]interface{} {
	return map[string]interface{}{
		common.AerospikeClusterKey: map[string]interface{}{
			fmt.Sprintf("%s-%s", prefix, "test-cluster"): map[string]interface{}{
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
			fmt.Sprintf("%s-%s", prefix, "test-routine"): map[string]interface{}{
				"backup-policy":      "test-policy",
				"interval-cron":      "@daily",
				"incr-interval-cron": "@hourly",
				"namespaces":         []string{"test"},
				"source-cluster":     fmt.Sprintf("%s-%s", prefix, "test-cluster"),
				"storage":            "local",
			},
		},
	}
}

func getWrongBackupConfBytes(prefix string) ([]byte, error) {
	backupConfig := getBackupConfigInMap(prefix)

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

func updateBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
	if err := cl.Update(testCtx, backup); err != nil {
		return err
	}

	return waitForBackup(cl, backup, timeout)
}

func deleteBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
	if err := cl.Delete(testCtx, backup); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// Wait for the finalizer to be removed
	for {
		_, err := getBackupObj(cl, backup.Name, backup.Namespace)

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
			status.OnDemandBackups = backup.Spec.OnDemandBackups

			if !reflect.DeepEqual(status, backup.Status) {
				pkgLog.Info("Backup status not updated yet")
				return false, nil
			}
			return true, nil
		})
}

// validateTriggeredBackup validates if the backup is triggered by checking the current config of backup-service
func validateTriggeredBackup(k8sClient client.Client, backupServiceName, backupServiceNamespace string,
	backup *asdbv1beta1.AerospikeBackup) error {
	var backupK8sService corev1.Service

	validateNewEntries := func(current *model.Config, desiredConfigMap map[string]interface{}, fieldPath string) error {
		newCluster := desiredConfigMap[common.AerospikeClusterKey].(map[string]interface{})

		for clusterName := range newCluster {
			if _, ok := current.AerospikeClusters[clusterName]; !ok {
				return fmt.Errorf("cluster %s not found in %s backup config", clusterName, fieldPath)
			}
		}

		pkgLog.Info(fmt.Sprintf("Cluster info is found in %s backup config", fieldPath))

		routines := desiredConfigMap[common.BackupRoutinesKey].(map[string]interface{})

		for routineName := range routines {
			if _, ok := current.BackupRoutines[routineName]; !ok {
				return fmt.Errorf("routine %s not found in %s backup config", routineName, fieldPath)
			}
		}

		if len(routines) != len(current.BackupRoutines) {
			return fmt.Errorf("backup routine count mismatch in %s backup config", fieldPath)
		}

		pkgLog.Info(fmt.Sprintf("Backup routines info is found in %s backup config", fieldPath))

		return nil
	}

	// Validate from backup service configmap
	var configmap corev1.ConfigMap
	if err := k8sClient.Get(testCtx,
		types.NamespacedName{Name: backupServiceName, Namespace: backupServiceNamespace}, &configmap,
	); err != nil {
		return err
	}

	var config model.Config

	if err := yaml.Unmarshal([]byte(configmap.Data[common.BackupServiceConfigYAML]), &config); err != nil {
		return err
	}

	desiredConfigMap := make(map[string]interface{})

	if err := yaml.Unmarshal(backup.Spec.Config.Raw, &desiredConfigMap); err != nil {
		return err
	}

	if err := validateNewEntries(&config, desiredConfigMap, "configMap"); err != nil {
		return err
	}

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

	if err := yaml.Unmarshal(body, &config); err != nil {
		return err
	}

	return validateNewEntries(&config, desiredConfigMap, "backup-service API")
}

func namePrefix(nsNm types.NamespacedName) string {
	return nsNm.Namespace + "-" + nsNm.Name
}
