package backup

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
)

// SingleBackupReconciler reconciles a single AerospikeBackup object
type SingleBackupReconciler struct {
	client.Client
	Recorder   record.EventRecorder
	aeroBackup *asdbv1beta1.AerospikeBackup
	// KubeClient and KubeConfig are used to exec into the backup service pods to check that they have the latest
	// ConfigMap before a reload; client.Client cannot run pods/exec.
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Scheme     *k8sRuntime.Scheme
	Log        logr.Logger
}

// finishReconcile logs the reconcile exit once at the boundary. It holds any
// AerospikeBackup-specific finish logic so it can grow independently of other controllers.
func (r *SingleBackupReconciler) finishReconcile(result ctrl.Result, recErr error) error {
	logValues := common.ReconcileExitLogValues(result, recErr)

	if recErr != nil {
		r.Log.Error(recErr, "Reconcile failed", logValues...)

		return recErr
	}

	r.Log.Info("Reconcile completed", logValues...)

	return nil
}

func (r *SingleBackupReconciler) Reconcile(ctx context.Context) (result ctrl.Result, recErr error) {
	defer func() {
		// finishReconcile returns the error to assign here so we avoid *error params; recErr is Reconcile's named return.
		recErr = r.finishReconcile(result, recErr)
	}()

	// Skip reconcile if the backup service version is less than 3.0.0.
	// This is a safe check to avoid any issue after AKO upgrade due to older backup service versions
	if _, err := asdbv1beta1.ValidateBackupSvcSupportedVersion(r.Client,
		r.aeroBackup.Spec.BackupService.Name,
		r.aeroBackup.Spec.BackupService.Namespace); err != nil {
		r.Log.Info("Skipping reconcile, backup service version is older than the minimum supported",
			"backupService", utils.NewNamespacedName(
				r.aeroBackup.Spec.BackupService.Namespace, r.aeroBackup.Spec.BackupService.Name),
			"minVersion", asdbv1beta1.BackupSvcMinSupportedVersion,
			"err", err)

		return reconcile.Result{}, nil
	}

	// Check DeletionTimestamp to see if the backup is being deleted
	if !r.aeroBackup.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleting AerospikeBackup")

		if err := r.cleanUpAndRemoveFinalizer(ctx, finalizerName); err != nil {
			return reconcile.Result{}, err
		}

		r.Recorder.Eventf(
			r.aeroBackup, corev1.EventTypeNormal, "Deleted",
			"Successfully deleted backup resources",
		)
		// Stop reconciliation as the backup is being deleted
		return reconcile.Result{}, nil
	}

	// The backup is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(ctx, finalizerName); err != nil {
		return reconcile.Result{}, err
	}

	configMapChanged, err := r.reconcileConfigMap(ctx)
	if err != nil {
		bs := r.aeroBackup.Spec.BackupService
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"ConfigMapReconcileFailed", "Failed to reconcile ConfigMap %s",
			utils.NamespacedName(bs.Namespace, bs.Name))

		return reconcile.Result{}, err
	}

	if err := r.reconcileBackup(ctx, configMapChanged); err != nil {
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"BackupReconcileFailed", "Failed to reconcile backup")

		return reconcile.Result{}, err
	}

	if err := r.updateStatus(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"StatusUpdateFailed", "Failed to update status")

		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SingleBackupReconciler) addFinalizer(ctx context.Context, finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(
		r.aeroBackup.Finalizers, finalizerName,
	) {
		r.aeroBackup.Finalizers = append(
			r.aeroBackup.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroBackup); err != nil {
			return fmt.Errorf("add finalizer: %w", err)
		}
	}

	return nil
}

func (r *SingleBackupReconciler) cleanUpAndRemoveFinalizer(ctx context.Context, finalizerName string) error {
	if utils.ContainsString(r.aeroBackup.Finalizers, finalizerName) {
		r.Log.Info("Removing finalizer")

		if err := r.removeBackupInfoFromConfigMap(ctx); err != nil {
			return fmt.Errorf("remove backup info from ConfigMap: %w", err)
		}

		backupServiceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
		if err != nil {
			return fmt.Errorf("get backup service client: %w", err)
		}

		if err := common.ReloadBackupServiceConfigInPods(ctx, r.Log, r.Client,
			common.NewPodExecFunc(r.KubeClient, r.KubeConfig), backupServiceClient,
			&r.aeroBackup.Spec.BackupService); err != nil {
			return fmt.Errorf("reload backup service config: %w", err)
		}

		// Remove finalizer from the list
		r.aeroBackup.Finalizers = utils.RemoveString(
			r.aeroBackup.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroBackup); err != nil {
			return fmt.Errorf("remove finalizer: %w", err)
		}

		r.Log.Info("Removed finalizer")
	}

	return nil
}

