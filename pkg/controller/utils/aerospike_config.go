package utils

import (
	"fmt"

	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
)

const (
	ConfKeyNamespace = "namespace"

	ConfKeyMemorySize = "memory-size"

	ConfKeyStorageEngine = "storage-engine"
	ConfKeyFilesize      = "filesize"
	ConfKeyDevice        = "device"
	ConfKeyFile          = "file"

	ConfKeyNetwork = "network"
	ConfKeyTLS     = "tls"

	ConfKeySecurity = "security"
)

// IsTLS tells if cluster is tls enabled
func IsTLS(aerospikeConfig aerospikev1alpha1.Values) bool {
	if confInterface, ok := aerospikeConfig[ConfKeyNetwork]; ok {
		if networkConf, ok := confInterface.(map[string]interface{}); ok {
			if _, ok := networkConf[ConfKeyTLS]; ok {
				return true
			}
		}
	}

	return false
}

// IsSecurityEnabled tells if security is enabled in cluster
// TODO: can a invalid map come here
func IsSecurityEnabled(aerospikeConfig aerospikev1alpha1.Values) (bool, error) {
	// security conf
	if confInterface, ok := aerospikeConfig[ConfKeySecurity]; ok {
		if secConf, ok := confInterface.(map[string]interface{}); ok {
			if enabled, ok := secConf["enable-security"]; ok {
				if _, ok := enabled.(bool); ok {
					return enabled.(bool), nil
				} else {
					return false, fmt.Errorf("Invalid aerospike.security conf. enable-security not valid %v", confInterface)
				}
			} else {
				return false, fmt.Errorf("Invalid aerospike.security conf. enable-security key not present %v", confInterface)
			}
		} else {
			return false, fmt.Errorf("Invalid aerospike.security conf. Not a valid map %v", confInterface)
		}
	}
	return false, nil
}
