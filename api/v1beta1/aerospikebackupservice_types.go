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
	// +optional
	PodSpec ServicePodSpec `json:"podSpec,omitempty"`

	// Resources defines the requests and limits for the backup service container.
	// Resources.Limits should be more than Resources.Requests.
	//
	// Deprecated: Resources field is now part of spec.podSpec.serviceContainer
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources"
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// SecretMounts is the list of secret to be mounted in the backup service.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Service SecretMounts"
	// +optional
	SecretMounts []SecretMount `json:"secrets,omitempty"`

	// Service defines the Kubernetes service configuration for the backup service.
	// It is used to expose the backup service deployment. By default, the service type is ClusterIP.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="K8s Service"
	// +optional
	Service *Service `json:"service,omitempty"`

	// OperatorClientCert configures how the operator connects to the backup service over HTTPS.
	// When set, the operator uses the service.https listener from config, which must be enabled.
	// When not set, the operator uses the service.http listener, which must be enabled.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Operator Client Cert"
	// +optional
	OperatorClientCert *OperatorClientCertSpec `json:"operatorClientCert,omitempty"`
}

// AerospikeBackupServiceStatus defines the observed state of AerospikeBackupService
//
//nolint:govet // for readbility
type AerospikeBackupServiceStatus struct {
	// Image is the image for the backup service.
	// +optional
	Image string `json:"image,omitempty"`

	// Config is the free form configuration for the backup service in YAML format.
	// This config is used to start the backup service. The config is passed as a file to the backup service.
	// It includes: service, backup-policies, storage, secret-agent.
	// +optional
	Config runtime.RawExtension `json:"config,omitempty"`

	// Specify additional configuration for the AerospikeBackupService pods
	// +optional
	PodSpec ServicePodSpec `json:"podSpec,omitempty"`

	// Resources define the requests and limits for the backup service container.
	// Resources.Limits should be more than Resources.Requests.
	//
	// Deprecated: Resources field is now part of status.podSpec.serviceContainer
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// SecretMounts is the list of secret to be mounted in the backup service.
	// +optional
	SecretMounts []SecretMount `json:"secrets,omitempty"`

	// Service defines the Kubernetes service configuration for the backup service.
	// It is used to expose the backup service deployment. By default, the service type is ClusterIP.
	// +optional
	Service *Service `json:"service,omitempty"`

	// OperatorClientCert is the configuration the operator uses to connect to the backup service over HTTPS.
	// +optional
	OperatorClientCert *OperatorClientCertSpec `json:"operatorClientCert,omitempty"`

	// ContextPath is the backup service API context path
	// +optional
	ContextPath string `json:"contextPath,omitempty"`

	// Phase denotes Backup service phase
	Phase AerospikeBackupServicePhase `json:"phase"`

	// Scheme is the URL scheme the operator uses to connect to the backup service.
	// An empty value means HTTP.
	// +kubebuilder:validation:Enum=HTTP;HTTPS
	// +optional
	Scheme corev1.URIScheme `json:"scheme,omitempty"`

	// Port is the port the operator uses to connect to the backup service.
	// +optional
	Port int32 `json:"port,omitempty"`
}

// OperatorClientCertSpec configures the TLS connection from the operator to the backup service HTTPS listener.
type OperatorClientCertSpec struct {
	// SecretCertSource is the Secret holding the CA certificate used to verify the backup service certificate
	// and, when the backup service requires client certificates, the operator's client certificate and key.
	SecretCertSource SecretCertSource `json:"secretCertSource"`

	// ServerName is the host name expected in the backup service certificate.
	// Defaults to <name>.<namespace>.svc of the AerospikeBackupService.
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// SecretCertSource is a Secret in the namespace of the AerospikeBackupService holding the operator's TLS files.
// Each *Filename field is a key in the Secret's data, for example ca.crt.
type SecretCertSource struct {
	// SecretName is the name of the Secret.
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`

	// CaCertsFilename is the key of the CA certificate that signed the backup service certificate.
	// When set, only this CA is trusted. If not set, the system CA certificates are used.
	// +optional
	CaCertsFilename string `json:"caCertsFilename,omitempty"`

	// ClientCertFilename is the key of the operator's client certificate.
	// Required when the backup service sets client-auth to require-and-verify.
	// Must be set together with ClientKeyFilename.
	// +optional
	ClientCertFilename string `json:"clientCertFilename,omitempty"`

	// ClientKeyFilename is the key of the private key for ClientCertFilename.
	// +optional
	ClientKeyFilename string `json:"clientKeyFilename,omitempty"`
}

type ServicePodSpec struct {
	// ServiceContainerSpec configures the backup service container
	// created by the operator.
	// +optional
	ServiceContainerSpec ServiceContainerSpec `json:"serviceContainer,omitempty"`

	// MetaData to add to the pod.
	// +optional
	ObjectMeta AerospikeObjectMeta `json:"metadata,omitempty"`

	// SchedulingPolicy controls pods placement on Kubernetes nodes.
	SchedulingPolicy `json:",inline"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the backup service pod.
	// Defaults to "aerospike-backup-service" if not provided.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of
	// the images used by this PodSpec.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

type ServiceContainerSpec struct {
	// SecurityContext defines the security context for the backup service container.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// Resources defines the requests and limits for the backup service container.
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations="aerospike-kubernetes-operator/version=4.6.0"
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

type AerospikeObjectMeta struct {
	// Key - Value pair that may be set by external tools to store and retrieve arbitrary metadata
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Key - Value pairs that can be used to organize and categorize scope and select objects
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// SchedulingPolicy controls pod placement on Kubernetes nodes.
type SchedulingPolicy struct { //nolint:govet // for readability
	// Affinity rules for pod placement.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations for this pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector constraints for this pod.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}