// reconcileConfigMap writes the cluster and routines of this backup into the backup service ConfigMap. It returns
// true if that changed the config, for example when the backup service was recreated with a ConfigMap that has
// none of them, so the backup service must reload even though this backup's spec is already applied.
func (r *SingleBackupReconciler) reconcileConfigMap(ctx context.Context) (bool, error) {
	bs := r.aeroBackup.Spec.BackupService

	cm, err := r.getBackupSvcConfigMap(ctx)
	if err != nil {
		return false, fmt.Errorf("get backup service ConfigMap %s: %w",
			utils.NamespacedName(bs.Namespace, bs.Name), err)
	}

	r.Log.Info("Updating existing backup service ConfigMap",
		"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name),
	)

	specConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return false, err
	}

	cmConfig := make(map[string]interface{})

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &cmConfig)
	if err != nil {
		return false, fmt.Errorf("unmarshal backup service config: %w", err)
	}

	// Merge this backup's cluster and routines from spec into a copy, so cmConfig stays as it is in the ConfigMap for
	// the comparison below.
	mergedConfig := lib.DeepCopy(cmConfig).(map[string]interface{})

	mergedClusters, err := common.GetConfigSection(mergedConfig, asdbv1beta1.AerospikeClustersKey)
	if err != nil {
		return false, fmt.Errorf("get aerospike-clusters section from backup service config: %w", err)
	}

	specCluster := specConfig[asdbv1beta1.AerospikeClusterKey].(map[string]interface{})

	var clusterName string

	// There will always be only one cluster in the backup config.
	// Cluster name in the CR will always be unique.
	// Uniqueness is maintained by having a prefix with format <backup-namespace>-<backup-name>-<cluster-name>.
	// It is enforced by the webhook.
	for name, clusterInfo := range specCluster {
		clusterName = name
		mergedClusters[name] = clusterInfo
	}

	mergedConfig[asdbv1beta1.AerospikeClustersKey] = mergedClusters

	mergedRoutines, err := common.GetConfigSection(mergedConfig, asdbv1beta1.BackupRoutinesKey)
	if err != nil {
		return false, fmt.Errorf("get backup-routines section from backup service config: %w", err)
	}

	specRoutines := specConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})

	// Remove the routines of this backup that are no longer in its spec
	staleRoutines := r.routinesToDelete(specRoutines, mergedRoutines, clusterName)

	for idx := range staleRoutines {
		delete(mergedRoutines, staleRoutines[idx])
	}

	// Add or update the routines of this backup from its spec
	for name, routine := range specRoutines {
		mergedRoutines[name] = routine
	}

	mergedConfig[asdbv1beta1.BackupRoutinesKey] = mergedRoutines

	// Compare the parsed configs, not the text: a ConfigMap that the backup service controller has just created
	// holds the spec as JSON, and this function writes YAML.
	changed := !reflect.DeepEqual(cmConfig, mergedConfig)

	// A ConfigMap written by an older operator has no hash yet, and the reload waits for it, so write it then too.
	if !changed && cm.Data[asdbv1beta1.BackupServiceConfigHashKey] != "" {
		r.Log.Info("Backup service ConfigMap already has this backup's config",
			"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name))

		return false, nil
	}

	updatedConfig, err := yaml.Marshal(mergedConfig)
	if err != nil {
		return false, fmt.Errorf("marshal backup service config: %w", err)
	}

	if err := common.SetBackupSvcConfigData(cm.Data, string(updatedConfig)); err != nil {
		return false, err
	}

	if err := r.Update(ctx, cm, common.UpdateOption); err != nil {
		return false, fmt.Errorf("update backup service ConfigMap %s: %w",
			utils.NamespacedName(bs.Namespace, bs.Name), err)
	}

	r.Log.Info("Updated backup service ConfigMap",
		"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name),
	)
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "ConfigMapUpdated",
		"Updated backup service ConfigMap %s", utils.NamespacedName(bs.Namespace, bs.Name))

	return changed, nil
}

