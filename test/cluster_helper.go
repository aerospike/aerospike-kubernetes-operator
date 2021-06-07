package test

import (
	goctx "context"
	"fmt"
	"time"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	latestClusterImage = "aerospike/aerospike-server-enterprise:5.4.0.5"
	imageToUpgrade     = "aerospike/aerospike-server-enterprise:5.5.0.3"
)

var (
	retryInterval      = time.Second * 5
	cascadeDeleteFalse = false
	cascadeDeleteTrue  = true
)

func scaleUpClusterTest(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, increaseBy int32) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size = aeroCluster.Spec.Size + increaseBy
	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	// Wait for aerocluster to reach 2 replicas
	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(increaseBy))
}

func scaleDownClusterTest(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, decreaseBy int32) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	aeroCluster.Spec.Size = aeroCluster.Spec.Size - decreaseBy
	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	// How much time to wait in scaleDown
	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(decreaseBy))
}

func rollingRestartClusterTest(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change config
	if _, ok := aeroCluster.Spec.AerospikeConfig.Value["service"]; !ok {
		aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{}
	}
	aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = 15000

	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func upgradeClusterTest(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, image string) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change config
	aeroCluster.Spec.Image = image
	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func getCluster(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName) (*asdbv1alpha1.AerospikeCluster, error) {
	aeroCluster := &asdbv1alpha1.AerospikeCluster{}
	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		return nil, err
	}
	return aeroCluster, nil
}

func getClusterIfExists(k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName) (*asdbv1alpha1.AerospikeCluster, error) {
	aeroCluster := &asdbv1alpha1.AerospikeCluster{}
	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("error getting cluster: %v", err)
	}

	return aeroCluster, nil
}

func deleteCluster(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	if err := k8sClient.Delete(ctx, aeroCluster); err != nil {
		return err
	}

	// Wait for all pod to get deleted.
	// TODO: Maybe add these checks in cluster delete itself.
	//time.Sleep(time.Second * 12)

	clusterNamespacedName := getClusterNamespacedName(aeroCluster.Name, aeroCluster.Namespace)
	for {
		// t.Logf("Waiting for cluster %v to be deleted", aeroCluster.Name)
		existing, err := getClusterIfExists(k8sClient, ctx, clusterNamespacedName)
		if err != nil {
			return err
		}
		if existing == nil {
			break
		}
		time.Sleep(time.Second)
	}

	// Wait for all rmoved PVCs to be terminated.
	for {
		newPVCList, err := getAeroClusterPVCList(aeroCluster, k8sClient)

		if err != nil {
			return fmt.Errorf("error getting PVCs: %v", err)
		}

		pending := false
		for _, pvc := range newPVCList {
			if utils.IsPVCTerminating(&pvc) {
				// t.Logf("Waiting for PVC %v to terminate", pvc.Name)
				pending = true
				break
			}
		}

		if !pending {
			break
		}

		time.Sleep(time.Second)
	}

	return nil
}

