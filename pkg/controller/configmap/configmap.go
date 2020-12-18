package configmap

import (
	"encoding/json"
	"fmt"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	log "github.com/inconshreveable/log15"
)

var pkglog = log.New(log.Ctx{"module": "lib.asconfig"})

const (
	AerospikeTemplateConfFileName = "aerospike.template.conf"
	NetworkPolicyHashKeyFileName  = "networkPolicyHash"
	AerospikeConfHashFileName     = "aerospikeConfHash"
)

// CreateConfigMapData create configMap data
func CreateConfigMapData(aeroCluster *aerospikev1alpha1.AerospikeCluster, rack aerospikev1alpha1.Rack) (map[string]string, error) {
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
	confData[NetworkPolicyHashKeyFileName] = policyHash

	return confData, nil
}

func BuildConfigTemplate(aeroCluster *aerospikev1alpha1.AerospikeCluster, rack aerospikev1alpha1.Rack) (string, error) {
	version := strings.Split(aeroCluster.Spec.Image, ":")

	config := rack.AerospikeConfig

	pkglog.Debug("AerospikeConfig", log.Ctx{"config": config, "image": aeroCluster.Spec.Image})

	asConf, err := asconfig.NewMapAsConfig(version[1], config)
	if err != nil {
		return "", fmt.Errorf("Failed to load config map by lib: %v", err)
	}

	// No need for asConf version validation, it's already validated in admission webhook

	confFile := asConf.ToConfFile()
	pkglog.Debug("AerospikeConfig", log.Ctx{"conf": confFile})

	return confFile, nil
}

// func writeLogContext(buf *bytes.Buffer, conf Conf, indent int) {
// 	var keys []string
// 	for k := range conf {
// 		keys = append(keys, k)
// 	}

// 	sort.Strings(keys)

// 	for _, context := range keys {
// 		if context == "name" {
// 			// ignore generated field
// 			continue
// 		}
// 		writeField(buf, "context "+context, conf[context].(string), indent)
// 	}
// }
