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

package v1alpha1

import (
	"fmt"

	lib "github.com/aerospike/aerospike-management-lib"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AerospikeClusterSpec defines the desired state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// Aerospike cluster size
	Size int32 `json:"size"`
	// Aerospike server image
	Image string `json:"image"`
	// Storage specify persistent storage to use for the Aerospike pods.
	Storage AerospikeStorageSpec `json:"storage,omitempty"`
	// AerospikeAccessControl has the Aerospike roles and users definitions. Required if aerospike cluster security is enabled.
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default
	// +kubebuilder:pruning:PreserveUnknownFields
	AerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig"`
	// Define resources requests and limits for Aerospike Server Container. Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources"`
	// ValidationPolicy controls validation of the Aerospike cluster resource.
	ValidationPolicy *ValidationPolicySpec `json:"validationPolicy,omitempty"`
	// RackConfig Configures the operator to deploy rack aware Aerospike cluster. Pods will be deployed in given racks based on given configuration
	RackConfig RackConfig `json:"rackConfig,omitempty"`
	// AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.
	AerospikeNetworkPolicy AerospikeNetworkPolicy `json:"aerospikeNetworkPolicy,omitempty"`
	// Certificates to connect to Aerospike. If omitted then certs are taken from the secret 'aerospike-secret'.
	// +optional
	OperatorClientCertSpec *AerospikeOperatorClientCertSpec `json:"operatorClientCertSpec,omitempty"`
	// Additional configuration for create Aerospike pods.
	PodSpec AerospikePodSpec `json:"podSpec,omitempty"`
}

