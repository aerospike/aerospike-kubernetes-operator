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

// AerospikeBackupSpec defines the desired state of AerospikeBackup
// +k8s:openapi-gen=true
type AerospikeBackupSpec struct {
	ServiceConfig *ServiceConfig `json:"service-config"`
	BackupConfig  *BackupConfig  `json:"backup-config"`
}

type BackupConfig struct {
	AerospikeCluster *Cluster                        `json:"aerospike-cluster"`
	Storage          map[string]*model.Storage       `json:"storage"`
	BackupRoutines   map[string]*model.BackupRoutine `json:"backup-routines"`
	BackupPolicies   map[string]*model.BackupPolicy  `json:"backup-policies"`
}

type ServiceConfig struct {
	// The address to listen on.
	Address *string `json:"address,omitempty" default:"0.0.0.0"`
	// The port to listen on.
	Port *int `json:"port,omitempty" default:"8080"`
	// ContextPath customizes path for the API endpoints.
	ContextPath *string `json:"context-path,omitempty" default:"/"`
}

type Cluster struct {
	model.AerospikeCluster `json:",inline"`
	Name                   string `json:"name"`
}

// AerospikeBackupStatus defines the observed state of AerospikeBackup
type AerospikeBackupStatus struct {
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
