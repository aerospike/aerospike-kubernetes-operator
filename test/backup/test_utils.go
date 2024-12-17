package backup

import (
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/aerospike/aerospike-backup-service/v2/pkg/dto"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/pkg/backup-service"
	backupservice "github.com/aerospike/aerospike-kubernetes-operator/test/backup_service"
)

const (
	timeout   = 2 * time.Minute
	interval  = 2 * time.Second
	namespace = "test"
)

var testCtx = context.TODO()

var backupServiceName, backupServiceNamespace string

var pkgLog = ctrl.Log.WithName("aerospikebackup")

var aerospikeNsNm = types.NamespacedName{
	Name:      "aerocluster",
	Namespace: namespace,
}

func NewBackup(backupNsNm types.NamespacedName) (*asdbv1beta1.AerospikeBackup, error) {
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
		asdbv1beta1.AerospikeClusterKey: map[string]interface{}{
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
		asdbv1beta1.BackupRoutinesKey: map[string]interface{}{
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
	backupConfig[asdbv1beta1.BackupRoutinesKey] = []interface{}{
		backupConfig[asdbv1beta1.BackupRoutinesKey],
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

func CreateBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
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

func DeleteBackup(cl client.Client, backup *asdbv1beta1.AerospikeBackup) error {
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
func validateTriggeredBackup(k8sClient client.Client, backup *asdbv1beta1.AerospikeBackup) error {
	validateNewEntries := func(currentConfigInMap map[string]interface{}, desiredConfigInMap map[string]interface{},
		fieldPath string) error {
		newCluster := desiredConfigInMap[asdbv1beta1.AerospikeClusterKey].(map[string]interface{})

		for clusterName := range newCluster {
			if _, ok := currentConfigInMap[asdbv1beta1.AerospikeClustersKey].(map[string]interface{})[clusterName]; !ok {
				return fmt.Errorf("cluster %s not found in %s backup config", clusterName, fieldPath)
			}
		}

		pkgLog.Info(fmt.Sprintf("Cluster info is found in %s backup config", fieldPath))

		routines := desiredConfigInMap[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})

		for routineName := range routines {
			if _, ok := currentConfigInMap[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})[routineName]; !ok {
				return fmt.Errorf("routine %s not found in %s backup config", routineName, fieldPath)
			}
		}

		if len(routines) != len(currentConfigInMap[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})) {
			return fmt.Errorf("backup routine count mismatch in %s backup config", fieldPath)
		}

		pkgLog.Info(fmt.Sprintf("Backup routines info is found in %s backup config", fieldPath))

		return nil
	}

	// Validate from backup service configmap
	var configmap corev1.ConfigMap
	if err := k8sClient.Get(testCtx,
		types.NamespacedName{
			Name:      backup.Spec.BackupService.Name,
			Namespace: backup.Spec.BackupService.Namespace,
		}, &configmap,
	); err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	if err := yaml.Unmarshal([]byte(configmap.Data[asdbv1beta1.BackupServiceConfigYAML]), &backupSvcConfig); err != nil {
		return err
	}

	desiredConfigInMap := make(map[string]interface{})

	if err := yaml.Unmarshal(backup.Spec.Config.Raw, &desiredConfigInMap); err != nil {
		return err
	}

	if err := validateNewEntries(backupSvcConfig, desiredConfigInMap, "configMap"); err != nil {
		return err
	}

	config, err := backupservice.GetAPIBackupSvcConfig(k8sClient, backup.Spec.BackupService.Name,
		backup.Spec.BackupService.Namespace)
	if err != nil {
		return err
	}

	return validateNewEntries(config, desiredConfigInMap, "backup-service API")
}

func namePrefix(nsNm types.NamespacedName) string {
	return nsNm.Namespace + "-" + nsNm.Name
}

func GetBackupDataPaths(k8sClient client.Client, backup *asdbv1beta1.AerospikeBackup) ([]string, error) {
	var backupK8sService corev1.Service

	// Wait for Service LB IP to be populated
	if err := wait.PollUntilContextTimeout(testCtx, interval, timeout, true,
		func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx,
				types.NamespacedName{
					Name:      backup.Spec.BackupService.Name,
					Namespace: backup.Spec.BackupService.Namespace,
				},
				&backupK8sService,
			); err != nil {
				return false, err
			}

			if backupK8sService.Status.LoadBalancer.Ingress == nil {
				return false, nil
			}

			return true, nil
		}); err != nil {
		return nil, err
	}

	var (
		config          dto.Config
		backupDataPaths []string
	)

	if err := yaml.Unmarshal(backup.Spec.Config.Raw, &config); err != nil {
		return backupDataPaths, err
	}

	serviceClient := backup_service.Client{
		Address: backupK8sService.Status.LoadBalancer.Ingress[0].IP,
		Port:    8081,
	}

	if err := wait.PollUntilContextTimeout(testCtx, interval, timeout, true,
		func(_ context.Context) (bool, error) {
			for routineName := range config.BackupRoutines {
				backups, err := serviceClient.GetFullBackupsForRoutine(routineName)
				if err != nil {
					return false, nil
				}

				if len(backups) == 0 {
					pkgLog.Info("No backups found for routine", "name", routineName)
					return false, nil
				}

				for idx := range backups {
					backupMeta := backups[idx].(map[string]interface{})
					backupDataPaths = append(backupDataPaths, backupMeta["key"].(string))
				}
			}

			return true, nil
		}); err != nil {
		return backupDataPaths, err
	}

	return backupDataPaths, nil
}
