package common

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// GetConfigSection returns the section of the config with the given name.
func GetConfigSection(config map[string]interface{}, section string) (map[string]interface{}, error) {
	sectionIface, ok := config[section]
	if !ok {
		return map[string]interface{}{}, nil
	}

	sectionMap, ok := sectionIface.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("section %q is not a map", section)
	}

	return sectionMap, nil
}

func GetBackupServicePodList(k8sClient client.Client, name, namespace string) (*corev1.PodList, error) {
	var podList corev1.PodList

	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(name))
	listOps := &client.ListOptions{
		Namespace: namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(context.TODO(), &podList, listOps); err != nil {
		return nil, fmt.Errorf("list backup service Pods %s: %w", utils.NamespacedName(namespace, name), err)
	}

	return &podList, nil
}

func ReloadBackupServiceConfigInPods(
	k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	log logr.Logger,
	backupSvc *v1beta1.BackupService,
) error {
	log.Info("Reloading backup service config")

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		podList, err := GetBackupServicePodList(k8sClient,
			backupSvc.Name,
			backupSvc.Namespace,
		)
		if err != nil {
			return err
		}

		for idx := range podList.Items {
			pod := podList.Items[idx]
			annotations := pod.Annotations

			if annotations == nil {
				annotations = make(map[string]string)
			}

			annotations[v1beta1.RefreshTimeKey] = time.Now().Format(time.RFC3339)

			pod.Annotations = annotations

			if err := k8sClient.Update(context.TODO(), &pod); err != nil {
				return fmt.Errorf("update backup service Pod %s: %w", utils.GetNamespacedNameString(&pod), err)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	// Waiting for 1 second so that pods get the latest configMap update.
	time.Sleep(1 * time.Second)

	if err := backupServiceClient.ApplyConfig(); err != nil {
		return err
	}

	return validateBackupSvcConfigReload(k8sClient, backupServiceClient, log, backupSvc)
}

func validateBackupSvcConfigReload(k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	log logr.Logger,
	backupSvc *v1beta1.BackupService,
) error {
	apiBackupSvcConfig, err := backupServiceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	desiredData, err := GetBackupSvcConfigFromCM(k8sClient, backupSvc)
	if err != nil {
		return err
	}

	synced, err := IsBackupSvcFullConfigSynced(apiBackupSvcConfig, desiredData, log)
	if err != nil {
		return err
	}

	if !synced {
		log.Info("Backup service config not yet updated in Pods, requeue")
		return fmt.Errorf("backup service config not yet updated in Pods")
	}

	log.Info("Reloaded backup service config")

	return nil
}

func IsBackupSvcFullConfigSynced(currentBackupSvcConfig map[string]interface{}, desired string,
	log logr.Logger,
) (bool, error) {
	desiredBackupSvcConfig := make(map[string]interface{})

	if err := yaml.Unmarshal([]byte(desired), &desiredBackupSvcConfig); err != nil {
		return false, err
	}

	log.Info("Fetched backup service config from backup service via API", "config", currentBackupSvcConfig)
	log.Info("Found backup service config in backup service ConfigMap", "config", desiredBackupSvcConfig)

	return reflect.DeepEqual(currentBackupSvcConfig, desiredBackupSvcConfig), nil
}

func GetBackupSvcConfigFromCM(k8sClient client.Client, backupSvc *v1beta1.BackupService) (string, error) {
	var cm corev1.ConfigMap

	if err := k8sClient.Get(context.TODO(), types.NamespacedName{
		Namespace: backupSvc.Namespace,
		Name:      backupSvc.Name,
	}, &cm); err != nil {
		return "", fmt.Errorf("get backup service ConfigMap %s: %w",
			utils.NamespacedName(backupSvc.Namespace, backupSvc.Name), err)
	}

	return cm.Data[v1beta1.BackupServiceConfigYAML], nil
}
