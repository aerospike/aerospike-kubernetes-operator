package test

import (
	goctx "context"
	"errors"
	"fmt"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	internalerrors "github.com/aerospike/aerospike-kubernetes-operator/errors"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/info"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	baseImage           = "aerospike/aerospike-server-enterprise"
	prevServerVersion   = "6.0.0.1"
	pre6Version         = "5.7.0.17"
	version6            = "6.0.0.5"
	latestServerVersion = "6.1.0.1"
	invalidVersion      = "3.0.0.4"
	pre5Version         = "4.9.0.33"
)

var (
	retryInterval      = time.Second * 5
	cascadeDeleteFalse = false
	cascadeDeleteTrue  = true
	logger             = logr.Discard()
	prevImage          = fmt.Sprintf("%s:%s", baseImage, prevServerVersion)
	latestImage        = fmt.Sprintf("%s:%s", baseImage, latestServerVersion)
	version6Image      = fmt.Sprintf("%s:%s", baseImage, version6)
	invalidImage       = fmt.Sprintf("%s:%s", baseImage, invalidVersion)
	pre5Image          = fmt.Sprintf("%s:%s", baseImage, pre5Version)
	pre6Image          = fmt.Sprintf("%s:%s", baseImage, pre6Version)
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
	log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change config
	if _, ok := aeroCluster.Spec.AerospikeConfig.Value["service"]; !ok {
		aeroCluster.Spec.AerospikeConfig.Value["service"] = map[string]interface{}{}
	}

	aeroCluster.Spec.AerospikeConfig.Value["service"].(map[string]interface{})["proto-fd-max"] = defaultProtofdmax + 1

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

func rollingRestartClusterByUpdatingNamespaceStorageTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace storage-engine
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["data-in-memory"] = true
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["filesize"] = 2000000000

	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)

	return err
}

func rollingRestartClusterByAddingNamespaceDynamicallyTest(
	k8sClient client.Client, ctx goctx.Context,
	dynamicNs map[string]interface{},
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace list
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = append(nsList, dynamicNs)
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)

	return err
}

func rollingRestartClusterByRemovingNamespaceDynamicallyTest(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	// Change namespace list
	nsList := aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	nsList = nsList[:len(nsList)-1]
	aeroCluster.Spec.AerospikeConfig.Value["namespaces"] = nsList

	err = k8sClient.Update(ctx, aeroCluster)
	if err != nil {
		return err
	}

	err = waitForAerospikeCluster(
		k8sClient, ctx, aeroCluster, int(aeroCluster.Spec.Size), retryInterval,
		getTimeout(aeroCluster.Spec.Size),
	)

	return err
}

