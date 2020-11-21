package configmap

import (
	"fmt"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	log "github.com/inconshreveable/log15"
)

var pkglog = log.New(log.Ctx{"module": "lib.asconfig"})

// CreateConfigMapData create configMap data
func CreateConfigMapData(aeroCluster *aerospikev1alpha1.AerospikeCluster, rack aerospikev1alpha1.Rack) (map[string]string, error) {
	// Add config template
	temp, err := buildConfigTemplate(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("Failed to build config template: %v", err)
	}
	confData, err := getBaseConfData(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("Failed to build config template: %v", err)
	}

	confData["aerospike.template.conf"] = temp

	return confData, nil
}

func buildConfigTemplate(aeroCluster *aerospikev1alpha1.AerospikeCluster, rack aerospikev1alpha1.Rack) (string, error) {
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
