package common

import (
	"bytes"
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
	"github.com/aerospike/aerospike-backup-service/v3/pkg/validation"
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

	if err = yaml.Unmarshal([]byte(desired), &desiredBackupSvcConfig); err != nil {
		return false, nil, nil, fmt.Errorf("unmarshal backup service config from ConfigMap data: %w", err)
	}

	current, desiredConfig, err := ComparableBackupSvcConfigs(currentBackupSvcConfig, desiredBackupSvcConfig)
	if err != nil {
		return false, nil, nil, err
	}

	// Only normalized configs are logged: the raw ConfigMap data holds literal secrets.
	log.V(1).Info("Fetched backup service config from backup service via API", "config", current)
	log.V(1).Info("Found backup service config in backup service ConfigMap", "config", desiredConfig)

	return reflect.DeepEqual(current, desiredConfig), current, desiredConfig, nil
}

// ComparableBackupSvcConfigs normalizes a config returned by the backup service API and the desired config from
// the ConfigMap with NormalizeBackupSvcConfig, so that they can be compared: the API hides literal secret values
// (ABS 3.7.0 and later), so comparing it with raw ConfigMap data would never match.
//
// A config that cannot be normalized is an error, not a reason to compare raw values: a raw comparison never
// matches, so the caller would reload on every reconcile without ever verifying the result. An invalid ConfigMap
// would fail the reload anyway. An API config the vendored backup service module cannot read comes from a backup
// service version this operator does not support, so no comparison is possible until the versions match.
func ComparableBackupSvcConfigs(current, desired map[string]interface{},
) (comparableCurrent, comparableDesired map[string]interface{}, err error) {
	normalizedDesired, err := NormalizeBackupSvcConfig(desired)
	if err != nil {
		return nil, nil, fmt.Errorf("validate backup service config in ConfigMap: %w", err)
	}

	// The API config is normalized too: versions before 3.7.0 return literal secret values, and the running
	// image may render fields differently from the vendored module. Normalizing a normalized config is a no-op.
	normalizedCurrent, err := NormalizeBackupSvcConfig(current)
	if err != nil {
		return nil, nil, fmt.Errorf("read backup service config from API, the backup service version may not be "+
			"supported by this operator: %w", err)
	}

	return normalizedCurrent, normalizedDesired, nil
}

// NormalizeBackupSvcConfig renders a backup service config the way the backup service renders
// GET /v1/config. The config is decoded into the backup service DTOs, converted to its model and back,
// and marshalled with every secret field replaced by a fixed placeholder. A literal secret becomes the
// placeholder, a secret agent reference stays as it is, enum values are canonicalized, and fields the
// backup service omits on output are omitted. Normalizing a config twice gives the same result, so a
// config read from the API and one read from the ConfigMap can both be normalized and then compared.
func NormalizeBackupSvcConfig(config map[string]interface{}) (map[string]interface{}, error) {
	normalizedDTO, err := normalizedBackupSvcDTO(config)
	if err != nil {
		return nil, err
	}

	rendered, err := decoder.Marshal(normalizedDTO, decoder.JSON, true)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized backup service config: %w", err)
	}

	normalized := make(map[string]interface{})

	if err := json.Unmarshal(rendered, &normalized); err != nil {
		return nil, fmt.Errorf("unmarshal normalized backup service config: %w", err)
	}

	return normalized, nil
}

// normalizedBackupSvcDTO converts a config to the backup service model and back, the form the backup service
// stores it in. Secret values are kept; they are redacted only when the DTO is marshalled.
func normalizedBackupSvcDTO(config map[string]interface{}) (*dto.Config, error) {
	dtoConfig, err := ToBackupSvcDTOConfig(config)
	if err != nil {
		return nil, err
	}

	modelConfig, err := dtoConfig.ToModel()
	if err != nil {
		return nil, fmt.Errorf("convert backup service config to model: %w", err)
	}

	return dto.NewConfigFromModel(modelConfig), nil
}