func (r *SingleBackupReconciler) removeBackupInfoFromConfigMap(ctx context.Context) error {
	bs := r.aeroBackup.Spec.BackupService

	cm, err := r.getBackupSvcConfigMap(ctx)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Backup service ConfigMap not found, skipping update",
				"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name))

			return nil
		}

		return fmt.Errorf("get backup service ConfigMap %s: %w",
			utils.NamespacedName(bs.Namespace, bs.Name), err)
	}

	r.Log.Info("Removing backup info from ConfigMap",
		"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name),
	)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
	if err != nil {
		return fmt.Errorf("unmarshal backup service config: %w", err)
	}

	var clusterName string

	if clusterIface, ok := backupSvcConfig[asdbv1beta1.AerospikeClustersKey]; ok {
		if clusterMap, ok := clusterIface.(map[string]interface{}); ok {
			currentCluster := specBackupConfig[asdbv1beta1.AerospikeClusterKey].(map[string]interface{})
			for name := range currentCluster {
				clusterName = name
				delete(clusterMap, name)
			}

			if len(clusterMap) == 0 {
				delete(backupSvcConfig, asdbv1beta1.AerospikeClustersKey)
			} else {
				backupSvcConfig[asdbv1beta1.AerospikeClustersKey] = clusterMap
			}
		}
	}

	if routineIface, ok := backupSvcConfig[asdbv1beta1.BackupRoutinesKey]; ok {
		if routineMap, ok := routineIface.(map[string]interface{}); ok {
			routinesToBeDelete := r.routinesToDelete(nil, routineMap, clusterName)

			for idx := range routinesToBeDelete {
				delete(routineMap, routinesToBeDelete[idx])
			}

			if len(routineMap) == 0 {
				delete(backupSvcConfig, asdbv1beta1.BackupRoutinesKey)
			} else {
				backupSvcConfig[asdbv1beta1.BackupRoutinesKey] = routineMap
			}
		}
	}

	updatedConfig, err := yaml.Marshal(backupSvcConfig)
	if err != nil {
		return fmt.Errorf("marshal backup service config: %w", err)
	}

	if err := common.SetBackupSvcConfigData(cm.Data, string(updatedConfig)); err != nil {
		return err
	}

	if err := r.Update(ctx, cm, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service ConfigMap %s: %w",
			utils.NamespacedName(bs.Namespace, bs.Name), err)
	}

	r.Log.Info("Removed backup info from ConfigMap",
		"configMap", utils.NewNamespacedName(bs.Namespace, bs.Name),
	)

	return nil
}

func (r *SingleBackupReconciler) triggerOnDemandBackup() error {
	r.Log.Info("Reconciling on-demand backup")

	// There can be only one on-demand backup allowed right now.
	if len(r.aeroBackup.Status.OnDemandBackups) > 0 &&
		r.aeroBackup.Spec.OnDemandBackups[0].ID == r.aeroBackup.Status.OnDemandBackups[0].ID {
		r.Log.Info("On-demand backup already triggered for the same ID",
			"id", r.aeroBackup.Status.OnDemandBackups[0].ID)

		return nil
	}

	r.Log.Info("Triggering on-demand backup",
		"id", r.aeroBackup.Spec.OnDemandBackups[0].ID, "routine", r.aeroBackup.Spec.OnDemandBackups[0].RoutineName)

	backupServiceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return fmt.Errorf("get backup service client: %w", err)
	}

	if err = backupServiceClient.TriggerOnDemandBackup(
		r.aeroBackup.Spec.OnDemandBackups[0].RoutineName,
		r.aeroBackup.Spec.OnDemandBackups[0].Type,
		r.aeroBackup.Spec.OnDemandBackups[0].Delay,
	); err != nil {
		return fmt.Errorf("trigger on-demand backup %s: %w", r.aeroBackup.Spec.OnDemandBackups[0].ID, err)
	}

	r.Log.Info("Triggered on-demand backup",
		"id", r.aeroBackup.Spec.OnDemandBackups[0].ID,
		"routine", r.aeroBackup.Spec.OnDemandBackups[0].RoutineName)
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "OnDemandBackupTriggered",
		"Triggered on-demand backup %s", r.aeroBackup.Spec.OnDemandBackups[0].ID)

	r.Log.Info("Reconciled on-demand backup")

	return nil
}

func (r *SingleBackupReconciler) reconcileBackup(ctx context.Context, configMapChanged bool) error {
	if err := r.reconcileScheduledBackup(ctx, configMapChanged); err != nil {
		return err
	}

	return r.reconcileOnDemandBackup()
}

