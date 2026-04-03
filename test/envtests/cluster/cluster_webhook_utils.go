package cluster

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

const (
	aerospikeConfigVolName = "aerospike-config-secret"
	workDir                = "/opt/aerospike"
	otherStorageClass      = "other-storage-class"
)

func uniqueNamespacedName(suffix string) types.NamespacedName {
	name := fmt.Sprintf("ko481-%s-%d", suffix, GinkgoParallelProcess())

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func storageForDevice(devicePath string) asdbv1.AerospikeStorageSpec {
	cd := false
	initM := asdbv1.AerospikeVolumeMethodDeleteFiles

	return asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: &cd,
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputInitMethod:    &initM,
			InputCascadeDelete: &cd,
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: devicePath,
				},
			},
			{
				Name: "workdir",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: workDir,
				},
			},
			{
				Name: aerospikeConfigVolName,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: test.AerospikeSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}

func rackNSOverride(devicePath string) *asdbv1.AerospikeConfigSpec {
	return &asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			asdbv1.ConfKeyNamespace: []interface{}{
				map[string]interface{}{
					"name":               "test",
					"replication-factor": 2,
					"strong-consistency": true,
					asdbv1.ConfKeyStorageEngine: map[string]interface{}{
						"type":    "device",
						"devices": []interface{}{devicePath},
					},
				},
			},
		},
	}
}

func deleteCluster(ctx context.Context, nsName types.NamespacedName) {
	aeroCluster := &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsName.Name,
			Namespace: nsName.Namespace,
		},
	}
	// Delete the cluster after each test
	Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
}

func patchFirstPVStorageClassSpec(storage *asdbv1.AerospikeStorageSpec) {
	for i := range storage.Volumes {
		if storage.Volumes[i].Source.PersistentVolume != nil {
			storage.Volumes[i].Source.PersistentVolume.StorageClass = otherStorageClass

			return
		}
	}

	Fail("no PersistentVolume volume found in storage spec")
}
