package utils

import (
	"fmt"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
)

const (
	// Namespace keys.
	confKeyNamespace     = "namespaces"
	confKeyMemorySize    = "memory-size"
	confKeyStorageEngine = "storage-engine"
	confKeyFilesize      = "filesize"
	confKeyDevice        = "devices"
	confKeyFile          = "files"
	confKeyNetwork       = "network"
	confKeyTLS           = "tls"
	confKeySecurity      = "security"

	// XDR keys.
	confKeyXdr         = "xdr"
	confKeyXdrDlogPath = "xdr-digestlog-path"

	// Service section keys.
	confKeyService       = "service"
	confKeyWorkDirectory = "work-directory"

	// Defaults.
	defaultWorkDirectory = "/opt/aerospike"
)

// IsTLS tells if cluster is tls enabled
func IsTLS(aerospikeConfig aerospikev1alpha1.Values) bool {
	if confInterface, ok := aerospikeConfig[confKeyNetwork]; ok {
		if networkConf, ok := confInterface.(map[string]interface{}); ok {
			if _, ok := networkConf[confKeyTLS]; ok {
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
	if confInterface, ok := aerospikeConfig[confKeySecurity]; ok {
		if secConf, ok := confInterface.(map[string]interface{}); ok {
			if enabled, ok := secConf["enable-security"]; ok {
				if _, ok := enabled.(bool); ok {
					return enabled.(bool), nil
				}
				return false, fmt.Errorf("Invalid aerospike.security conf. enable-security not valid %v", confInterface)

			}
			return false, fmt.Errorf("Invalid aerospike.security conf. enable-security key not present %v", confInterface)
		}
		return false, fmt.Errorf("Invalid aerospike.security conf. Not a valid map %v", confInterface)
	}
	return false, nil
}

// ListAerospikeNamespaces returns the list of namespaecs in the input aerospikeConfig.
// Assumes the namespace section is validated.
func ListAerospikeNamespaces(aerospikeConfig aerospikev1alpha1.Values) ([]string, error) {
	namespaces := make([]string, 5)
	// Get namespace config.
	if confs, ok := aerospikeConfig[confKeyNamespace].([]interface{}); ok {
		for _, nsConf := range confs {
			namespaceConf, ok := nsConf.(map[string]interface{})

			if !ok {
				// Should never happen
				return nil, fmt.Errorf("Invalid namespaces config: %v", nsConf)
			}
			namespaces = append(namespaces, namespaceConf["name"].(string))
		}
	}
	return namespaces, nil
}

// IsAerospikeNamespacePresent indicates if the namespace is present in aerospikeConfig.
// Assumes the namespace section is validated.
func IsAerospikeNamespacePresent(aerospikeConfig aerospikev1alpha1.Values, namespaceName string) bool {
	// Get namespace config.
	if confs, ok := aerospikeConfig[confKeyNamespace].([]interface{}); ok {
		for _, nsConf := range confs {
			namespaceConf, ok := nsConf.(map[string]interface{})

			if !ok {
				// Should never happen
				return false
			}

			if namespaceConf["name"] == namespaceName {
				return true
			}
		}
	}
	return false
}

// GetWorkDirectory returns the Aerospike work directory to use for aerospikeConfig.
func GetWorkDirectory(aerospikeConfig aerospikev1alpha1.Values) string {
	// Get namespace config.
	serviceTmp := aerospikeConfig[confKeyService]

	if serviceTmp != nil {
		serviceConf := serviceTmp.(map[string]interface{})
		workDir, ok := serviceConf[confKeyWorkDirectory]
		if ok {
			return workDir.(string)
		}
	}

	return defaultWorkDirectory
}

// IsXdrEnabled indicates if XDR is enabled in aerospikeConfig.
func IsXdrEnabled(aerospikeConfig aerospikev1alpha1.Values) bool {
	xdrConf := aerospikeConfig[confKeyXdr]
	return xdrConf != nil
}

// GetDigestLogFile returns the xdr digest file path if configured.
func GetDigestLogFile(aerospikeConfig aerospikev1alpha1.Values) (*string, error) {
	if xdrConfTmp, ok := aerospikeConfig[confKeyXdr]; ok {
		xdrConf, ok := xdrConfTmp.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("aerospikeConfig.xdr not a valid map %v", aerospikeConfig[confKeyXdr])
		}
		dglog, ok := xdrConf[confKeyXdrDlogPath]
		if !ok {
			return nil, fmt.Errorf("%s is missing in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}
		if _, ok := dglog.(string); !ok {
			return nil, fmt.Errorf("%s is not a valid string in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}

		// "/opt/aerospike/xdr/digestlog 100G"
		if len(strings.Fields(dglog.(string))) != 2 {
			return nil, fmt.Errorf("%s is not in valid format (/opt/aerospike/xdr/digestlog 100G) in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}

		return &strings.Fields(dglog.(string))[0], nil
	}

	return nil, fmt.Errorf("xdr not configured")
}
