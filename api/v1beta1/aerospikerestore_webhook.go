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
	"reflect"
	"time"

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

const defaultPollingPeriod time.Duration = 60 * time.Second

func (r *AerospikeRestore) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1beta1-aerospikerestore,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikerestores,verbs=create;update,versions=v1beta1,name=maerospikerestore.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &AerospikeRestore{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeRestore) Default() {
	arLog := logf.Log.WithName(namespacedName(r))

	arLog.Info("Setting defaults for aerospikeRestore")

	if r.Spec.PollingPeriod.Duration.Seconds() == 0 {
		r.Spec.PollingPeriod.Duration = defaultPollingPeriod
	}
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikerestore,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikerestores,verbs=create;update,versions=v1beta1,name=vaerospikerestore.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeRestore{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateCreate() (admission.Warnings, error) {
	arLog := logf.Log.WithName(namespacedName(r))

	arLog.Info("Validate create")

	k8sClient, gErr := getK8sClient()
	if gErr != nil {
		return nil, gErr
	}

	if err := ValidateBackupSvcSupportedVersion(k8sClient,
		r.Spec.BackupService.Name,
		r.Spec.BackupService.Namespace,
	); err != nil {
		return nil, err
	}

	if err := r.validateRestoreConfig(k8sClient); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	arLog := logf.Log.WithName(namespacedName(r))

	arLog.Info("Validate update")

	oldRestore := old.(*AerospikeRestore)

	if !reflect.DeepEqual(oldRestore.Spec, r.Spec) {
		return nil, fmt.Errorf("aerospikeRestore Spec is immutable")
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateDelete() (admission.Warnings, error) {
	arLog := logf.Log.WithName(namespacedName(r))

	arLog.Info("Validate delete")

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeRestore) validateRestoreConfig(k8sClient client.Client) error {
	restoreConfig := make(map[string]interface{})

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &restoreConfig); err != nil {
		return err
	}

	backupSvcConfig, err := getBackupServiceFullConfig(k8sClient, r.Spec.BackupService.Name,
		r.Spec.BackupService.Namespace)
	if err != nil {
		return err
	}

	switch r.Spec.Type {
	case Full, Incremental:
		var restoreRequest dto.RestoreRequest

		if _, ok := restoreConfig[RoutineKey]; ok {
			return fmt.Errorf("routine field is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if _, ok := restoreConfig[TimeKey]; ok {
			return fmt.Errorf("time field is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if err := yaml.UnmarshalStrict(r.Spec.Config.Raw, &restoreRequest); err != nil {
			return err
		}

		return validation.ValidateRestoreRequest(&restoreRequest, backupSvcConfig)

	case Timestamp:
		var restoreRequest dto.RestoreTimestampRequest

		if _, ok := restoreConfig[SourceKey]; ok {
			return fmt.Errorf("source field is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if err := yaml.UnmarshalStrict(r.Spec.Config.Raw, &restoreRequest); err != nil {
			return err
		}

		return validation.ValidateRestoreTimestampRequest(&restoreRequest, backupSvcConfig)

	default:
		// Code flow should not come here
		return fmt.Errorf("unknown restore type %s", r.Spec.Type)
	}
}