func deployCluster(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	return deployClusterWithTO(k8sClient, ctx, aeroCluster, retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func deployClusterWithTO(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster, retryInterval, timeout time.Duration) error {
	// Use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, aeroCluster)
	if err != nil {
		return err
	}
	// Wait for aerocluster to reach desired cluster size.
	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, timeout)
}

func updateCluster(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

// TODO: remove it
func updateAndWait(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}
	// Currently waitForAerospikeCluster doesn't check for config update
	// How to validate if its old cluster or new cluster with new config
	// Hence sleep.
	// TODO: find another way or validate config also
	time.Sleep(5 * time.Second)

	return waitForAerospikeCluster(k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func getClusterPodList(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := k8sClient.List(ctx, podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func validateResource(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1alpha1.AerospikeCluster) error {
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	mem := aeroCluster.Spec.Resources.Requests.Memory()
	cpu := aeroCluster.Spec.Resources.Requests.Cpu()

	for _, p := range podList.Items {
		for _, cnt := range p.Spec.Containers {
			stMem := cnt.Resources.Requests.Memory()
			if !mem.Equal(*stMem) {
				return fmt.Errorf("resource memory not matching. want %v, got %v", mem.String(), stMem.String())
			}
			limitMem := cnt.Resources.Limits.Memory()
			if !mem.Equal(*limitMem) {
				return fmt.Errorf("limit memory not matching. want %v, got %v", mem.String(), limitMem.String())
			}

			stCPU := cnt.Resources.Requests.Cpu()
			if !cpu.Equal(*stCPU) {
				return fmt.Errorf("resource cpu not matching. want %v, got %v", cpu.String(), stCPU.String())
			}
			limitCPU := cnt.Resources.Limits.Cpu()
			if !cpu.Equal(*limitCPU) {
				return fmt.Errorf("resource cpu not matching. want %v, got %v", cpu.String(), limitCPU.String())
			}
		}
	}
	return nil
}

// // CreateBasicCluster deploy a basic dummy cluster with 2 nodes
// func CreateBasicCluster(k8sClient client.Client, ctx goctx.Context) {
// 	// get namespace
// 	// namespace, err := ctx.GetNamespace()
// 	// if err != nil {
// 	// 	return err
// 	// }
// 	clusterName := "aerocluster"
// 	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

// 	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
// 	t.Run("Positive", func(t *testing.T) {
// 		if err := deployCluster(k8sClient, ctx, aeroCluster); err != nil {
// 			return err
// 		}
// 	})
// }

// feature-key file needed
func createAerospikeClusterPost460(clusterNamespacedName types.NamespacedName, size int32, image string) *asdbv1alpha1.AerospikeCluster {
	// create Aerospike custom resource
	mem := resource.MustParse("2Gi")
	cpu := resource.MustParse("200m")
	aeroCluster := &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  size,
			Image: image,
			Storage: asdbv1alpha1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				Volumes: []asdbv1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/test/dev/xvdf",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
					},
					{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
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
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{

					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
					},
					"security": map[string]interface{}{
						"enable-security": true,
					},
					"network": map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":                "aerospike-a-0.test-runner",
							"tls-authenticate-client": "any",
						},
						"heartbeat": map[string]interface{}{
							"tls-name": "aerospike-a-0.test-runner",
						},
						"fabric": map[string]interface{}{
							"tls-name": "aerospike-a-0.test-runner",
						},
						"tls": []map[string]interface{}{
							{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
								"key-file":  "/etc/aerospike/secret/svc_key.pem",
								"ca-file":   "/etc/aerospike/secret/cacert.pem",
							},
							// {
							// 	"name":      "aerospike-a-0.test-runner",
							// 	"cert-file": "/etc/aerospike/secret/hb_cluster_chain.pem",
							// 	"key-file":  "/etc/aerospike/secret/hb_key.pem",
							// 	"ca-file":   "/etc/aerospike/secret/cacert.pem",
							// },
							// {
							// 	"name":      "aerospike-a-0.test-runner",
							// 	"cert-file": "/etc/aerospike/secret/fb_cluster_chain.pem",
							// 	"key-file":  "/etc/aerospike/secret/fb_key.pem",
							// 	"ca-file":   "/etc/aerospike/secret/cacert.pem",
							// },
						},
					},
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"memory-size":        1000955200,
							"replication-factor": 2,
							"storage-engine": map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{"/test/dev/xvdf"},
							},
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

func createDummyRackAwareAerospikeCluster(clusterNamespacedName types.NamespacedName, size int32) *asdbv1alpha1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []asdbv1alpha1.Rack{{ID: 1}}
	rackConf := asdbv1alpha1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf
	return aeroCluster
}

func createDummyAerospikeCluster(clusterNamespacedName types.NamespacedName, size int32) *asdbv1alpha1.AerospikeCluster {
	return createDummyAerospikeClusterWithOption(clusterNamespacedName, size, true)
}

var defaultProtofdmax int64 = 15000

func createDummyAerospikeClusterWithOption(clusterNamespacedName types.NamespacedName, size int32, cascadeDelete bool) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("1Gi")
	cpu := resource.MustParse("200m")
	// create Aerospike custom resource
	aeroCluster := &asdbv1alpha1.AerospikeCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "asdb.aerospike.com/v1alpha1",
			Kind:       "AerospikeCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  size,
			Image: latestClusterImage,
			Storage: asdbv1alpha1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteFalse,
				},
				FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteFalse,
				},
				Volumes: []asdbv1alpha1.AerospikePersistentVolumeSpec{
					{
						Path:         "/test/dev/xvdf",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
					},
					{
						Path:         "/opt/aerospike",
						SizeInGB:     1,
						StorageClass: storageClass,
						VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
					},
				},
			},
			AerospikeAccessControl: &asdbv1alpha1.AerospikeAccessControlSpec{
				Users: []asdbv1alpha1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
							"read-write",
						},
					},
				},
			},
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"proto-fd-max":     defaultProtofdmax,
					},
					"security": map[string]interface{}{
						"enable-security": true,
					},
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"memory-size":        1000955200,
							"replication-factor": 1,
							"storage-engine": map[string]interface{}{
								"type":    "device",
								"devices": []interface{}{"/test/dev/xvdf"},
							},
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

