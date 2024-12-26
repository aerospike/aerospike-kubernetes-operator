package common

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

// GetConfigSection returns the section of the config with the given name.
func GetConfigSection(config map[string]interface{}, section string) (map[string]interface{}, error) {
	sectionIface, ok := config[section]
	if !ok {
		return map[string]interface{}{}, nil
	}

	sectionMap, ok := sectionIface.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not a map", section)
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
		return nil, err
	}

	return &podList, nil
}

func ReloadBackupServiceConfigInPods(
	k8sClient client.Client,
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
			return fmt.Errorf("failed to get backup service pod list, error: %v", err)
		}

		for idx := range podList.Items {
			pod := podList.Items[idx]
			annotations := pod.Annotations

			if annotations == nil {
				annotations = make(map[string]string)
			}

			count, ok := annotations[v1beta1.ForceRefreshKey]
			if ok {
				countInt, _ := strconv.Atoi(count)
				annotations[v1beta1.ForceRefreshKey] = fmt.Sprintf("%d", countInt+1)
			} else {
				annotations[v1beta1.ForceRefreshKey] = "1"
			}

			annotations[v1beta1.RefreshTimeKey] = time.Now().Format(time.RFC3339)

			pod.Annotations = annotations

			if err := k8sClient.Update(context.TODO(), &pod); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	// Waiting for 1 second so that pods get the latest configMap update.
	time.Sleep(1 * time.Second)

	backupServiceClient, err := backup_service.GetBackupServiceClient(k8sClient, backupSvc)
	if err != nil {
		return err
	}

	err = backupServiceClient.ApplyConfig()
	if err != nil {
		return err
	}

	// TODO:// uncomment this when backup service removes default fields from the GET config API response
	// return validateBackupSvcConfigReload(k8sClient, backupServiceClient, log, backupSvc)
	log.Info("Reloaded backup service config")

	return nil
}

//nolint:unused // for future use
func validateBackupSvcConfigReload(k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	log logr.Logger,
	backupSvc *v1beta1.BackupService,
) error {
	apiBackupSvcConfig, err := backupServiceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	var cm corev1.ConfigMap

	if err := k8sClient.Get(context.TODO(), types.NamespacedName{
		Namespace: backupSvc.Namespace,
		Name:      backupSvc.Name,
	}, &cm); err != nil {
		return err
	}

	configMapBackupSvcConfig := make(map[string]interface{})

	data := cm.Data[v1beta1.BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &configMapBackupSvcConfig); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("API Backup Service Config: %v", apiBackupSvcConfig))
	log.Info(fmt.Sprintf("ConfigMap Backup Service Config: %v", configMapBackupSvcConfig))

	if !reflect.DeepEqual(apiBackupSvcConfig, configMapBackupSvcConfig) {
		log.Info("Backup service config not yet updated in pods, requeue")
		return fmt.Errorf("backup service config not yet updated in pods")
	}

	return nil
}