// FindStaticBackupSvcConfigChange returns a non-nil staticChangeErr if a field that the backup service reads only
// at start differs between two AerospikeBackupService configs. Both are normalized first, so a difference the
// backup service does not keep, such as the case of an enum value, is not a change. Secret values are compared
// as they are. An empty oldConfig means no config was applied yet: the pods started from the current ConfigMap.
func FindStaticBackupSvcConfigChange(oldConfig, newConfig []byte) (staticChangeErr, err error) {
	if len(oldConfig) == 0 {
		return nil, nil
	}

	oldDTO, err := normalizedBackupSvcDTOFromYAML(oldConfig)
	if err != nil {
		return nil, fmt.Errorf("normalize applied backup service config: %w", err)
	}

	newDTO, err := normalizedBackupSvcDTOFromYAML(newConfig)
	if err != nil {
		return nil, fmt.Errorf("normalize desired backup service config: %w", err)
	}

	return validation.ValidateStaticFieldChanges(oldDTO, newDTO), nil
}

func normalizedBackupSvcDTOFromYAML(rawConfig []byte) (*dto.Config, error) {
	config := make(map[string]interface{})

	if err := yaml.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal backup service config: %w", err)
	}

	return normalizedBackupSvcDTO(config)
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

// BackupSvcConfigAction is how a controller brings the running backup service in line with its ConfigMap.
type BackupSvcConfigAction int

const (
	// BackupSvcConfigNoAction means the backup service already runs the ConfigMap config.
	BackupSvcConfigNoAction BackupSvcConfigAction = iota
	// BackupSvcConfigReload means the backup service is told to reload the ConfigMap and the result is verified.
	BackupSvcConfigReload
	// BackupSvcConfigRestart means the backup service pods are recreated so that they read the ConfigMap at start.
	BackupSvcConfigRestart
)

// BackupSvcConfigChange describes how the backup service config differs from what the controller wants running.
type BackupSvcConfigChange struct {
	// Synced is the comparison of the API config with the ConfigMap, both normalized, so it sees every change
	// except one to a secret value.
	Synced bool
	// SpecConfigApplied is true when the spec equals the status, which holds the last applied config.
	SpecConfigApplied bool
	// StaticChange is true when a field the backup service reads only at start changed.
	StaticChange bool
	// HiddenChange is true when the spec differs from the status in a way the API cannot show: a secret value, or
	// a difference the backup service does not keep, such as the case of an enum value.
	HiddenChange bool
}

// Action decides how to apply the change.
//
// A difference that Synced sees is reloaded and verified, unless a static field changed.
//
// When Synced is true but the spec differs from the status, every difference the API can show is already running.
// Either the whole change is already running, for example a restart succeeded but the status update did not, or
// what is left cannot be verified through the API: a secret value, which it shows as "[secret]". The pods are
// restarted to load it.
func (c BackupSvcConfigChange) Action() BackupSvcConfigAction {
	switch {
	case !c.Synced && c.StaticChange:
		return BackupSvcConfigRestart
	case !c.Synced:
		return BackupSvcConfigReload
	case !c.SpecConfigApplied && c.HiddenChange:
		return BackupSvcConfigRestart
	default:
		return BackupSvcConfigNoAction
	}
}

// ApplyBackupSvcConfigAction runs the action BackupSvcConfigChange.Action chose. restart is passed in because each
// controller records the applied config in its own status.
//
//nolint:logcheck // ctx for client calls; explicit logger (no contextual logging in AKO).
func ApplyBackupSvcConfigAction(
	ctx context.Context,
	log logr.Logger,
	k8sClient client.Client,
	backupServiceClient *backup_service.Client,
	backupSvc *v1beta1.BackupService,
	action BackupSvcConfigAction,
	restart func(context.Context) error,
) error {
	switch action {
	case BackupSvcConfigRestart:
		log.Info("Restarting backup service Pod to load the config")

		return restart(ctx)
	case BackupSvcConfigReload:
		log.Info("Detected backup service config change, reloading config")

		if err := ReloadBackupServiceConfigInPods(ctx, log, k8sClient, backupServiceClient, backupSvc); err != nil {
			return fmt.Errorf("reload backup service config: %w", err)
		}
	case BackupSvcConfigNoAction:
		log.Info("Skipping update, backup service config is already latest")
	}

	return nil
}

