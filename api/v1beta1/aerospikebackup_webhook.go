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

	"k8s.io/apimachinery/pkg/types"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"
)

// log is for logging in this package.
var aerospikebackuplog = logf.Log.WithName("aerospikebackup-resource")

func (r *AerospikeBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1beta1-aerospikebackup,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackups,verbs=create;update,versions=v1beta1,name=maerospikebackup.kb.io,admissionReviewVersions=v1

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

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	aerospikebackuplog.Info("validate update", "name", r.Name)

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
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

	if _, ok := configMap["aerospike-cluster"]; !ok {
		return fmt.Errorf("aerospike-cluster field is required in config")
	}

	cluster, ok := configMap["aerospike-cluster"].(map[string]interface{})
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

	if len(config.AerospikeClusters) == 0 {
		config.AerospikeClusters = make(map[string]*model.AerospikeCluster)
	}

	for name, aeroCluster := range aeroClusters {
		config.AerospikeClusters[name] = aeroCluster
	}

	if _, ok = configMap["backup-routines"]; !ok {
		return fmt.Errorf("backup-routines field is required in config")
	}

	routines, ok := configMap["backup-routines"].(map[string]interface{})
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

	return config.Validate()
}

func getK8sClient() (client.Client, error) {
	restConfig := ctrl.GetConfigOrDie()

	cl, err := client.New(restConfig, client.Options{
		Scheme: clientGoScheme.Scheme,
	})
	if err != nil {
		return nil, err
	}

	return cl, nil
}
