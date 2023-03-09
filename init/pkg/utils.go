package pkg

import (
	goctx "context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

type initializeTemplateInput struct {
	NetworkPolicy    asdbv1beta1.AerospikeNetworkPolicy
	WorkDir          string
	FabricPort       int32
	PodPort          int32
	PodTLSPort       int32
	HeartBeatPort    int32
	HeartBeatTLSPort int32
	FabricTLSPort    int32
	MultiPodPerHost  bool
	HostNetwork      bool
}

func getNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func getCluster(ctx goctx.Context, k8sClient client.Client,
	clusterNamespacedName types.NamespacedName) (*asdbv1beta1.AerospikeCluster, error) {
	logrus.Info("Get aerospike cluster ", "cluster-name=", clusterNamespacedName)

	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

func getBaseConfData(aeroCluster *asdbv1beta1.AerospikeCluster, rack *asdbv1beta1.Rack) initializeTemplateInput {
	var initTemplateInput initializeTemplateInput

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

	asConfig := aeroCluster.Spec.AerospikeConfig

	var serviceTLSPortParam int32
	if _, serviceTLSPort := asdbv1beta1.GetServiceTLSNameAndPort(asConfig); serviceTLSPort != nil {
		serviceTLSPortParam = int32(*serviceTLSPort)
	}

	var servicePortParam int32
	if servicePort := asdbv1beta1.GetServicePort(asConfig); servicePort != nil {
		servicePortParam = int32(*servicePort)
	}

	var hbTLSPortParam int32
	if _, hbTLSPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(asConfig); hbTLSPort != nil {
		hbTLSPortParam = int32(*hbTLSPort)
	}

	var hbPortParam int32
	if hbPort := asdbv1beta1.GetHeartbeatPort(asConfig); hbPort != nil {
		hbPortParam = int32(*hbPort)
	}

	var fabricTLSPortParam int32
	if _, fabricTLSPort := asdbv1beta1.GetFabricTLSNameAndPort(asConfig); fabricTLSPort != nil {
		fabricTLSPortParam = int32(*fabricTLSPort)
	}

	var fabricPortParam int32
	if fabricPort := asdbv1beta1.GetFabricPort(asConfig); fabricPort != nil {
		fabricPortParam = int32(*fabricPort)
	}

	initTemplateInput = initializeTemplateInput{
		WorkDir:          workDir,
		MultiPodPerHost:  aeroCluster.Spec.PodSpec.MultiPodPerHost,
		NetworkPolicy:    aeroCluster.Spec.AerospikeNetworkPolicy,
		PodPort:          servicePortParam,
		PodTLSPort:       serviceTLSPortParam,
		HeartBeatPort:    hbPortParam,
		HeartBeatTLSPort: hbTLSPortParam,
		FabricPort:       fabricPortParam,
		FabricTLSPort:    fabricTLSPortParam,
		HostNetwork:      aeroCluster.Spec.PodSpec.HostNetwork,
	}

	return initTemplateInput
}

func GetRackIDNodeIDFromPodName(podName string) (rackStr, nodeID string, err error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("failed to get rackID from podName %s", podName)
	}
	// Podname format stsname-ordinal
	// stsname ==> clustername-rackid
	rackStr = parts[len(parts)-2]
	nodeID = rackStr + "a" + parts[len(parts)-1]

	return rackStr, nodeID, nil
}

func getRack(podName string, aeroCluster *asdbv1beta1.AerospikeCluster) (*asdbv1beta1.Rack, error) {
	res := strings.Split(podName, "-")

	rackID, err := strconv.Atoi(res[len(res)-2])
	if err != nil {
		return nil, err
	}

	logrus.Info("Checking for rack in rackConfig ", "rack-id=", rackID)

	racks := aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		rack := &racks[idx]
		if rack.ID == rackID {
			return rack, nil
		}
	}

	return nil, fmt.Errorf("rack with rack-id %d not found", rackID)
}

func getClusterName(name string) string {
	res := strings.Split(name, "-")
	res1 := res[0 : len(res)-2]

	return strings.Join(res1, "-")
}

func (initp *InitParams) makeWorkDir() error {
	if initp.initTemplateInput.WorkDir != "" {
		defaultWorkDir := filepath.Join("workdir", "filesystem-volumes", initp.initTemplateInput.WorkDir)

		requiredDirs := [3]string{"smd", "usr/udf/lua", "xdr"}
		for _, d := range requiredDirs {
			toCreate := filepath.Join(defaultWorkDir, d)
			logrus.Info("Creating directory ", toCreate)

			if err := os.MkdirAll(toCreate, 0644); err != nil { //nolint:gocritic // file permission
				return err
			}
		}
	}

	return nil
}
