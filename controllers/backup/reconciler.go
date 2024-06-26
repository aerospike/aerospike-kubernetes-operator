package backup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

const BackupConfigYAML = "aerospike-backup-service.yml"

// SingleClusterReconciler reconciles a single AerospikeCluster
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

		// Stop reconciliation as the cluster is being deleted
		return reconcile.Result{}, nil
	}

	// The cluster is not being deleted, add finalizer if not added already
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
		return fmt.Errorf("Backup Service configMap not found, name: %s namespace: %s",
			r.aeroBackup.Spec.BackupService.Name, r.aeroBackup.Spec.BackupService.Namespace)
	}

	r.Log.Info("Updating existing ConfigMap for Backup",
		"name", r.aeroBackup.Name, "namespace", r.aeroBackup.Namespace)

	backupDataMap := make(map[string]interface{})
	cmDataMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &backupDataMap); err != nil {
		return err
	}

	data := cm.Data[BackupConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &cmDataMap); err != nil {
		return err
	}

	if _, ok := cmDataMap["aerospike-clusters"]; !ok {
		cmDataMap["aerospike-clusters"] = make(map[string]interface{})
	}

	clusterMap, ok := cmDataMap["aerospike-clusters"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospike-clusters is not a map")
	}

	newCluster := backupDataMap["aerospike-cluster"].(map[string]interface{})

	for name, cluster := range newCluster {
		clusterMap[name] = cluster
	}

	cmDataMap["aerospike-clusters"] = clusterMap

	if _, ok = cmDataMap["backup-routines"]; !ok {
		cmDataMap["backup-routines"] = make(map[string]interface{})
	}

	routineMap, ok := cmDataMap["backup-routines"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("backup-routines is not a map")
	}

	newRoutines := backupDataMap["backup-routines"].(map[string]interface{})

	for name, routine := range newRoutines {
		routineMap[name] = routine
	}

	cmDataMap["backup-routines"] = routineMap

	updatedConfig, err := yaml.Marshal(cmDataMap)
	if err != nil {
		return err
	}

	cm.Data[BackupConfigYAML] = string(updatedConfig)

	if err := r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap: %v",
			err,
		)
	}

	r.Log.Info("Updated Backup Service ConfigMap for Backup",
		"name", r.aeroBackup.Name, "namespace", r.aeroBackup.Namespace)

	return nil
}

func (r *SingleBackupReconciler) ScheduleOnDemandBackup() error {
	r.Log.Info("Schedule on-demand backup", "name", r.aeroBackup.Name, "namespace", r.aeroBackup.Namespace)

	backupServiceClient, err := backup_service.GetBackupServiceClient(r.Client, r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	if err := backupServiceClient.ScheduleBackup(r.aeroBackup.Spec.OnDemand.RoutineName); err != nil {
		r.Log.Error(err, "Failed to schedule on-demand backup")
		return err
	}

	return nil
}

func (r *SingleBackupReconciler) reconcileBackup() error {
	r.Log.Info("Registering backup", "name", r.aeroBackup.Name, "namespace", r.aeroBackup.Namespace)

	serviceClient, err := backup_service.GetBackupServiceClient(r.Client, r.aeroBackup.Spec.BackupService)
	if err != nil {
		return err
	}

	config, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	r.Log.Info("Fetched backup service config", "config", config)

	backupConfigMap := make(map[string]interface{})
	if err = yaml.Unmarshal(r.aeroBackup.Spec.Config.Raw, &backupConfigMap); err != nil {
		return err
	}

	if backupConfigMap["aerospike-cluster"] != nil {
		cluster := backupConfigMap["aerospike-cluster"].(map[string]interface{})

		currentClusters, err := common.GetConfigSection(config, "aerospike-clusters")
		if err != nil {
			return err
		}

		for name, clusterConfig := range cluster {
			if _, ok := currentClusters[name]; ok {
				if err = serviceClient.PutCluster(name, clusterConfig); err != nil {
					return err
				}
			} else {
				if err = serviceClient.UpdateCluster(name, clusterConfig); err != nil {
					return err
				}
			}
		}
	}

	if backupConfigMap["backup-routines"] != nil {
		routines := backupConfigMap["backup-routines"].(map[string]interface{})

		currentRoutines, err := common.GetConfigSection(config, "backup-routines")
		if err != nil {
			return err
		}
		for name, routine := range routines {
			if _, ok := currentRoutines[name]; ok {
				if err := serviceClient.PutBackupRoutine(name, routine); err != nil {
					return err
				}
			} else {
				if err := serviceClient.UpdateBackupRoutine(name, routine); err != nil {
					return err
				}
			}
		}
	}

	//if r.aeroBackup.Spec.Config.BackupPolicies != nil {
	//	for name, policy := range r.aeroBackup.Spec.Config.BackupPolicies {
	//		if _, ok := config.BackupPolicies[name]; ok {
	//			if err := serviceClient.PutBackupPolicy(name, policy); err != nil {
	//				return err
	//			}
	//		} else {
	//			if err := serviceClient.UpdateBackupPolicy(name, policy); err != nil {
	//				return err
	//			}
	//		}
	//	}
	//}

	//if r.aeroBackup.Spec.Config.Storage != nil {
	//	for name, storage := range r.aeroBackup.Spec.Config.Storage {
	//		if _, ok := config.Storage[name]; ok {
	//			if err := serviceClient.PutStorage(name, storage); err != nil {
	//				return err
	//			}
	//		} else {
	//			if err := serviceClient.UpdateStorage(name, storage); err != nil {
	//				return err
	//			}
	//		}
	//	}
	//}

	// Apply the updated configuration for the changes to take effect
	if err = serviceClient.ApplyConfig(); err != nil {
		return err
	}

	// Schedule on-demand backup if given
	if r.aeroBackup.Spec.OnDemand != nil {
		if err = r.ScheduleOnDemandBackup(); err != nil {
			r.Log.Error(err, "Failed to schedule backup")
			return err
		}
	}

	return nil
}
