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
	"k8s.io/apimachinery/pkg/types"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/pkg/model"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
)

func (r *AerospikeBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// Implemented Defaulter interface for future reference
var _ webhook.Defaulter = &AerospikeBackup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeBackup) Default() {
	abLog := logf.Log.WithName(namespacedName(r))

	abLog.Info("Setting defaults for aerospikeBackup")
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackup,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackups,verbs=create;update,versions=v1beta1,name=vaerospikebackup.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateCreate() (admission.Warnings, error) {
	abLog := logf.Log.WithName(namespacedName(r))

	abLog.Info("Validate create")

	if len(r.Spec.OnDemandBackups) != 0 {
		return nil, fmt.Errorf("onDemand backups config cannot be specified while creating backup")
	}

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	abLog := logf.Log.WithName(namespacedName(r))

	abLog.Info("Validate update")

	oldObj := old.(*AerospikeBackup)

	if !reflect.DeepEqual(r.Spec.BackupService, oldObj.Spec.BackupService) {
		return nil, fmt.Errorf("backup service cannot be updated")
	}

	if err := r.validateBackupConfig(); err != nil {
		return nil, err
	}

	if err := r.validateAerospikeClusterUpdate(oldObj); err != nil {
		return nil, err
	}

	if err := r.validateOnDemandBackupsUpdate(oldObj); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackup) ValidateDelete() (admission.Warnings, error) {
	abLog := logf.Log.WithName(namespacedName(r))

	abLog.Info("Validate delete")

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeBackup) validateBackupConfig() error {
	backupConfig := make(map[string]interface{})

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &backupConfig); err != nil {
		return err
	}

	if _, ok := backupConfig[common.ServiceKey]; ok {
		return fmt.Errorf("service field cannot be specified in backup config")
	}

	if _, ok := backupConfig[common.BackupPoliciesKey]; ok {
		return fmt.Errorf("backup-policies field cannot be specified in backup config")
	}

	if _, ok := backupConfig[common.StorageKey]; ok {
		return fmt.Errorf("storage field cannot be specified in backup config")
	}

	if _, ok := backupConfig[common.SecretAgentsKey]; ok {
		return fmt.Errorf("secret-agent field cannot be specified in backup config")
	}

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

	var backupSvcConfig model.Config

	if err := yaml.UnmarshalStrict(backupSvc.Spec.Config.Raw, &backupSvcConfig); err != nil {
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

	err = updateValidateBackupSvcConfig(aeroClusters, backupRoutines, &backupSvcConfig)
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

func (r *AerospikeBackup) getValidatedAerospikeClusters(backupConfig map[string]interface{},
) (map[string]*model.AerospikeCluster, error) {
	if _, ok := backupConfig[common.AerospikeClusterKey]; !ok {
		return nil, fmt.Errorf("aerospike-cluster field is required field in backup config")
	}

	cluster, ok := backupConfig[common.AerospikeClusterKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("aerospike-cluster field is not in the right format")
	}

	clusterBytes, cErr := yaml.Marshal(cluster)
	if cErr != nil {
		return nil, cErr
	}

	aeroClusters := make(map[string]*model.AerospikeCluster)

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

	oldCluster := oldObjConfig[common.AerospikeClusterKey].(map[string]interface{})
	newCluster := currentConfig[common.AerospikeClusterKey].(map[string]interface{})

	for clusterName := range newCluster {
		if _, ok := oldCluster[clusterName]; !ok {
			return fmt.Errorf("aerospike-cluster name cannot be updated")
		}
	}

	return nil
}

func (r *AerospikeBackup) getValidatedBackupRoutines(
	backupConfig map[string]interface{},
	aeroClusters map[string]*model.AerospikeCluster,
) (map[string]*model.BackupRoutine, error) {
	if _, ok := backupConfig[common.BackupRoutinesKey]; !ok {
		return nil, fmt.Errorf("backup-routines field is required in backup config")
	}

	routines, ok := backupConfig[common.BackupRoutinesKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("backup-routines field is not in the right format")
	}

	routineBytes, rErr := yaml.Marshal(routines)
	if rErr != nil {
		return nil, rErr
	}

	backupRoutines := make(map[string]*model.BackupRoutine)

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
	clusters map[string]*model.AerospikeCluster,
	routines map[string]*model.BackupRoutine,
	backupSvcConfig *model.Config,
) error {
	if len(backupSvcConfig.AerospikeClusters) == 0 {
		backupSvcConfig.AerospikeClusters = make(map[string]*model.AerospikeCluster)
	}

	for name, cluster := range clusters {
		backupSvcConfig.AerospikeClusters[name] = cluster
	}

	if len(backupSvcConfig.BackupRoutines) == 0 {
		backupSvcConfig.BackupRoutines = make(map[string]*model.BackupRoutine)
	}

	for name, routine := range routines {
		backupSvcConfig.BackupRoutines[name] = routine
	}

	// Add empty placeholders for missing backupSvcConfig sections. This is required for validation to work.
	if backupSvcConfig.ServiceConfig == nil {
		backupSvcConfig.ServiceConfig = &model.BackupServiceConfig{}
	}

	if backupSvcConfig.ServiceConfig.HTTPServer == nil {
		backupSvcConfig.ServiceConfig.HTTPServer = &model.HTTPServerConfig{}
	}

	if backupSvcConfig.ServiceConfig.Logger == nil {
		backupSvcConfig.ServiceConfig.Logger = &model.LoggerConfig{}
	}

	return backupSvcConfig.Validate()
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
