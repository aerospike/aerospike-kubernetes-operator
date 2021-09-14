package test

import (
	goctx "context"
	"fmt"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	latestClusterImage  = "aerospike/aerospike-server-enterprise:5.4.0.5"
	imageToUpgrade      = "aerospike/aerospike-server-enterprise:5.5.0.3"
	baseImage           = "aerospike/aerospike-server-enterprise"
	latestServerVersion = "5.6.0.7"
)

var (
	retryInterval      = time.Second * 5
	cascadeDeleteFalse = false
	cascadeDeleteTrue  = true
	logger             = logr.Discard()
)

func scaleUpClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, increaseBy int32,
) error {
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
	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(increaseBy),
	)
}

func scaleDownClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, decreaseBy int32,
) error {
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
	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(decreaseBy),
	)
}

func rollingRestartClusterTest(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) error {
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

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)

	if err != nil {
		return err
	}

	// Verify that the change has been applied on the cluster.
	return validateAerospikeConfigServiceClusterUpdate(
		log, k8sClient, ctx, clusterNamespacedName, []string{"proto-fd-max"},
	)
}

func validateAerospikeConfigServiceClusterUpdate(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName, updatedKeys []string,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for _, pod := range aeroCluster.Status.Pods {
		// TODO:
		// We may need to check for all keys in aerospikeConfig in rack
		// but we know that we are changing for service only for now
		host := &as.Host{
			Name: pod.HostExternalIP, Port: int(pod.ServicePort),
			TLSName: pod.Aerospike.TLSName,
		}
		asinfo := info.NewAsInfo(log, host, getClientPolicy(aeroCluster, k8sClient))
		confs, err := getAsConfig(asinfo, "service")
		if err != nil {
			return err
		}
		svcConfs := confs["service"].(lib.Stats)

		inputSvcConf := aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})
		for _, k := range updatedKeys {
			v, ok := inputSvcConf[k]
			if !ok {
				return fmt.Errorf(
					"config %s missing in aerospikeConfig %v", k, svcConfs,
				)
			}
			cv, ok := svcConfs[k]
			if !ok {
				return fmt.Errorf(
					"config %s missing in aerospike config asinfo %v", k,
					svcConfs,
				)
			}

			strV := fmt.Sprintf("%v", v)
			strCv := fmt.Sprintf("%v", cv)
			if strV != strCv {
				return fmt.Errorf(
					"config %s mismatch with config. got %v:%T, want %v:%T, aerospikeConfig %v",
					k, cv, cv, v, v, svcConfs,
				)
			}
		}
	}

	return nil
}

func upgradeClusterTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, image string,
) error {
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

	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func getCluster(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*asdbv1beta1.AerospikeCluster, error) {
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		return nil, err
	}
	return aeroCluster, nil
}

func getClusterIfExists(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*asdbv1beta1.AerospikeCluster, error) {
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("error getting cluster: %v", err)
	}

	return aeroCluster, nil
}

