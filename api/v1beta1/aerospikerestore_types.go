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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service"
	// BackupService is the backup service reference.
	BackupService *BackupService `json:"backupService"`

	//+kubebuilder:validation:Enum=Full;Incremental;TimeStamp
	// Type is the restore type
	Type RestoreType `json:"type"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Config"
	// Config is the configuration for the restore.
	Config runtime.RawExtension `json:"config"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Service Polling Period"
	// PollingPeriod is the polling period for restore service.
	PollingPeriod metav1.Duration `json:"pollingPeriod,omitempty"`
}

// AerospikeRestoreStatus defines the observed state of AerospikeRestore
type AerospikeRestoreStatus struct {
	JobID *int64 `json:"job-id,omitempty"`
	// Phase denotes the current phase of Aerospike restore operation.
	Phase AerospikeRestorePhase `json:"phase,omitempty"`

	RestoreResult runtime.RawExtension `json:"restoreResult,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.restoreResult.status`

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
