package backup

import (
	"context"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
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
	// The cluster is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(finalizerName); err != nil {
		r.Log.Error(err, "Failed to add finalizer")
		return reconcile.Result{}, err
	}

	// Get backup service config map
	// TODO: How to read this value
	cm := &v1.ConfigMap{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: "aerospike",
		Name:      "aerospike-backup-service-cm",
	}, cm); err != nil {
		r.Log.Error(err, "Failed to get backup service config map")
		return ctrl.Result{}, err
	}

	if err := r.UpdateConfigMap(cm); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.MakeAPICalls(); err != nil {
		return ctrl.Result{}, err
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

func (r *SingleBackupReconciler) UpdateConfigMap(cm *v1.ConfigMap) error {
	backupConfig := cm.Data[BackupConfigYAML]
	config := &model.Config{}

	if err := yaml.Unmarshal([]byte(backupConfig), config); err != nil {
		return err
	}

	if r.aeroBackup.Spec.BackupConfig.BackupPolicies != nil {
		for name, policy := range r.aeroBackup.Spec.BackupConfig.BackupPolicies {
			config.BackupPolicies[name] = policy
		}
	}

	if r.aeroBackup.Spec.BackupConfig.BackupRoutines != nil {
		for name, routine := range r.aeroBackup.Spec.BackupConfig.BackupRoutines {
			config.BackupRoutines[name] = routine
		}
	}

	if r.aeroBackup.Spec.BackupConfig.AerospikeCluster != nil {
		config.AerospikeClusters[r.aeroBackup.Spec.BackupConfig.AerospikeCluster.Name] = &r.aeroBackup.Spec.BackupConfig.AerospikeCluster.AerospikeCluster
	}

	if r.aeroBackup.Spec.BackupConfig.Storage != nil {
		for name, storage := range r.aeroBackup.Spec.BackupConfig.Storage {
			config.Storage[name] = storage
		}
	}

	updateConfig, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	cm.Data[BackupConfigYAML] = string(updateConfig)

	return r.Client.Update(context.TODO(), cm)
}

func (r *SingleBackupReconciler) MakeAPICalls() error {
	serviceClient := backup_service.GetBackupServiceClient(r.aeroBackup.Spec.ServiceConfig)

	config, err := serviceClient.GetBackupServiceConfig()
	if err != nil {
		return nil
	}

	if r.aeroBackup.Spec.BackupConfig.BackupPolicies != nil {
		for name, policy := range r.aeroBackup.Spec.BackupConfig.BackupPolicies {
			if _, ok := config.BackupPolicies[name]; ok {
				// do PUT call
				_ = policy
			} else {
				// do POST call
			}
		}
	}

	if r.aeroBackup.Spec.BackupConfig.BackupRoutines != nil {
		for name, routine := range r.aeroBackup.Spec.BackupConfig.BackupRoutines {
			if _, ok := config.BackupRoutines[name]; ok {
				// do PUT call
				_ = routine
			} else {
				// do POST call
			}
		}
	}

	if r.aeroBackup.Spec.BackupConfig.AerospikeCluster != nil {
		if _, ok := config.AerospikeClusters[r.aeroBackup.Spec.BackupConfig.AerospikeCluster.Name]; ok {
			// do PUT call
		} else {
			// do POST call
		}
	}

	if r.aeroBackup.Spec.BackupConfig.Storage != nil {
		for name, storage := range r.aeroBackup.Spec.BackupConfig.Storage {
			if _, ok := config.Storage[name]; ok {
				// do PUT call
				_ = storage
			} else {
				// do POST call
			}
		}
	}

	return nil
}
