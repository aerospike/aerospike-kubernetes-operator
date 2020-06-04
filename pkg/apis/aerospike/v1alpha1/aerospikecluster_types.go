package v1alpha1

import (
	lib "github.com/aerospike/aerospike-management-lib"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AerospikeClusterSpec defines the desired state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// Aerospike cluster size
	Size int32 `json:"size"`
	// Aerospike cluster build
	Build string `json:"build"`
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
	// BlockStorage has block storage info.
	BlockStorage []BlockStorageSpec `json:"blockStorage,omitempty"`
	// FileStorage has filesystem storage info.
	FileStorage []FileStorageSpec `json:"fileStorage,omitempty"`
	// AerospikeConfigSecret has secret info created by user. User needs to create this secret having tls files, feature key for cluster
	AerospikeConfigSecret AerospikeConfigSecretSpec `json:"aerospikeConfigSecret,omitempty"`
	// AerospikeAuthSecret has secret info created by user. User needs to create this secret from password literal
	// AerospikeAccessControl has the Aerospike roles and users. Required if aerospike cluster security is enabled.
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default
	AerospikeConfig Values `json:"aerospikeConfig"`
	// Define resources requests and limits for Aerospike Server Container. Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources"`
}

// AerospikeRoleSpec specifies an Aerospike database role and its associated privileges.
type AerospikeRoleSpec struct {
	Privileges []string `json:"privileges"`
	Whitelist  []string `json:"whitelist,omitempty"`
}

