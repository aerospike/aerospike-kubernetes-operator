package common

import (
	"context"
	"errors"
	"fmt"
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

// Bounds of the wait for kubelet to write a new ConfigMap into the backup service pods before a reload. The timeout
// is the one waitForDeploymentToBeReady uses.
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
			return "", fmt.Errorf("exec %v in Pod %s: %w, output: %s", cmd, utils.NamespacedName(pod.Namespace, pod.Name), err,
				strings.TrimSpace(stdout+stderr))
		}

		return stdout, nil
	}
}

// SetBackupSvcConfigData sets the backup service config in ConfigMap data together with its hash. Every write of
// the config must go through it, otherwise the hash in the pods no longer tells which config they have.
func SetBackupSvcConfigData(data map[string]string, config string) error {
	hash, err := utils.GetHash(config)
	if err != nil {
		return fmt.Errorf("hash backup service config: %w", err)
	}

	data[v1beta1.BackupServiceConfigYAML] = config
	data[v1beta1.BackupServiceConfigHashKey] = hash

	return nil
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
	// written the latest ConfigMap into the pods. Apply fails if the backup service cannot read or load the file.
	if err := waitForConfigInPods(ctx, log, k8sClient, podExec, backupSvc,
		configInPodsPollInterval, configInPodsTimeout); err != nil {
		return err
	}

	if err := backupServiceClient.ApplyConfig(); err != nil {
		return fmt.Errorf("apply backup service config: %w", err)
	}

	log.Info("Reloaded backup service config")

	return nil
}

// waitForConfigInPods waits until every running backup service pod has the config that is in the ConfigMap now.
// kubelet writes all keys of a ConfigMap volume at once, by switching the volume's ..data link to a new directory,
// so a pod whose hash file matches the hash stored in the ConfigMap also has that config file, or a newer one.
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
		wantHash, err := getBackupSvcConfigHash(pollCtx, k8sClient, backupSvc)
		if err != nil {
			return false, err
		}

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

// getBackupSvcConfigHash returns the hash stored with the config in the backup service ConfigMap. Every operator
// write of the config sets both keys together, through SetBackupSvcConfigData.
func getBackupSvcConfigHash(
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

	hash := cm.Data[v1beta1.BackupServiceConfigHashKey]
	if hash == "" {
		return "", fmt.Errorf("backup service ConfigMap %s has no %s key",
			utils.NamespacedName(backupSvc.Namespace, backupSvc.Name), v1beta1.BackupServiceConfigHashKey)
	}

	return hash, nil
}
