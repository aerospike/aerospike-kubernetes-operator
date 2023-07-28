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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AerospikeClusterSpec defines the desired state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterSpec struct { //nolint:govet // for readability
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Adds custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// Aerospike cluster size
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Size"
	Size int32 `json:"size"`
	// Aerospike server image
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Server Image"
	Image string `json:"image"`
	// Storage specify persistent storage to use for the Aerospike pods
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage"
	Storage AerospikeStorageSpec `json:"storage,omitempty"`
	// Has the Aerospike roles and users definitions. Required if aerospike cluster security is enabled.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Access Control"
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// Sets config in aerospike.conf file. Other configs are taken as default
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Aerospike Server Configuration"
	// +kubebuilder:pruning:PreserveUnknownFields
	AerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig"`
	// ValidationPolicy controls validation of the Aerospike cluster resource.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Validation Policy"
	ValidationPolicy *ValidationPolicySpec `json:"validationPolicy,omitempty"`
	// RackConfig Configures the operator to deploy rack aware Aerospike cluster.
	// Pods will be deployed in given racks based on given configuration
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rack Config"
	RackConfig RackConfig `json:"rackConfig,omitempty"`
	// AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Aerospike Network Policy"
	AerospikeNetworkPolicy AerospikeNetworkPolicy `json:"aerospikeNetworkPolicy,omitempty"`
	// Certificates to connect to Aerospike.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Operator Client Cert"
	OperatorClientCertSpec *AerospikeOperatorClientCertSpec `json:"operatorClientCert,omitempty"`
	// Specify additional configuration for the Aerospike pods
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Configuration"
	PodSpec AerospikePodSpec `json:"podSpec,omitempty"`
	// SeedsFinderServices creates additional Kubernetes service that allow
	// clients to discover Aerospike cluster nodes.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Seeds Finder Services"
	SeedsFinderServices SeedsFinderServices `json:"seedsFinderServices,omitempty"`
	// RosterNodeBlockList is a list of blocked nodeIDs from roster in a strong-consistency setup
	RosterNodeBlockList []string `json:"rosterNodeBlockList,omitempty"`
}

type SeedsFinderServices struct {
	// LoadBalancer created to discover Aerospike Cluster nodes from outside of
	// Kubernetes cluster.
	LoadBalancer *LoadBalancerSpec `json:"loadBalancer,omitempty"`
}

// LoadBalancerSpec contains specification for Service with type LoadBalancer.
// +k8s:openapi-gen=true
type LoadBalancerSpec struct { //nolint:govet // for readability
	// +kubebuilder:validation:Enum=Local;Cluster
	// +optional
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicyType `json:"externalTrafficPolicy,omitempty"`

	// +patchStrategy=merge
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" patchStrategy:"merge"`

	// Port Exposed port on load balancer. If not specified TargetPort is used.
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// TargetPort Target port. If not specified the tls-port of network.service stanza is used from Aerospike config.
	// If there is no tls port configured then regular port from network.service is used.
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +optional
	TargetPort int32 `json:"targetPort,omitempty"`

	// +optional
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges,omitempty" patchStrategy:"merge"`
}

type AerospikeOperatorClientCertSpec struct { //nolint:govet // for readability
	// If specified, this name will be added to tls-authenticate-client list by the operator
	// +optional
	TLSClientName               string `json:"tlsClientName,omitempty"`
	AerospikeOperatorCertSource `json:",inline"`
}

// AerospikeOperatorCertSource Represents the source of Aerospike ClientCert for Operator.
// Only one of its members may be specified.
type AerospikeOperatorCertSource struct {
	// +optional
	SecretCertSource *AerospikeSecretCertSource `json:"secretCertSource,omitempty"`
	// +optional
	CertPathInOperator *AerospikeCertPathInOperatorSource `json:"certPathInOperator,omitempty"`
}

