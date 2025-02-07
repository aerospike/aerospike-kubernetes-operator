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
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/validation"
)

// SetupAerospikeBackupWebhookWithManager registers the webhook for AerospikeBackup in the manager.
func SetupAerospikeBackupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&AerospikeBackup{}).
		WithDefaulter(&AerospikeBackupCustomDefaulter{}).
		WithValidator(&AerospikeBackupCustomValidator{}).
		Complete()
}

// +kubebuilder:object:generate=false
// Above marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type AerospikeBackupCustomDefaulter struct {
	// Default values for various AerospikeBackup fields
}

// Implemented webhook.CustomDefaulter interface for future reference
var _ webhook.CustomDefaulter = &AerospikeBackupCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (abd *AerospikeBackupCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	backup, ok := obj.(*AerospikeBackup)
	if !ok {
		return fmt.Errorf("expected AerospikeBackup, got %T", obj)
	}

	abLog := logf.Log.WithName(namespacedName(backup))

	abLog.Info("Setting defaults for aerospikeBackup")

	return nil
}

// +kubebuilder:object:generate=false
type AerospikeBackupCustomValidator struct {
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackup,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackups,verbs=create;update,versions=v1beta1,name=vaerospikebackup.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &AerospikeBackupCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (abv *AerospikeBackupCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	backup, ok := obj.(*AerospikeBackup)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackup, got %T", obj)
	}

	abLog := logf.Log.WithName(namespacedName(backup))

	abLog.Info("Validate create")

	if err := backup.validate(); err != nil {
		return nil, err
	}

	if len(backup.Spec.OnDemandBackups) != 0 {
		return nil, fmt.Errorf("onDemand backups config cannot be specified while creating backup")
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (abv *AerospikeBackupCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
	backup, ok := newObj.(*AerospikeBackup)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackup, got %T", newObj)
	}

	abLog := logf.Log.WithName(namespacedName(backup))

	abLog.Info("Validate update")

	oldObject := oldObj.(*AerospikeBackup)

	if !reflect.DeepEqual(backup.Spec.BackupService, oldObject.Spec.BackupService) {
		return nil, fmt.Errorf("backup service cannot be updated")
	}

	if err := backup.validate(); err != nil {
		return nil, err
	}

	if err := backup.validateAerospikeClusterUpdate(oldObject); err != nil {
		return nil, err
	}

	if err := backup.validateOnDemandBackupsUpdate(oldObject); err != nil {
		return nil, err
	}

	return nil, nil
}

func (r *AerospikeBackup) validate() error {
	k8sClient, gErr := getK8sClient()
	if gErr != nil {
		return gErr
	}

	if err := ValidateBackupSvcSupportedVersion(k8sClient,
		r.Spec.BackupService.Name,
		r.Spec.BackupService.Namespace,
	); err != nil {
		return err
	}

	return r.validateBackupConfig(k8sClient)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (abv *AerospikeBackupCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	backup, ok := obj.(*AerospikeBackup)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackup, got %T", obj)
	}

	abLog := logf.Log.WithName(namespacedName(backup))

	abLog.Info("Validate delete")

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeBackup) validateBackupConfig(k8sClient client.Client) error {
	backupConfig := make(map[string]interface{})

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &backupConfig); err != nil {
		return err
	}

	if _, ok := backupConfig[ServiceKey]; ok {
		return fmt.Errorf("service field cannot be specified in backup config")
	}

	if _, ok := backupConfig[BackupPoliciesKey]; ok {
		return fmt.Errorf("backup-policies field cannot be specified in backup config")
	}

	if _, ok := backupConfig[StorageKey]; ok {
		return fmt.Errorf("storage field cannot be specified in backup config")
	}

	if _, ok := backupConfig[SecretAgentsKey]; ok {
		return fmt.Errorf("secret-agent field cannot be specified in backup config")
	}

	backupSvcConfig, err := getBackupServiceFullConfig(k8sClient, r.Spec.BackupService.Name,
		r.Spec.BackupService.Namespace)
	if err != nil {
		return err
	}

	aeroClusters, err := r.getValidatedAerospikeClusters(backupConfig)
	if err != nil {
		return err
	}

	backupRoutines, err := r.getValidatedBackupRoutines(backupConfig, aeroClusters)
	if err != nil {
		return err
	}

	err = updateValidateBackupSvcConfig(aeroClusters, backupRoutines, backupSvcConfig)
	if err != nil {
		return err
	}

	// Validate on-demand backup
	if len(r.Spec.OnDemandBackups) > 0 {
		if _, ok := backupSvcConfig.BackupRoutines[r.Spec.OnDemandBackups[0].RoutineName]; !ok {
			return fmt.Errorf("invalid onDemand config, backup routine %s not found",
				r.Spec.OnDemandBackups[0].RoutineName)
		}
	}

	return nil
}

func (r *AerospikeBackup) getValidatedAerospikeClusters(backupConfig map[string]interface{},
) (map[string]*dto.AerospikeCluster, error) {
	if _, ok := backupConfig[AerospikeClusterKey]; !ok {
		return nil, fmt.Errorf("aerospike-cluster field is required field in backup config")
	}

	cluster, ok := backupConfig[AerospikeClusterKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("aerospike-cluster field is not in the right format")
	}

	clusterBytes, cErr := yaml.Marshal(cluster)
	if cErr != nil {
		return nil, cErr
	}

	aeroClusters := make(map[string]*dto.AerospikeCluster)

	if err := yaml.UnmarshalStrict(clusterBytes, &aeroClusters); err != nil {
		return nil, err
	}

	if len(aeroClusters) != 1 {
		return aeroClusters, fmt.Errorf("only one aerospike cluster is allowed in backup config")
	}

	for clusterName := range aeroClusters {
		if err := validateName(r.NamePrefix(), clusterName); err != nil {
			return nil, fmt.Errorf("invalid cluster name %s, %s", clusterName, err.Error())
		}
	}

	return aeroClusters, nil
}

