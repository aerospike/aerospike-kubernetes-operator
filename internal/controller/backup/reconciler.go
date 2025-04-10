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
)

// SingleBackupReconciler reconciles a single AerospikeBackup object
type SingleBackupReconciler struct {
	client.Client
	Recorder   record.EventRecorder
	aeroBackup *asdbv1beta1.AerospikeBackup
	KubeConfig *rest.Config
	Scheme     *k8sRuntime.Scheme
	Log        logr.Logger
}

func (r *SingleBackupReconciler) Reconcile() (result ctrl.Result, recErr error) {
	// Skip reconcile if the backup service version is less than 3.0.0.
	// This is a safe check to avoid any issue after AKO upgrade due to older backup service versions
	if err := asdbv1beta1.ValidateBackupSvcSupportedVersion(r.Client,
		r.aeroBackup.Spec.BackupService.Name,
		r.aeroBackup.Spec.BackupService.Namespace); err != nil {
		r.Log.Info(fmt.Sprintf("Skipping reconcile as backup service version is less than %s",
			asdbv1beta1.BackupSvcMinSupportedVersion))
		return reconcile.Result{}, nil
	}

	// Check DeletionTimestamp to see if the backup is being deleted
	if !r.aeroBackup.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleting AerospikeBackup")

		if err := r.cleanUpAndRemoveFinalizer(finalizerName); err != nil {
			r.Log.Error(err, "Failed to remove finalizer")
			return reconcile.Result{}, err
		}

		r.Recorder.Eventf(
			r.aeroBackup, corev1.EventTypeNormal, "Deleted",
			"Deleted AerospikeBackup %s/%s", r.aeroBackup.Namespace,
			r.aeroBackup.Name,
		)
		// Stop reconciliation as the backup is being deleted
		return reconcile.Result{}, nil
	}

	// The backup is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(finalizerName); err != nil {
		r.Log.Error(err, "Failed to add finalizer")
		return reconcile.Result{}, err
	}

	if err := r.reconcileConfigMap(); err != nil {
		r.Log.Error(err, "Failed to reconcile config map")
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"ConfigMapReconcileFailed", "Failed to reconcile config map %s",
			r.aeroBackup.Spec.BackupService.String())

		return reconcile.Result{}, err
	}

	if err := r.reconcileBackup(); err != nil {
		r.Log.Error(err, "Failed to reconcile backup")
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"BackupReconcileFailed", "Failed to reconcile backup %s/%s",
			r.aeroBackup.Namespace, r.aeroBackup.Name)

		return reconcile.Result{}, err
	}

	if err := r.updateStatus(); err != nil {
		r.Log.Error(err, "Failed to update status")
		r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeWarning,
			"StatusUpdateFailed", "Failed to update AerospikeBackup status %s/%s",
			r.aeroBackup.Namespace, r.aeroBackup.Name)

		return reconcile.Result{}, err
	}

	r.Log.Info("Reconcile completed successfully")

	return ctrl.Result{}, nil
}

func (r *SingleBackupReconciler) addFinalizer(finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(
		r.aeroBackup.ObjectMeta.Finalizers, finalizerName,
	) {
		r.aeroBackup.ObjectMeta.Finalizers = append(
			r.aeroBackup.ObjectMeta.Finalizers, finalizerName,
		)

		return r.Client.Update(context.TODO(), r.aeroBackup)
	}

	return nil
}

func (r *SingleBackupReconciler) cleanUpAndRemoveFinalizer(finalizerName string) error {
	if utils.ContainsString(r.aeroBackup.ObjectMeta.Finalizers, finalizerName) {
		r.Log.Info("Removing finalizer")

		if err := r.removeBackupInfoFromConfigMap(); err != nil {
			return err
		}

		backupServiceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
		if err != nil {
			return err
		}

		if err := common.ReloadBackupServiceConfigInPods(r.Client, backupServiceClient,
			r.Log, &r.aeroBackup.Spec.BackupService); err != nil {
			return err
		}

		// Remove finalizer from the list
		r.aeroBackup.ObjectMeta.Finalizers = utils.RemoveString(
			r.aeroBackup.ObjectMeta.Finalizers, finalizerName,
		)

		if err := r.Client.Update(context.TODO(), r.aeroBackup); err != nil {
			return err
		}

		r.Log.Info("Removed finalizer")
	}

	return nil
}

