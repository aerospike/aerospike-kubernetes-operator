package pkg

import (
	goctx "context"
	"os"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

type InitParams struct {
	k8sClient   client.Client
	aeroCluster *asdbv1beta1.AerospikeCluster
	networkInfo *networkInfo
	podName     string
	namespace   string
	rackID      string
	nodeID      string
	workDir     string
}

func PopulateInitParams() (*InitParams, error) {
	var (
		k8sClient client.Client
		cfg       = ctrl.GetConfigOrDie()
		scheme    = runtime.NewScheme()
	)

	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}

	if err := asdbv1beta1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	var err error

	if k8sClient, err = client.New(
		cfg, client.Options{Scheme: scheme},
	); err != nil {
		return nil, err
	}

	podName := os.Getenv("MY_POD_NAME")
	namespace := os.Getenv("MY_POD_NAMESPACE")
	clusterName := os.Getenv("MY_POD_CLUSTER_NAME")
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	aeroCluster, err := getCluster(goctx.TODO(), k8sClient, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return nil, err
	}

	networkInfo, err := getNetworkInfo(k8sClient, podName, aeroCluster)
	if err != nil {
		return nil, err
	}

	nodeID, err := GetNodeIDFromPodName(podName)
	if err != nil {
		return nil, err
	}

	workDir := asdbv1beta1.GetWorkDirectory(rack.AerospikeConfig)
	volume := rack.Storage.GetVolumeForAerospikePath(workDir)

	if volume != nil {
		// Init container mounts all volumes by name. Update workdir to reflect that path.
		// For example
		// volume name: aerospike-workdir
		// path: /opt/aerospike
		// config-workdir: /opt/aerospike/workdir/
		// workDir = aerospike-workdir/workdir
		workDir = "/" + volume.Name + "/" + strings.TrimPrefix(
			workDir, volume.Aerospike.Path,
		)
	}

	initp := InitParams{
		aeroCluster: aeroCluster,
		k8sClient:   k8sClient,
		podName:     podName,
		namespace:   namespace,
		rackID:      strconv.Itoa(rack.ID),
		nodeID:      nodeID,
		networkInfo: networkInfo,
		workDir:     workDir,
	}

	return &initp, nil
}
