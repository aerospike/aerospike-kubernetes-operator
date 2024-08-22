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

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
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
	// Check DeletionTimestamp to see if the backup is being deleted
	if !r.aeroBackup.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.removeFinalizer(finalizerName); err != nil {
			r.Log.Error(err, "Failed to remove finalizer")
			return reconcile.Result{}, err
		}

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
		return reconcile.Result{}, err
	}

	if err := r.reconcileBackup(); err != nil {
		r.Log.Error(err, "Failed to reconcile backup")
		return reconcile.Result{}, err
	}

	if err := r.updateStatus(); err != nil {
		r.Log.Error(err, "Failed to update status")
		return reconcile.Result{}, err
	}

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

		if err := r.Client.Update(context.TODO(), r.aeroBackup); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleBackupReconciler) removeFinalizer(finalizerName string) error {
	if utils.ContainsString(r.aeroBackup.ObjectMeta.Finalizers, finalizerName) {
		if err := r.removeBackupInfoFromConfigMap(); err != nil {
			return err
		}

		if err := r.unregisterBackup(); err != nil {
			return err
		}

		r.Log.Info("Removing finalizer")
		// Remove finalizer from the list
		r.aeroBackup.ObjectMeta.Finalizers = utils.RemoveString(
			r.aeroBackup.ObjectMeta.Finalizers, finalizerName,
		)

		if err := r.Client.Update(context.TODO(), r.aeroBackup); err != nil {
			return err
		}
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
		"name", r.aeroBackup.Spec.BackupService.String(),
	)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	data := cm.Data[common.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
	if err != nil {
		return err
	}

	clusterMap, err := common.GetConfigSection(backupSvcConfig, common.AerospikeClustersKey)
	if err != nil {
		return err
	}

	cluster := specBackupConfig[common.AerospikeClusterKey].(map[string]interface{})

	var clusterName string

	// There will always be only one cluster in the backup config.
	// Cluster name in the CR will always be unique.
	// Uniqueness is maintained by having a prefix with format <backup-namespace>-<backup-name>-<cluster-name>.
	// It is enforced by the webhook.
	for name, clusterInfo := range cluster {
		clusterName = name
		clusterMap[name] = clusterInfo
	}

	backupSvcConfig[common.AerospikeClustersKey] = clusterMap

	routineMap, err := common.GetConfigSection(backupSvcConfig, common.BackupRoutinesKey)
	if err != nil {
		return err
	}

	routines := specBackupConfig[common.BackupRoutinesKey].(map[string]interface{})

	// Remove the routines which are not in spec
	routinesToBeDeleted := r.routinesToDelete(routines, routineMap, clusterName)

	for idx := range routinesToBeDeleted {
		delete(routineMap, routinesToBeDeleted[idx])
	}

	// Add/update spec routines
	for name, routine := range routines {
		routineMap[name] = routine
	}

	backupSvcConfig[common.BackupRoutinesKey] = routineMap

	updatedConfig, err := yaml.Marshal(backupSvcConfig)
	if err != nil {
		return err
	}

	cm.Data[common.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap, name: %s, error %v",
			r.aeroBackup.Spec.BackupService.String(), err,
		)
	}

	r.Log.Info("Updated Backup Service ConfigMap for Backup",
		"name", r.aeroBackup.Spec.BackupService.String(),
	)

	return nil
}

