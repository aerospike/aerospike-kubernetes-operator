// +build !noac

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
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
				"files":    []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize": 2000955200,
			},
		},
	},
}

func TestJumpVersion(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Errorf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	var aeroCluster *aerospikev1alpha1.AerospikeCluster = nil
	var deployImage = ""
	t.Run("CrashRecovery", func(t *testing.T) {
		deployImage = "aerospike/aerospike-server-enterprise:4.7.0.10"
		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigCrashingPre5, deployImage, ctx)
		err := aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, 100*time.Millisecond, 10*time.Second, t)
		if err == nil {
			// Cluster should have crashed.
			t.Fatalf("Cluster should have crashed - but did not")
		}

		// Cluster should recover once correct config is provided.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPre5, deployImage, ctx)
		err = aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have recovered - but did not: %v", err)
		}

		err = waitForVersion(t, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have been on %s - but is not: %v", deployImage, err)
		}

	})

	t.Run("TestRegularUpgrade", func(t *testing.T) {
		deployImage = "aerospike/aerospike-server-enterprise:4.8.0.11"
		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPre5, deployImage, ctx)
		err := aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have recovered - but did not: %v", err)
		}

		err = waitForVersion(t, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have been on %s - but is not: %v", deployImage, err)
		}
	})

	t.Run("TestInvalidUpgrade", func(t *testing.T) {
		deployImage = "aerospike/aerospike-server-enterprise:5.0.0.4"
		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPost5, deployImage, ctx)
		err := aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err == nil || !strings.Contains(err.Error(), "jump required to version") {
			t.Fatalf("Cluster should have not have upgraded - but did not: %v", err)
		}
	})

	t.Run("TestValidUpgrade", func(t *testing.T) {
		deployImage = "aerospike/aerospike-server-enterprise:4.9.0.8"
		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPre5, deployImage, ctx)
		err := aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err != nil {
			t.Fatalf("Cluster should have upgraded - but did not: %v", err)
		}
		err = waitForVersion(t, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have been on %s - but is not: %v", deployImage, err)
		}

		deployImage = "aerospike/aerospike-server-enterprise:5.0.0.4"
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPost5, deployImage, ctx)
		err = aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err != nil {
			t.Fatalf("Cluster should have upgraded - but did not: %v", err)
		}

		err = waitForVersion(t, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have been on %s - but is not: %v", deployImage, err)
		}
	})

	t.Run("TestValidDowngrade", func(t *testing.T) {
		deployImage = "aerospike/aerospike-server-enterprise:4.9.0.8"
		// Save cluster variable as well for cleanup.
		aeroCluster = getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfigPre5, deployImage, ctx)
		err := aerospikeClusterCreateUpdateWithTO(aeroCluster, ctx, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO, t)
		if err != nil {
			t.Fatalf("Cluster should have upgraded - but did not: %v", err)
		}

		err = waitForVersion(t, aeroCluster, deployImage, jumpTestWaitForVersionInterval, jumpTestWaitForVersionTO)
		if err != nil {
			// Cluster should have recovered.
			t.Fatalf("Cluster should have been on %s - but is not: %v", deployImage, err)
		}
	})

	if aeroCluster != nil {
		deleteCluster(t, f, ctx, aeroCluster)
	}
}

func getAerospikeClusterSpecWithAerospikeConfig(aerospikeConfig map[string]interface{}, image string, ctx *framework.TestCtx) *aerospikev1alpha1.AerospikeCluster {
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")

	kubeNs, _ := ctx.GetNamespace()
	cascadeDelete := true

	// create Aerospike custom resource
	return &aerospikev1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jumpversiontest",
			Namespace: kubeNs,
		},
		Spec: aerospikev1alpha1.AerospikeClusterSpec{
			Size:  jumpTestClusterSize,
			Image: image,
			Storage: aerospikev1alpha1.AerospikeStorageSpec{
				FileSystemVolumePolicy: aerospikev1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []aerospikev1alpha1.AerospikePersistentVolumeSpec{
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
				},
			},
			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
				SecretName: tlsSecretName,
				MountPath:  "/etc/aerospike/secret",
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
			AerospikeConfig: aerospikeConfig,
		},
	}
}

// waitForVersion waits for the cluster to have all nodes at input Aerospike version.
func waitForVersion(t *testing.T, aeroCluster *aerospikev1alpha1.AerospikeCluster, image string, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Refresh cluster object.
		err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, aeroCluster)
		if err != nil {
			t.Logf("Could not read cluster state: %v", err)
			return false, nil
		}

		for _, pod := range aeroCluster.Status.Pods {
			if !strings.HasSuffix(pod.Image, image) {
				t.Logf("Node : %s expected image: %s status reported image: %s", pod.Aerospike.NodeID, image, pod.Image)
				return false, nil
			}
		}

		return true, nil
	})

	if err != nil {
		return err
	}

	client, err := getClient(aeroCluster, &framework.Global.Client.Client)
	if err != nil {
		// Client should have been created.
		return fmt.Errorf("Could not create client: %v", err)
	}
	client.Close()

	return nil
}
