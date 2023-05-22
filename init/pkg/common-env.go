package pkg

import (
	goctx "context"
	"os"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var (
	myPodTLSEnabled = os.Getenv("MY_POD_TLS_ENABLED")
	clusterName     = os.Getenv("MY_POD_CLUSTER_NAME")
)

type InitParams struct {
	k8sClient   client.Client
	aeroCluster *asdbv1.AerospikeCluster
	rack        *asdbv1.Rack
	networkInfo *networkInfo
	podName     string
	namespace   string
	rackID      string
	nodeID      string
	workDir     string
	logger      logr.Logger
}

func PopulateInitParams(ctx goctx.Context) (*InitParams, error) {
	var (
		k8sClient client.Client
		cfg       = ctrl.GetConfigOrDie()
		scheme    = runtime.NewScheme()
	)

	logger := ctrl.Log.WithName("init-setup")
	opts := zap.Options{
		Development: true,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}

	if err := asdbv1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	logger.Info("Gathering all the required info from environment variables, k8s cluster and AerospikeCluster")

	var err error

	if k8sClient, err = client.New(
		cfg, client.Options{Scheme: scheme},
	); err != nil {
		return nil, err
	}

	podName := os.Getenv("MY_POD_NAME")
	namespace := os.Getenv("MY_POD_NAMESPACE")
	clusterNamespacedName := getNamespacedName(clusterName, namespace)

	aeroCluster, err := getCluster(ctx, k8sClient, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	rack, err := getRack(logger, podName, aeroCluster)
	if err != nil {
		return nil, err
	}

	nodeID, err := getNodeIDFromPodName(podName)
	if err != nil {
		return nil, err
	}

	workDir := asdbv1.GetWorkDirectory(rack.AerospikeConfig)
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

	initParams := InitParams{
		aeroCluster: aeroCluster,
		rack:        rack,
		k8sClient:   k8sClient,
		podName:     podName,
		namespace:   namespace,
		rackID:      strconv.Itoa(rack.ID),
		nodeID:      nodeID,
		workDir:     workDir,
		logger:      logger,
	}

	if err := initParams.setNetworkInfo(ctx); err != nil {
		return nil, err
	}

	logger.Info("Gathered all the required info")

	return &initParams, nil
}