// feature-key file needed
func createBasicTLSCluster(clusterNamespacedName types.NamespacedName, size int32) *asdbv1alpha1.AerospikeCluster {
	mem := resource.MustParse("1Gi")
	cpu := resource.MustParse("200m")
	// create Aerospike custom resource
	aeroCluster := &asdbv1alpha1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1alpha1.AerospikeClusterSpec{
			Size:  size,
			Image: latestClusterImage,
			Storage: asdbv1alpha1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				FileSystemVolumePolicy: asdbv1alpha1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteTrue,
				},
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
			AerospikeConfigSecret: asdbv1alpha1.AerospikeConfigSecretSpec{
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
			AerospikeConfig: &asdbv1alpha1.AerospikeConfigSpec{
				Value: map[string]interface{}{

					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
					},
					"security": map[string]interface{}{
						"enable-security": true,
					},
					"network": map[string]interface{}{
						"service": map[string]interface{}{
							"tls-name":                "aerospike-a-0.test-runner",
							"tls-authenticate-client": "any",
						},
						"heartbeat": map[string]interface{}{
							"tls-name": "aerospike-a-0.test-runner",
						},
						"fabric": map[string]interface{}{
							"tls-name": "aerospike-a-0.test-runner",
						},
						"tls": []map[string]interface{}{
							{
								"name":      "aerospike-a-0.test-runner",
								"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
								"key-file":  "/etc/aerospike/secret/svc_key.pem",
								"ca-file":   "/etc/aerospike/secret/cacert.pem",
							},
							// {
							// 	"name":      "aerospike-a-0.test-runner",
							// 	"cert-file": "/etc/aerospike/secret/hb_cluster_chain.pem",
							// 	"key-file":  "/etc/aerospike/secret/hb_key.pem",
							// 	"ca-file":   "/etc/aerospike/secret/cacert.pem",
							// },
							// {
							// 	"name":      "aerospike-a-0.test-runner",
							// 	"cert-file": "/etc/aerospike/secret/fb_cluster_chain.pem",
							// 	"key-file":  "/etc/aerospike/secret/fb_key.pem",
							// 	"ca-file":   "/etc/aerospike/secret/cacert.pem",
							// },
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

func createSSDStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
		{
			Path:         "/test/dev/xvdf",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
		},
		{
			Path:         "/opt/aerospike",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
		},
	}

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type":    "device",
				"devices": []interface{}{"/test/dev/xvdf"},
			},
		},
	}
	return aeroCluster
}

func createHDDAndDataInMemStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
		{
			Path:         "/opt/aerospike",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
		}, {
			Path:         "/opt/aerospike/data",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
		},
	}

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type":           "device",
				"files":          []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
	}
	return aeroCluster
}

func createHDDAndDataInIndexStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
		{
			Path:         "/dev/xvdf1",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
		},
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
	}
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"single-bin":         true,
			"data-in-index":      true,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type":           "device",
				"files":          []interface{}{"/opt/aerospike/data/test.dat"},
				"filesize":       2000955200,
				"data-in-memory": true,
			},
		},
	}
	return aeroCluster
}

func createDataInMemWithoutPersistentStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
		{
			Path:         "/opt/aerospike",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
		},
	}
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type": "memory",
			},
		},
	}

	return aeroCluster
}

func createShadowDeviceStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = []asdbv1alpha1.AerospikePersistentVolumeSpec{
		{
			Path:         "/test/dev/xvdf",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
		},
		{
			Path:         "/dev/nvme0n1",
			SizeInGB:     1,
			StorageClass: "local-ssd",
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeBlock,
		},
		{
			Path:         "/opt/aerospike",
			SizeInGB:     1,
			StorageClass: storageClass,
			VolumeMode:   asdbv1alpha1.AerospikeVolumeModeFilesystem,
		},
	}

	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = []interface{}{
		map[string]interface{}{
			"name":               "test",
			"memory-size":        2000955200,
			"replication-factor": repFact,
			"storage-engine": map[string]interface{}{
				"type": "device",
				"devices": []interface{}{"/dev/nvme0n1	/test/dev/xvdf"},
			},
		},
	}
	return aeroCluster
}

func createPMEMStorageCluster(clusterNamespacedName types.NamespacedName, size int32, repFact int32, multiPodPerHost bool) *asdbv1alpha1.AerospikeCluster {
	return nil
}

func aerospikeClusterCreateUpdateWithTO(k8sClient client.Client, desired *asdbv1alpha1.AerospikeCluster, ctx goctx.Context, retryInterval, timeout time.Duration) error {
	current := &asdbv1alpha1.AerospikeCluster{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current)
	if err != nil {
		// Deploy the cluster.
		// t.Logf("Deploying cluster at %v", time.Now().Format(time.RFC850))
		if err := deployClusterWithTO(k8sClient, ctx, desired, retryInterval, timeout); err != nil {
			return err
		}
		// t.Logf("Deployed cluster at %v", time.Now().Format(time.RFC850))
		return nil
	}
	// Apply the update.
	if desired.Spec.AerospikeAccessControl != nil {
		current.Spec.AerospikeAccessControl = &asdbv1alpha1.AerospikeAccessControlSpec{}
		lib.DeepCopy(&current.Spec, &desired.Spec)
	} else {
		current.Spec.AerospikeAccessControl = nil
	}
	lib.DeepCopy(&current.Spec.AerospikeConfig.Value, &desired.Spec.AerospikeConfig.Value)

	err = k8sClient.Update(ctx, current)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(k8sClient, ctx, desired, int(desired.Spec.Size), retryInterval, timeout)
}

func aerospikeClusterCreateUpdate(k8sClient client.Client, desired *asdbv1alpha1.AerospikeCluster, ctx goctx.Context) error {
	return aerospikeClusterCreateUpdateWithTO(k8sClient, desired, ctx, retryInterval, getTimeout(1))
}
