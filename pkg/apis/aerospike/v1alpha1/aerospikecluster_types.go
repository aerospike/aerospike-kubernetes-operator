package v1alpha1

import (
	"fmt"

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
	// Storage specified persistent storage to use for the Aerospike pods.
	Storage AerospikeStorageSpec `json:"storage,omitempty"`
	// AerospikeConfigSecret has secret info created by user. User needs to create this secret having tls files, feature key for cluster
	AerospikeConfigSecret AerospikeConfigSecretSpec `json:"aerospikeConfigSecret,omitempty"`
	// AerospikeAccessControl has the Aerospike roles and users definitions. Required if aerospike cluster security is enabled.
	AerospikeAccessControl *AerospikeAccessControlSpec `json:"aerospikeAccessControl,omitempty"`
	// AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default
	AerospikeConfig Values `json:"aerospikeConfig"`
	// Define resources requests and limits for Aerospike Server Container. Please contact aerospike for proper sizing exercise
	// Only Memory and Cpu resources can be given
	// Resources.Limits should be more than Resources.Requests.
	Resources *corev1.ResourceRequirements `json:"resources"`
	// ValidationPolicy controls validation of the Aerospike cluster resource.
	ValidationPolicy *ValidationPolicySpec `json:"validationPolicy,omitempty"`
	// RackConfig
	RackConfig RackConfig `json:"rackConfig,omitempty"`
}

// rackConfig:
//   policy:
//     - region
//     - zone
//   racks:
//     - id: 0
//       region: us-east1
//       zone: use-east1-1a
//     - id: 2
//       region: us-east1
//       zone: use-east1-1b

// RackConfig specifies all racks and related policies
type RackConfig struct {
	RackPolicy []RackPolicy `json:"rackPolicy"`
	Racks      []Rack       `json:"racks"`
}

// RackPolicy controls how racks are identified.
type RackPolicy string

const (
	// Zone for creating pods.
	Zone      RackPolicy = "zone"
	// Region for creating pods.
	Region    RackPolicy = "region"
	// RackLabel for creating pods.
	RackLabel RackPolicy = "rackLabel"
	// NodeName for creating pods.
	NodeName  RackPolicy = "nodeName"
)

// Rack specifies single rack config
type Rack struct {
	ID     int    `json:"ID"`
	Zone   string `json:"zone,omitempty"`
	Region string `json:"region,omitempty"`
	// Node should have a label {key:RackLabel, value:<RackLable>}
	RackLabel string `json:"rackLabel,omitempty"`
	NodeName  string `json:"nodeName,omitempty"`
}

