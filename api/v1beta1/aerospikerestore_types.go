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
	// AerospikeRestoreInProgress means the AerospikeRestore CR is being reconciled and restore operation is going on.
	AerospikeRestoreInProgress AerospikeRestorePhase = "InProgress"

	// AerospikeRestoreCompleted means the AerospikeRestore CR has been reconciled and restore operation is completed.
	AerospikeRestoreCompleted AerospikeRestorePhase = "Completed"

	// AerospikeRestoreFailed means the AerospikeRestore CR has been reconciled and restore operation is failed.
	AerospikeRestoreFailed AerospikeRestorePhase = "Failed"
)

type RestoreType string

const (
	Full        RestoreType = "Full"
	Incremental RestoreType = "Incremental"
	Timestamp   RestoreType = "Timestamp"
)

// AerospikeRestoreSpec defines the desired state of AerospikeRestore
// +k8s:openapi-gen=true
//
//nolint:govet // for readability
type AerospikeRestoreSpec struct {
	// BackupService is the backup service reference i.e. name and namespace.
	// It is used to communicate to the backup service to trigger restores. This field is immutable
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service"
	BackupService BackupService `json:"backupService"`

	// Type is the type of restore. It can of type Full, Incremental, and Timestamp.
	// Based on the restore type, the relevant restore config should be given.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Type"
	// +kubebuilder:validation:Enum=Full;Incremental;Timestamp
	Type RestoreType `json:"type"`

	// Config is the free form configuration for the restore in YAML format.
	// This config is used to trigger restores. It includes: destination, policy, source, secret-agent, time and routine.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Config"
	Config runtime.RawExtension `json:"config"`

	// PollingPeriod is the polling period for restore operation status.
	// It is used to poll the restore service to fetch restore operation status.
	// Default is 60 seconds.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restore Service Polling Period"
	// +optional
	PollingPeriod metav1.Duration `json:"pollingPeriod,omitempty"`
}

// AerospikeRestoreStatus defines the observed state of AerospikeRestore
type AerospikeRestoreStatus struct {
	// JobID is the restore operation job id.
	// +optional
	JobID *int64 `json:"job-id,omitempty"`

	// RestoreResult is the result of the restore operation.
	// +optional
	RestoreResult runtime.RawExtension `json:"restoreResult,omitempty"`

	// Phase denotes the current phase of Aerospike restore operation.
	Phase AerospikeRestorePhase `json:"phase"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="aerospike-kubernetes-operator/version=4.2.0-dev1"
// +kubebuilder:printcolumn:name="Backup Service Name",type=string,JSONPath=`.spec.backupService.name`
// +kubebuilder:printcolumn:name="Backup Service Namespace",type=string,JSONPath=`.spec.backupService.namespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AerospikeRestore is the Schema for the aerospikerestores API
//
//nolint:govet // auto-generated
type AerospikeRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeRestoreSpec   `json:"spec,omitempty"`
	Status AerospikeRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AerospikeRestoreList contains a list of AerospikeRestore
type AerospikeRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeRestore{}, &AerospikeRestoreList{})
}
