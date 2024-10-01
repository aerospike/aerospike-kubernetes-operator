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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AerospikeBackupSpec defines the desired state of AerospikeBackup for a given AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeBackupSpec struct {
	// BackupService is the backup service reference i.e. name and namespace.
	// It is used to communicate to the backup service to trigger backups. This field is immutable
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service"
	BackupService BackupService `json:"backupService"`

	// Config is the free form configuration for the backup in YAML format.
	// This config is used to trigger backups. It includes: aerospike-cluster, backup-routines.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Config"
	Config runtime.RawExtension `json:"config"`

	// OnDemandBackups is the configuration for on-demand backups.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="On Demand Backups"
	// +kubebuilder:validation:MaxItems:=1
	OnDemandBackups []OnDemandBackupSpec `json:"onDemandBackups,omitempty"`
}

type BackupService struct {
	// Backup service name
	Name string `json:"name"`

	// Backup service namespace
	Namespace string `json:"namespace"`
}

func (b *BackupService) String() string {
	return fmt.Sprintf("%s/%s", b.Namespace, b.Name)
}

type OnDemandBackupSpec struct {
	// ID is the unique identifier for the on-demand backup.
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`

	// RoutineName is the routine name used to trigger on-demand backup.
	RoutineName string `json:"routineName"`

	// Delay is the interval before starting the on-demand backup.
	Delay metav1.Duration `json:"delay,omitempty"`
}

// AerospikeBackupStatus defines the observed state of AerospikeBackup
type AerospikeBackupStatus struct {
	// BackupService is the backup service reference i.e. name and namespace.
	BackupService BackupService `json:"backupService"`

	// Config is the configuration for the backup in YAML format.
	// This config is used to trigger backups. It includes: aerospike-cluster, backup-routines.
	Config runtime.RawExtension `json:"config"`

	// OnDemandBackups is the configuration for on-demand backups.
	OnDemandBackups []OnDemandBackupSpec `json:"onDemandBackups,omitempty"`

	// TODO: finalize the status and phase
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="aerospike-kubernetes-operator/version=3.3.1"
// +kubebuilder:printcolumn:name="Backup Service Name",type=string,JSONPath=`.spec.backupService.name`
// +kubebuilder:printcolumn:name="Backup Service Namespace",type=string,JSONPath=`.spec.backupService.namespace`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AerospikeBackup is the Schema for the aerospikebackup API
type AerospikeBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeBackupSpec   `json:"spec,omitempty"`
	Status AerospikeBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AerospikeBackupList contains a list of AerospikeBackup
type AerospikeBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeBackup{}, &AerospikeBackupList{})
}