func validateAerospikeConfigServiceClusterUpdate(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, updatedKeys []string,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	for _, pod := range aeroCluster.Status.Pods {
		// TODO:
		// We may need to check for all keys in aerospikeConfig in rack
		// but we know that we are changing for service only for now
		host, err := createHost(pod)
		if err != nil {
			return err
		}
		asinfo := info.NewAsInfo(
			log, host, getClientPolicy(aeroCluster, k8sClient),
		)
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
	if err := UpdateClusterImage(aeroCluster, image); err != nil {
		return err
	}
	if err = k8sClient.Update(ctx, aeroCluster); err != nil {
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
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("error getting cluster: %v", err)
	}

	return aeroCluster, nil
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
		//Pods still may exist in terminating state for some time even if CR is deleted. Keeping them breaks some
		//tests which use the same cluster name and run one after another. Thus, waiting pods to disappear.
		allClustersPods, err := getClusterPodList(
			k8sClient, ctx, aeroCluster,
		)
		if err != nil {
			return err
		}
		if existing == nil && len(allClustersPods.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	// Wait for all removed PVCs to be terminated.
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

func updateSTS(
	k8sClient client.Client, ctx goctx.Context,
	sts *appsv1.StatefulSet,
) error {
	err := k8sClient.Update(ctx, sts)
	return err
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
					"network": getNetworkTLSConfig(),
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
	aeroCluster.Spec.Storage = getBasicStorageSpecObject()
	aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
	aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
	return aeroCluster
}

// feature-key file needed
func createAerospikeClusterPost560(
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
					"security": map[string]interface{}{},
					"network":  getNetworkTLSConfig(),
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
	aeroCluster.Spec.Storage = getBasicStorageSpecObject()
	aeroCluster.Spec.Storage.BlockVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
	aeroCluster.Spec.Storage.FileSystemVolumePolicy.InputCascadeDelete = &cascadeDeleteTrue
	return aeroCluster
}

func createDummyRackAwareWithStorageAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	// Will be used in Update also
	aeroCluster := createDummyAerospikeClusterWithoutStorage(clusterNamespacedName, size)
	// This needs to be changed based on setup. update zone, region, nodeName according to setup
	racks := []asdbv1beta1.Rack{
		{
			ID: 1,
		},
	}
	inputStorage := getBasicStorageSpecObject()
	racks[0].InputStorage = &inputStorage
	rackConf := asdbv1beta1.RackConfig{Racks: racks}
	aeroCluster.Spec.RackConfig = rackConf
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

func createDummyAerospikeClusterWithoutStorage(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	return createDummyAerospikeClusterWithRFAndStorage(clusterNamespacedName, size, 1, nil)
}

func createDummyAerospikeClusterWithRF(
	clusterNamespacedName types.NamespacedName, size int32, rf int,
) *asdbv1beta1.AerospikeCluster {
	storage := getBasicStorageSpecObject()
	return createDummyAerospikeClusterWithRFAndStorage(clusterNamespacedName, size, rf, &storage)
}

func createDummyAerospikeClusterWithRFAndStorage(
	clusterNamespacedName types.NamespacedName, size int32, rf int, storage *asdbv1beta1.AerospikeStorageSpec,
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
			Image: latestImage,
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
				AerospikeInitContainerSpec: &asdbv1beta1.
					AerospikeInitContainerSpec{},
			},

			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"proto-fd-max":     defaultProtofdmax,
					},
					"security": map[string]interface{}{},
					"network":  getNetworkConfig(),
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"memory-size":        1000955200,
							"replication-factor": rf,
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
	if storage != nil {
		aeroCluster.Spec.Storage = *storage
	}
	return aeroCluster
}

func createDummyAerospikeCluster(
	clusterNamespacedName types.NamespacedName, size int32,
) *asdbv1beta1.AerospikeCluster {
	return createDummyAerospikeClusterWithRF(clusterNamespacedName, size, 1)
}

func UpdateClusterImage(
	aerocluster *asdbv1beta1.AerospikeCluster, image string,
) error {
	outgoingVersion, err := asdbv1beta1.GetImageVersion(aerocluster.Spec.Image)
	if err != nil {
		return err
	}

	incomingVersion, err := asdbv1beta1.GetImageVersion(image)
	if err != nil {
		return err
	}
	ov, err := asconfig.CompareVersions(outgoingVersion, "5.7.0")
	if err != nil {
		return err
	}
	nv, err := asconfig.CompareVersions(incomingVersion, "5.7.0")

	switch {
	case nv >= 0 && ov >= 0:
		fallthrough
	case nv < 0 && ov < 0:
		aerocluster.Spec.Image = image
		return nil
	case nv >= 0 && ov < 0:
		enableSecurityFlag, err := asdbv1beta1.IsSecurityEnabled(
			outgoingVersion, aerocluster.Spec.AerospikeConfig,
		)
		if err != nil && !errors.Is(err, internalerrors.NotFoundError) {
			return err
		}
		aerocluster.Spec.Image = image
		if enableSecurityFlag {
			securityConfigMap := aerocluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})
			delete(securityConfigMap, "enable-security")
			return nil
		}
		delete(aerocluster.Spec.AerospikeConfig.Value, "security")
	default:
		enableSecurityFlag, err := asdbv1beta1.IsSecurityEnabled(
			outgoingVersion, aerocluster.Spec.AerospikeConfig,
		)
		if err != nil {
			return err
		}
		aerocluster.Spec.Image = image
		if enableSecurityFlag {
			securityConfigMap := aerocluster.Spec.AerospikeConfig.Value["security"].(map[string]interface{})
			securityConfigMap["enable-security"] = true
			return nil
		}
		aerocluster.Spec.AerospikeConfig.Value["security"] = map[string]interface{}{"enable-security": false}
	}
	return nil
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
			Image: latestImage,
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
					"security": map[string]interface{}{},
					"network":  getNetworkTLSConfig(),
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

func getBasicStorageSpecObject() asdbv1beta1.AerospikeStorageSpec {

	storage := asdbv1beta1.AerospikeStorageSpec{
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
	}
	return storage
}