type CaCertsSource struct {
	SecretName string `json:"secretName"`
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`
}

type AerospikeSecretCertSource struct {
	// +optional
	CaCertsSource *CaCertsSource `json:"caCertsSource,omitempty"`
	SecretName    string         `json:"secretName"`
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`
	// +optional
	CaCertsFilename string `json:"caCertsFilename,omitempty"`
	// +optional
	ClientCertFilename string `json:"clientCertFilename,omitempty"`
	// +optional
	ClientKeyFilename string `json:"clientKeyFilename,omitempty"`
}

// AerospikeCertPathInOperatorSource contain configuration for certificates used by operator to connect to aerospike
// cluster.
// All paths are on operator's filesystem.
type AerospikeCertPathInOperatorSource struct {
	// +optional
	CaCertsPath string `json:"caCertsPath,omitempty"`
	// +optional
	ClientCertPath string `json:"clientCertPath,omitempty"`
	// +optional
	ClientKeyPath string `json:"clientKeyPath,omitempty"`
}

type AerospikeObjectMeta struct {
	// Key - Value pair that may be set by external tools to store and retrieve arbitrary metadata
	Annotations map[string]string `json:"annotations,omitempty"`
	// Key - Value pairs that can be used to organize and categorize scope and select objects
	Labels map[string]string `json:"labels,omitempty"`
}