func (r *SingleBackupReconciler) removeBackupInfoFromConfigMap() error {
	cm, err := r.getBackupSvcConfigMap()
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Backup Service ConfigMap not found, skip updating",
				"name", r.aeroBackup.Spec.BackupService.String())
			return nil
		}

		return err
	}

	r.Log.Info("Removing Backup info from existing ConfigMap",
		"name", r.aeroBackup.Spec.BackupService.String(),
	)

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	backupSvcConfig := make(map[string]interface{})

	data := cm.Data[common.BackupServiceConfigYAML]

	err = yaml.Unmarshal([]byte(data), &backupSvcConfig)
	if err != nil {
		return err
	}

	var clusterName string

	if clusterIface, ok := backupSvcConfig[common.AerospikeClustersKey]; ok {
		if clusterMap, ok := clusterIface.(map[string]interface{}); ok {
			currentCluster := specBackupConfig[common.AerospikeClusterKey].(map[string]interface{})
			for name := range currentCluster {
				clusterName = name
				delete(clusterMap, name)
			}

			backupSvcConfig[common.AerospikeClustersKey] = clusterMap
		}
	}

	if routineIface, ok := backupSvcConfig[common.BackupRoutinesKey]; ok {
		if routineMap, ok := routineIface.(map[string]interface{}); ok {
			routinesToBeDelete := r.routinesToDelete(nil, routineMap, clusterName)

			for idx := range routinesToBeDelete {
				delete(routineMap, routinesToBeDelete[idx])
			}

			backupSvcConfig[common.BackupRoutinesKey] = routineMap
		}
	}

	updatedConfig, err := yaml.Marshal(backupSvcConfig)
	if err != nil {
		return err
	}

	cm.Data[common.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap, name: %s, error %v",
			r.aeroBackup.Spec.BackupService.String(), err,
		)
	}

	r.Log.Info("Removed Backup info from existing ConfigMap",
		"name", r.aeroBackup.Spec.BackupService.String(),
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

	r.Log.Info("Schedule on-demand backup",
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

	r.Log.Info("Reconciled scheduled backup")

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

	if specBackupConfig[common.AerospikeClusterKey] != nil {
		cluster := specBackupConfig[common.AerospikeClusterKey].(map[string]interface{})

		currentClusters, gErr := common.GetConfigSection(backupSvcConfig, common.AerospikeClustersKey)
		if gErr != nil {
			return gErr
		}

		// TODO: Remove these API calls when hot reload is implemented
		for name, clusterConfig := range cluster {
			if _, ok := currentClusters[name]; ok {
				// Only update if there is any change
				if !reflect.DeepEqual(currentClusters[name], clusterConfig) {
					r.Log.Info("Cluster config has been changed, updating it", "cluster", name)

					err = serviceClient.PutCluster(name, clusterConfig)
					if err != nil {
						return err
					}
				}
			} else {
				r.Log.Info("Adding new cluster", "cluster", name)

				err = serviceClient.AddCluster(name, clusterConfig)
				if err != nil {
					return err
				}

				r.Log.Info("Added new cluster", "cluster", name)
			}
		}
	}

	if specBackupConfig[common.BackupRoutinesKey] != nil {
		routines := specBackupConfig[common.BackupRoutinesKey].(map[string]interface{})

		currentRoutines, gErr := common.GetConfigSection(backupSvcConfig, common.BackupRoutinesKey)
		if gErr != nil {
			return gErr
		}

		// TODO: Remove these API calls when hot reload is implemented
		for name, routine := range routines {
			if _, ok := currentRoutines[name]; ok {
				// Only update if there is any change
				if !reflect.DeepEqual(currentRoutines[name], routine) {
					r.Log.Info("Routine config has been changed, updating it", "routine", name)

					err = serviceClient.PutBackupRoutine(name, routine)
					if err != nil {
						return err
					}
				}
			} else {
				r.Log.Info("Adding new backup routine", "routine", name)

				err = serviceClient.AddBackupRoutine(name, routine)
				if err != nil {
					return err
				}

				r.Log.Info("Added new backup routine", "routine", name)
			}
		}
	}

	// If there are routines that are removed, unregister them
	err = r.deregisterBackupRoutines(serviceClient, backupSvcConfig, specBackupConfig)
	if err != nil {
		return err
	}

	// Apply the updated configuration for the changes to take effect
	err = serviceClient.ApplyConfig()
	if err != nil {
		return err
	}

	r.Log.Info("Reconciled scheduled backup")

	return nil
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

func (r *SingleBackupReconciler) unregisterBackup() error {
	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	backupSvcConfig, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	specBackupConfig, err := r.getBackupConfigInMap()
	if err != nil {
		return err
	}

	err = r.deregisterBackupRoutines(serviceClient, backupSvcConfig, specBackupConfig)
	if err != nil {
		return err
	}

	if specBackupConfig[common.AerospikeClusterKey] != nil {
		cluster := specBackupConfig[common.AerospikeClusterKey].(map[string]interface{})

		currentClusters, gErr := common.GetConfigSection(backupSvcConfig, common.AerospikeClustersKey)
		if gErr != nil {
			return gErr
		}

		for name := range cluster {
			if _, ok := currentClusters[name]; ok {
				err = serviceClient.DeleteCluster(name)
				if err != nil {
					return err
				}
			}
		}
	}

	// Apply the updated configuration for the changes to take effect
	err = serviceClient.ApplyConfig()
	if err != nil {
		return err
	}

	return nil
}

func (r *SingleBackupReconciler) deregisterBackupRoutines(
	serviceClient *backup_service.Client,
	backupSvcConfig,
	specBackupConfig map[string]interface{},
) error {
	allRoutines, err := common.GetConfigSection(backupSvcConfig, common.BackupRoutinesKey)
	if err != nil {
		return err
	}

	cluster := specBackupConfig[common.AerospikeClusterKey].(map[string]interface{})

	var clusterName string

	// There will always be only one cluster in the backup config
	for name := range cluster {
		clusterName = name
	}

	specRoutines := make(map[string]interface{})

	// Ignore routines from the spec if the backup is being deleted
	if r.aeroBackup.DeletionTimestamp.IsZero() {
		specRoutines = specBackupConfig[common.BackupRoutinesKey].(map[string]interface{})
	}

	routinesToBeDelete := r.routinesToDelete(specRoutines, allRoutines, clusterName)

	for idx := range routinesToBeDelete {
		r.Log.Info("Unregistering backup routine", "routine", routinesToBeDelete[idx])

		if err := serviceClient.DeleteBackupRoutine(routinesToBeDelete[idx]); err != nil {
			return err
		}

		r.Log.Info("Unregistered backup routine", "routine", routinesToBeDelete[idx])
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
		if strings.HasPrefix(name, r.aeroBackup.NamePrefix()) &&
			allRoutines[name].(map[string]interface{})[common.SourceClusterKey].(string) == clusterName {
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
