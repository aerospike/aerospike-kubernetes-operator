package common

import (
	"context"
	"encoding/json"
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

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto/decoder"
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

func GetBackupServicePodList(
	ctx context.Context, k8sClient client.Client, name, namespace string,
) (*corev1.PodList, error) {
	var podList corev1.PodList

	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(name))
	listOps := &client.ListOptions{
		Namespace: namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(ctx, &podList, listOps); err != nil {
		return nil, fmt.Errorf("list backup service Pods %s: %w", utils.NamespacedName(namespace, name), err)
	}

	return &podList, nil
}

// ReloadBackupServiceConfigInPods reloads backup service configuration in running Pods.
//
//nolint:logcheck // ctx for client calls; explicit logger (no contextual logging in AKO).
func ReloadBackupServiceConfigInPods(
	ctx context.Context,
	log logr.Logger,
	k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	backupSvc *v1beta1.BackupService,
) error {
	log.Info("Reloading backup service config")

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		podList, err := GetBackupServicePodList(ctx, k8sClient,
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

			if err := k8sClient.Update(ctx, &pod); err != nil {
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
		return fmt.Errorf("apply backup service config: %w", err)
	}

	return validateBackupSvcConfigReload(ctx, log, k8sClient, backupServiceClient, backupSvc)
}

//nolint:logcheck // ctx for client calls; explicit logger (no contextual logging in AKO).
func validateBackupSvcConfigReload(ctx context.Context, log logr.Logger, k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	backupSvc *v1beta1.BackupService,
) error {
	apiBackupSvcConfig, err := backupServiceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	desiredData, err := GetBackupSvcConfigFromCM(ctx, k8sClient, backupSvc)
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

// IsBackupSvcFullConfigSynced reports whether the config loaded in the backup service, as returned by its API,
// matches the desired config from the ConfigMap. Both are normalized first, see ComparableBackupSvcConfigs.
func IsBackupSvcFullConfigSynced(currentBackupSvcConfig map[string]interface{}, desired string,
	log logr.Logger,
) (bool, error) {
	synced, _, _, err := CompareBackupSvcConfigs(currentBackupSvcConfig, desired, log)

	return synced, err
}

// CompareBackupSvcConfigs is IsBackupSvcFullConfigSynced that also returns the two configs it compared, so a
// caller that needs them again does not normalize them a second time.
func CompareBackupSvcConfigs(currentBackupSvcConfig map[string]interface{}, desired string, log logr.Logger,
) (synced bool, comparableCurrent, comparableDesired map[string]interface{}, err error) {
	desiredBackupSvcConfig := make(map[string]interface{})

	if err := yaml.Unmarshal([]byte(desired), &desiredBackupSvcConfig); err != nil {
		return false, nil, nil, fmt.Errorf("unmarshal backup service config from ConfigMap data: %w", err)
	}

	current, desiredConfig, normalized := ComparableBackupSvcConfigs(log, currentBackupSvcConfig, desiredBackupSvcConfig)

	// Raw ConfigMap data holds literal secrets, so only normalized configs are logged.
	if normalized {
		log.V(1).Info("Fetched backup service config from backup service via API", "config", current)
		log.V(1).Info("Found backup service config in backup service ConfigMap", "config", desiredConfig)
	}

	return reflect.DeepEqual(current, desiredConfig), current, desiredConfig, nil
}

// ComparableBackupSvcConfigs prepares a config returned by the backup service API and a desired config
// for comparison. The API hides literal secret values (ABS 3.7.0 and later), so comparing it with raw
// ConfigMap data would never match. Both configs are normalized with NormalizeBackupSvcConfig and returned
// with normalized set to true. If either config cannot be normalized, both are returned unchanged with
// normalized set to false, so that callers fall back to comparing raw values instead of failing.
func ComparableBackupSvcConfigs(log logr.Logger, current, desired map[string]interface{},
) (comparableCurrent, comparableDesired map[string]interface{}, normalized bool) {
	normalizedCurrent, err := NormalizeBackupSvcConfig(current)
	if err != nil {
		log.Info("Failed to normalize backup service config from API, comparing raw config", "err", err)

		return current, desired, false
	}

	normalizedDesired, err := NormalizeBackupSvcConfig(desired)
	if err != nil {
		log.Info("Failed to normalize desired backup service config, comparing raw config", "err", err)

		return current, desired, false
	}

	return normalizedCurrent, normalizedDesired, true
}

// NormalizeBackupSvcConfig renders a backup service config the way the backup service renders
// GET /v1/config. The config is decoded into the backup service DTOs, converted to its model and back,
// and marshalled with every secret field replaced by a fixed placeholder. A literal secret becomes the
// placeholder, a secret agent reference stays as it is, enum values are canonicalized, and fields the
// backup service omits on output are omitted. Normalizing a config twice gives the same result, so a
// config read from the API and one read from the ConfigMap can both be normalized and then compared.
func NormalizeBackupSvcConfig(config map[string]interface{}) (map[string]interface{}, error) {
	dtoConfig, err := ToBackupSvcDTOConfig(config)
	if err != nil {
		return nil, err
	}

	modelConfig, err := dtoConfig.ToModel()
	if err != nil {
		return nil, fmt.Errorf("convert backup service config to model: %w", err)
	}

	rendered, err := decoder.Marshal(dto.NewConfigFromModel(modelConfig), decoder.JSON, true)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized backup service config: %w", err)
	}

	normalized := make(map[string]interface{})

	if err := json.Unmarshal(rendered, &normalized); err != nil {
		return nil, fmt.Errorf("unmarshal normalized backup service config: %w", err)
	}

	return normalized, nil
}

// ToBackupSvcDTOConfig decodes a backup service config map into the backup service DTO.
// Unknown fields are ignored, so a config from a newer backup service can still be decoded.
func ToBackupSvcDTOConfig(config map[string]interface{}) (*dto.Config, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal backup service config: %w", err)
	}

	var dtoConfig dto.Config

	if err := json.Unmarshal(data, &dtoConfig); err != nil {
		return nil, fmt.Errorf("unmarshal backup service config: %w", err)
	}

	return &dtoConfig, nil
}

func GetBackupSvcConfigFromCM(
	ctx context.Context, k8sClient client.Client, backupSvc *v1beta1.BackupService,
) (string, error) {
	var cm corev1.ConfigMap

	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: backupSvc.Namespace,
		Name:      backupSvc.Name,
	}, &cm); err != nil {
		return "", fmt.Errorf("get backup service ConfigMap %s: %w",
			utils.NamespacedName(backupSvc.Namespace, backupSvc.Name), err)
	}

	return cm.Data[v1beta1.BackupServiceConfigYAML], nil
}
