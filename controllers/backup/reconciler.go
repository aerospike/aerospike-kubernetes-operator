package backup

import (
	"context"
	"fmt"
	"reflect"

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
	cm := &corev1.ConfigMap{}

	if err := r.Client.Get(context.TODO(),
		types.NamespacedName{
			Namespace: r.aeroBackup.Spec.BackupService.Namespace,
			Name:      r.aeroBackup.Spec.BackupService.Name,
		}, cm,
	); err != nil {
		return fmt.Errorf("backup Service configMap not found, name: %s namespace: %s",
			r.aeroBackup.Spec.BackupService.Name, r.aeroBackup.Spec.BackupService.Namespace)
	}

	r.Log.Info("Updating existing ConfigMap for Backup",
		"name", r.aeroBackup.Spec.BackupService.Name,
		"namespace", r.aeroBackup.Spec.BackupService.Namespace,
	)

	specBackupConfigMap := make(map[string]interface{})
	backupSvcConfigMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &specBackupConfigMap); err != nil {
		return err
	}

	data := cm.Data[common.BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &backupSvcConfigMap); err != nil {
		return err
	}

	if _, ok := backupSvcConfigMap[common.AerospikeClustersKey]; !ok {
		backupSvcConfigMap[common.AerospikeClustersKey] = make(map[string]interface{})
	}

	clusterMap, ok := backupSvcConfigMap[common.AerospikeClustersKey].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospike-clusters is not a map")
	}

	newCluster := specBackupConfigMap[common.AerospikeClusterKey].(map[string]interface{})

	for name, cluster := range newCluster {
		clusterMap[name] = cluster
	}

	backupSvcConfigMap[common.AerospikeClustersKey] = clusterMap

	if _, ok = backupSvcConfigMap[common.BackupRoutinesKey]; !ok {
		backupSvcConfigMap[common.BackupRoutinesKey] = make(map[string]interface{})
	}

	routineMap, ok := backupSvcConfigMap[common.BackupRoutinesKey].(map[string]interface{})
	if !ok {
		return fmt.Errorf("backup-routines is not a map")
	}

	newRoutines := specBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{})

	for name, routine := range newRoutines {
		routineMap[name] = routine
	}

	// Remove the routines which are not in spec
	if len(r.aeroBackup.Status.Config.Raw) != 0 {
		statusBackupConfigMap := make(map[string]interface{})

		if err := yaml.Unmarshal(r.aeroBackup.Status.Config.Raw, &statusBackupConfigMap); err != nil {
			return err
		}

		for name := range statusBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{}) {
			if _, ok := specBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{})[name]; !ok {
				delete(routineMap, name)
			}
		}
	}

	backupSvcConfigMap[common.BackupRoutinesKey] = routineMap

	updatedConfig, err := yaml.Marshal(backupSvcConfigMap)
	if err != nil {
		return err
	}

	cm.Data[common.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap: %v",
			err,
		)
	}

	r.Log.Info("Updated Backup Service ConfigMap for Backup",
		"name", r.aeroBackup.Spec.BackupService.Name,
		"namespace", r.aeroBackup.Spec.BackupService.Namespace,
	)

	return nil
}

func (r *SingleBackupReconciler) removeBackupInfoFromConfigMap() error {
	cm := &corev1.ConfigMap{}

	if err := r.Client.Get(context.TODO(),
		types.NamespacedName{
			Namespace: r.aeroBackup.Spec.BackupService.Namespace,
			Name:      r.aeroBackup.Spec.BackupService.Name,
		}, cm,
	); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}

	r.Log.Info("Removing Backup info from existing ConfigMap",
		"name", r.aeroBackup.Spec.BackupService.Name,
		"namespace", r.aeroBackup.Spec.BackupService.Namespace,
	)

	backupDataMap := make(map[string]interface{})
	cmDataMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &backupDataMap); err != nil {
		return err
	}

	data := cm.Data[common.BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &cmDataMap); err != nil {
		return err
	}

	if clusterIface, ok := cmDataMap[common.AerospikeClustersKey]; ok {
		if clusterMap, ok := clusterIface.(map[string]interface{}); ok {
			currentCluster := backupDataMap[common.AerospikeClusterKey].(map[string]interface{})
			for name := range currentCluster {
				delete(clusterMap, name)
			}

			cmDataMap[common.AerospikeClustersKey] = clusterMap
		}
	}

	if routineIface, ok := cmDataMap[common.BackupRoutinesKey]; ok {
		if routineMap, ok := routineIface.(map[string]interface{}); ok {
			currentRoutines := backupDataMap[common.BackupRoutinesKey].(map[string]interface{})
			for name := range currentRoutines {
				delete(routineMap, name)
			}

			cmDataMap[common.BackupRoutinesKey] = routineMap
		}
	}

	updatedConfig, err := yaml.Marshal(cmDataMap)
	if err != nil {
		return err
	}

	cm.Data[common.BackupServiceConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap: %v",
			err,
		)
	}

	r.Log.Info("Removed Backup info from existing ConfigMap",
		"name", r.aeroBackup.Spec.BackupService.Name,
		"namespace", r.aeroBackup.Spec.BackupService.Namespace,
	)

	return nil
}

