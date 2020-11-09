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
							Description: "Storage specify persistent storage to use for the Aerospike pods.",
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
							Description: "RackConfig Configures the operator to deploy rack aware Aerospike cluster. Pods will be deployed in given racks based on given configuration",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.RackConfig"),
						},
					},
					"aerospikeNetworkPolicy": {
						SchemaProps: spec.SchemaProps{
							Description: "AerospikeNetworkPolicy specifies how clients and tools access the Aerospike cluster.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeNetworkPolicy"),
						},
					},
					"podSpec": {
						SchemaProps: spec.SchemaProps{
							Description: "Additional configuration for create Aerospike pods.",
							Ref:         ref("github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePodSpec"),
						},
					},
				},
				Required: []string{"size", "build", "aerospikeConfig", "resources"},
			},
		},
		Dependencies: []string{
			"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeAccessControlSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeConfigSecretSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeNetworkPolicy", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikePodSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.AerospikeStorageSpec", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.RackConfig", "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1.ValidationPolicySpec", "k8s.io/api/core/v1.ResourceRequirements"},
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
							Description: "PodName is the K8s pod name.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"podIP": {
						SchemaProps: spec.SchemaProps{
							Description: "PodIP in the K8s network.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"hostInternalIP": {
						SchemaProps: spec.SchemaProps{
							Description: "HostInternalIP of the K8s host this pod is scheduled on.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"hostExternalIP": {
						SchemaProps: spec.SchemaProps{
							Description: "HostExternalIP of the K8s host this pod is scheduled on.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"podPort": {
						SchemaProps: spec.SchemaProps{
							Description: "PodPort is the port K8s intenral Aerospike clients can connect to.",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"servicePort": {
						SchemaProps: spec.SchemaProps{
							Description: "ServicePort is the port Aerospike clients outside K8s can connect to.",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"clusterName": {
						SchemaProps: spec.SchemaProps{
							Description: "ClusterName is the name of the Aerospike cluster this pod belongs to.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"nodeID": {
						SchemaProps: spec.SchemaProps{
							Description: "NodeID is the unique Aerospike ID for this pod.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"build": {
						SchemaProps: spec.SchemaProps{
							Description: "Build is the Aerospike build this pod is running.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"rackID": {
						SchemaProps: spec.SchemaProps{
							Description: "RackID of rack to which this node belongs",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"tlsname": {
						SchemaProps: spec.SchemaProps{
							Description: "TLSName is the TLS name of this pod in the Aerospike cluster.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"accessEndpoints": {
						SchemaProps: spec.SchemaProps{
							Description: "AccessEndpoints are the access endpoints for this pod.",
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
					"alternateAccessEndpoints": {
						SchemaProps: spec.SchemaProps{
							Description: "AlternateAccessEndpoints are the alternate access endpoints for this pod.",
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
					"tlsAccessEndpoints": {
						SchemaProps: spec.SchemaProps{
							Description: "TLSAccessEndpoints are the TLS access endpoints for this pod.",
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
					"tlsAlternateAccessEndpoints": {
						SchemaProps: spec.SchemaProps{
							Description: "TLSAlternateAccessEndpoints are the alternate TLS access endpoints for this pod.",
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
				Required: []string{"podName", "podIP", "podPort", "servicePort", "clusterName", "nodeID", "build", "rackID"},
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
					"initMethod": {
						SchemaProps: spec.SchemaProps{
							Description: "InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to \"none\".",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"cascadeDelete": {
						SchemaProps: spec.SchemaProps{
							Description: "CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"effectiveInitMethod": {
						SchemaProps: spec.SchemaProps{
							Description: "Effective/operative value to use as the volume init method after applying defaults.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"effectiveCascadeDelete": {
						SchemaProps: spec.SchemaProps{
							Description: "Effective/operative value to use for cascade delete after applying defaults.",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"path": {
						SchemaProps: spec.SchemaProps{
							Description: "Path is the device path where block 'block' mode volumes are attached to the pod or the mount path for 'filesystem' mode.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"configMap": {
						SchemaProps: spec.SchemaProps{
							Description: "Name of the configmap for 'configmap' mode volumes.",
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
				Required: []string{"effectiveCascadeDelete", "path", "storageClass", "volumeMode", "sizeInGB"},
			},
		},
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
							Description: "Volumes list to attach to created pods.",
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
