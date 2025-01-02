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

// +kubebuilder:validation:Enum=InProgress;Completed;Error
type AerospikeBackupServicePhase string

// These are the valid phases of Aerospike Backup Service reconcile flow.
const (
	// AerospikeBackupServiceInProgress means the AerospikeBackupService CR is being reconciled and operations are
	// in-progress state. This phase denotes that AerospikeBackupService resources are gradually getting deployed.
	AerospikeBackupServiceInProgress AerospikeBackupServicePhase = "InProgress"
	// AerospikeBackupServiceCompleted means the AerospikeBackupService CR has been reconciled.
	// This phase denotes that the AerospikeBackupService resources have been deployed/upgraded successfully and is
	// ready to use.
	AerospikeBackupServiceCompleted AerospikeBackupServicePhase = "Completed"
	// AerospikeBackupServiceError means the AerospikeBackupService operation is in error state because of some reason
	// like incorrect backup service config, incorrect image, etc.
	AerospikeBackupServiceError AerospikeBackupServicePhase = "Error"
)

// AerospikeBackupServiceSpec defines the desired state of AerospikeBackupService
// +k8s:openapi-gen=true
//
//nolint:govet // for readability
type AerospikeBackupServiceSpec struct {
	// Image is the image for the backup service.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service Image"
	Image string `json:"image"`

	// Config is the free form configuration for the backup service in YAML format.
	// This config is used to start the backup service. The config is passed as a file to the backup service.
	// It includes: service, backup-policies, storage, secret-agent.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service Config"
	Config runtime.RawExtension `json:"config"`

	// Specify additional configuration for the AerospikeBackupService pods
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Configuration"
	PodSpec ServicePodSpec `json:"podSpec,omitempty"`

	// Resources defines the requests and limits for the backup service container.
	// Resources.Limits should be more than Resources.Requests.
	// Deprecated: Resources field is now part of spec.podSpec.serviceContainer
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources"
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// SecretMounts is the list of secret to be mounted in the backup service.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service SecretMounts"
	SecretMounts []SecretMount `json:"secrets,omitempty"`

	// Service defines the Kubernetes service configuration for the backup service.
	// It is used to expose the backup service deployment. By default, the service type is ClusterIP.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="K8s Service"
	Service *Service `json:"service,omitempty"`
}

// AerospikeBackupServiceStatus defines the observed state of AerospikeBackupService
//
//nolint:govet // for readbility
type AerospikeBackupServiceStatus struct {
	// Image is the image for the backup service.
	Image string `json:"image,omitempty"`

	// Config is the free form configuration for the backup service in YAML format.
	// This config is used to start the backup service. The config is passed as a file to the backup service.
	// It includes: service, backup-policies, storage, secret-agent.
	Config runtime.RawExtension `json:"config,omitempty"`

	// Specify additional configuration for the AerospikeBackupService pods
	PodSpec ServicePodSpec `json:"podSpec,omitempty"`

	// SecretMounts is the list of secret to be mounted in the backup service.
	SecretMounts []SecretMount `json:"secrets,omitempty"`

	// Service defines the Kubernetes service configuration for the backup service.
	// It is used to expose the backup service deployment. By default, the service type is ClusterIP.
	Service *Service `json:"service,omitempty"`

	// ContextPath is the backup service API context path
	ContextPath string `json:"contextPath,omitempty"`

	// Phase denotes Backup service phase
	Phase AerospikeBackupServicePhase `json:"phase"`

	// Port is the listening port of backup service
	Port int32 `json:"port,omitempty"`
}

type ServicePodSpec struct {
	// ServiceContainerSpec configures the backup service container
	// created by the operator.
	ServiceContainerSpec ServiceContainerSpec `json:"serviceContainer,omitempty"`

	// MetaData to add to the pod.
	ObjectMeta AerospikeObjectMeta `json:"metadata,omitempty"`

	// SchedulingPolicy  controls pods placement on Kubernetes nodes.
	SchedulingPolicy `json:",inline"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of
	// the images used by this PodSpec.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

type ServiceContainerSpec struct {
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// Resources defines the requests and limits for the backup service container.
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="aerospike-kubernetes-operator/version=3.4.1"
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Service Type",type=string,JSONPath=`.spec.service.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AerospikeBackupService is the Schema for the aerospikebackupservices API
type AerospikeBackupService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeBackupServiceSpec   `json:"spec,omitempty"`
	Status AerospikeBackupServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AerospikeBackupServiceList contains a list of AerospikeBackupService
type AerospikeBackupServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeBackupService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeBackupService{}, &AerospikeBackupServiceList{})
}

// SecretMount specifies the secret and its corresponding volume mount options.
type SecretMount struct {
	// SecretName is the name of the secret to be mounted.
	SecretName string `json:"secretName"`

	// VolumeMount is the volume mount options for the secret.
	VolumeMount corev1.VolumeMount `json:"volumeMount"`
}

// Service specifies the Kubernetes service related configuration.
type Service struct {
	// Type is the Kubernetes service type.
	Type corev1.ServiceType `json:"type"`
}
