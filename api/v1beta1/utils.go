package v1beta1

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// AerospikeClusterValidationWebhookPath  validation webhook path
	AerospikeClusterValidationWebhookPath = "/admission/reviews/aerospikeclusters/validating"
	// AerospikeClusterMutationWebhookPath mutation webhook path
	AerospikeClusterMutationWebhookPath = "/admission/reviews/aerospikeclusters/mutating"
)

const (
	// DefaultRackID is the ID for the default rack created when no racks are specified.
	DefaultRackID = 0
	MaxRackID     = 1000000
	MinRackID     = 1

	ServiceTLSPort     = 4333
	ServiceTLSPortName = "svc-tls-port"
	ServicePort        = 3000
	ServicePortName    = "service"

	HeartbeatTLSPort     = 3012
	HeartbeatTLSPortName = "hb-tls-port"
	HeartbeatPort        = 3002
	HeartbeatPortName    = "heartbeat"

	FabricTLSPort     = 3011
	FabricTLSPortName = "fb-tls-port"
	FabricPort        = 3001
	FabricPortName    = "fabric"

	InfoPort     = 3003
	InfoPortName = "info"
)

const baseVersion = "4.9.0.3"

// ContainsString check whether list contains given string
func ContainsString(list []string, ele string) bool {
	for _, listEle := range list {
		if ele == listEle {
			return true
		}
	}
	return false
}

