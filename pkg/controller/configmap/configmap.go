package configmap

import (
	"fmt"
	"strings"

	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/citrusleaf/aerospike-management-lib/asconfig"
	log "github.com/inconshreveable/log15"
)

var pkglog = log.New(log.Ctx{"module": "lib.asconfig"})

// CreateConfigMapData create configMap data
func CreateConfigMapData(aeroCluster *aerospikev1alpha1.AerospikeCluster) (map[string]string, error) {
	// Add config template
	temp, err := buildConfigTemplate(aeroCluster)
	if err != nil {
		return nil, fmt.Errorf("Failed to build config template: %v", err)
	}
	confData["aerospike.template.conf"] = temp

	return confData, nil
}

func buildConfigTemplate(aeroCluster *aerospikev1alpha1.AerospikeCluster) (string, error) {
	version := strings.Split(aeroCluster.Spec.Build, ":")

	// Check if passed aerospikeConfig is valid or not
	config := aeroCluster.Spec.AerospikeConfig
	pkglog.Debug("AerospikeConfig", log.Ctx{"config": config, "build": aeroCluster.Spec.Build})

	asConf, err := asconfig.NewMapAsConfig(version[1], config)
	if err != nil {
		return "", fmt.Errorf("Failed to load config map by lib: %v", err)
	}
	valid, validationErr, err := asConf.IsValid(version[1])
	if !valid {
		for _, e := range validationErr {
			pkglog.Info("validation failed", log.Ctx{"err": *e})
		}
		return "", fmt.Errorf("generated config not valid for version %s: %v", version, err)
	}
	confFile := asConf.ToConfFile()
	pkglog.Debug("AerospikeConfig", log.Ctx{"conf": confFile})

	return confFile, nil
}