// IsHiddenConfigChange reports whether two configs differ, with secret values as they are. It is called only
// when the API config already matches the ConfigMap, so any difference left is one GET /v1/config cannot show: a
// secret value, or a difference the backup service does not keep, such as the case of an enum value. Telling the
// two apart is not needed, the pods are restarted in both cases. Comparing the configs with secrets redacted would
// miss a secret changed together with such a difference.
//
// A config is either a backup service config or an AerospikeBackup config, whose single cluster is under
// aerospike-cluster. The configs are compared as backup service DTOs, so formatting differences in the raw YAML are
// ignored. An empty oldConfig means no config was applied yet: the pods started from the current ConfigMap.
func IsHiddenConfigChange(oldConfig, newConfig []byte) (bool, error) {
	if len(oldConfig) == 0 {
		return false, nil
	}

	oldRendered, err := renderBackupSvcConfig(oldConfig)
	if err != nil {
		return false, err
	}

	newRendered, err := renderBackupSvcConfig(newConfig)
	if err != nil {
		return false, err
	}

	return !bytes.Equal(oldRendered, newRendered), nil
}

// renderBackupSvcConfig decodes a config into the backup service DTO and renders it with secrets as they are.
func renderBackupSvcConfig(rawConfig []byte) ([]byte, error) {
	config := make(map[string]interface{})

	if err := yaml.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal backup service config: %w", err)
	}

	// An AerospikeBackup config holds its cluster under aerospike-cluster, which the backup service DTO does not
	// have. Decoding drops unknown keys, so without the rename the cluster and its password would be ignored.
	if cluster, ok := config[v1beta1.AerospikeClusterKey]; ok {
		config[v1beta1.AerospikeClustersKey] = cluster
		delete(config, v1beta1.AerospikeClusterKey)
	}

	dtoConfig, err := ToBackupSvcDTOConfig(config)
	if err != nil {
		return nil, err
	}

	rendered, err := decoder.Marshal(dtoConfig, decoder.JSON, false)
	if err != nil {
		return nil, fmt.Errorf("marshal backup service config: %w", err)
	}

	return rendered, nil
}

// PatchStatusConfig records the applied config in the status of obj with setConfig. A merge patch carries no
// resourceVersion, so it does not fail with a conflict when obj changed since it was read; a failed write would
// otherwise repeat the restart on the next reconcile. The patch is sent from a copy so that obj keeps the spec this
// reconcile applied, and only the new resourceVersion is copied back for the status update at the end.
func PatchStatusConfig[T client.Object](ctx context.Context, k8sClient client.Client, obj T, setConfig func(T)) error {
	original := obj.DeepCopyObject().(T)
	patched := obj.DeepCopyObject().(T)
	setConfig(patched)

	if err := k8sClient.Status().Patch(ctx, patched, client.MergeFrom(original)); err != nil {
		return err
	}

	setConfig(obj)
	obj.SetResourceVersion(patched.GetResourceVersion())

	return nil
}

// DeleteBackupServicePods deletes the backup service pods. The Deployment recreates them, and the new pods read
// the current ConfigMap when they start.
func DeleteBackupServicePods(ctx context.Context, k8sClient client.Client, backupSvc *v1beta1.BackupService) error {
	podList, err := GetBackupServicePodList(ctx, k8sClient, backupSvc.Name, backupSvc.Namespace)
	if err != nil {
		return err
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if err := k8sClient.Delete(ctx, pod); err != nil {
			return fmt.Errorf("delete backup service Pod %s: %w", utils.GetNamespacedNameString(pod), err)
		}
	}

	return nil
}