type AerospikeOperatorClientCertSpec struct {
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

type AerospikeSecretCertSource struct {
	SecretName string `json:"secretName"`
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`
	// +optional
	CaCertsFilename string `json:"caCertsFilename,omitempty"`
	// +optional
	ClientCertFilename string `json:"clientCertFilename,omitempty"`
	// +optional
	ClientKeyFilename string `json:"clientKeyFilename,omitempty"`
}

// AerospikeCertPathInOperatorSource contain configuration for certificates used by operator to connect to aerospike cluster.
// All paths are on operator's filesystem.
type AerospikeCertPathInOperatorSource struct {
	// +optional
	CaCertsPath string `json:"caCertsPath,omitempty"`
	// +optional
	ClientCertPath string `json:"clientCertPath,omitempty"`
	// +optional
	ClientKeyPath string `json:"clientKeyPath,omitempty"`
}

func (c *AerospikeOperatorClientCertSpec) IsClientCertConfigured() bool {
	return (c.SecretCertSource != nil && c.SecretCertSource.ClientCertFilename != "") ||
		(c.CertPathInOperator != nil && c.CertPathInOperator.ClientCertPath != "")
}

func (c *AerospikeOperatorClientCertSpec) validate() error {
	if (c.SecretCertSource == nil) == (c.CertPathInOperator == nil) {
		return fmt.Errorf("either \"secretCertSource\" or \"certPathInOperator\" must be set in \"operatorClientCertSpec\" but not both: %+v", c)
	}
	if c.SecretCertSource != nil && (c.SecretCertSource.ClientCertFilename == "") != (c.SecretCertSource.ClientKeyFilename == "") {
		return fmt.Errorf("both \"clientCertFilename\" and \"clientKeyFilename\" should be either set or not set in \"secretCertSource\": %+v", c.SecretCertSource)
	}
	if c.CertPathInOperator != nil && (c.CertPathInOperator.ClientCertPath == "") != (c.CertPathInOperator.ClientKeyPath == "") {
		return fmt.Errorf("both \"clientCertPath\" and \"clientKeyPath\" should be either set or not set in \"certPathInOperator\": %+v", c.CertPathInOperator)
	}
	if c.TLSClientName != "" && !c.IsClientCertConfigured() {
		return fmt.Errorf("tlsClientName is provided but client certificate is not: secretCertSource=%+v, certPathInOperator=%v+v", c.SecretCertSource, c.CertPathInOperator)
	}
	return nil
}

// AerospikePodSpec contain configuration for created Aeropsike cluster pods.
type AerospikePodSpec struct {
	// Sidecars to add to pods.
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	InitContainers []corev1.Container `json:"initContainers,omitempty"`

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
	// To enable hostNetwork multiPodPerHost must be true.
	HostNetwork bool `json:"hostNetwork,omitempty"`

	// DnsPolicy same as https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-s-dns-policy. If hostNetwork is true and policy is not specified, it defaults to ClusterFirstWithHostNet
	InputDNSPolicy *corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// Effective value of the DNSPolicy
	DNSPolicy corev1.DNSPolicy `json:"effectiveDNSPolicy,omitempty"`
}

type RackPodSpec struct {
	SchedulingPolicy `json:",inline"`
}

type SchedulingPolicy struct {
	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
}

// ValidatePodSpecChange indicates if a change to to pod spec is safe to apply.
func (v *AerospikePodSpec) ValidatePodSpecChange(new AerospikePodSpec) error {
	// All changes are valid for now.
	return nil
}

// SetDefaults applies defaults to the pod spec.
func (v *AerospikePodSpec) SetDefaults() error {
	if v.InputDNSPolicy == nil {
		if v.HostNetwork {
			v.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		} else {
			v.DNSPolicy = corev1.DNSClusterFirst
		}
	} else {
		v.DNSPolicy = *v.InputDNSPolicy
	}

	return nil
}

// RackConfig specifies all racks and related policies
type RackConfig struct {
	// List of Aerospike namespaces for which rack feature will be enabled
	Namespaces []string `json:"namespaces,omitempty"`
	// Racks is the list of all racks
	// +nullable
	Racks []Rack `json:"racks,omitempty"`
}

// Rack specifies single rack config
type Rack struct {
	// Identifier for the rack
	ID int `json:"id"`
	// Zone name for setting rack affinity. Rack pods will be deployed to given Zone
	Zone string `json:"zone,omitempty"`
	// Region name for setting rack affinity. Rack pods will be deployed to given Region
	Region string `json:"region,omitempty"`
	// Racklabel for setting rack affinity. Rack pods will be deployed in k8s nodes having rackLable {aerospike.com/rack-label: <rack-label>}
	RackLabel string `json:"rackLabel,omitempty"`
	// K8s Node name for setting rack affinity. Rack pods will be deployed in given k8s Node
	NodeName string `json:"nodeName,omitempty"`
	// AerospikeConfig overrides the common AerospikeConfig for this Rack. This is merged with global Aerospike config.
	// +kubebuilder:pruning:PreserveUnknownFields
	InputAerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig,omitempty"`
	// Effective/operative Aerospike config. The resultant is merge of rack Aerospike config and the global Aerospike config
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
	// skipWorkDirValidate validates that Aerospike work directory is mounted on a persistent file storage. Defaults to false.
	SkipWorkDirValidate bool `json:"skipWorkDirValidate"`

	// ValidateXdrDigestLogFile validates that xdr digest log file is mounted on a persistent file storage. Defaults to false.
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

// AerospikeAccessControlSpec specifies the roles and users to setup on the database fo access control.
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

// AerospikeVolumeInitMethod specifies how block volumes should be initialized.
// +kubebuilder:validation:Enum=none;dd;blkdiscard;deleteFiles
// +k8s:openapi-gen=true
type AerospikeVolumeInitMethod string

const (
	// AerospikeVolumeInitMethodNone specifies the block volume should not be initialized.
	AerospikeVolumeInitMethodNone AerospikeVolumeInitMethod = "none"

	// AerospikeVolumeInitMethodDD specifies the block volume should be zeroed using dd command.
	AerospikeVolumeInitMethodDD AerospikeVolumeInitMethod = "dd"

	// AerospikeVolumeInitMethodBlkdiscard specifies the block volume should be zeroed using blkdiscard command.
	AerospikeVolumeInitMethodBlkdiscard AerospikeVolumeInitMethod = "blkdiscard"

	// AerospikeVolumeInitMethodDeleteFiles specifies the filesystem volume should initialized by deleting files.
	AerospikeVolumeInitMethodDeleteFiles AerospikeVolumeInitMethod = "deleteFiles"
)

// AerospikePersistentVolumePolicySpec contains policies to manage persistent volumes.
type AerospikePersistentVolumePolicySpec struct {
	// InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none".
	InputInitMethod *AerospikeVolumeInitMethod `json:"initMethod,omitempty"`

	// CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster.
	InputCascadeDelete *bool `json:"cascadeDelete,omitempty"`

	// Effective/operative value to use as the volume init method after applying defaults.
	InitMethod AerospikeVolumeInitMethod `json:"effectiveInitMethod,omitempty"`

	// Effective/operative value to use for cascade delete after applying defaults.
	CascadeDelete bool `json:"effectiveCascadeDelete,omitempty"`
}

// SetDefaults applies default values to unset fields of the policy using corresponding fields from defaultPolicy
func (v *AerospikePersistentVolumePolicySpec) SetDefaults(defaultPolicy *AerospikePersistentVolumePolicySpec) {
	if v.InputInitMethod == nil {
		v.InitMethod = defaultPolicy.InitMethod
	} else {
		v.InitMethod = *v.InputInitMethod
	}

	if v.InputCascadeDelete == nil {
		v.CascadeDelete = defaultPolicy.CascadeDelete
	} else {
		v.CascadeDelete = *v.InputCascadeDelete
	}
}

type AerospikeServerVolumeAttachment struct {
	Path              string `json:"path"`
	AttachmentOptions `json:",inline"`
}
type VolumeAttachment struct {
	ContainerName     string `json:"containerName"`
	Path              string `json:"path"`
	AttachmentOptions `json:",inline"`
}

type AttachmentOptions struct {
	// +optional
	MountOptions `json:"mountOptions,omitempty"`
	// DeviceOptions
}

type MountOptions struct {
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
	// Behaves similarly to SubPath but environment variable references $(VAR_NAME) are expanded using the container's environment.
	// Defaults to "" (volume's root).
	// SubPathExpr and SubPath are mutually exclusive.
	// +optional
	SubPathExpr string `json:"subPathExpr,omitempty"`
}

type PersistentVolumeSpec struct {
	// StorageClass should be pre-created by user.
	StorageClass string `json:"storageClass"`

	// VolumeMode specifies if the volume is block/raw or a filesystem.
	VolumeMode corev1.PersistentVolumeMode `json:"volumeMode"`

	// Size of volume.
	Size resource.Quantity `json:"size"`

	// Name for creating PVC for this volume, Name or path should be given
	// Name string `json:"name"`

	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty" protobuf:"bytes,1,rep,name=accessModes,casttype=PersistentVolumeAccessMode"`

	// A label query over volumes to consider for binding.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// Represents the source of a volume to mount.
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

	// Name for this volume, Name or path should be given
	Name string `json:"name"`

	Source VolumeSource `json:"source,omitempty"`

	// Information where this volume will be mounted on AerospikeServer container
	// +optional
	Aerospike *AerospikeServerVolumeAttachment `json:"aerospike,omitempty"`

	// List of sidecar containers, where this volume will be mounted
	// +optional
	Sidecars []VolumeAttachment `json:"sidecars,omitempty"`

	// List of additional init containers, where this volume will be mounted
	// +optional
	InitContainers []VolumeAttachment `json:"initContainers,omitempty"`
}

// AerospikeStorageSpec lists persistent volumes to claim and attach to Aerospike pods and persistence policies.
// +k8s:openapi-gen=true
type AerospikeStorageSpec struct {
	// FileSystemVolumePolicy contains default policies for filesystem volumes.
	FileSystemVolumePolicy AerospikePersistentVolumePolicySpec `json:"filesystemVolumePolicy,omitempty"`

	// BlockVolumePolicy contains default policies for block volumes.
	BlockVolumePolicy AerospikePersistentVolumePolicySpec `json:"blockVolumePolicy,omitempty"`

	// Volumes list to attach to created pods.
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Volumes []VolumeSpec `json:"volumes,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// AerospikeClusterStatusSpec captures the current status of the cluster.
type AerospikeClusterStatusSpec struct {
	// Aerospike cluster size
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
	MultiPodPerHost bool `json:"multiPodPerHost,omitempty"`
	// Storage specify persistent storage to use for the Aerospike pods.
	Storage AerospikeStorageSpec `json:"storage,omitempty"`
	// AerospikeAccessControl has the Aerospike roles and users definitions. Required if aerospike cluster security is enabled.
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default
	// +kubebuilder:pruning:PreserveUnknownFields
	// +nullable
	AerospikeConfig *AerospikeConfigSpec `json:"aerospikeConfig,omitempty"`
	// Define resources requests and limits for Aerospike Server Container. Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// ValidationPolicy controls validation of the Aerospike cluster resource.
	ValidationPolicy *ValidationPolicySpec `json:"validationPolicy,omitempty"`
	// RackConfig Configures the operator to deploy rack aware Aerospike cluster. Pods will be deployed in given racks based on given configuration
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
}

// AerospikeClusterStatus defines the observed state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// +nullable
	// The current state of Aerospike cluster.
	AerospikeClusterStatusSpec `json:",inline"`

	// Details about the current condition of the AerospikeCluster resource.
	//Conditions []apiextensions.CustomResourceDefinitionCondition `json:"conditions"`

	// Pods has Aerospike specific status of the pods. This is map instead of the conventional map as list convention to allow each pod to patch update its own status. The map key is the name of the pod.
	// +patchStrategy=strategic
	Pods map[string]AerospikePodStatus `json:"pods" patchStrategy:"strategic"`
}

// AerospikeNetworkType specifies the type of network address to use.
// +kubebuilder:validation:Enum=pod;hostInternal;hostExternal
// +k8s:openapi-gen=true
type AerospikeNetworkType string

const (
	// AerospikeNetworkTypeUnspecified implies using default access.
	AerospikeNetworkTypeUnspecified AerospikeNetworkType = ""

	// AerospikeNetworkTypePod specifies access using the PodIP and actual Aerospike service port.
	AerospikeNetworkTypePod AerospikeNetworkType = "pod"

	// AerospikeNetworkTypeHostInternal specifies access using the Kubernetes host's internal IP. If the cluster runs single pod per Kunernetes host, the access port will the actual aerospike port else it will be a mapped port.
	AerospikeNetworkTypeHostInternal AerospikeNetworkType = "hostInternal"

	// AerospikeNetworkTypeHostExternal specifies access using the Kubernetes host's external IP. If the cluster runs single pod per Kunernetes host, the access port will the actual aerospike port else it will be a mapped port.
	AerospikeNetworkTypeHostExternal AerospikeNetworkType = "hostExternal"
)

// AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.
type AerospikeNetworkPolicy struct {
	// AccessType is the type of network address to use for Aerospike access address.
	// Defaults to hostInternal.
	AccessType AerospikeNetworkType `json:"access,omitempty"`

	// AlternateAccessType is the type of network address to use for Aerospike alternate access address.
	// Defaults to hostExternal.
	AlternateAccessType AerospikeNetworkType `json:"alternateAccess,omitempty"`

	// TLSAccessType is the type of network address to use for Aerospike TLS access address.
	// Defaults to hostInternal.
	TLSAccessType AerospikeNetworkType `json:"tlsAccess,omitempty"`

	// TLSAlternateAccessType is the type of network address to use for Aerospike TLS alternate access address.
	// Defaults to hostExternal.
	TLSAlternateAccessType AerospikeNetworkType `json:"tlsAlternateAccess,omitempty"`
}

// SetDefaults applies default to unspecified fields on the network policy.
func (v *AerospikeNetworkPolicy) SetDefaults() {
	if v.AccessType == AerospikeNetworkTypeUnspecified {
		v.AccessType = AerospikeNetworkTypeHostInternal
	}

	if v.AlternateAccessType == AerospikeNetworkTypeUnspecified {
		v.AlternateAccessType = AerospikeNetworkTypeHostExternal
	}

	if v.TLSAccessType == AerospikeNetworkTypeUnspecified {
		v.TLSAccessType = AerospikeNetworkTypeHostInternal
	}

	if v.TLSAlternateAccessType == AerospikeNetworkTypeUnspecified {
		v.TLSAlternateAccessType = AerospikeNetworkTypeHostExternal
	}
}

// AerospikeInstanceSummary defines the observed state of a pod's Aerospike Server Instance.
// +k8s:openapi-gen=true
type AerospikeInstanceSummary struct {
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

// AerospikePodStatus contains the Aerospike specific status of the Aerospike serverpods.
// +k8s:openapi-gen=true
type AerospikePodStatus struct {
	// Image is the Aerospike image this pod is running.
	Image string `json:"image"`
	// PodIP in the K8s network.
	PodIP string `json:"podIP"`
	// HostInternalIP of the K8s host this pod is scheduled on.
	HostInternalIP string `json:"hostInternalIP,omitempty"`
	// HostExternalIP of the K8s host this pod is scheduled on.
	HostExternalIP string `json:"hostExternalIP,omitempty"`
	// PodPort is the port K8s intenral Aerospike clients can connect to.
	PodPort int `json:"podPort"`
	// ServicePort is the port Aerospike clients outside K8s can connect to.
	ServicePort int32 `json:"servicePort"`

	// Aerospike server instance summary for this pod.
	Aerospike AerospikeInstanceSummary `json:"aerospike,omitempty"`

	// InitializedVolumePaths is the list of device path that have already been initialized for main container/initContianer.
	InitializedVolumePaths []string `json:"initializedVolumePaths"`

	// AerospikeConfigHash is ripemd160 hash of aerospikeConfig used by this pod
	AerospikeConfigHash string `json:"aerospikeConfigHash"`

	// NetworkPolicyHash is ripemd160 hash of NetworkPolicy used by this pod
	NetworkPolicyHash string `json:"networkPolicyHash"`

	// PodSpecHash is ripemd160 hash of PodSpec used by this pod
	PodSpecHash string `json:"podSpecHash"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AerospikeCluster is the Schema for the aerospikeclusters API
type AerospikeCluster struct {
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

// Deepcopy

// DeepCopy implements deepcopy func for RackConfig
func (v *RackConfig) DeepCopy() *RackConfig {
	src := *v
	var dst = RackConfig{Racks: []Rack{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for Rack
func (v *Rack) DeepCopy() *Rack {
	src := *v
	var dst = Rack{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for ValidationPolicy.
func (v *ValidationPolicySpec) DeepCopy() *ValidationPolicySpec {
	src := *v
	var dst = ValidationPolicySpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeRoleSpec
func (v *AerospikeRoleSpec) DeepCopy() *AerospikeRoleSpec {
	src := *v
	var dst = AerospikeRoleSpec{Privileges: []string{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeUserSpec
func (v *AerospikeUserSpec) DeepCopy() *AerospikeUserSpec {
	src := *v
	var dst = AerospikeUserSpec{Roles: []string{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeClientAdminPolicy
func (v *AerospikeClientAdminPolicy) DeepCopy() *AerospikeClientAdminPolicy {
	src := *v
	var dst = AerospikeClientAdminPolicy{Timeout: 2000}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeAccessControlSpec
func (v *AerospikeAccessControlSpec) DeepCopy() *AerospikeAccessControlSpec {
	src := *v
	var dst = AerospikeAccessControlSpec{Roles: []AerospikeRoleSpec{}, Users: []AerospikeUserSpec{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikePersistentVolumePolicySpec.
func (v *AerospikePersistentVolumePolicySpec) DeepCopy() *AerospikePersistentVolumePolicySpec {
	src := *v
	var dst = AerospikePersistentVolumePolicySpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikePersistentVolumeSpec.
func (v *VolumeSpec) DeepCopy() *VolumeSpec {
	src := *v
	var dst = VolumeSpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeStorageSpec.
func (v *AerospikeStorageSpec) DeepCopy() *AerospikeStorageSpec {
	src := *v
	var dst = AerospikeStorageSpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeNetworkpolicy
func (v *AerospikeNetworkPolicy) DeepCopy() *AerospikeNetworkPolicy {
	src := *v
	var dst = AerospikeNetworkPolicy{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikeInstanceSummary
func (v *AerospikeInstanceSummary) DeepCopy() *AerospikeInstanceSummary {
	src := *v
	var dst = AerospikeInstanceSummary{}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for AerospikePodStatus
func (v *AerospikePodStatus) DeepCopy() *AerospikePodStatus {
	src := *v
	var dst = AerospikePodStatus{}
	lib.DeepCopy(dst, src)
	return &dst
}

// CopySpecToStatus copy spec in status. Spec to Status DeepCopy doesn't work. It fails in reflect lib.
func CopySpecToStatus(spec AerospikeClusterSpec) (*AerospikeClusterStatusSpec, error) {

	status := AerospikeClusterStatusSpec{}

	status.Size = spec.Size
	status.Image = spec.Image

	// Storage
	statusStorage := AerospikeStorageSpec{}
	if err := lib.DeepCopy(&statusStorage, &spec.Storage); err != nil {
		return nil, err
	}
	status.Storage = statusStorage

	if spec.AerospikeAccessControl != nil {
		// AerospikeAccessControl
		statusAerospikeAccessControl := &AerospikeAccessControlSpec{}
		if err := lib.DeepCopy(statusAerospikeAccessControl, spec.AerospikeAccessControl); err != nil {
			return nil, err
		}
		status.AerospikeAccessControl = statusAerospikeAccessControl
	}

	// AerospikeConfig
	statusAerospikeConfig := &AerospikeConfigSpec{}
	if err := lib.DeepCopy(statusAerospikeConfig, spec.AerospikeConfig); err != nil {
		return nil, err
	}
	status.AerospikeConfig = statusAerospikeConfig

	if spec.Resources != nil {
		// Resources
		statusResources := &corev1.ResourceRequirements{}
		if err := lib.DeepCopy(statusResources, spec.Resources); err != nil {
			return nil, err
		}
		status.Resources = statusResources
	}

	if spec.ValidationPolicy != nil {
		// ValidationPolicy
		statusValidationPolicy := &ValidationPolicySpec{}
		if err := lib.DeepCopy(statusValidationPolicy, spec.ValidationPolicy); err != nil {
			return nil, err
		}
		status.ValidationPolicy = statusValidationPolicy
	}

	// RackConfig
	statusRackConfig := RackConfig{}
	if err := lib.DeepCopy(&statusRackConfig, &spec.RackConfig); err != nil {
		return nil, err
	}
	status.RackConfig = statusRackConfig

	// AerospikeNetworkPolicy
	statusAerospikeNetworkPolicy := AerospikeNetworkPolicy{}
	if err := lib.DeepCopy(&statusAerospikeNetworkPolicy, &spec.AerospikeNetworkPolicy); err != nil {
		return nil, err
	}
	status.AerospikeNetworkPolicy = statusAerospikeNetworkPolicy

	if spec.OperatorClientCertSpec != nil {
		clientCertSpec := &AerospikeOperatorClientCertSpec{}
		if err := lib.DeepCopy(clientCertSpec, spec.OperatorClientCertSpec); err != nil {
			return nil, err
		}
		status.OperatorClientCertSpec = clientCertSpec
	}

	// Storage
	statusPodSpec := AerospikePodSpec{}
	if err := lib.DeepCopy(&statusPodSpec, &spec.PodSpec); err != nil {
		return nil, err
	}
	status.PodSpec = statusPodSpec

	return &status, nil
}

// CopyStatusToSpec copy status in spec. Status to Spec DeepCopy doesn't work. It fails in reflect lib.
func CopyStatusToSpec(status AerospikeClusterStatusSpec) (*AerospikeClusterSpec, error) {

	spec := AerospikeClusterSpec{}

	spec.Size = status.Size
	spec.Image = status.Image

	// Storage
	specStorage := AerospikeStorageSpec{}
	if err := lib.DeepCopy(&specStorage, &status.Storage); err != nil {
		return nil, err
	}
	spec.Storage = specStorage

	if status.AerospikeAccessControl != nil {
		// AerospikeAccessControl
		specAerospikeAccessControl := &AerospikeAccessControlSpec{}
		if err := lib.DeepCopy(specAerospikeAccessControl, status.AerospikeAccessControl); err != nil {
			return nil, err
		}
		spec.AerospikeAccessControl = specAerospikeAccessControl
	}

	// AerospikeConfig
	specAerospikeConfig := &AerospikeConfigSpec{}
	if err := lib.DeepCopy(specAerospikeConfig, status.AerospikeConfig); err != nil {
		return nil, err
	}
	spec.AerospikeConfig = specAerospikeConfig

	if status.Resources != nil {
		// Resources
		specResources := &corev1.ResourceRequirements{}
		if err := lib.DeepCopy(specResources, status.Resources); err != nil {
			return nil, err
		}
		spec.Resources = specResources
	}

	if status.ValidationPolicy != nil {
		// ValidationPolicy
		specValidationPolicy := &ValidationPolicySpec{}
		if err := lib.DeepCopy(specValidationPolicy, status.ValidationPolicy); err != nil {
			return nil, err
		}
		spec.ValidationPolicy = specValidationPolicy
	}

	// RackConfig
	specRackConfig := RackConfig{}
	if err := lib.DeepCopy(&specRackConfig, &status.RackConfig); err != nil {
		return nil, err
	}
	spec.RackConfig = specRackConfig

	// AerospikeNetworkPolicy
	specAerospikeNetworkPolicy := AerospikeNetworkPolicy{}
	if err := lib.DeepCopy(&specAerospikeNetworkPolicy, &status.AerospikeNetworkPolicy); err != nil {
		return nil, err
	}
	spec.AerospikeNetworkPolicy = specAerospikeNetworkPolicy

	if status.OperatorClientCertSpec != nil {
		clientCertSpec := &AerospikeOperatorClientCertSpec{}
		if err := lib.DeepCopy(clientCertSpec, status.OperatorClientCertSpec); err != nil {
			return nil, err
		}
		spec.OperatorClientCertSpec = clientCertSpec
	}

	// Storage
	specPodSpec := AerospikePodSpec{}
	if err := lib.DeepCopy(&specPodSpec, &status.PodSpec); err != nil {
		return nil, err
	}
	spec.PodSpec = specPodSpec

	return &spec, nil
}
