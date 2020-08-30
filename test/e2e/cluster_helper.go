package e2e

import (
	"context"
	"testing"
	"time"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	lib "github.com/aerospike/aerospike-management-lib"
)

// feature-key file needed
func createAerospikeClusterPost460(clusterNamespacedName types.NamespacedName, size int32, build string) *aerospikev1alpha1.AerospikeCluster {
	// create memcached custom resource
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: build,
			Storage: aerospikev1alpha1.AerospikeStorageSpec{
				Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/test/dev/xvdf",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
					},
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeAccessControl: &aerospikev1alpha1.AerospikeAccessControlSpec{
				Users: []aerospikev1alpha1.AerospikeUserSpec{
					aerospikev1alpha1.AerospikeUserSpec{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
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

func createDummyRackAwareAerospikeCluster(clusterNamespacedName types.NamespacedName, size int32) *aerospikev1alpha1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []aerospikev1alpha1.Rack{{ID: 1}}
	rackConf := aerospikev1alpha1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf
	return aeroCluster
}

func createDummyAerospikeCluster(clusterNamespacedName types.NamespacedName, size int32) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	// create memcached custom resource
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: latestClusterBuild,
			Storage: aerospikev1alpha1.AerospikeStorageSpec{
				Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/test/dev/xvdf",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
					},
					aerospikev1alpha1.AerospikePersistentVolumeSpec{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: "ssd",
						VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeAccessControl: &aerospikev1alpha1.AerospikeAccessControlSpec{
				Users: []aerospikev1alpha1.AerospikeUserSpec{
					aerospikev1alpha1.AerospikeUserSpec{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
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
func createBasicTLSCluster(clusterNamespacedName types.NamespacedName, size int32) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	// create memcached custom resource
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  size,
			Build: latestClusterBuild,
			AerospikeAccessControl: &aerospikev1alpha1.AerospikeAccessControlSpec{
				Users: []aerospikev1alpha1.AerospikeUserSpec{
					aerospikev1alpha1.AerospikeUserSpec{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
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

func createSSDStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{
		Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/test/dev/xvdf",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
			},
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/opt/aerospike",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
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

func createHDDAndDataInMemStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost

	aeroCluster.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{
		Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/opt/aerospike",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
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

func createHDDAndDataInIndexStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []aerospikev1alpha1.AerospikePersistentVolumeSpec{
		aerospikev1alpha1.AerospikePersistentVolumeSpec{
			Path:         "/dev/xvdf1",
			SizeInGB:     1,
			StorageClass: "ssd",
			VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
		},
		aerospikev1alpha1.AerospikePersistentVolumeSpec{
			Path:         "/opt/aerospike",
			SizeInGB:     1,
			StorageClass: "ssd",
			VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
		},
		aerospikev1alpha1.AerospikePersistentVolumeSpec{
			Path:         "/opt/aerospike/data",
			SizeInGB:     1,
			StorageClass: "ssd",
			VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
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

func createDataInMemWithoutPersistentStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
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

func createShadowDeviceStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost

	aeroCluster.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{
		Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/test/dev/xvdf",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
			},
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/dev/nvme0n1",
				SizeInGB:     1,
				StorageClass: "local-ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeBlock,
			},
			aerospikev1alpha1.AerospikePersistentVolumeSpec{
				Path:         "/opt/aerospike",
				SizeInGB:     1,
				StorageClass: "ssd",
				VolumeMode:   aerospikev1alpha1.AerospikeVolumeModeFilesystem,
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

func createPMEMStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *aerospikev1alpha1.AerospikeCluster {
	return nil
}

func aerospikeClusterCreateUpdateWithTO(desired *aerospikev1alpha1.AerospikeCluster, ctx *framework.TestCtx, retryInterval, timeout time.Duration, t *testing.T) error {
	current := &aerospikev1alpha1.AerospikeCluster{}
	err := framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		// Deploy the cluster.
		t.Logf("Deploying cluster at %v", time.Now().Format(time.RFC850))
		if err := deployClusterWithTO(t, framework.Global, ctx, desired, retryInterval, timeout); err != nil {
			return err
		}
		t.Logf("Deployed cluster at %v", time.Now().Format(time.RFC850))
		return nil
	}
	// Apply the update.
	if desired.Spec.AerospikeAccessControl != nil {
		current.Spec.AerospikeAccessControl = &aerospikev1alpha1.AerospikeAccessControlSpec{}
		lib.DeepCopy(&current.Spec, &desired.Spec)
	} else {
		current.Spec.AerospikeAccessControl = nil
	}
	lib.DeepCopy(&current.Spec.AerospikeConfig, &desired.Spec.AerospikeConfig)

	err = framework.Global.Client.Update(context.TODO(), current)
	if err != nil {
		return err
	}

	waitForAerospikeCluster(t, framework.Global, desired, int(desired.Spec.Size), retryInterval, timeout)
	return nil
}

func aerospikeClusterCreateUpdate(desired *aerospikev1alpha1.AerospikeCluster, ctx *framework.TestCtx, t *testing.T) error {
	return aerospikeClusterCreateUpdateWithTO(desired, ctx, retryInterval, getTimeout(1), t)
}