// GetWorkDirectory returns the Aerospike work directory to use for aerospikeConfig.
func GetWorkDirectory(aerospikeConfigSpec AerospikeConfigSpec) string {
	// Get namespace config.
	aerospikeConfig := aerospikeConfigSpec.Value

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

const (
	// Namespace keys.
	confKeyNamespace     = "namespaces"
	confKeyMemorySize    = "memory-size"
	confKeyStorageEngine = "storage-engine"
	confKeyFilesize      = "filesize"
	confKeyDevice        = "devices"
	confKeyFile          = "files"
	confKeyTLS           = "tls"
	confKeySecurity      = "security"

	// Network section keys.
	confKeyNetwork          = "network"
	confKeyNetworkService   = "service"
	confKeyNetworkHeartbeat = "heartbeat"
	confKeyNetworkFabric    = "fabric"

	// XDR keys.
	confKeyXdr         = "xdr"
	confKeyXdrDlogPath = "xdr-digestlog-path"

	// Service section keys.
	confKeyService       = "service"
	confKeyWorkDirectory = "work-directory"

	// Defaults.
	defaultWorkDirectory = "/opt/aerospike"
)
const (
	AerospikeServerContainerName      string = "aerospike-server"
	AerospikeServerInitContainerName  string = "aerospike-init"
	AerospikeServerInitContainerImage string = "aerospike/aerospike-kubernetes-init:0.0.14"
)

func ClusterNamespacedName(aeroCluster *AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (registry string, name string, version string) {
	if tag == "" {
		return "", "", ""
	}
	r := regexp.MustCompile(`(?P<registry>[^/]+/)?(?P<image>[^:]+)(?P<version>:.+)?`)
	matches := r.FindStringSubmatch(tag)
	return matches[1], matches[2], strings.TrimPrefix(matches[3], ":")
}

// IsTLS tells if cluster is tls enabled
func IsTLS(aerospikeConfigSpec *AerospikeConfigSpec) bool {
	aerospikeConfig := aerospikeConfigSpec.Value
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
func IsSecurityEnabled(aerospikeConfigSpec *AerospikeConfigSpec) (bool, error) {
	aerospikeConfig := aerospikeConfigSpec.Value
	if len(aerospikeConfig) == 0 {
		return false, fmt.Errorf("missing aerospike configuration in cluster state")
	}
	// security conf
	if confInterface, ok := aerospikeConfig[confKeySecurity]; ok {
		if secConf, ok := confInterface.(map[string]interface{}); ok {
			if enabled, ok := secConf["enable-security"]; ok {
				if _, ok := enabled.(bool); ok {
					return enabled.(bool), nil
				}
				return false, fmt.Errorf("invalid aerospike.security conf. enable-security not valid %v", confInterface)

			}
			return false, fmt.Errorf("invalid aerospike.security conf. enable-security key not present %v", confInterface)
		}
		return false, fmt.Errorf("invalid aerospike.security conf. Not a valid map %v", confInterface)
	}
	return false, nil
}

// ListAerospikeNamespaces returns the list of namespaecs in the input aerospikeConfig.
// Assumes the namespace section is validated.
func ListAerospikeNamespaces(aerospikeConfigSpec AerospikeConfigSpec) ([]string, error) {
	aerospikeConfig := aerospikeConfigSpec.Value

	namespaces := make([]string, 5)
	// Get namespace config.
	if confs, ok := aerospikeConfig[confKeyNamespace].([]interface{}); ok {
		for _, nsConf := range confs {
			namespaceConf, ok := nsConf.(map[string]interface{})

			if !ok {
				// Should never happen
				return nil, fmt.Errorf("invalid namespaces config: %v", nsConf)
			}
			namespaces = append(namespaces, namespaceConf["name"].(string))
		}
	}
	return namespaces, nil
}

// IsAerospikeNamespacePresent indicates if the namespace is present in aerospikeConfig.
// Assumes the namespace section is validated.
func IsAerospikeNamespacePresent(aerospikeConfigSpec AerospikeConfigSpec, namespaceName string) bool {
	aerospikeConfig := aerospikeConfigSpec.Value

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

// IsXdrEnabled indicates if XDR is enabled in aerospikeConfig.
func IsXdrEnabled(aerospikeConfigSpec AerospikeConfigSpec) bool {
	aerospikeConfig := aerospikeConfigSpec.Value

	xdrConf := aerospikeConfig[confKeyXdr]
	return xdrConf != nil
}

func readClientNamesFromConfig(serviceConf map[string]interface{}) ([]string, error) {
	var result []string
	clientNames, exists := serviceConf["tls-authenticate-client"]
	if !exists {
		return result, nil
	}
	switch clientNames.(type) {
	case string:
		result = append(result, clientNames.(string))
	case bool:
		result = append(result, strconv.FormatBool(clientNames.(bool)))
	case []interface{}:
		for _, name := range clientNames.([]interface{}) {
			if nameStr, ok := name.(string); ok {
				result = append(result, nameStr)
			} else if nameBool, ok := name.(bool); ok {
				result = append(result, strconv.FormatBool(nameBool))
			} else {
				return result, fmt.Errorf("tls-authenticate-client value \"%v\" has a wrong type (%t)", name, name)
			}
		}
	default:
		return result, fmt.Errorf("wrong type (%T) for tls-authenticate-client (%v)", clientNames, clientNames)
	}
	return result, nil
}

func isClientCertNameValidationEnabled(clientNames []string) bool {
	for _, clientName := range clientNames {
		if strings.EqualFold(clientName, "false") || strings.EqualFold(clientName, "any") {
			return false
		}
	}
	// if empty the "any" is used by default what mean client name validation disabled.
	return len(clientNames) > 0
}

// GetDigestLogFile returns the xdr digest file path if configured.
func GetDigestLogFile(aerospikeConfigSpec AerospikeConfigSpec) (*string, error) {
	aerospikeConfig := aerospikeConfigSpec.Value

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

func GetServiceTLSNameAndPort(aeroConf *AerospikeConfigSpec) (string, int) {
	if networkConfTmp, ok := aeroConf.Value[confKeyNetwork]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		serviceConf := networkConf[confKeyService].(map[string]interface{})
		if tlsName, ok := serviceConf["tls-name"]; ok {
			if tlsPort, portConfigured := serviceConf["tls-port"]; portConfigured {
				return tlsName.(string), int(tlsPort.(float64))
			} else {
				return tlsName.(string), ServiceTLSPort
			}
		}
	}
	return "", 0
}

func GetServicePort(aeroConf *AerospikeConfigSpec) int {
	return GetPortFromConfig(aeroConf, confKeyNetworkService, "port", ServicePort)
}

func GetHeartbeatPort(aeroConf *AerospikeConfigSpec) int {
	return GetPortFromConfig(aeroConf, confKeyNetworkHeartbeat, "port", HeartbeatPort)
}

func GetHeartbeatTLSPort(aeroConf *AerospikeConfigSpec) int {
	return GetPortFromConfig(aeroConf, confKeyNetworkHeartbeat, "tls-port", HeartbeatTLSPort)
}

func GetFabricPort(aeroConf *AerospikeConfigSpec) int {
	return GetPortFromConfig(aeroConf, confKeyNetworkFabric, "port", FabricPort)
}

func GetFabricTLSPort(aeroConf *AerospikeConfigSpec) int {
	return GetPortFromConfig(aeroConf, confKeyNetworkFabric, "tls-port", FabricTLSPort)
}

func GetPortFromConfig(aeroConf *AerospikeConfigSpec, connectionType string, paramName string, defaultValue int) int {
	if networkConf, ok := aeroConf.Value[confKeyNetwork]; ok {
		if connectionConfig, ok := networkConf.(map[string]interface{})[connectionType]; ok {
			if port, ok := connectionConfig.(map[string]interface{})[paramName]; ok {
				return int(port.(float64))
			}
		}
	}
	return defaultValue
}
