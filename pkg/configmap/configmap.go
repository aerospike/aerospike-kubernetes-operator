package configmap

import (
	"encoding/json"
	"fmt"
	"strings"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

var pkglog = ctrl.Log.WithName("lib.asconfig")

const (
	AerospikeTemplateConfFileName = "aerospike.template.conf"
	NetworkPolicyHashFileName     = "networkPolicyHash"
	PodSpecHashFileName           = "podSpecHash"
	AerospikeConfHashFileName     = "aerospikeConfHash"
)

// CreateConfigMapData create configMap data
func CreateConfigMapData(aeroCluster *asdbv1alpha1.AerospikeCluster, rack asdbv1alpha1.Rack) (map[string]string, error) {
	// Add config template
	confTemp, err := BuildConfigTemplate(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("Failed to build config template: %v", err)
	}

	// Add conf file
	confData, err := getBaseConfData(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("Failed to build config template: %v", err)
	}
	confData[AerospikeTemplateConfFileName] = confTemp

	// Add conf hash
	confHash, err := utils.GetHash(confTemp)
	if err != nil {
		return nil, err
	}
	confData[AerospikeConfHashFileName] = confHash

	// Add networkPolicy hash
	policy := aeroCluster.Spec.AerospikeNetworkPolicy
	policyStr, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	policyHash, err := utils.GetHash(string(policyStr))
	if err != nil {
		return nil, err
	}
	confData[NetworkPolicyHashFileName] = policyHash

	// Add podSpec hash
	podSpec := aeroCluster.Spec.PodSpec
	podSpecStr, err := json.Marshal(podSpec)
	if err != nil {
		return nil, err
	}
	podSpecHash, err := utils.GetHash(string(podSpecStr))
	if err != nil {
		return nil, err
	}
	confData[PodSpecHashFileName] = podSpecHash

	return confData, nil
}

func BuildConfigTemplate(aeroCluster *asdbv1alpha1.AerospikeCluster, rack asdbv1alpha1.Rack) (string, error) {
	log := pkglog.WithValues("aerospikecluster", utils.ClusterNamespacedName(aeroCluster))

	version := strings.Split(aeroCluster.Spec.Image, ":")

	configMap := rack.AerospikeConfig.Value
	log.V(1).Info("AerospikeConfig", "config", configMap, "image", aeroCluster.Spec.Image)

	asConf, err := asconfig.NewMapAsConfig(version[1], configMap)
	if err != nil {
		return "", fmt.Errorf("Failed to load config map by lib: %v", err)
	}

	// No need for asConf version validation, it's already validated in admission webhook

	confFile := asConf.ToConfFile()
	log.V(1).Info("AerospikeConfig", "conf", confFile)

	return confFile, nil
}
