package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

const (
	configInPodsPollInterval = 2 * time.Second
	configInPodsTimeout      = 2 * time.Minute
)

// PodExecFunc runs a command in a container of a pod and returns its stdout.
type PodExecFunc func(pod types.NamespacedName, container string, cmd []string) (string, error)

// NewPodExecFunc returns a PodExecFunc that runs commands through the pods/exec API.
func NewPodExecFunc(kubeClient *kubernetes.Clientset, kubeConfig *rest.Config) PodExecFunc {
	return func(pod types.NamespacedName, container string, cmd []string) (string, error) {
		stdout, stderr, err := utils.Exec(pod, container, cmd, kubeClient, kubeConfig)
		if err != nil {
			// The exec runs with a TTY, so the command's error message is in stdout, not stderr.
			return "", fmt.Errorf("exec %v in Pod %s: %w, output: %s", cmd, pod, err,
				strings.TrimSpace(stdout+stderr))
		}

		return stdout, nil
	}
}

// BackupSvcConfigHash returns the value written under BackupServiceConfigHashKey for a backup service config.
func BackupSvcConfigHash(config string) string {
	sum := sha256.Sum256([]byte(config))

	return hex.EncodeToString(sum[:])
}

// SetBackupSvcConfigData sets the backup service config in ConfigMap data together with its hash. Every write of
// the config must go through it, otherwise the hash in the pods no longer tells which config they have.
func SetBackupSvcConfigData(data map[string]string, config string) {
	data[v1beta1.BackupServiceConfigYAML] = config
	data[v1beta1.BackupServiceConfigHashKey] = BackupSvcConfigHash(config)
}

// ReloadBackupServiceConfigInPods reloads backup service configuration in running Pods.
//
//nolint:logcheck // ctx for client calls; explicit logger (no contextual logging in AKO).
func ReloadBackupServiceConfigInPods(
	ctx context.Context,
	log logr.Logger,
	k8sClient client.Client,
	podExec PodExecFunc,
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

	// Apply makes the backup service read its config file again, so it must not be sent before kubelet has
	// written the latest ConfigMap into the pods. The API hides literal secrets, so after apply a change to a
	// secret alone cannot be seen; checking the file in the pods first is the only way to know it is loaded.
	if err := waitForConfigInPods(ctx, log, k8sClient, podExec, backupSvc,
		configInPodsPollInterval, configInPodsTimeout); err != nil {
		return err
	}

	if err := backupServiceClient.ApplyConfig(); err != nil {
		return fmt.Errorf("apply backup service config: %w", err)
	}

	return validateBackupSvcConfigReload(ctx, log, k8sClient, backupServiceClient, backupSvc)
}

// waitForConfigInPods waits until every running backup service pod has the config that is in the ConfigMap now.
// kubelet writes all keys of a ConfigMap volume at once, by switching the volume's ..data link to a new directory,
// so a pod whose hash file matches the hash of the ConfigMap config also has that config file, or a newer one.
// The ConfigMap is read again on every poll: a newer write while waiting changes the hash to wait for.
//
//nolint:logcheck // ctx for client calls; explicit logger (no contextual logging in AKO).
func waitForConfigInPods(
	ctx context.Context,
	log logr.Logger,
	k8sClient client.Client,
	podExec PodExecFunc,
	backupSvc *v1beta1.BackupService,
	interval, timeout time.Duration,
) error {
	hashFile := v1beta1.BackupServiceConfigMountPath + "/" + v1beta1.BackupServiceConfigHashKey

	var lastErr error

	if err := wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(pollCtx context.Context) (bool, error) {
		config, err := GetBackupSvcConfigFromCM(pollCtx, k8sClient, backupSvc)
		if err != nil {
			return false, err
		}

		wantHash := BackupSvcConfigHash(config)

		podList, err := GetBackupServicePodList(pollCtx, k8sClient, backupSvc.Name, backupSvc.Namespace)
		if err != nil {
			return false, err
		}

		running := 0

		for idx := range podList.Items {
			pod := &podList.Items[idx]

			// A pod that is not running yet reads the latest ConfigMap when it starts; a terminating pod is going away.
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}

			// A crashing backup service cannot read the file either, so do not wait for it until the timeout.
			if err := utils.CheckPodFailed(pod); err != nil {
				return false, fmt.Errorf("check backup service Pod %s: %w", utils.GetNamespacedNameString(pod), err)
			}

			running++

			out, err := podExec(utils.GetNamespacedName(pod), v1beta1.AerospikeBackupServiceKey,
				[]string{"cat", hashFile})
			if err != nil {
				lastErr = err

				log.V(1).Info("Backup service Pod config not readable yet", "pod", utils.GetNamespacedName(pod),
					"err", err)

				return false, nil
			}

			// The exec runs with a TTY, which can turn the trailing newline into "\r\n".
			if gotHash := strings.TrimSpace(out); gotHash != wantHash {
				lastErr = fmt.Errorf("backup service Pod %s has config hash %q, ConfigMap has %q",
					utils.GetNamespacedNameString(pod), gotHash, wantHash)

				log.Info("Waiting for backup service Pod to get the latest ConfigMap",
					"pod", utils.GetNamespacedName(pod))

				return false, nil
			}
		}

		if running == 0 {
			lastErr = errors.New("no running backup service Pod")

			return false, nil
		}

		return true, nil
	}); err != nil {
		if lastErr != nil {
			return fmt.Errorf("wait for backup service Pods to get the latest ConfigMap: %w: %w", err, lastErr)
		}

		return fmt.Errorf("wait for backup service Pods to get the latest ConfigMap: %w", err)
	}

	log.Info("Backup service Pods have the latest ConfigMap")

	return nil
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