func (r *AerospikeBackup) validateOnDemandBackupsUpdate(oldObj *AerospikeBackup) error {
	// Check if backup config is updated along with onDemand backup add/update
	if !reflect.DeepEqual(r.Spec.OnDemandBackups, r.Status.OnDemandBackups) &&
		!reflect.DeepEqual(r.Spec.Config.Raw, r.Status.Config.Raw) {
		return fmt.Errorf("can not add/update onDemand backup along with backup config change")
	}

	if len(r.Spec.OnDemandBackups) > 0 && len(oldObj.Spec.OnDemandBackups) > 0 {
		// Check if onDemand backup spec is updated
		if r.Spec.OnDemandBackups[0].ID == oldObj.Spec.OnDemandBackups[0].ID &&
			!reflect.DeepEqual(r.Spec.OnDemandBackups[0], oldObj.Spec.OnDemandBackups[0]) {
			return fmt.Errorf("existing onDemand backup cannot be updated. " +
				"However, It can be removed and a new onDemand backup can be added")
		}

		// Check if previous onDemand backup is completed before allowing new onDemand backup
		if r.Spec.OnDemandBackups[0].ID != oldObj.Spec.OnDemandBackups[0].ID && (len(r.Status.OnDemandBackups) == 0 ||
			r.Status.OnDemandBackups[0].ID != oldObj.Spec.OnDemandBackups[0].ID) {
			return fmt.Errorf("can not add new onDemand backup when previous onDemand backup is not completed")
		}
	}

	return nil
}

func (r *AerospikeBackup) validateAerospikeClusterUpdate(oldObj *AerospikeBackup) error {
	oldObjConfig := make(map[string]interface{})
	currentConfig := make(map[string]interface{})

	if err := yaml.Unmarshal(oldObj.Spec.Config.Raw, &oldObjConfig); err != nil {
		return err
	}

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &currentConfig); err != nil {
		return err
	}

	oldCluster := oldObjConfig[AerospikeClusterKey].(map[string]interface{})
	newCluster := currentConfig[AerospikeClusterKey].(map[string]interface{})

	for clusterName := range newCluster {
		if _, ok := oldCluster[clusterName]; !ok {
			return fmt.Errorf("aerospike-cluster name cannot be updated")
		}
	}

	return nil
}

func (r *AerospikeBackup) getValidatedBackupRoutines(
	backupConfig map[string]interface{},
	aeroClusters map[string]*dto.AerospikeCluster,
) (map[string]*dto.BackupRoutine, error) {
	if _, ok := backupConfig[BackupRoutinesKey]; !ok {
		return nil, fmt.Errorf("backup-routines field is required in backup config")
	}

	routines, ok := backupConfig[BackupRoutinesKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("backup-routines field is not in the right format")
	}

	routineBytes, rErr := yaml.Marshal(routines)
	if rErr != nil {
		return nil, rErr
	}

	backupRoutines := make(map[string]*dto.BackupRoutine)

	if err := yaml.UnmarshalStrict(routineBytes, &backupRoutines); err != nil {
		return nil, err
	}

	if len(backupRoutines) == 0 {
		return nil, fmt.Errorf("backup-routines field cannot be empty")
	}

	// validate:
	// 1. if the correct format name is given
	// 2. if only correct aerospike cluster (the one referred in Backup CR) is used in backup routines
	for routineName, routine := range backupRoutines {
		if err := validateName(r.NamePrefix(), routineName); err != nil {
			return nil, fmt.Errorf("invalid backup routine name %s, %s", routineName, err.Error())
		}

		if _, ok := aeroClusters[routine.SourceCluster]; !ok {
			return nil, fmt.Errorf("cluster %s not found in backup aerospike-cluster config", routine.SourceCluster)
		}
	}

	return backupRoutines, nil
}

func updateValidateBackupSvcConfig(
	clusters map[string]*dto.AerospikeCluster,
	routines map[string]*dto.BackupRoutine,
	backupSvcConfig *dto.Config,
) error {
	if len(backupSvcConfig.AerospikeClusters) == 0 {
		backupSvcConfig.AerospikeClusters = make(map[string]*dto.AerospikeCluster)
	}

	for name, cluster := range clusters {
		backupSvcConfig.AerospikeClusters[name] = cluster
	}

	if len(backupSvcConfig.BackupRoutines) == 0 {
		backupSvcConfig.BackupRoutines = make(map[string]*dto.BackupRoutine)
	}

	for name, routine := range routines {
		backupSvcConfig.BackupRoutines[name] = routine
	}

	// Add empty placeholders for missing backupSvcConfig sections. This is required for validation to work.
	if backupSvcConfig.ServiceConfig.HTTPServer == nil {
		backupSvcConfig.ServiceConfig.HTTPServer = &dto.HTTPServerConfig{}
	}

	if backupSvcConfig.ServiceConfig.Logger == nil {
		backupSvcConfig.ServiceConfig.Logger = &dto.LoggerConfig{}
	}

	return validation.ValidateConfiguration(backupSvcConfig)
}

func (r *AerospikeBackup) NamePrefix() string {
	return r.Namespace + "-" + r.Name
}

func validateName(reqPrefix, name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if !strings.HasPrefix(name, reqPrefix) {
		return fmt.Errorf("name should start with %s", reqPrefix)
	}

	return nil
}
