package e2e

import (
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// feature-key file needed
func createAerospikeClusterPost460(clusterName, namespace string, size int32, build string) *aerospikev1alpha1.AerospikeCluster {
	// create memcached custom resource
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: build,
			BlockStorage: []aerospikev1alpha1.BlockStorageSpec{
				aerospikev1alpha1.BlockStorageSpec{
					StorageClass: "ssd",
					VolumeDevices: []aerospikev1alpha1.VolumeDevice{
						aerospikev1alpha1.VolumeDevice{
							DevicePath: "/test/dev/xvdf",
							SizeInGB:   1,
						},
					},
				},
			},
			AerospikeAuthSecret: aerospikev1alpha1.AerospikeAuthSecretSpec{
				SecretName: authSecretName,
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			MultiPodPerHost: true,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: aerospikev1alpha1.Values{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
				},
				"security": map[string]interface{}{
					"enable-security": true,
				},
				"network": map[string]interface{}{
					"service": map[string]interface{}{
						"tls-name":                "bob-cluster-a",
						"tls-authenticate-client": "any",
					},
					"heartbeat": map[string]interface{}{
						"tls-name": "bob-cluster-b",
					},
					"fabric": map[string]interface{}{
						"tls-name": "bob-cluster-c",
					},
					"tls": []map[string]interface{}{
						map[string]interface{}{
							"name":      "bob-cluster-a",
							"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/svc_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
						map[string]interface{}{
							"name":      "bob-cluster-b",
							"cert-file": "/etc/aerospike/secret/hb_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/hb_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
						map[string]interface{}{
							"name":      "bob-cluster-c",
							"cert-file": "/etc/aerospike/secret/fb_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/fb_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
					},
				},
				"namespace": []interface{}{
					map[string]interface{}{
						"name":               "test",
						"memory-size":        1000955200,
						"replication-factor": 2,
						"storage-engine": map[string]interface{}{
							"device": []interface{}{"/test/dev/xvdf"},
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

func createDummyAerospikeCluster(clusterName, namespace string, size int32) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	// create memcached custom resource
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: latestClusterBuild,
			BlockStorage: []aerospikev1alpha1.BlockStorageSpec{
				aerospikev1alpha1.BlockStorageSpec{
					StorageClass: "ssd",
					VolumeDevices: []aerospikev1alpha1.VolumeDevice{
						aerospikev1alpha1.VolumeDevice{
							DevicePath: "/test/dev/xvdf",
							SizeInGB:   1,
						},
					},
				},
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			MultiPodPerHost: true,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: aerospikev1alpha1.Values{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
				},
				"security": map[string]interface{}{
					"enable-security": true,
				},
				"namespace": []interface{}{
					map[string]interface{}{
						"name":               "test",
						"memory-size":        1000955200,
						"replication-factor": 1,
						"storage-engine": map[string]interface{}{
							"device": []interface{}{"/test/dev/xvdf"},
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

// feature-key file needed
func createBasicTLSCluster(clusterName, namespace string, size int32) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	// create memcached custom resource
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: latestClusterBuild,
			AerospikeAuthSecret: aerospikev1alpha1.AerospikeAuthSecretSpec{
				SecretName: authSecretName,
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			MultiPodPerHost: true,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu,
					corev1.ResourceMemory: mem,
				},
			},
			AerospikeConfig: aerospikev1alpha1.Values{
				"service": map[string]interface{}{
					"feature-key-file": "/etc/aerospike/secret/features.conf",
				},
				"security": map[string]interface{}{
					"enable-security": true,
				},
				"network": map[string]interface{}{
					"service": map[string]interface{}{
						"tls-name":                "bob-cluster-a",
						"tls-authenticate-client": "any",
					},
					"heartbeat": map[string]interface{}{
						"tls-name": "bob-cluster-b",
					},
					"fabric": map[string]interface{}{
						"tls-name": "bob-cluster-c",
					},
					"tls": []map[string]interface{}{
						map[string]interface{}{
							"name":      "bob-cluster-a",
							"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/svc_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
						map[string]interface{}{
							"name":      "bob-cluster-b",
							"cert-file": "/etc/aerospike/secret/hb_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/hb_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
						map[string]interface{}{
							"name":      "bob-cluster-c",
							"cert-file": "/etc/aerospike/secret/fb_cluster_chain.pem",
							"key-file":  "/etc/aerospike/secret/fb_key.pem",
							"ca-file":   "/etc/aerospike/secret/cacert.pem",
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

func createSSDStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterName, namespace, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.BlockStorage = []aerospikev1alpha1.BlockStorageSpec{
		aerospikev1alpha1.BlockStorageSpec{
			StorageClass: "ssd",
			VolumeDevices: []aerospikev1alpha1.VolumeDevice{
				aerospikev1alpha1.VolumeDevice{
					DevicePath: "/test/dev/xvdf",
					SizeInGB:   1,
				},
			},
		},
	}
	aeroCluster.Spec.AerospikeConfig["namespace"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"device": []interface{}{"/test/dev/xvdf"},
			},
		},
	}
	return aeroCluster
}

func createHDDAndDataInMemStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterName, namespace, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.FileStorage = []aerospikev1alpha1.FileStorageSpec{
		aerospikev1alpha1.FileStorageSpec{
			StorageClass: "ssd",
			VolumeMounts: []aerospikev1alpha1.VolumeMount{
				aerospikev1alpha1.VolumeMount{
					MountPath: "/opt/aerospike/data",
					SizeInGB:  1,
				},
			},
		},
	}
	aeroCluster.Spec.AerospikeConfig["namespace"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"file":           []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
	}
	return aeroCluster
}

func createHDDAndDataInIndexStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterName, namespace, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.FileStorage = []aerospikev1alpha1.FileStorageSpec{
		aerospikev1alpha1.FileStorageSpec{
			StorageClass: "ssd",
			VolumeMounts: []aerospikev1alpha1.VolumeMount{
				aerospikev1alpha1.VolumeMount{
					MountPath: "/opt/aerospike/data",
					SizeInGB:  1,
				},
			},
		},
	}
	aeroCluster.Spec.AerospikeConfig["namespace"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"single-bin":         true,
			"data-in-index":      true,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"file":           []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
	}
	return aeroCluster
}

func createDataInMemWithoutPersistentStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterName, namespace, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.AerospikeConfig["namespace"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine":     "memory",
		},
	}

	return aeroCluster
}

func createShadowDeviceStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterName, namespace, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.BlockStorage = []aerospikev1alpha1.BlockStorageSpec{
		aerospikev1alpha1.BlockStorageSpec{
			StorageClass: "ssd",
			VolumeDevices: []aerospikev1alpha1.VolumeDevice{
				aerospikev1alpha1.VolumeDevice{
					DevicePath: "/test/dev/xvdf",
					SizeInGB:   1,
				},
			},
		},
		aerospikev1alpha1.BlockStorageSpec{
			StorageClass: "local-scsi",
			VolumeDevices: []aerospikev1alpha1.VolumeDevice{
				aerospikev1alpha1.VolumeDevice{
					DevicePath: "/dev/nvme0n1",
					SizeInGB:   1,
				},
			},
		},
	}
	aeroCluster.Spec.AerospikeConfig["namespace"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"device": []interface{}{"/dev/nvme0n1	/test/dev/xvdf"},
			},
		},
	}
	return aeroCluster
}

func createPMEMStorageCluster(clusterName, namespace string, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	return nil
}
