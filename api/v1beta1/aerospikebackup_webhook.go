/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

// log is for logging in this package.
var aerospikebackuplog = logf.Log.WithName("aerospikebackup-resource")

func (r *AerospikeBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

var _ webhook.Defaulter = &AerospikeBackup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeBackup) Default() {
	aerospikebackuplog.Info("default", "name", r.Name)
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackup,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackups,verbs=create;update,versions=v1beta1,name=vaerospikebackup.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateCreate() (admission.Warnings, error) {
	aerospikebackuplog.Info("validate create", "name", r.Name)

	if len(r.Spec.OnDemand) != 0 && r.Spec.Config.Raw != nil {
		return nil, fmt.Errorf("onDemand and backup config cannot be specified together while creating backup")
	}

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	aerospikebackuplog.Info("validate update", "name", r.Name)

	oldObj := old.(*AerospikeBackup)

	if !reflect.DeepEqual(r.Spec.BackupService, oldObj.Spec.BackupService) {
		return nil, fmt.Errorf("backup service cannot be updated")
	}

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
	}

	if len(r.Spec.OnDemand) > 0 && len(oldObj.Spec.OnDemand) > 0 {
		// Check if onDemand backup spec is updated
		if r.Spec.OnDemand[0].ID == oldObj.Spec.OnDemand[0].ID &&
			!reflect.DeepEqual(r.Spec.OnDemand[0], oldObj.Spec.OnDemand[0]) {
			return nil, fmt.Errorf("onDemand backup cannot be updated")
		}

		// Check if previous onDemand backup is completed before allowing new onDemand backup
		if r.Spec.OnDemand[0].ID != oldObj.Spec.OnDemand[0].ID && (len(r.Status.OnDemand) == 0 ||
			r.Status.OnDemand[0].ID != oldObj.Spec.OnDemand[0].ID) {
			return nil,
				fmt.Errorf("can not add new onDemand backup when previous onDemand backup is not completed")
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateDelete() (admission.Warnings, error) {
	aerospikebackuplog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeBackup) validateBackupConfig() error {
	var backupSvc AerospikeBackupService

	cl, gErr := getK8sClient()
	if gErr != nil {
		return gErr
	}

	if err := cl.Get(context.TODO(),
		types.NamespacedName{Name: r.Spec.BackupService.Name, Namespace: r.Spec.BackupService.Namespace},
		&backupSvc); err != nil {
		return err
	}

	var config model.Config

	if err := yaml.Unmarshal(backupSvc.Spec.Config.Raw, &config); err != nil {
		return err
	}

	configMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &configMap); err != nil {
		return err
	}

	if _, ok := configMap[common.AerospikeClusterKey]; !ok {
		return fmt.Errorf("aerospike-cluster field is required in config")
	}

	cluster, ok := configMap[common.AerospikeClusterKey].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospike-cluster field is not in the right format")
	}

	clusterBytes, cErr := yaml.Marshal(cluster)
	if cErr != nil {
		return cErr
	}

	aeroClusters := make(map[string]*model.AerospikeCluster)

	if err := yaml.Unmarshal(clusterBytes, &aeroClusters); err != nil {
		return err
	}

	if len(aeroClusters) != 1 {
		return fmt.Errorf("only one aerospike cluster is allowed in backup config")
	}

	if len(config.AerospikeClusters) == 0 {
		config.AerospikeClusters = make(map[string]*model.AerospikeCluster)
	}

	for name, aeroCluster := range aeroClusters {
		config.AerospikeClusters[name] = aeroCluster
	}

	if _, ok = configMap[common.BackupRoutinesKey]; !ok {
		return fmt.Errorf("backup-routines field is required in config")
	}

	routines, ok := configMap[common.BackupRoutinesKey].(map[string]interface{})
	if !ok {
		return fmt.Errorf("backup-routines field is not in the right format")
	}

	routineBytes, rErr := yaml.Marshal(routines)
	if rErr != nil {
		return rErr
	}

	backupRoutines := make(map[string]*model.BackupRoutine)

	if err := yaml.Unmarshal(routineBytes, &backupRoutines); err != nil {
		return err
	}

	if len(config.BackupRoutines) == 0 {
		config.BackupRoutines = make(map[string]*model.BackupRoutine)
	}

	for name, routine := range backupRoutines {
		config.BackupRoutines[name] = routine
	}

	// Add empty placeholders for missing config sections. This is required for validation to work.
	if config.ServiceConfig == nil {
		config.ServiceConfig = &model.BackupServiceConfig{}
	}

	if config.ServiceConfig.HTTPServer == nil {
		config.ServiceConfig.HTTPServer = &model.HTTPServerConfig{}
	}

	if config.ServiceConfig.Logger == nil {
		config.ServiceConfig.Logger = &model.LoggerConfig{}
	}

	if err := config.Validate(); err != nil {
		return err
	}

	// Validate on-demand backup
	if len(r.Spec.OnDemand) > 0 {
		if _, ok = config.BackupRoutines[r.Spec.OnDemand[0].RoutineName]; !ok {
			return fmt.Errorf("backup routine %s not found", r.Spec.OnDemand[0].RoutineName)
		}
	}

	return nil
}

func getK8sClient() (client.Client, error) {
	restConfig := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	utilRuntime.Must(asdbv1.AddToScheme(scheme))
	utilRuntime.Must(clientGoScheme.AddToScheme(scheme))
	utilRuntime.Must(AddToScheme(scheme))

	cl, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return cl, nil
}
