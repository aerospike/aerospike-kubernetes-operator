// +build !noac

package test

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
)

const (
	jumpTestClusterSize            = 2
	jumpTestWaitForVersionInterval = 1 * time.Second
	jumpTestWaitForVersionTO       = 10 * time.Minute
)

var aerospikeConfigPre5 = map[string]interface{}{
	"service": map[string]interface{}{
		"feature-key-file": "/etc/aerospike/secret/features.conf",
		"migrate-threads":  4,
	},
	"xdr": map[string]interface{}{
		"enable-xdr":                true,
		"xdr-digestlog-path":        "/opt/aerospike/xdr/digestlog 5G",
		"xdr-compression-threshold": 1000,
		"datacenters": []interface{}{
			map[string]interface{}{
				"name": "REMOTE_DC_1",
			},
		},
	},
	"security": map[string]interface{}{"enable-security": true},
	"namespaces": []interface{}{
		map[string]interface{}{
			"name":                   "test",
			"enable-xdr":             true,
			"memory-size":            3000000000,
			"migrate-sleep":          0,
			"xdr-remote-datacenters": "REMOTE_DC_1",
			"storage-engine": map[string]interface{}{
				"type":     "device",
				"files":    []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize": 2000955200,
			},
		},
	},
}

var aerospikeConfigCrashingPre5 = map[string]interface{}{
	"service": map[string]interface{}{
		"feature-key-file": "/etc/aerospike/secret/features.conf",
		"migrate-threads":  4,
	},

	"xdr": map[string]interface{}{
		"enable-xdr":                true,
		"xdr-digestlog-path":        "/opt/aerospike/xdr/digestlog 5G",
		"xdr-compression-threshold": 1000,
		"datacenters": []interface{}{
			map[string]interface{}{
				"name":                  "REMOTE_DC_1",
				"dc-node-address-ports": "IP PORT",
			},
		},
	},
	"security": map[string]interface{}{"enable-security": true},
	"namespaces": []interface{}{
		map[string]interface{}{
			"name":                   "test",
			"migrate-sleep":          0,
			"enable-xdr":             true,
			"xdr-remote-datacenters": "REMOTE_DC_1",
			"memory-size":            3000000000,
			"storage-engine": map[string]interface{}{
				"type":     "device",
				"files":    []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize": 2000955200,
			},
		},
	},
}

var aerospikeConfigPost5 = map[string]interface{}{
	"service": map[string]interface{}{
		"feature-key-file": "/etc/aerospike/secret/features.conf",
		"migrate-threads":  4,
	},

	"xdr": map[string]interface{}{
		"dcs": []interface{}{
			map[string]interface{}{
				"name":                         "test_dc",
				"use-alternate-access-address": true,
				"namespaces": []interface{}{
					map[string]interface{}{
						"name":     "test",
						"delay-ms": 10,
					},
				},
			},
		},
	},
	"security": map[string]interface{}{"enable-security": true},
	"namespaces": []interface{}{
		map[string]interface{}{
			"name":          "test",
			"memory-size":   3000000000,
			"migrate-sleep": 0,
			"storage-engine": map[string]interface{}{
				"type":     "device",
				"files":    []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize": 2000955200,
			},
		},
	},
}

var _ = Describe("JumpVersion", func() {

	ctx := goctx.Background()

	Context("When doing valid operations", func() {

		clusterName := "jumpversion"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		AfterEach(func() {
			aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
			Expect(err).ToNot(HaveOccurred())

			deleteCluster(k8sClient, ctx, aeroCluster)
		})

		It("Try CrashRecovery", func() {
			deployImage := "aerospike/aerospike-server-enterprise:4.7.0.10"
			// Save cluster variable as well for cleanup.
			aeroCluster := getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigCrashingPre5, deployImage, ctx)
			err := aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, 100*time.Millisecond, 10*time.Second)
			Expect(err).To(HaveOccurred(), "Cluster should have crashed - but did not")

			// Cluster should recover once correct config is provided.
			aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPre5, deployImage, ctx)
			err = aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have recovered - but did not: %v", err)

			err = waitForVersion(ctx, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have been on %s - but is not: %v", deployImage, err)
		})

		It("Try RegularUpgrade", func() {

			By("Doing regular upgrade")

			deployImage := "aerospike/aerospike-server-enterprise:4.8.0.11"
			// Save cluster variable as well for cleanup.
			aeroCluster := getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPre5, deployImage, ctx)
			err := aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have recovered - but did not: %v", err)

			err = waitForVersion(ctx, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have been on %s - but is not: %v", deployImage, err)

			By("Doing jump version upgrade")

			deployImage = "aerospike/aerospike-server-enterprise:5.0.0.4"
			// Save cluster variable as well for cleanup.
			aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPost5, deployImage, ctx)
			err = aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)

			if err == nil || !strings.Contains(err.Error(), "jump required to version") {
				Expect(err).To(HaveOccurred(), "Cluster should have not have upgraded - but did not: %v", err)
			}
		})

		It("Try ValidUpgrade", func() {
			deployImage := "aerospike/aerospike-server-enterprise:4.9.0.8"
			// Save cluster variable as well for cleanup.
			aeroCluster := getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPre5, deployImage, ctx)
			err := aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have upgraded - but did not: %v", err)

			err = waitForVersion(ctx, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have been on %s - but is not: %v", deployImage, err)

			deployImage = "aerospike/aerospike-server-enterprise:5.0.0.4"
			aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPost5, deployImage, ctx)
			err = aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have upgraded - but did not: %v", err)

			err = waitForVersion(ctx, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have been on %s - but is not: %v", deployImage, err)
		})

		It("Try ValidDowngrade", func() {
			deployImage := "aerospike/aerospike-server-enterprise:4.9.0.8"
			// Save cluster variable as well for cleanup.
			aeroCluster := getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName, aerospikeConfigPre5, deployImage, ctx)
			err := aerospikeClusterCreateUpdateWithTO(k8sClient, aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have upgraded - but did not: %v", err)

			err = waitForVersion(ctx, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
			Expect(err).ToNot(HaveOccurred(), "Cluster should have been on %s - but is not: %v", deployImage, err)
		})
	})
})

func getAerospikeClusterSpecWithAerospikeConfig(clusterNamespacedName types.NamespacedName, aerospikeConfig map[string]interface{}, image string, ctx goctx.Context) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	cascadeDelete := true

	// create Aerospike custom resource
	return &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  jumpTestClusterSize,
			Image: image,
			Storage: asdbv1alpha1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
					{
						Path:         "/opt/aerospike/data",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
			},
			AerospikeAccessControl: &asdbv1alpha1.AerospikeAccessControlSpec{
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: aerospikeConfig,
			},
		},
	}
}

// waitForVersion waits for the cluster to have all nodes at input Aerospike version.
func waitForVersion(ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster, image string, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Refresh cluster object.
		err = k8sClient.Get(ctx, types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, aeroCluster)
		if err != nil {
			// t.Logf("Could not read cluster state: %v", err)
			return false, nil
		}

		for _, pod := range aeroCluster.Status.Pods {
			if !strings.HasSuffix(pod.Image, image) {
				// t.Logf("Node : %s expected image: %s status reported image: %s", pod.Aerospike.NodeID, image, pod.Image)
				return false, nil
			}
		}

		return true, nil
	})

	if err != nil {
		return err
	}

	client, err := getClient(aeroCluster, k8sClient)
	if err != nil {
		// Client should have been created.
		return fmt.Errorf("could not create client: %v", err)
	}
	client.Close()

	return nil
}
