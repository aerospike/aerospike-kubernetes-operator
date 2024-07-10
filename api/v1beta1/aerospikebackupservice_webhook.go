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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
)

// log is for logging in this package.
var aerospikebackupservicelog = logf.Log.WithName("aerospikebackupservice-resource")

func (r *AerospikeBackupService) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

var _ webhook.Defaulter = &AerospikeBackupService{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeBackupService) Default() {
	aerospikebackupservicelog.Info("default", "name", r.Name)
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackupservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackupservices,verbs=create;update,versions=v1beta1,name=vaerospikebackupservice.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeBackupService{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateCreate() (admission.Warnings, error) {
	aerospikebackupservicelog.Info("validate create", "name", r.Name)

	if err := r.validateBackupServiceConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	aerospikebackupservicelog.Info("validate update", "name", r.Name)

	if err := r.validateBackupServiceConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeBackupService) ValidateDelete() (admission.Warnings, error) {
	aerospikebackupservicelog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeBackupService) validateBackupServiceConfig() error {
	var config model.Config

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &config); err != nil {
		return err
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

	return config.Validate()
}