func (r *SingleBackupReconciler) reconcileConfigMap() error {
	cm, err := r.getBackupSvcConfigMap()
	if err != nil {
		return fmt.Errorf("failed to fetch Backup Service configMap, name: %s, error %v",
			r.aeroBackup.Spec.BackupService.String(), err.Error())
	}

	r.Log.Info("Updating existing ConfigMap for Backup",
		"configmap", r.aeroBackup.Spec.BackupService.String(),
	)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
	if err != nil {
		return err
	}

	clusterMap, err := common.GetConfigSection(backupSvcConfig, asdbv1beta1.AerospikeClustersKey)
	if err != nil {
		return err
	}

	cluster := specBackupConfig[asdbv1beta1.AerospikeClusterKey].(map[string]interface{})

	var clusterName string

	// There will always be only one cluster in the backup config.
	// Cluster name in the CR will always be unique.
	// Uniqueness is maintained by having a prefix with format <backup-namespace>-<backup-name>-<cluster-name>.
	// It is enforced by the webhook.
	for name, clusterInfo := range cluster {
		clusterName = name
		clusterMap[name] = clusterInfo
	}

	backupSvcConfig[asdbv1beta1.AerospikeClustersKey] = clusterMap

	routineMap, err := common.GetConfigSection(backupSvcConfig, asdbv1beta1.BackupRoutinesKey)
	if err != nil {
		return err
	}

	routines := specBackupConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{})

	// Remove the routines which are not in spec
	routinesToBeDeleted := r.routinesToDelete(routines, routineMap, clusterName)

	for idx := range routinesToBeDeleted {
		delete(routineMap, routinesToBeDeleted[idx])
	}

	// Add/update spec routines
	for name, routine := range routines {
		routineMap[name] = routine
	}

	backupSvcConfig[asdbv1beta1.BackupRoutinesKey] = routineMap

	updatedConfig, err := yaml.Marshal(backupSvcConfig)
	if err != nil {
		return err
	}

	cm.Data[asdbv1beta1.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap, name: %s, error %v",
			r.aeroBackup.Spec.BackupService.String(), err,
		)
	}

	r.Log.Info("Updated Backup Service ConfigMap for Backup",
		"configmap", r.aeroBackup.Spec.BackupService.String(),
	)
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "ConfigMapUpdated",
		"Updated Backup Service ConfigMap %s for Backup %s/%s", r.aeroBackup.Spec.BackupService.String(),
		r.aeroBackup.Namespace, r.aeroBackup.Name)

	return nil
}

func (r *SingleBackupReconciler) removeBackupInfoFromConfigMap() error {
	cm, err := r.getBackupSvcConfigMap()
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Backup Service ConfigMap not found, skip updating",
				"configmap", r.aeroBackup.Spec.BackupService.String())
			return nil
		}

		return err
	}

	r.Log.Info("Removing Backup info from existing ConfigMap",
		"configmap", r.aeroBackup.Spec.BackupService.String(),
	)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
	if err != nil {
		return err
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
		return err
	}

	cm.Data[asdbv1beta1.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap, name: %s, error %v",
			r.aeroBackup.Spec.BackupService.String(), err,
		)
	}

	r.Log.Info("Removed Backup info from existing ConfigMap",
		"configmap", r.aeroBackup.Spec.BackupService.String(),
	)

	return nil
}

func (r *SingleBackupReconciler) scheduleOnDemandBackup() error {
	r.Log.Info("Reconciling on-demand backup")

	// There can be only one on-demand backup allowed right now.
	if len(r.aeroBackup.Status.OnDemandBackups) > 0 &&
		r.aeroBackup.Spec.OnDemandBackups[0].ID == r.aeroBackup.Status.OnDemandBackups[0].ID {
		r.Log.Info("On-demand backup already scheduled for the same ID",
			"ID", r.aeroBackup.Status.OnDemandBackups[0].ID)
		return nil
	}

	r.Log.Info("Scheduling on-demand backup",
		"ID", r.aeroBackup.Spec.OnDemandBackups[0].ID, "routine", r.aeroBackup.Spec.OnDemandBackups[0].RoutineName)

	backupServiceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	if err = backupServiceClient.ScheduleBackup(r.aeroBackup.Spec.OnDemandBackups[0].RoutineName,
		r.aeroBackup.Spec.OnDemandBackups[0].Delay); err != nil {
		r.Log.Error(err, "Failed to schedule on-demand backup")
		return err
	}

	r.Log.Info("Scheduled on-demand backup", "ID", r.aeroBackup.Spec.OnDemandBackups[0].ID,
		"routine", r.aeroBackup.Spec.OnDemandBackups[0].RoutineName)
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "OnDemandBackupScheduled",
		"Scheduled on-demand backup %s/%s", r.aeroBackup.Namespace, r.aeroBackup.Name)

	r.Log.Info("Reconciled on-demand backup")

	return nil
}

