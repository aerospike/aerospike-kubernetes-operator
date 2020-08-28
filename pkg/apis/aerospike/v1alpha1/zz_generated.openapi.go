// +build !ignore_autogenerated

// This file was autogenerated by openapi-gen. Do not edit it manually!

package v1alpha1

import (
	spec "github.com/go-openapi/spec"
	common "k8s.io/kube-openapi/pkg/common"
)

func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeCluster":              schema_pkg_apis_aerospike_v1alpha1_AerospikeCluster(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterSpec":          schema_pkg_apis_aerospike_v1alpha1_AerospikeClusterSpec(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterStatus":        schema_pkg_apis_aerospike_v1alpha1_AerospikeClusterStatus(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeNodeSummary":          schema_pkg_apis_aerospike_v1alpha1_AerospikeNodeSummary(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumeSpec": schema_pkg_apis_aerospike_v1alpha1_AerospikePersistentVolumeSpec(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePodStatus":            schema_pkg_apis_aerospike_v1alpha1_AerospikePodStatus(ref),
		"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeStorageSpec":          schema_pkg_apis_aerospike_v1alpha1_AerospikeStorageSpec(ref),
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikeCluster(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikeCluster is the Schema for the aerospikeclusters API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikeClusterSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikeClusterSpec defines the desired state of AerospikeCluster",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"size": {
						SchemaProps: spec.SchemaProps{
							Description: "Aerospike cluster size",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"build": {
						SchemaProps: spec.SchemaProps{
							Description: "Aerospike cluster build",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"multiPodPerHost": {
						SchemaProps: spec.SchemaProps{
							Description: "If set true then multiple pods can be created per Kubernetes Node. This will create a NodePort service for each Pod. NodePort, as the name implies, opens a specific port on all the Kubernetes Nodes , and any traffic that is sent to this port is forwarded to the service. Here service picks a random port in range (30000-32767), so these port should be open.\n\nIf set false then only single pod can be created per Kubernetes Node. This will create Pods using hostPort setting. The container port will be exposed to the external network at <hostIP>:<hostPort>, where the hostIP is the IP address of the Kubernetes Node where the container is running and the hostPort is the port requested by the user.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"storage": {
						SchemaProps: spec.SchemaProps{
							Description: "Storage specified persistent storage to use for the Aerospike pods.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeStorageSpec"),
						},
					},
					"aerospikeConfigSecret": {
						SchemaProps: spec.SchemaProps{
							Description: "AerospikeConfigSecret has secret info created by user. User needs to create this secret having tls files, feature key for cluster",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeConfigSecretSpec"),
						},
					},
					"aerospikeAccessControl": {
						SchemaProps: spec.SchemaProps{
							Description: "AerospikeAccessControl has the Aerospike roles and users definitions. Required if aerospike cluster security is enabled.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeAccessControlSpec"),
						},
					},
					"aerospikeConfig": {
						SchemaProps: spec.SchemaProps{
							Description: "AerospikeConfig sets config in aerospike.conf file. Other configs are taken as default",
							Type:        []string{"object"},
							AdditionalProperties: &spec.SchemaOrBool{
								Allows: true,
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Type:   []string{"object"},
										Format: "",
									},
								},
							},
						},
					},
					"resources": {
						SchemaProps: spec.SchemaProps{
							Description: "Define resources requests and limits for Aerospike Server Container. Please contact aerospike for proper sizing exercise Only Memory and Cpu resources can be given Resources.Limits should be more than Resources.Requests.",
							Ref:         ref("k8s.io/api/core/v1.ResourceRequirements"),
						},
					},
					"validationPolicy": {
						SchemaProps: spec.SchemaProps{
							Description: "ValidationPolicy controls validation of the Aerospike cluster resource.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.ValidationPolicySpec"),
						},
					},
					"rackConfig": {
						SchemaProps: spec.SchemaProps{
							Description: "RackConfig",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.RackConfig"),
						},
					},
				},
				Required: []string{"size", "build", "aerospikeConfig", "resources"},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeAccessControlSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeConfigSecretSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeStorageSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.RackConfig", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.ValidationPolicySpec", "k8s.io/api/core/v1.ResourceRequirements"},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikeClusterStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikeClusterStatus defines the observed state of AerospikeCluster",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"AerospikeClusterSpec": {
						SchemaProps: spec.SchemaProps{
							Description: "The current state of Aerospike cluster.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterSpec"),
						},
					},
					"nodes": {
						SchemaProps: spec.SchemaProps{
							Description: "Nodes contains Aerospike node specific  state of cluster.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeNodeSummary"),
									},
								},
							},
						},
					},
					"podStatus": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-patch-strategy": "strategic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "PodStatus has Aerospike specific status of the pods. This is map instead of the conventional map as list convention to allow each pod to patch update its status. The map key is the name of the pod.",
							Type:        []string{"object"},
							AdditionalProperties: &spec.SchemaOrBool{
								Allows: true,
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePodStatus"),
									},
								},
							},
						},
					},
				},
				Required: []string{"AerospikeClusterSpec", "nodes", "podStatus"},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeClusterSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeNodeSummary", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePodStatus"},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikeNodeSummary(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikeNodeSummary defines the observed state of AerospikeClusterNode",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"podName": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"ip": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"port": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
					"tlsname": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"clusterName": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"nodeID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"build": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"rackID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
				},
				Required: []string{"podName", "ip", "port", "tlsname", "clusterName", "nodeID", "build"},
			},
		},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikePersistentVolumeSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikePersistentVolumeSpec describes a persistent volume to claim and attach to Aerospike pods.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"AerospikePersistentVolumePolicySpec": {
						SchemaProps: spec.SchemaProps{
							Description: "Contains  policies for this volumes.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumePolicySpec"),
						},
					},
					"path": {
						SchemaProps: spec.SchemaProps{
							Description: "Path is the device path where block 'block' mode volumes are attached to the pod or the mount path for 'filesystem' mode.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"storageClass": {
						SchemaProps: spec.SchemaProps{
							Description: "StorageClass should be pre-created by user.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"volumeMode": {
						SchemaProps: spec.SchemaProps{
							Description: "VolumeMode specifies if the volume is block/raw or a filesystem.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"sizeInGB": {
						SchemaProps: spec.SchemaProps{
							Description: "SizeInGB Size of volume in GB.",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
				},
				Required: []string{"AerospikePersistentVolumePolicySpec", "path", "storageClass", "volumeMode", "sizeInGB"},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumePolicySpec"},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikePodStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikePodStatus contains the Aerospike specific status of the Aerospike serverpods.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"initializedVolumePaths": {
						SchemaProps: spec.SchemaProps{
							Description: "InitializedVolumePaths is the list of device path that have already been initialized.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Type:   []string{"string"},
										Format: "",
									},
								},
							},
						},
					},
				},
				Required: []string{"initializedVolumePaths"},
			},
		},
	}
}

func schema_pkg_apis_aerospike_v1alpha1_AerospikeStorageSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AerospikeStorageSpec lists persistent volumes to claim and attach to Aerospike pods and persistence policies.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"filesystemVolumePolicy": {
						SchemaProps: spec.SchemaProps{
							Description: "FileSystemVolumePolicy contains default policies for filesystem volumes.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumePolicySpec"),
						},
					},
					"blockVolumePolicy": {
						SchemaProps: spec.SchemaProps{
							Description: "BlockVolumePolicy contains default policies for block volumes.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumePolicySpec"),
						},
					},
					"volumes": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-map-keys":   "path",
								"x-kubernetes-list-type":       "map",
								"x-kubernetes-patch-merge-key": "path",
								"x-kubernetes-patch-strategy":  "merge",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "Volumes is the list of to attach to created pods.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumeSpec"),
									},
								},
							},
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumePolicySpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePersistentVolumeSpec"},
	}
}
