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
	"github.com/abhishekdwivedi3060/aerospike-backup-service/pkg/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=InProgress;Completed;Failed
type AerospikeRestorePhase string

// These are the valid phases of Aerospike restore operation.
const (
	AerospikeRestoreInProgress AerospikeRestorePhase = "InProgress"
	AerospikeRestoreCompleted  AerospikeRestorePhase = "Completed"
	AerospikeRestoreFailed     AerospikeRestorePhase = "Failed"
)

type RestoreType string

const (
	Full        RestoreType = "Full"
	Incremental RestoreType = "Incremental"
	TimeStamp   RestoreType = "TimeStamp"
)

// AerospikeRestoreSpec defines the desired state of AerospikeRestore
// +k8s:openapi-gen=true
type AerospikeRestoreSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Config"
	ServiceConfig *ServiceConfig `json:"service-config"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Config"
	RestoreConfig *RestoreConfig `json:"restore-config"`
}

// AerospikeRestoreStatus defines the observed state of AerospikeRestore
type AerospikeRestoreStatus struct {
	JobID int64 `json:"job-id"`
	// Phase denotes the current phase of Aerospike restore operation.
	Phase AerospikeRestorePhase `json:"phase,omitempty"`

	RestoreResult *model.RestoreResult `json:"restore-result,omitempty"`

	Error string `json:"error,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AerospikeRestore is the Schema for the aerospikerestores API
type AerospikeRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeRestoreSpec   `json:"spec,omitempty"`
	Status AerospikeRestoreStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AerospikeRestoreList contains a list of AerospikeRestore
type AerospikeRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeRestore{}, &AerospikeRestoreList{})
}

type RestoreConfig struct {
	//+kubebuilder:validation:Enum=Full;Incremental;TimeStamp
	Type RestoreType `json:"type"`

	model.RestoreRequest `json:",inline"`
	// Required epoch time for recovery. The closest backup before the timestamp will be applied.
	Time int64 `json:"time,omitempty"`
	// The backup routine name.
	Routine string `json:"routine,omitempty"`
}