func getPodsList(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(clusterNamespacedName.Name))
	listOps := &client.ListOptions{
		Namespace:     clusterNamespacedName.Namespace,
		LabelSelector: labelSelector,
	}

	if err := k8sClient.List(ctx, podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func deleteCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	if err := k8sClient.Delete(ctx, aeroCluster); err != nil {
		return err
	}

	// Wait for all pod to get deleted.
	// TODO: Maybe add these checks in cluster delete itself.
	//time.Sleep(time.Second * 12)

	clusterNamespacedName := getClusterNamespacedName(
		aeroCluster.Name, aeroCluster.Namespace,
	)
	for {
		// t.Logf("Waiting for cluster %v to be deleted", aeroCluster.Name)
		existing, err := getClusterIfExists(
			k8sClient, ctx, clusterNamespacedName,
		)
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

func deployCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	return deployClusterWithTO(
		k8sClient, ctx, aeroCluster, retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func deployClusterWithTO(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
	retryInterval, timeout time.Duration,
) error {
	// Use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, aeroCluster)
	if err != nil {
		return err
	}
	// Wait for aerocluster to reach desired cluster size.
	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		timeout,
	)
}

func updateCluster(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

// TODO: remove it
func updateAndWait(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	err := k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}
	// Currently waitForAerospikeCluster doesn't check for config update
	// How to validate if its old cluster or new cluster with new config
	// Hence sleep.
	// TODO: find another way or validate config also
	time.Sleep(5 * time.Second)

	return waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)
}

func getClusterPodList(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := k8sClient.List(ctx, podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func validateResource(
	k8sClient client.Client, ctx goctx.Context,
	aeroCluster *asdbv1beta1.AerospikeCluster,
) error {
	if aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources == nil {
		return fmt.Errorf("resources can not be nil for validation")
	}
	podList, err := getClusterPodList(k8sClient, ctx, aeroCluster)
	if err != nil {
		return err
	}

	mem := aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources.Requests.Memory()
	cpu := aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources.Requests.Cpu()

	for _, p := range podList.Items {
		for _, cnt := range p.Spec.Containers {
			stMem := cnt.Resources.Requests.Memory()
			if !mem.Equal(*stMem) {
				return fmt.Errorf(
					"resource memory not matching. want %v, got %v",
					mem.String(), stMem.String(),
				)
			}
			limitMem := cnt.Resources.Limits.Memory()
			if !mem.Equal(*limitMem) {
				return fmt.Errorf(
					"limit memory not matching. want %v, got %v", mem.String(),
					limitMem.String(),
				)
			}

			stCPU := cnt.Resources.Requests.Cpu()
			if !cpu.Equal(*stCPU) {
				return fmt.Errorf(
					"resource cpu not matching. want %v, got %v", cpu.String(),
					stCPU.String(),
				)
			}
			limitCPU := cnt.Resources.Limits.Cpu()
			if !cpu.Equal(*limitCPU) {
				return fmt.Errorf(
					"resource cpu not matching. want %v, got %v", cpu.String(),
					limitCPU.String(),
				)
			}
		}
	}
	return nil
}

// feature-key file needed
func createAerospikeClusterPost460(
	clusterNamespacedName types.NamespacedName, size int32, image string,
) *asdbv1beta1.AerospikeCluster {
	// create Aerospike custom resource

	aeroCluster := &asdbv1beta1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:  size,
			Image: image,
			Storage: asdbv1beta1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				Volumes: []asdbv1beta1.VolumeSpec{
					{
						Name: "ns",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   v1.PersistentVolumeBlock,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/test/dev/xvdf",
						},
					},
					{
						Name: "workdir",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   v1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1beta1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},
			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
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

			PodSpec: asdbv1beta1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
			OperatorClientCertSpec: &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         tlsSecretName,
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			},
			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
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

func createDummyRackAwareAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, size)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []asdbv1beta1.Rack{{ID: 1}}
	rackConf := asdbv1beta1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf
	return aeroCluster
}

var defaultProtofdmax int64 = 15000

func createDummyAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1beta1.AerospikeCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "asdb.aerospike.com/v1beta1",
			Kind:       "AerospikeCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:  size,
			Image: latestClusterImage,
			Storage: asdbv1beta1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteFalse,
				},
				FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteFalse,
				},
				Volumes: []asdbv1beta1.VolumeSpec{
					{
						Name: "ns",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   v1.PersistentVolumeBlock,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/test/dev/xvdf",
						},
					},
					{
						Name: "workdir",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   v1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1beta1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},
			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
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

			PodSpec: asdbv1beta1.AerospikePodSpec{
				MultiPodPerHost: true,
			},

			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
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
func createBasicTLSCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	// create Aerospike custom resource
	aeroCluster := &asdbv1beta1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:  size,
			Image: latestClusterImage,
			Storage: asdbv1beta1.AerospikeStorageSpec{
				BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputInitMethod:    &aerospikeVolumeInitMethodDeleteFiles,
					InputCascadeDelete: &cascadeDeleteTrue,
				},
				Volumes: []asdbv1beta1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   v1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1beta1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},
			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
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

			PodSpec: asdbv1beta1.AerospikePodSpec{
				MultiPodPerHost: true,
			},

			OperatorClientCertSpec: &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         tlsSecretName,
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			},
			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
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
						},
					},
				},
			},
		},
	}
	return aeroCluster
}

func createSSDStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1beta1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, []asdbv1beta1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   v1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/test/dev/xvdf",
				},
			},
		}...,
	)

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

func createHDDAndDataInMemStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1beta1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, []asdbv1beta1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   v1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/data",
				},
			},
		}...,
	)

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

func createHDDAndDataInIndexStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1beta1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = multiPodPerHost
	aeroCluster.Spec.Storage.Volumes = append(
		aeroCluster.Spec.Storage.Volumes, []asdbv1beta1.VolumeSpec{
			{
				Name: "device",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   v1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/dev/xvdf1",
				},
			},
			{
				Name: "ns",
				Source: asdbv1beta1.VolumeSource{
					PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: storageClass,
						VolumeMode:   v1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
					Path: "/opt/aerospike/data",
				},
			},
		}...,
	)
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

func createDataInMemWithoutPersistentStorageCluster(
	clusterNamespacedName types.NamespacedName, size int32, repFact int32,
	multiPodPerHost bool,
) *asdbv1beta1.AerospikeCluster {
	aeroCluster := createBasicTLSCluster(clusterNamespacedName, size)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = multiPodPerHost
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

func aerospikeClusterCreateUpdateWithTO(
	k8sClient client.Client, desired *asdbv1beta1.AerospikeCluster,
	ctx goctx.Context, retryInterval, timeout time.Duration,
) error {
	current := &asdbv1beta1.AerospikeCluster{}
	err := k8sClient.Get(
		ctx,
		types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace},
		current,
	)
	if err != nil {
		// Deploy the cluster.
		// t.Logf("Deploying cluster at %v", time.Now().Format(time.RFC850))
		if err := deployClusterWithTO(
			k8sClient, ctx, desired, retryInterval, timeout,
		); err != nil {
			return err
		}
		// t.Logf("Deployed cluster at %v", time.Now().Format(time.RFC850))
		return nil
	}
	// Apply the update.
	if desired.Spec.AerospikeAccessControl != nil {
		current.Spec.AerospikeAccessControl = &asdbv1beta1.AerospikeAccessControlSpec{}
		lib.DeepCopy(&current.Spec, &desired.Spec)
	} else {
		current.Spec.AerospikeAccessControl = nil
	}
	lib.DeepCopy(
		&current.Spec.AerospikeConfig.Value,
		&desired.Spec.AerospikeConfig.Value,
	)

	err = k8sClient.Update(ctx, current)
	if err != nil {
		return err
	}

	return waitForAerospikeCluster(
		k8sClient, ctx, desired, int(desired.Spec.Size), retryInterval, timeout,
	)
}

func aerospikeClusterCreateUpdate(
	k8sClient client.Client, desired *asdbv1beta1.AerospikeCluster,
	ctx goctx.Context,
) error {
	return aerospikeClusterCreateUpdateWithTO(
		k8sClient, desired, ctx, retryInterval, getTimeout(1),
	)
}