// DeepCopy implements deepcopy func for RackConfig
func (v *RackConfig) DeepCopy() *RackConfig {
	src := *v
	var dst = RackConfig{Racks: []Rack{}, RackPolicy: []RackPolicy{}}
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

// ValidationPolicySpec controls validation of the Aerospike cluster resource.
type ValidationPolicySpec struct {
	// skipWorkDirValidate validates that Aerospike work directory is mounted on a persistent file storage. Defaults to false.
	SkipWorkDirValidate bool `json:"skipWorkDirValidate"`

	// ValidateXdrDigestLogFile validates that xdr digest log file is mounted on a persistent file storage. Defaults to false.
	SkipXdrDlogFileValidate bool `json:"skipXdrDlogFileValidate"`
}

// DeepCopy implements deepcopy func for ValidationPolicy.
func (v *ValidationPolicySpec) DeepCopy() *ValidationPolicySpec {
	src := *v
	var dst = ValidationPolicySpec{}
	lib.DeepCopy(dst, src)
	return &dst
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

// DeepCopy implements deepcopy func for AerospikeRoleSpec
func (v *AerospikeRoleSpec) DeepCopy() *AerospikeRoleSpec {
	src := *v
	var dst = AerospikeRoleSpec{Privileges: []string{}}
	lib.DeepCopy(dst, src)
	return &dst
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

// DeepCopy implements deepcopy func for AerospikeAccessControlSpec
func (v *AerospikeAccessControlSpec) DeepCopy() *AerospikeAccessControlSpec {
	src := *v
	var dst = AerospikeAccessControlSpec{Roles: []AerospikeRoleSpec{}, Users: []AerospikeUserSpec{}}
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

// AerospikeVolumeMode specifies if the volume is a block/raw or filesystem.
// +kubebuilder:validation:Enum=filesystem;block
// +k8s:openapi-gen=true
type AerospikeVolumeMode string

const (
	// AerospikeVolumeModeFilesystem specifies a volume that has a filesystem.
	AerospikeVolumeModeFilesystem AerospikeVolumeMode = "filesystem"

	// AerospikeVolumeModeBlock specifies the volume is a block/raw device.
	AerospikeVolumeModeBlock AerospikeVolumeMode = "block"
)

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
	InitMethod *AerospikeVolumeInitMethod `json:"initMethod"`

	// CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster. Defaults to true.
	CascadeDelete *bool `json:"cascadeDelete"`
}

// SetDefaults applies default values to unset fields of the policy using corresponding fields from defaultPolicy
func (v *AerospikePersistentVolumePolicySpec) SetDefaults(defaultPolicy *AerospikePersistentVolumePolicySpec) {
	if v.InitMethod == nil {
		v.InitMethod = defaultPolicy.InitMethod
	}

	if v.CascadeDelete == nil {
		v.CascadeDelete = defaultPolicy.CascadeDelete
	}
}

// DeepCopy implements deepcopy func for AerospikePersistentVolumePolicySpec.
func (v *AerospikePersistentVolumePolicySpec) DeepCopy() *AerospikePersistentVolumePolicySpec {
	src := *v
	var dst = AerospikePersistentVolumePolicySpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// AerospikePersistentVolumeSpec describes a persistent volume to claim and attach to Aerospike pods.
// +k8s:openapi-gen=true
type AerospikePersistentVolumeSpec struct {
	// Contains  policies for this volumes.
	AerospikePersistentVolumePolicySpec

	// Path is the device path where block 'block' mode volumes are attached to the pod or the mount path for 'filesystem' mode.
	Path string `json:"path"`

	// StorageClass should be pre-created by user.
	StorageClass string `json:"storageClass"`

	// VolumeMode specifies if the volume is block/raw or a filesystem.
	VolumeMode AerospikeVolumeMode `json:"volumeMode"`

	// SizeInGB Size of volume in GB.
	SizeInGB int32 `json:"sizeInGB"`
}

// DeepCopy implements deepcopy func for AerospikePersistentVolumeSpec.
func (v *AerospikePersistentVolumeSpec) DeepCopy() *AerospikePersistentVolumeSpec {
	src := *v
	var dst = AerospikePersistentVolumeSpec{}
	lib.DeepCopy(dst, src)
	return &dst
}

// IsSafeChange indicates if a change to a volume is safe to allow.
func (v *AerospikePersistentVolumeSpec) IsSafeChange(new AerospikePersistentVolumeSpec) bool {
	return v.Path == new.Path && v.StorageClass == new.StorageClass && v.VolumeMode == new.VolumeMode && v.SizeInGB == new.SizeInGB
}

// AerospikeStorageSpec lists persistent volumes to claim and attach to Aerospike pods and persistence policies.
// +k8s:openapi-gen=true
type AerospikeStorageSpec struct {
	// FileSystemVolumePolicy contains default policies for filesystem volumes.
	FileSystemVolumePolicy AerospikePersistentVolumePolicySpec `json:"filesystemVolumePolicy,omitempty"`

	// BlockVolumePolicy contains default policies for block volumes.
	BlockVolumePolicy AerospikePersistentVolumePolicySpec `json:"blockVolumePolicy,omitempty"`

	// Volumes is the list of to attach to created pods.
	// +patchMergeKey=path
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=path
	Volumes []AerospikePersistentVolumeSpec `json:"volumes,omitempty" patchStrategy:"merge" patchMergeKey:"path"`
}

// ValidateStorageSpecChange indicates if a change to to storage spec is safe to apply.
func (v *AerospikeStorageSpec) ValidateStorageSpecChange(new AerospikeStorageSpec) error {
	if len(new.Volumes) != len(v.Volumes) {
		return fmt.Errorf("Cannot add or remove storage volumes dynamically")
	}

	for i, newVolume := range new.Volumes {
		oldVolume := v.Volumes[i]
		if !oldVolume.IsSafeChange(newVolume) {
			return fmt.Errorf("Cannot change volumes old: %v new %v", oldVolume, newVolume)
		}
	}

	return nil
}

// SetDefaults sets default values for storage spec fields.
func (v *AerospikeStorageSpec) SetDefaults() {
	defaultFilesystemInitMethod := AerospikeVolumeInitMethodDeleteFiles
	defaultBlockInitMethod := AerospikeVolumeInitMethodNone
	defaultCascadeDelete := true

	// Set storage level defaults.
	v.FileSystemVolumePolicy.SetDefaults(&AerospikePersistentVolumePolicySpec{InitMethod: &defaultFilesystemInitMethod, CascadeDelete: &defaultCascadeDelete})
	v.BlockVolumePolicy.SetDefaults(&AerospikePersistentVolumePolicySpec{InitMethod: &defaultBlockInitMethod, CascadeDelete: &defaultCascadeDelete})

	for i := range v.Volumes {
		// Use storage spec values as defaults for the volumes.
		if v.Volumes[i].VolumeMode == AerospikeVolumeModeBlock {
			v.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&v.BlockVolumePolicy)
		} else {
			v.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&v.FileSystemVolumePolicy)
		}
	}
}

// DeepCopy implements deepcopy func for AerospikeStorageSpec.
func (v *AerospikeStorageSpec) DeepCopy() *AerospikeStorageSpec {
	src := *v
	var dst = AerospikeStorageSpec{}
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

	// Nodes contains Aerospike node specific  state of cluster.
	Nodes []AerospikeNodeSummary `json:"nodes"`

	// Details about the current condition of the AerospikeCluster resource.
	//Conditions []apiextensions.CustomResourceDefinitionCondition `json:"conditions"`

	// PodStatus has Aerospike specific status of the pods. This is map instead of the conventional map as list convention to allow each pod to patch update its status. The map key is the name of the pod.
	// +patchStrategy=strategic
	PodStatus map[string]AerospikePodStatus `json:"podStatus" patchStrategy:"strategic"`

	// TODO:
	// Give asadm info
	// Give pod specific summary
	// Give service list, to be used by client
	// Error status
}

// AerospikeNodeSummary defines the observed state of AerospikeClusterNode
// +k8s:openapi-gen=true
type AerospikeNodeSummary struct {
	PodName     string `json:"podName"`
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

// AerospikePodStatus contains the Aerospike specific status of the Aerospike serverpods.
// +k8s:openapi-gen=true
type AerospikePodStatus struct {
	// InitializedVolumePaths is the list of device path that have already been initialized.
	InitializedVolumePaths []string `json:"initializedVolumePaths"`
}

// DeepCopy implements deepcopy func for AerospikePodStatus
func (v *AerospikePodStatus) DeepCopy() *AerospikePodStatus {
	src := *v
	var dst = AerospikePodStatus{}
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