func (r *SingleBackupReconciler) reconcileScheduledBackup(ctx context.Context, configMapChanged bool) error {
	r.Log.Info("Reconciling scheduled backup")

	// Status is written only after a successful reconcile, so a config in spec that differs from status has
	// not been applied yet. Spec and status hold real values, so a change to a secret alone is found too.
	specApplied := reflect.DeepEqual(r.aeroBackup.Spec.Config.Raw, r.aeroBackup.Status.Config.Raw)

	if !specApplied || configMapChanged {
		r.Log.Info("Backup config not yet loaded in backup service, reload required",
			"specApplied", specApplied, "configMapChanged", configMapChanged)

		if err := r.reloadBackupServiceConfig(ctx); err != nil {
			// With spec already applied, the reload is needed only because this pass changed the ConfigMap, and the
			// next pass finds the ConfigMap already changed. Mark the config as not applied, so the next pass
			// reloads again.
			if specApplied {
				r.markConfigNotApplied(ctx)
			}

			return err
		}
	}

	r.Log.Info("Reconciled scheduled backup")
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "BackupScheduled",
		"Reconciled scheduled backup")

	return nil
}

func (r *SingleBackupReconciler) reloadBackupServiceConfig(ctx context.Context) error {
	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return fmt.Errorf("get backup service client: %w", err)
	}

	if err := common.ReloadBackupServiceConfigInPods(ctx, r.Log, r.Client,
		common.NewPodExecFunc(r.KubeClient, r.KubeConfig), serviceClient, &r.aeroBackup.Spec.BackupService); err != nil {
		return fmt.Errorf("reload backup service config: %w", err)
	}

	return nil
}

// markConfigNotApplied sets an empty status config, so it differs from spec until a reload succeeds and the
// reconcile writes status again. A failure is only logged: the reconcile already returns the reload error.
func (r *SingleBackupReconciler) markConfigNotApplied(ctx context.Context) {
	patchTarget := r.aeroBackup.DeepCopy()
	patchTarget.Status.Config = k8sRuntime.RawExtension{Raw: []byte("{}")}

	if err := common.PatchStatus(ctx, r.Client, patchTarget, &patchTarget.Status); err != nil {
		r.Log.Error(err, "Failed to mark backup config as not applied")
		return
	}

	// Copy back only status and resourceVersion so r.aeroBackup.Spec is never overwritten.
	r.aeroBackup.Status = patchTarget.Status
	r.aeroBackup.ResourceVersion = patchTarget.ResourceVersion
}

func (r *SingleBackupReconciler) reconcileOnDemandBackup() error {
	// Trigger on-demand backup if given
	if len(r.aeroBackup.Spec.OnDemandBackups) > 0 {
		if err := r.triggerOnDemandBackup(); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleBackupReconciler) updateStatus(ctx context.Context) error {
	patchTarget := r.aeroBackup.DeepCopy()
	patchTarget.Status.BackupService = r.aeroBackup.Spec.BackupService
	patchTarget.Status.Config = r.aeroBackup.Spec.Config
	patchTarget.Status.OnDemandBackups = r.aeroBackup.Spec.OnDemandBackups

	if err := common.PatchStatus(ctx, r.Client, patchTarget, &patchTarget.Status); err != nil {
		return fmt.Errorf("update AerospikeBackup status: %w", err)
	}

	// Copy back only status and resourceVersion so r.aeroBackup.Spec is never overwritten.
	r.aeroBackup.Status = patchTarget.Status
	r.aeroBackup.ResourceVersion = patchTarget.ResourceVersion

	return nil
}

func (r *SingleBackupReconciler) getBackupSvcConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}

	if err := r.Get(ctx,
		types.NamespacedName{
			Namespace: r.aeroBackup.Spec.BackupService.Namespace,
			Name:      r.aeroBackup.Spec.BackupService.Name,
		}, cm,
	); err != nil {
		return nil, err
	}

	return cm, nil
}

func (r *SingleBackupReconciler) routinesToDelete(
	specRoutines, allRoutines map[string]interface{}, clusterName string,
) []string {
	var routinesTobeDeleted []string

	for name := range allRoutines {
		if _, ok := specRoutines[name]; ok {
			continue
		}

		// Delete any dangling backup-routines related to this cluster
		// Strict prefix check might fail for cases where the prefix is same.
		if strings.HasPrefix(name, asdbv1beta1.NamePrefix(utils.GetNamespacedName(r.aeroBackup))) &&
			allRoutines[name].(map[string]interface{})[asdbv1beta1.SourceClusterKey].(string) == clusterName {
			routinesTobeDeleted = append(routinesTobeDeleted, name)
		}
	}

	return routinesTobeDeleted
}

func (r *SingleBackupReconciler) getBackupConfigInMap() (map[string]interface{}, error) {
	backupConfig := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &backupConfig); err != nil {
		return backupConfig, fmt.Errorf("unmarshal backup spec config: %w", err)
	}

	return backupConfig, nil
}
