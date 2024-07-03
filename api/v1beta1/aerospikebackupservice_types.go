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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AerospikeBackupServiceSpec defines the desired state of AerospikeBackupService
//
//nolint:govet // for readability
type AerospikeBackupServiceSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service Image"
	// Image is the image for the backup service.
	Image string `json:"image"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service Config"
	// Config is the configuration for the backup service.
	Config runtime.RawExtension `json:"config"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources"
	// Resources is the resource requirements for the backup service.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service Volume"
	// SecretMounts is the list of secret to be mounted in the backup service.
	SecretMounts []SecretMount `json:"secrets,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service"
	Service *Service `json:"service,omitempty"`
}

// AerospikeBackupServiceStatus defines the observed state of AerospikeBackupService
type AerospikeBackupServiceStatus struct {
	// Backup Service API context path
	ContextPath string `json:"contextPath"`

	// Backup service config hash
	ConfigHash string `json:"configHash"`

	// Backup service listening port
	Port int32 `json:"port"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AerospikeBackupService is the Schema for the aerospikebackupservices API
type AerospikeBackupService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeBackupServiceSpec   `json:"spec,omitempty"`
	Status AerospikeBackupServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AerospikeBackupServiceList contains a list of AerospikeBackupService
type AerospikeBackupServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeBackupService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeBackupService{}, &AerospikeBackupServiceList{})
}

type SecretMount struct {
	SecretName string `json:"secretName"`

	VolumeMount corev1.VolumeMount `json:"volumeMount"`
}

type Service struct {
	Type corev1.ServiceType `json:"type"`
}
