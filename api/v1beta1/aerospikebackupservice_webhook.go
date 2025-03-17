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

	set "github.com/deckarep/golang-set/v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/validation"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

const MinSupportedVersion = "3.0.0"

// SetupAerospikeBackupServiceWebhookWithManager registers the webhook for AerospikeBackupService in the manager.
func SetupAerospikeBackupServiceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&AerospikeBackupService{}).
		WithDefaulter(&AerospikeBackupServiceCustomDefaulter{}).
		WithValidator(&AerospikeBackupServiceCustomValidator{}).
		Complete()
}

// +kubebuilder:object:generate=false
// Above marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type AerospikeBackupServiceCustomDefaulter struct {
	// Default values for various AerospikeBackupService fields
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1beta1-aerospikebackupservice,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackupservices,verbs=create;update,versions=v1beta1,name=maerospikebackupservice.kb.io,admissionReviewVersions=v1

var _ webhook.CustomDefaulter = &AerospikeBackupCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (absd *AerospikeBackupServiceCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	backupSvc, ok := obj.(*AerospikeBackupService)
	if !ok {
		return fmt.Errorf("expected AerospikeBackupService, got %T", obj)
	}

	absLog := logf.Log.WithName(namespacedName(backupSvc))

	absLog.Info("Setting defaults for aerospikeBackupService")

	if backupSvc.Spec.Resources != nil && backupSvc.Spec.PodSpec.ServiceContainerSpec.Resources == nil {
		backupSvc.Spec.PodSpec.ServiceContainerSpec.Resources = backupSvc.Spec.Resources
	}

	return nil
}

// +kubebuilder:object:generate=false
type AerospikeBackupServiceCustomValidator struct {
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikebackupservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikebackupservices,verbs=create;update,versions=v1beta1,name=vaerospikebackupservice.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &AerospikeBackupServiceCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (absv *AerospikeBackupServiceCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	backupSvc, ok := obj.(*AerospikeBackupService)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackupService, got %T", obj)
	}

	absLog := logf.Log.WithName(namespacedName(backupSvc))

	absLog.Info("Validate create")

	return backupSvc.validate()
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (absv *AerospikeBackupServiceCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
	backupSvc, ok := newObj.(*AerospikeBackupService)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackupService, got %T", newObj)
	}

	absLog := logf.Log.WithName(namespacedName(backupSvc))

	absLog.Info("Validate update")

	return backupSvc.validate()
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (absv *AerospikeBackupServiceCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	backupSvc, ok := obj.(*AerospikeBackupService)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeBackupService, got %T", obj)
	}

	absLog := logf.Log.WithName(namespacedName(backupSvc))

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

	return r.validateServicePodSpec()
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

func (r *AerospikeBackupService) validateServicePodSpec() (admission.Warnings, error) {
	if err := validatePodObjectMeta(&r.Spec.PodSpec.ObjectMeta); err != nil {
		return nil, err
	}

	return r.validateResources()
}

func (r *AerospikeBackupService) validateResources() (admission.Warnings, error) {
	var warn admission.Warnings

	if r.Spec.Resources != nil && r.Spec.PodSpec.ServiceContainerSpec.Resources != nil {
		if !reflect.DeepEqual(r.Spec.Resources, r.Spec.PodSpec.ServiceContainerSpec.Resources) {
			return nil, fmt.Errorf("resources mismatch, different resources requirements found in " +
				"spec.resources and spec.podSpec.serviceContainer.resources")
		}

		warn = []string{"spec.resources field is deprecated, " +
			"resources field is now part of spec.podSpec.serviceContainer"}
	}

	if r.Spec.PodSpec.ServiceContainerSpec.Resources != nil {
		resources := r.Spec.PodSpec.ServiceContainerSpec.Resources
		if resources.Limits != nil && resources.Requests != nil &&
			((resources.Limits.Cpu().Cmp(*resources.Requests.Cpu()) < 0) ||
				(resources.Limits.Memory().Cmp(*resources.Requests.Memory()) < 0)) {
			return warn, fmt.Errorf("resources.Limits cannot be less than resource.Requests. Resources %v",
				resources)
		}
	}

	return warn, nil
}

func validatePodObjectMeta(objectMeta *AerospikeObjectMeta) error {
	for label := range objectMeta.Labels {
		if label == asdbv1.AerospikeAppLabel || label == asdbv1.AerospikeCustomResourceLabel {
			return fmt.Errorf(
				"label: %s is internally set by operator and shouldn't be specified by user",
				label,
			)
		}
	}

	return nil
}
