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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
)

// log is for logging in this package.
var aerospikeRestoreLog = logf.Log.WithName("aerospikerestore-resource")

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
	aerospikeRestoreLog.Info("default", "name", r.Name)

	if r.Spec.PollingPeriod.Duration.Seconds() == 0 {
		r.Spec.PollingPeriod.Duration = defaultPollingPeriod
	}
}

//nolint:lll // for readability
//+kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikerestore,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikerestores,verbs=create;update,versions=v1beta1,name=vaerospikerestore.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &AerospikeRestore{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateCreate() (admission.Warnings, error) {
	aerospikeRestoreLog.Info("validate create", "name", r.Name)

	if err := r.validateRestoreConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	aerospikeRestoreLog.Info("validate update", "name", r.Name)

	oldRestore := old.(*AerospikeRestore)

	if !reflect.DeepEqual(oldRestore.Spec, r.Spec) {
		return nil, fmt.Errorf("AerospikeRestore Spec is immutable")
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *AerospikeRestore) ValidateDelete() (admission.Warnings, error) {
	aerospikeRestoreLog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *AerospikeRestore) validateRestoreConfig() error {
	restoreConfigInMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.Spec.Config.Raw, &restoreConfigInMap); err != nil {
		return err
	}

	switch r.Spec.Type {
	case Full, Incremental:
		var restoreRequest model.RestoreRequest

		if _, ok := restoreConfigInMap[common.RoutineKey]; ok {
			return fmt.Errorf("routine key is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if _, ok := restoreConfigInMap[common.TimeKey]; ok {
			return fmt.Errorf("time key is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if err := yaml.Unmarshal(r.Spec.Config.Raw, &restoreRequest); err != nil {
			return err
		}

		return restoreRequest.Validate()

	case TimeStamp:
		var restoreRequest model.RestoreTimestampRequest

		if _, ok := restoreConfigInMap[common.SourceKey]; ok {
			return fmt.Errorf("source key is not allowed in restore config for restore type %s", r.Spec.Type)
		}

		if err := yaml.Unmarshal(r.Spec.Config.Raw, &restoreRequest); err != nil {
			return err
		}

		return restoreRequest.Validate()

	default:
		// Code flow should not come here
		return fmt.Errorf("unknown restore type %s", r.Spec.Type)
	}
}