func (r *SingleBackupReconciler) scheduleOnDemandBackup() error {
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

	return nil
}

func (r *SingleBackupReconciler) reconcileBackup() error {
	if err := r.reconcileScheduledBackup(); err != nil {
		return err
	}

	return r.reconcileOnDemandBackup()
}

func (r *SingleBackupReconciler) reconcileScheduledBackup() error {
	specHash, err := utils.GetHash(string(r.aeroBackup.Spec.Config.Raw))
	if err != nil {
		return err
	}

	statusHash, err := utils.GetHash(string(r.aeroBackup.Status.Config.Raw))
	if err != nil {
		return err
	}

	if specHash == statusHash {
		r.Log.Info("Backup config not changed")
		return nil
	}

	r.Log.Info("Registering backup")

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, &r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	config, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	r.Log.Info("Fetched backup service config", "config", config)

	specBackupConfigMap := make(map[string]interface{})

	err = yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &specBackupConfigMap)
	if err != nil {
		return err
	}

	if specBackupConfigMap[common.AerospikeClusterKey] != nil {
		cluster := specBackupConfigMap[common.AerospikeClusterKey].(map[string]interface{})

		currentClusters, gErr := common.GetConfigSection(config, common.AerospikeClustersKey)
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
				err = serviceClient.AddCluster(name, clusterConfig)
				if err != nil {
					return err
				}
			}
		}
	}

	if specBackupConfigMap[common.BackupRoutinesKey] != nil {
		routines := specBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{})

		currentRoutines, gErr := common.GetConfigSection(config, common.BackupRoutinesKey)
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
				err = serviceClient.AddBackupRoutine(name, routine)
				if err != nil {
					return err
				}
			}
		}
	}

	// If there are routines that are removed, unregister them
	if len(r.aeroBackup.Status.Config.Raw) != 0 {
		err = r.unregisterBackupRoutines(specBackupConfigMap, serviceClient)
		if err != nil {
			return err
		}
	}

	// Apply the updated configuration for the changes to take effect
	err = serviceClient.ApplyConfig()
	if err != nil {
		return err
	}

	r.Log.Info("Registered backup")

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

	config, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	backupConfigMap := make(map[string]interface{})

	err = yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &backupConfigMap)
	if err != nil {
		return err
	}

	if backupConfigMap[common.BackupRoutinesKey] != nil {
		routines := backupConfigMap[common.BackupRoutinesKey].(map[string]interface{})

		currentRoutines, gErr := common.GetConfigSection(config, common.BackupRoutinesKey)
		if gErr != nil {
			return gErr
		}

		for name := range routines {
			if _, ok := currentRoutines[name]; ok {
				err = serviceClient.DeleteBackupRoutine(name)
				if err != nil {
					return err
				}
			}
		}
	}

	if backupConfigMap[common.AerospikeClusterKey] != nil {
		cluster := backupConfigMap[common.AerospikeClusterKey].(map[string]interface{})

		currentClusters, gErr := common.GetConfigSection(config, common.AerospikeClustersKey)
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

func (r *SingleBackupReconciler) unregisterBackupRoutines(
	specBackupConfigMap map[string]interface{},
	serviceClient *backup_service.Client,
) error {
	statusBackupConfigMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackup.Status.Config.Raw, &statusBackupConfigMap); err != nil {
		return err
	}

	// Unregister the backup routines which are removed
	for name := range statusBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{}) {
		if _, ok := specBackupConfigMap[common.BackupRoutinesKey].(map[string]interface{})[name]; !ok {
			r.Log.Info("Unregistering backup routine", "routine", name)

			if err := serviceClient.DeleteBackupRoutine(name); err != nil {
				return err
			}

			r.Log.Info("Unregistered backup routine", "routine", name)
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