// AerospikePodSpec contain configuration for created Aerospike cluster pods.
type AerospikePodSpec struct { //nolint:govet // for readability
	// AerospikeContainerSpec configures the aerospike-server container
	// created by the operator.
	AerospikeContainerSpec AerospikeContainerSpec `json:"aerospikeContainer,omitempty"`
	// AerospikeInitContainerSpec configures the aerospike-init container
	// created by the operator.
	AerospikeInitContainerSpec *AerospikeInitContainerSpec `json:"aerospikeInitContainer,omitempty"`
	// MetaData to add to the pod.
	AerospikeObjectMeta AerospikeObjectMeta `json:"metadata,omitempty"`
	// Sidecars to add to the pod.
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// InitContainers to add to the pods.
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// SchedulingPolicy  controls pods placement on Kubernetes nodes.
	SchedulingPolicy `json:",inline"`

	// If set true then multiple pods can be created per Kubernetes Node.
	// This will create a NodePort service for each Pod.
	// NodePort, as the name implies, opens a specific port on all the Kubernetes Nodes ,
	// and any traffic that is sent to this port is forwarded to the service.
	// Here service picks a random port in range (30000-32767), so these port should be open.
	//
	// If set false then only single pod can be created per Kubernetes Node.
	// This will create Pods using hostPort setting.
	// The container port will be exposed to the external network at <hostIP>:<hostPort>,
	// where the hostIP is the IP address of the Kubernetes Node where the container is running and
	// the hostPort is the port requested by the user.
	MultiPodPerHost bool `json:"multiPodPerHost,omitempty"`

	// HostNetwork enables host networking for the pod.
	// To enable hostNetwork multiPodPerHost must be false.
	HostNetwork bool `json:"hostNetwork,omitempty"`

	// DnsPolicy same as https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-s-dns-policy.
	// If hostNetwork is true and policy is not specified, it defaults to ClusterFirstWithHostNet
	InputDNSPolicy *corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// Effective value of the DNSPolicy
	DNSPolicy corev1.DNSPolicy `json:"effectiveDNSPolicy,omitempty"`

	// DNSConfig defines the DNS parameters of a pod in addition to those generated from DNSPolicy.
	// This is required field when dnsPolicy is set to `None`
	DNSConfig *corev1.PodDNSConfig `json:"dnsConfig,omitempty"`

	// SecurityContext holds pod-level security attributes and common container settings.
	// Optional: Defaults to empty.  See type description for default values of each field.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace to use for pulling any of
	// the images used by this PodSpec.
	// More info: https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

type AerospikeContainerSpec struct {
	// SecurityContext that will be added to aerospike-server container created by operator.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Define resources requests and limits for Aerospike Server Container.
	// Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

type AerospikeInitContainerSpec struct { //nolint:govet // for readability
	// ImageRegistry is the name of image registry for aerospike-init container image
	// ImageRegistry, e.g. docker.io, redhat.access.com
	ImageRegistry string `json:"imageRegistry,omitempty"`
	// SecurityContext that will be added to aerospike-init container created by operator.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Define resources requests and limits for Aerospike init Container.
	// Only Memory and Cpu resources can be given
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RackPodSpec provides rack specific overrides to the global pod spec.
type RackPodSpec struct {
	// SchedulingPolicy overrides for this rack.
	SchedulingPolicy `json:",inline"`
}

// SchedulingPolicy controls pod placement on Kubernetes nodes.
type SchedulingPolicy struct { //nolint:govet // for readability
	// Affinity rules for pod placement.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Tolerations for this pod.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// NodeSelector constraints for this pod.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// RackConfig specifies all racks and related policies
type RackConfig struct { //nolint:govet // for readability
	// List of Aerospike namespaces for which rack feature will be enabled
	Namespaces []string `json:"namespaces,omitempty"`
	// Racks is the list of all racks
	// +nullable
	Racks []Rack `json:"racks,omitempty"`
	// RollingUpdateBatchSize is the percentage/number of rack pods that will be restarted simultaneously
	// +optional
	RollingUpdateBatchSize *intstr.IntOrString `json:"rollingUpdateBatchSize,omitempty"`
}

// Rack specifies single rack config
type Rack struct { //nolint:govet // for readability
	// Identifier for the rack
	ID int `json:"id"`
	// Zone name for setting rack affinity. Rack pods will be deployed to given Zone
	Zone string `json:"zone,omitempty"`
	// Region name for setting rack affinity. Rack pods will be deployed to given Region
	Region string `json:"region,omitempty"`
	// RackLabel for setting rack affinity.
	// Rack pods will be deployed in k8s nodes having rackLabel {aerospike.com/rack-label: <rack-label>}
	RackLabel string `json:"rackLabel,omitempty"`
	// K8s Node name for setting rack affinity. Rack pods will be deployed in given k8s Node
	NodeName string `json:"nodeName,omitempty"`
	// AerospikeConfig overrides the common AerospikeConfig for this Rack. This is merged with global Aerospike config.
	// +kubebuilder:pruning:PreserveUnknownFields
	InputAerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig,omitempty"`
	// Effective/operative Aerospike config. The resultant is a merge of rack Aerospike config and the global
	// Aerospike config
	// +kubebuilder:pruning:PreserveUnknownFields
	AerospikeConfig AerospikeConfigSpec `json:"effectiveAerospikeConfig,omitempty"`
	// Storage specify persistent storage to use for the pods in this rack. This value overwrites the global storage config
	InputStorage *AerospikeStorageSpec `json:"storage,omitempty"`
	// Effective/operative storage. The resultant is user input if specified else global storage
	Storage AerospikeStorageSpec `json:"effectiveStorage,omitempty"`
	// PodSpec to use for the pods in this rack. This value overwrites the global storage config
	InputPodSpec *RackPodSpec `json:"podSpec,omitempty"`
	// Effective/operative PodSpec. The resultant is user input if specified else global PodSpec
	PodSpec RackPodSpec `json:"effectivePodSpec,omitempty"`
}

// ValidationPolicySpec controls validation of the Aerospike cluster resource.
type ValidationPolicySpec struct {
	// skipWorkDirValidate validates that Aerospike work directory is mounted on a persistent file storage.
	// Defaults to false.
	SkipWorkDirValidate bool `json:"skipWorkDirValidate"`

	// ValidateXdrDigestLogFile validates that xdr digest log file is mounted on a persistent file storage.
	// Defaults to false.
	SkipXdrDlogFileValidate bool `json:"skipXdrDlogFileValidate"`
}

// AerospikeRoleSpec specifies an Aerospike database role and its associated privileges.
type AerospikeRoleSpec struct {
	// Name of this role.
	Name string `json:"name"`

	// Privileges granted to this role.
	// +listType=set
	Privileges []string `json:"privileges"`

	// Whitelist of host address allowed for this role.
	// +listType=set
	Whitelist []string `json:"whitelist,omitempty"`

	// ReadQuota specifies permitted rate of read records for current role (the value is in RPS)
	ReadQuota uint32 `json:"readQuota,omitempty"`

	// WriteQuota specifies permitted rate of write records for current role (the value is in RPS)
	WriteQuota uint32 `json:"writeQuota,omitempty"`
}

// AerospikeUserSpec specifies an Aerospike database user, the secret name for the password and, associated roles.
type AerospikeUserSpec struct {
	// Name is the user's username.
	Name string `json:"name"`

	// SecretName has secret info created by user. User needs to create this secret from password literal.
	// eg: kubectl create secret generic dev-db-secret --from-literal=password='password'
	SecretName string `json:"secretName"`

	// Roles is the list of roles granted to the user.
	// +listType=set
	Roles []string `json:"roles"`
}

// AerospikeClientAdminPolicy specify the aerospike client admin policy for access control operations.
type AerospikeClientAdminPolicy struct {
	// Timeout for admin client policy in milliseconds.
	Timeout int `json:"timeout"`
}

// AerospikeAccessControlSpec specifies the roles and users to set up on the
// database fo access control.
type AerospikeAccessControlSpec struct {
	AdminPolicy *AerospikeClientAdminPolicy `json:"adminPolicy,omitempty"`

	// Roles is the set of roles to allow on the Aerospike cluster.
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Roles []AerospikeRoleSpec `json:"roles,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// Users is the set of users to allow on the Aerospike cluster.
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Users []AerospikeUserSpec `json:"users" patchStrategy:"merge" patchMergeKey:"name"`
}

// AerospikeVolumeMethod specifies how block volumes should be initialized.
// +kubebuilder:validation:Enum=none;dd;blkdiscard;deleteFiles
// +k8s:openapi-gen=true
type AerospikeVolumeMethod string

// AerospikePersistentVolumePolicySpec contains policies to manage persistent volumes.
type AerospikePersistentVolumePolicySpec struct {

	// InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods come up the
	// first time. Defaults to "none".
	InputInitMethod *AerospikeVolumeMethod `json:"initMethod,omitempty"`

	// WipeMethod determines how volumes attached to Aerospike server pods are wiped for dealing with storage format
	// changes.
	InputWipeMethod *AerospikeVolumeMethod `json:"wipeMethod,omitempty"`

	// CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is
	// terminated and removed from the cluster.
	InputCascadeDelete *bool `json:"cascadeDelete,omitempty"`

	// Effective/operative value to use as the volume init method after applying defaults.
	InitMethod AerospikeVolumeMethod `json:"effectiveInitMethod,omitempty"`

	// Effective/operative value to use as the volume wipe method after applying defaults.
	WipeMethod AerospikeVolumeMethod `json:"effectiveWipeMethod,omitempty"`

	// Effective/operative value to use for cascade delete after applying defaults.
	CascadeDelete bool `json:"effectiveCascadeDelete,omitempty"`
}

// AerospikeServerVolumeAttachment is a volume attachment in the Aerospike server container.
type AerospikeServerVolumeAttachment struct {
	// Path to attach the volume on the Aerospike server container.
	Path string `json:"path"`
	// AttachmentOptions that control how the volume is attached.
	AttachmentOptions `json:",inline"`
}

// VolumeAttachment specifies volume attachment to a container.
type VolumeAttachment struct {
	// ContainerName is the name of the container to attach this volume to.
	ContainerName string `json:"containerName"`
	// Path to attach the volume on the container.
	Path string `json:"path"`
	// AttachmentOptions that control how the volume is attached.
	AttachmentOptions `json:",inline"`
}

// AttachmentOptions that control how a volume is mounted or attached.
type AttachmentOptions struct {
	// +optional
	MountOptions `json:"mountOptions,omitempty"`
	// DeviceOptions
}

type MountOptions struct { //nolint:govet // for readability
	// Mounted read-only if true, read-write otherwise (false or unspecified).
	// Defaults to false.
	// +optional
	ReadOnly bool `json:"readOnly,omitempty"`
	// Path within the volume from which the container's volume should be mounted.
	// Defaults to "" (volume's root).
	// +optional
	SubPath string `json:"subPath,omitempty"`
	// mountPropagation determines how mounts are propagated from the host
	// to container and the other way around.
	// When not set, MountPropagationNone is used.
	// This field is beta in 1.10.
	// +optional
	MountPropagation *corev1.MountPropagationMode `json:"mountPropagation,omitempty"`
	// Expanded path within the volume from which the container's volume should be mounted.
	// Behaves similarly to SubPath but environment variable references $(
	// VAR_NAME) are expanded using the container's environment.
	// Defaults to "" (volume's root).
	// SubPathExpr and SubPath are mutually exclusive.
	// +optional
	SubPathExpr string `json:"subPathExpr,omitempty"`
}

// PersistentVolumeSpec describes a persistent volume to claim and attach to Aerospike pods.
// +k8s:openapi-gen=true
type PersistentVolumeSpec struct { //nolint:govet // for readability
	AerospikeObjectMeta `json:"metadata,omitempty"`

	// StorageClass should be pre-created by user.
	StorageClass string `json:"storageClass"`

	// VolumeMode specifies if the volume is block/raw or a filesystem.
	VolumeMode corev1.PersistentVolumeMode `json:"volumeMode"`

	// Size of volume.
	Size resource.Quantity `json:"size"`

	// Name for creating PVC for this volume, Name or path should be given
	// Name string `json:"name"`
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty" protobuf:"bytes,1,rep,name=accessModes,casttype=PersistentVolumeAccessMode"` //nolint:lll // for readability

	// A label query over volumes to consider for binding.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// VolumeSource is the source of a volume to mount.
// Only one of its members may be specified.
type VolumeSource struct {
	// EmptyDir represents a temporary directory that shares a pod's lifetime.
	// More info: https://kubernetes.io/docs/concepts/storage/volumes#emptydir
	// +optional
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty" protobuf:"bytes,2,opt,name=emptyDir"`

	// +optional
	Secret *corev1.SecretVolumeSource `json:"secret,omitempty" protobuf:"bytes,6,opt,name=secret"`

	// ConfigMap represents a configMap that should populate this volume
	// +optional
	ConfigMap *corev1.ConfigMapVolumeSource `json:"configMap,omitempty" protobuf:"bytes,19,opt,name=configMap"`

	// +optional
	PersistentVolume *PersistentVolumeSpec `json:"persistentVolume,omitempty"`
}

type VolumeSpec struct {
	// TODO: should this be inside source.PV or other type of source will also need this
	// Contains  policies for this volumes.
	AerospikePersistentVolumePolicySpec `json:",inline"`

	// Name for this volume, Name or path should be given.
	Name string `json:"name"`

	// Source of this volume.
	Source VolumeSource `json:"source,omitempty"`

	// Aerospike attachment of this volume on Aerospike server container.
	// +optional
	Aerospike *AerospikeServerVolumeAttachment `json:"aerospike,omitempty"`

	// Sidecars are side containers where this volume will be mounted
	// +optional
	Sidecars []VolumeAttachment `json:"sidecars,omitempty"`

	// InitContainers are additional init containers where this volume will be mounted
	// +optional
	InitContainers []VolumeAttachment `json:"initContainers,omitempty"`
}

// AerospikeStorageSpec lists persistent volumes to claim and attach to Aerospike pods and persistence policies.
// +k8s:openapi-gen=true
type AerospikeStorageSpec struct { //nolint:govet // for readability
	// FileSystemVolumePolicy contains default policies for filesystem volumes.
	FileSystemVolumePolicy AerospikePersistentVolumePolicySpec `json:"filesystemVolumePolicy,omitempty"`

	// BlockVolumePolicy contains default policies for block volumes.
	BlockVolumePolicy AerospikePersistentVolumePolicySpec `json:"blockVolumePolicy,omitempty"`

	// CleanupThreads contains maximum number of cleanup threads(dd or blkdiscard) per init container.
	CleanupThreads int `json:"cleanupThreads,omitempty"`

	// Volumes list to attach to created pods.
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Volumes []VolumeSpec `json:"volumes,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// AerospikeClusterStatusSpec captures the current status of the cluster.
type AerospikeClusterStatusSpec struct { //nolint:govet // for readability
	// Aerospike cluster size
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Cluster Size"
	Size int32 `json:"size,omitempty"`
	// Aerospike server image
	Image string `json:"image,omitempty"`
	// If set true then multiple pods can be created per Kubernetes Node.
	// This will create a NodePort service for each Pod.
	// NodePort, as the name implies, opens a specific port on all the Kubernetes Nodes ,
	// and any traffic that is sent to this port is forwarded to the service.
	// Here service picks a random port in range (30000-32767), so these port should be open.
	//
	// If set false then only single pod can be created per Kubernetes Node.
	// This will create Pods using hostPort setting.
	// The container port will be exposed to the external network at <hostIP>:<hostPort>,
	// where the hostIP is the IP address of the Kubernetes Node where the container is running and
	// the hostPort is the port requested by the user.
	// Deprecated: MultiPodPerHost is now part of podSpec
	MultiPodPerHost bool `json:"multiPodPerHost,omitempty"`
	// Storage specify persistent storage to use for the Aerospike pods.
	Storage AerospikeStorageSpec `json:"storage,omitempty"`
	// AerospikeAccessControl has the Aerospike roles and users definitions.
	// Required if aerospike cluster security is enabled.
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default
	// +kubebuilder:pruning:PreserveUnknownFields
	// +nullable
	AerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig,omitempty"`
	// Define resources requests and limits for Aerospike Server Container.
	// Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	// Deprecated: Resources field is now part of containerSpec
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// ValidationPolicy controls validation of the Aerospike cluster resource.
	ValidationPolicy *ValidationPolicySpec `json:"validationPolicy,omitempty"`
	// RackConfig Configures the operator to deploy rack aware Aerospike cluster.
	// Pods will be deployed in given racks based on given configuration
	// +nullable
	// +optional
	RackConfig RackConfig `json:"rackConfig,omitempty"`
	// AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.
	AerospikeNetworkPolicy AerospikeNetworkPolicy `json:"aerospikeNetworkPolicy,omitempty"`
	// Certificates to connect to Aerospike. If omitted then certs are taken from the secret 'aerospike-secret'.
	// +optional
	OperatorClientCertSpec *AerospikeOperatorClientCertSpec `json:"operatorClientCertSpec,omitempty"`
	// Additional configuration for create Aerospike pods.
	PodSpec AerospikePodSpec `json:"podSpec,omitempty"`
	// SeedsFinderServices describes services which are used for seeding Aerospike nodes.
	SeedsFinderServices SeedsFinderServices `json:"seedsFinderServices,omitempty"`
	// RosterNodeBlockList is a list of blocked nodeIDs from roster in a strong-consistency setup
	RosterNodeBlockList []string `json:"rosterNodeBlockList,omitempty"`
}

// AerospikeClusterStatus defines the observed state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterStatus struct { //nolint:govet // for readability
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Add custom validation
	// using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// +nullable
	// The current state of Aerospike cluster.
	AerospikeClusterStatusSpec `json:",inline"`

	// Details about the current condition of the AerospikeCluster resource.
	// Conditions []apiextensions.CustomResourceDefinitionCondition `json:"conditions"`

	// Pods has Aerospike specific status of the pods.
	// This is map instead of the conventional map as list convention to allow each pod to patch update its own
	// status. The map key is the name of the pod.
	// +patchStrategy=strategic
	Pods map[string]AerospikePodStatus `json:"pods" patchStrategy:"strategic"`
}

// AerospikeNetworkType specifies the type of network address to use.
// +k8s:openapi-gen=true
type AerospikeNetworkType string

// AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.
type AerospikeNetworkPolicy struct {
	// AccessType is the type of network address to use for Aerospike access address.
	// Defaults to hostInternal.
	// +kubebuilder:validation:Enum=pod;hostInternal;hostExternal;configuredIP;customInterface
	AccessType AerospikeNetworkType `json:"access,omitempty"`

	// CustomAccessNetworkNames is the list of the pod's network interfaces used for Aerospike access address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign
	// network interfaces to the pod.
	// Required with 'customInterface' access type.
	// +kubebuilder:validation:MinItems:=1
	CustomAccessNetworkNames []string `json:"customAccessNetworkNames,omitempty"`

	// AlternateAccessType is the type of network address to use for Aerospike alternate access address.
	// Defaults to hostExternal.
	// +kubebuilder:validation:Enum=pod;hostInternal;hostExternal;configuredIP;customInterface
	AlternateAccessType AerospikeNetworkType `json:"alternateAccess,omitempty"`

	// CustomAlternateAccessNetworkNames is the list of the pod's network interfaces used for Aerospike
	// alternate access address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign
	// network interfaces to the pod.
	// Required with 'customInterface' alternateAccess type
	// +kubebuilder:validation:MinItems:=1
	CustomAlternateAccessNetworkNames []string `json:"customAlternateAccessNetworkNames,omitempty"`

	// TLSAccessType is the type of network address to use for Aerospike TLS access address.
	// Defaults to hostInternal.
	// +kubebuilder:validation:Enum=pod;hostInternal;hostExternal;configuredIP;customInterface
	TLSAccessType AerospikeNetworkType `json:"tlsAccess,omitempty"`

	// CustomTLSAccessNetworkNames is the list of the pod's network interfaces used for Aerospike TLS access address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign
	// network interfaces to the pod.
	// Required with 'customInterface' tlsAccess type
	// +kubebuilder:validation:MinItems:=1
	CustomTLSAccessNetworkNames []string `json:"customTLSAccessNetworkNames,omitempty"`

	// TLSAlternateAccessType is the type of network address to use for Aerospike TLS alternate access address.
	// Defaults to hostExternal.
	// +kubebuilder:validation:Enum=pod;hostInternal;hostExternal;configuredIP;customInterface
	TLSAlternateAccessType AerospikeNetworkType `json:"tlsAlternateAccess,omitempty"`

	// CustomTLSAlternateAccessNetworkNames is the list of the pod's network interfaces used for Aerospike TLS
	// alternate access address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign
	// network interfaces to the pod.
	// Required with 'customInterface' tlsAlternateAccess type
	// +kubebuilder:validation:MinItems:=1
	CustomTLSAlternateAccessNetworkNames []string `json:"customTLSAlternateAccessNetworkNames,omitempty"`

	// FabricType is the type of network address to use for Aerospike fabric address.
	// Defaults is empty meaning all interfaces 'any'.
	// +kubebuilder:validation:Enum:=customInterface
	FabricType AerospikeNetworkType `json:"fabric,omitempty"`

	// CustomFabricNetworkNames is the list of the pod's network interfaces used for Aerospike fabric address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign
	// network interfaces to the pod.
	// Required with 'customInterface' fabric type
	// +kubebuilder:validation:MinItems:=1
	CustomFabricNetworkNames []string `json:"customFabricNetworkNames,omitempty"`

	// TLSFabricType is the type of network address to use for Aerospike TLS fabric address.
	// Defaults is empty meaning all interfaces 'any'.
	// +kubebuilder:validation:Enum:=customInterface
	TLSFabricType AerospikeNetworkType `json:"tlsFabric,omitempty"`

	// CustomTLSFabricNetworkNames is the list of the pod's network interfaces used for Aerospike TLS fabric address.
	// Each element in the list is specified with a namespace and the name of a NetworkAttachmentDefinition,
	// separated by a forward slash (/).
	// These elements must be defined in the pod annotation k8s.v1.cni.cncf.io/networks in order to assign network
	// interfaces to the pod.
	// Required with 'customInterface' tlsFabric type
	// +kubebuilder:validation:MinItems:=1
	CustomTLSFabricNetworkNames []string `json:"customTLSFabricNetworkNames,omitempty"`
}

// AerospikeInstanceSummary defines the observed state of a pod's Aerospike Server Instance.
// +k8s:openapi-gen=true
type AerospikeInstanceSummary struct { //nolint:govet // for readability
	// ClusterName is the name of the Aerospike cluster this pod belongs to.
	ClusterName string `json:"clusterName"`
	// NodeID is the unique Aerospike ID for this pod.
	NodeID string `json:"nodeID"`
	// RackID of rack to which this node belongs
	RackID int `json:"rackID,omitempty"`
	// TLSName is the TLS name of this pod in the Aerospike cluster.
	TLSName string `json:"tlsName,omitempty"`

	// AccessEndpoints are the access endpoints for this pod.
	AccessEndpoints []string `json:"accessEndpoints,omitempty"`
	// AlternateAccessEndpoints are the alternate access endpoints for this pod.
	AlternateAccessEndpoints []string `json:"alternateAccessEndpoints,omitempty"`
	// TLSAccessEndpoints are the TLS access endpoints for this pod.
	TLSAccessEndpoints []string `json:"tlsAccessEndpoints,omitempty"`
	// TLSAlternateAccessEndpoints are the alternate TLS access endpoints for this pod.
	TLSAlternateAccessEndpoints []string `json:"tlsAlternateAccessEndpoints,omitempty"`
}

// AerospikePodStatus contains the Aerospike specific status of the Aerospike
// server pods.
// +k8s:openapi-gen=true
type AerospikePodStatus struct { //nolint:govet // for readability
	// Image is the Aerospike image this pod is running.
	Image string `json:"image"`
	// PodIP in the K8s network.
	PodIP string `json:"podIP"`
	// HostInternalIP of the K8s host this pod is scheduled on.
	HostInternalIP string `json:"hostInternalIP,omitempty"`
	// HostExternalIP of the K8s host this pod is scheduled on.
	HostExternalIP string `json:"hostExternalIP,omitempty"`
	// PodPort is the port K8s internal Aerospike clients can connect to.
	PodPort int `json:"podPort"`
	// ServicePort is the port Aerospike clients outside K8s can connect to.
	ServicePort int32 `json:"servicePort,omitempty"`

	// Aerospike server instance summary for this pod.
	Aerospike AerospikeInstanceSummary `json:"aerospike,omitempty"`

	// InitializedVolumes is the list of volume names that have already been
	// initialized.
	InitializedVolumes []string `json:"initializedVolumes,omitempty"`

	// DirtyVolumes is the list of volume names that are removed
	// from aerospike namespaces and will be cleaned during init
	// if they are reused in any namespace.
	DirtyVolumes []string `json:"dirtyVolumes,omitempty"`

	// InitializedVolumePaths is deprecated version of InitializedVolumes.
	// +optional
	// +nullable
	InitializedVolumePaths []string `json:"initializedVolumePaths,omitempty"`

	// AerospikeConfigHash is ripemd160 hash of aerospikeConfig used by this pod
	AerospikeConfigHash string `json:"aerospikeConfigHash"`

	// NetworkPolicyHash is ripemd160 hash of NetworkPolicy used by this pod
	NetworkPolicyHash string `json:"networkPolicyHash"`

	// PodSpecHash is ripemd160 hash of PodSpec used by this pod
	PodSpecHash string `json:"podSpecHash"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:deprecatedversion
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.spec.size`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="MultiPodPerHost",type=boolean,JSONPath=`.spec.podSpec.MultiPodPerHost`
// +kubebuilder:printcolumn:name="HostNetwork",type=boolean,JSONPath=`.spec.podSpec.hostNetwork`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AerospikeCluster is the schema for the AerospikeCluster API
// +operator-sdk:csv:customresourcedefinitions:displayName="Aerospike Cluster",resources={{Service, v1},{Pod,v1},{StatefulSet,v1}}
//
//nolint:lll // for readability
type AerospikeCluster struct { //nolint:govet // for readability
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeClusterSpec   `json:"spec,omitempty"`
	Status AerospikeClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AerospikeClusterList contains a list of AerospikeCluster
type AerospikeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeCluster{}, &AerospikeClusterList{})
}
