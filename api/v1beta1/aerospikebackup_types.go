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

// AerospikeBackupSpec defines the desired state of AerospikeBackup
// +k8s:openapi-gen=true
type AerospikeBackupSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service"
	// BackupService is the backup service reference.
	BackupService *BackupService `json:"backupService"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Config"
	// Config is the configuration for the backup.
	Config runtime.RawExtension `json:"config"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="On Demand
	// OnDemand is the on demand backup configuration.
	// +kubebuilder:validation:MaxItems:=1
	OnDemand []OnDemandSpec `json:"onDemand,omitempty"`
}

type BackupService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type OnDemandSpec struct {
	// On demand backup ID
	ID string `json:"id,omitempty"`
	// Backup routine name
	RoutineName string `json:"routineName"`
	// Delay interval in milliseconds
	Delay metav1.Duration `json:"delay,omitempty"`
}

// AerospikeBackupStatus defines the observed state of AerospikeBackup
type AerospikeBackupStatus struct {
	OnDemand []OnDemandSpec `json:"onDemand,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AerospikeBackup is the Schema for the aerospikebackup API
type AerospikeBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeBackupSpec   `json:"spec,omitempty"`
	Status AerospikeBackupStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AerospikeBackupList contains a list of AerospikeBackup
type AerospikeBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeBackup{}, &AerospikeBackupList{})
}
