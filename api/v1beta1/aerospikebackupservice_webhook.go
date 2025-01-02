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
	"fmt"

	set "github.com/deckarep/golang-set/v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v2/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v2/pkg/validation"
)

const minSupportedVersion = "3.0.0"

func (r *AerospikeBackupService) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// Implemented Defaulter interface for future reference
var _ webhook.Defaulter = &AerospikeBackupService{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeBackupService) Default() {
	absLog := logf.Log.WithName(namespacedName(r))

	absLog.Info("Setting defaults for aerospikeBackupService")
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackupservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackupservices,verbs=create;update,versions=v1beta1,name=vaerospikebackupservice.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeBackupService{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateCreate() (admission.Warnings, error) {
	absLog := logf.Log.WithName(namespacedName(r))

	absLog.Info("Validate create")

	return r.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	absLog := logf.Log.WithName(namespacedName(r))

	absLog.Info("Validate update")

	return r.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateDelete() (admission.Warnings, error) {
	absLog := logf.Log.WithName(namespacedName(r))

	absLog.Info("Validate delete")

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeBackupService) validate() (admission.Warnings, error) {
	if err := ValidateBackupSvcVersion(r.Spec.Image); err != nil {
		return nil, err
	}

	if err := r.validateBackupServiceConfig(); err != nil {
		return nil, err
	}

	if err := r.validateBackupServiceSecrets(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (r *AerospikeBackupService) validateBackupServiceConfig() error {
	var config dto.Config

	if err := yaml.UnmarshalStrict(r.Spec.Config.Raw, &config); err != nil {
		return err
	}

	if len(config.BackupRoutines) != 0 {
		return fmt.Errorf("backup-routines field cannot be specified in backup service config")
	}

	if len(config.AerospikeClusters) != 0 {
		return fmt.Errorf("aerospike-clusters field cannot be specified in backup service config")
	}

	// Add empty placeholders for missing config sections. This is required for validation to work.
	if config.ServiceConfig.HTTPServer == nil {
		config.ServiceConfig.HTTPServer = &dto.HTTPServerConfig{}
	}

	if config.ServiceConfig.Logger == nil {
		config.ServiceConfig.Logger = &dto.LoggerConfig{}
	}

	return validation.ValidateConfiguration(&config)
}

func (r *AerospikeBackupService) validateBackupServiceSecrets() error {
	volumeNameSet := set.NewSet[string]()

	for _, secret := range r.Spec.SecretMounts {
		if volumeNameSet.Contains(secret.VolumeMount.Name) {
			return fmt.Errorf("duplicate volume name %s found in secrets field", secret.VolumeMount.Name)
		}

		volumeNameSet.Add(secret.VolumeMount.Name)
	}

	return nil
}