// DeepCopy implements deepcopy func for AerospikeRoleSpec
func (v *AerospikeRoleSpec) DeepCopy() *AerospikeRoleSpec {
	src := *v
	var dst = AerospikeRoleSpec{Privileges: []string{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// AerospikeUserSpec specifies an Aerospike database user, the secret name for the password and, associated roles.
type AerospikeUserSpec struct {
	// SecretName has secret info created by user. User needs to create this secret from password literal.
	// eg: kubectl create secret generic dev-db-secret --from-literal=password='password'
	SecretName string   `json:"secretName"`
	Roles      []string `json:"roles"`
}

// DeepCopy implements deepcopy func for AerospikeUserSpec
func (v *AerospikeUserSpec) DeepCopy() *AerospikeUserSpec {
	src := *v
	var dst = AerospikeUserSpec{Roles: []string{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// AerospikeClientAdminPolicy specify the aerospike client admin policy for access control operations.
type AerospikeClientAdminPolicy struct {
	// Timeout for admin client policy in milliseconds.
	Timeout int `json:"timeout"`
}

// DeepCopy implements deepcopy func for AerospikeClientAdminPolicy
func (v *AerospikeClientAdminPolicy) DeepCopy() *AerospikeClientAdminPolicy {
	src := *v
	var dst = AerospikeClientAdminPolicy{Timeout: 2000}
	lib.DeepCopy(dst, src)
	return &dst
}

// AerspikeAccessControlSpec specifies the roles and users to setup on the database fo access control.
type AerospikeAccessControlSpec struct {
	AdminPolicy *AerospikeClientAdminPolicy  `json:"adminPolicy,omitempty"`
	Roles       map[string]AerospikeRoleSpec `json:"roles,omitempty"`
	Users       map[string]AerospikeUserSpec `json:"users"`
}

// DeepCopy implements deepcopy func for AerospikeAccessControlSpec
func (v *AerospikeAccessControlSpec) DeepCopy() *AerospikeAccessControlSpec {
	src := *v
	var dst = AerospikeAccessControlSpec{Roles: map[string]AerospikeRoleSpec{}, Users: map[string]AerospikeUserSpec{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// AerospikeConfigSecretSpec has secret info created by user. User need to create secret having tls files, feature key for cluster
type AerospikeConfigSecretSpec struct {
	SecretName string `json:"secretName"`
	MountPath  string `json:"mountPath"`
}

// DeepCopy implements deepcopy func for Values
func (v *AerospikeConfigSecretSpec) DeepCopy() *AerospikeConfigSecretSpec {
	src := *v
	var dst = AerospikeConfigSecretSpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// BlockStorageSpec has storage info. StorageClass for devices
// +k8s:openapi-gen=true
type BlockStorageSpec struct {
	// StorageClass should be created by user
	StorageClass string `json:"storageClass"`
	// VolumeDevices is the list of block devices to be used by the container. Devices should be provisioned by user
	VolumeDevices []VolumeDevice `json:"volumeDevices"`
}

// VolumeDevice is device to be used by the container
// +k8s:openapi-gen=true
type VolumeDevice struct {
	// DevicePath is the path inside of the container that the device will be mapped to
	DevicePath string `json:"devicePath"`
	// SizeInGB Size of device in GB
	SizeInGB int32 `json:"sizeInGB"`
}

// DeepCopy implements deepcopy func for Values
func (v *BlockStorageSpec) DeepCopy() *BlockStorageSpec {
	src := *v
	var dst = BlockStorageSpec{VolumeDevices: []VolumeDevice{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for Values
func (v *VolumeDevice) DeepCopy() *VolumeDevice {
	src := *v
	var dst = VolumeDevice{}
	lib.DeepCopy(dst, src)
	return &dst
}

// FileStorageSpec has storage info. StorageClass for fileSystems
// +k8s:openapi-gen=true
type FileStorageSpec struct {
	// StorageClass should be created by user
	StorageClass string `json:"storageClass"`
	// Pod volumes to mount into the container's filesystem.
	VolumeMounts []VolumeMount `json:"volumeMounts"`
}

// VolumeMount is Pod volume to mount into the container's filesystem
// +k8s:openapi-gen=true
type VolumeMount struct {
	// Path within the container at which the volume should be mounted
	MountPath string `json:"mountPath"`
	// SizeInGB Size of mount volume
	SizeInGB int32 `json:"sizeInGB"`
}

// DeepCopy implements deepcopy func for Values
func (v *FileStorageSpec) DeepCopy() *FileStorageSpec {
	src := *v
	var dst = FileStorageSpec{VolumeMounts: []VolumeMount{}}
	lib.DeepCopy(dst, src)
	return &dst
}

// DeepCopy implements deepcopy func for Values
func (v *VolumeMount) DeepCopy() *VolumeMount {
	src := *v
	var dst = VolumeMount{}
	lib.DeepCopy(dst, src)
	return &dst
}

// Values used to take unstructured config
type Values map[string]interface{}

// DeepCopy implements deepcopy func for Values
func (v *Values) DeepCopy() *Values {
	src := *v
	var dst = make(Values)
	lib.DeepCopy(dst, src)
	return &dst
}

// AerospikeClusterStatus defines the observed state of AerospikeCluster
// +k8s:openapi-gen=true
type AerospikeClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// The current state of Aerospike cluster.
	AerospikeClusterSpec
	// Nodes tells the observed state of AerospikeClusterNodes
	Nodes []AerospikeNodeSummary `json:"nodes"`
	// Details about the current condition of the AerospikeCluster resource.
	//Conditions []apiextensions.CustomResourceDefinitionCondition `json:"conditions"`

	// TODO:
	// Give asadm info
	// Give pod specific summary
	// Give service list, to be used by client
	// Error status
}

// AerospikeNodeSummary defines the observed state of AerospikeClusterNode
// +k8s:openapi-gen=true
type AerospikeNodeSummary struct {
	ClusterName string `json:"clusterName"`
	NodeID      string `json:"nodeID"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	TLSName     string `json:"tlsname"`
	Build       string `json:"build"`
}

// DeepCopy implements deepcopy func for AerospikeNodeSummary
func (v *AerospikeNodeSummary) DeepCopy() *AerospikeNodeSummary {
	src := *v
	var dst = AerospikeNodeSummary{}
	lib.DeepCopy(dst, src)
	return &dst
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AerospikeCluster is the Schema for the aerospikeclusters API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=aerospikeclusters,scope=Namespaced
type AerospikeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AerospikeClusterSpec   `json:"spec,omitempty"`
	Status AerospikeClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AerospikeClusterList contains a list of AerospikeCluster
type AerospikeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AerospikeCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AerospikeCluster{}, &AerospikeClusterList{})
}