func (r *SingleBackupReconciler) reconcileBackup() error {
	if err := r.reconcileScheduledBackup(); err != nil {
		return err
	}

	return r.reconcileOnDemandBackup()
}

func (r *SingleBackupReconciler) reconcileScheduledBackup() error {
	r.Log.Info("Reconciling scheduled backup")

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	backupSvcConfig, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	r.Log.Info("Fetched backup service config", "config", backupSvcConfig)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	var (
		hotReloadRequired bool
		clusterName       string
	)

	if cluster, ok := specBackupConfig[asdbv1beta1.AerospikeClusterKey].(map[string]interface{}); ok {
		hotReloadRequired = r.checkForConfigUpdate(
			cluster,
			asdbv1beta1.AerospikeClustersKey,
			backupSvcConfig,
		)

		for name := range cluster {
			clusterName = name
		}
	}

	// Skip further checks if hotReloadRequired is already true
	if !hotReloadRequired {
		if routines, ok := specBackupConfig[asdbv1beta1.BackupRoutinesKey].(map[string]interface{}); ok {
			hotReloadRequired = r.checkForConfigUpdate(
				routines,
				asdbv1beta1.BackupRoutinesKey,
				backupSvcConfig,
			)

			if !hotReloadRequired {
				hotReloadRequired = r.checkForDeletedRoutines(routines, backupSvcConfig, clusterName)
			}
		}
	}

	if hotReloadRequired {
		err = common.ReloadBackupServiceConfigInPods(r.Client, serviceClient, r.Log, &r.aeroBackup.Spec.BackupService)
		if err != nil {
			return err
		}
	}

	r.Log.Info("Reconciled scheduled backup")
	r.Recorder.Eventf(r.aeroBackup, corev1.EventTypeNormal, "BackupScheduled",
		"Reconciled scheduled backup %s/%s", r.aeroBackup.Namespace, r.aeroBackup.Name)

	return nil
}

func (r *SingleBackupReconciler) checkForConfigUpdate(
	desiredConfig map[string]interface{},
	sectionKey string,
	backupSvcConfig map[string]interface{},
) bool {
	updated := false

	currentConfig, err := common.GetConfigSection(backupSvcConfig, sectionKey)
	if err != nil {
		r.Log.Error(err, "Failed to fetch config section", "section", sectionKey)
		return false
	}

	for name, config := range desiredConfig {
		if existingConfig, exists := currentConfig[name]; exists {
			if !reflect.DeepEqual(existingConfig, config) {
				r.Log.Info(
					fmt.Sprintf("%s config has changed, updating", sectionKey), "name", name,
				)

				updated = true
			}
		} else {
			r.Log.Info(
				fmt.Sprintf("Adding new entry in %s config", sectionKey), "name", name,
			)

			updated = true
		}
	}

	return updated
}

func (r *SingleBackupReconciler) checkForDeletedRoutines(
	desired map[string]interface{},
	currentConfig map[string]interface{},
	clusterName string,
) bool {
	currentRoutines, err := common.GetConfigSection(currentConfig, asdbv1beta1.BackupRoutinesKey)
	if err != nil {
		r.Log.Error(err, "Failed to fetch current routines")
		return false
	}

	toDelete := r.routinesToDelete(desired, currentRoutines, clusterName)
	if len(toDelete) > 0 {
		r.Log.Info("Routines to be deleted", "count", len(toDelete))
		return true
	}

	return false
}
func (r *SingleBackupReconciler) reconcileOnDemandBackup() error {
	// Schedule on-demand backup if given
	if len(r.aeroBackup.Spec.OnDemandBackups) > 0 {
		if err := r.scheduleOnDemandBackup(); err != nil {
			r.Log.Error(err, "Failed to schedule backup")
			return err
		}
	}

	return nil
}

func (r *SingleBackupReconciler) updateStatus() error {
	r.aeroBackup.Status.BackupService = r.aeroBackup.Spec.BackupService
	r.aeroBackup.Status.Config = r.aeroBackup.Spec.Config
	r.aeroBackup.Status.OnDemandBackups = r.aeroBackup.Spec.OnDemandBackups

	return r.Client.Status().Update(context.Background(), r.aeroBackup)
}

func (r *SingleBackupReconciler) getBackupSvcConfigMap() (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}

	if err := r.Client.Get(context.TODO(),
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
		return backupConfig, err
	}

	return backupConfig, nil
}
