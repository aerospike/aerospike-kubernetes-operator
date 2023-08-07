package v1

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	v1 "k8s.io/api/core/v1"

	internalerrors "github.com/aerospike/aerospike-kubernetes-operator/errors"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

const (
	// DefaultRackID is the ID for the default rack created when no racks are specified.
	DefaultRackID = 0
	MaxRackID     = 1000000
	MinRackID     = 1

	ServiceTLSPortName = "tls-service"
	ServicePortName    = "service"

	HeartbeatTLSPortName = "tls-heartbeat"
	HeartbeatPortName    = "heartbeat"

	FabricTLSPortName = "tls-fabric"
	FabricPortName    = "fabric"

	InfoPortName = "info"
)

const baseVersion = "4.9.0.3"

const (
	// Namespace keys.
	confKeyNamespace = "namespaces"
	confKeyTLSName   = "tls-name"

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
	AerospikeServerContainerName                   string = "aerospike-server"
	AerospikeInitContainerName                     string = "aerospike-init"
	AerospikeInitContainerRegistryEnvVar           string = "AEROSPIKE_KUBERNETES_INIT_REGISTRY"
	AerospikeInitContainerDefaultRegistry          string = "docker.io"
	AerospikeInitContainerDefaultRegistryNamespace string = "aerospike"
	AerospikeInitContainerDefaultRepoAndTag        string = "aerospike-kubernetes-init:2.0.0"

	AerospikeAppLabel            = "app"
	AerospikeCustomResourceLabel = "aerospike.com/cr"
	AerospikeRackIDLabel         = "aerospike.com/rack-id"
	AerospikeAPIVersionLabel     = "aerospike.com/api-version"
	AerospikeAPIVersion          = "v1"
)

// ContainsString check whether list contains given string
func ContainsString(list []string, ele string) bool {
	for _, listEle := range list {
		if strings.EqualFold(ele, listEle) {
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

func getInitContainerImage(registry string) string {
	return fmt.Sprintf(
		"%s/%s/%s", strings.TrimSuffix(registry, "/"),
		strings.TrimSuffix(AerospikeInitContainerDefaultRegistryNamespace, "/"),
		AerospikeInitContainerDefaultRepoAndTag,
	)
}

func GetAerospikeInitContainerImage(aeroCluster *AerospikeCluster) string {
	// Given in CR
	registry := ""
	if aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		registry = aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistry
	}

	if registry != "" {
		return getInitContainerImage(registry)
	}

	// Given in EnvVar
	registry, found := os.LookupEnv(AerospikeInitContainerRegistryEnvVar)
	if found {
		return getInitContainerImage(registry)
	}

	// Use default
	return getInitContainerImage(AerospikeInitContainerDefaultRegistry)
}

func ClusterNamespacedName(aeroCluster *AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (
	registry string, name string, version string,
) {
	if tag == "" {
		return "", "", ""
	}

	r := regexp.MustCompile(`(?P<registry>[^/]+/)?(?P<image>[^:]+)(?P<version>:.+)?`)
	matches := r.FindStringSubmatch(tag)

	if matches == nil {
		return "", "", ""
	}

	return matches[1], matches[2], strings.TrimPrefix(matches[3], ":")
}

// IsServiceTLSEnabled tells if service is tls enabled.
func IsServiceTLSEnabled(aerospikeConfigSpec *AerospikeConfigSpec) bool {
	aerospikeConfig := aerospikeConfigSpec.Value
	if networkConfInterface, ok := aerospikeConfig[confKeyNetwork]; ok {
		if networkConf, ok := networkConfInterface.(map[string]interface{}); ok {
			if serviceConfInterface, ok := networkConf[confKeyNetworkService]; ok {
				if serviceConf, ok := serviceConfInterface.(map[string]interface{}); ok {
					if _, ok := serviceConf[confKeyTLSName]; ok {
						return true
					}
				}
			}
		}
	}

	return false
}

// IsSecurityEnabled tells if security is enabled in cluster
// TODO: can an invalid map come here
func IsSecurityEnabled(
	version string, aerospikeConfig *AerospikeConfigSpec,
) (bool, error) {
	retval, err := asconfig.CompareVersions(version, "5.7.0")
	if err != nil {
		return false, err
	}

	if retval == -1 {
		return IsAttributeEnabled(
			aerospikeConfig, "security", "enable-security",
		)
	}

	if _, err := GetConfigContext(aerospikeConfig, "security"); err != nil {
		if errors.Is(err, internalerrors.ErrNotFound) {
			return false, nil
		}

		if errors.Is(err, internalerrors.ErrInvalidOrEmpty) && retval >= 0 {
			return true, nil
		}

		return false, err
	}

	return true, nil
}

func IsAttributeEnabled(
	aerospikeConfigSpec *AerospikeConfigSpec, context, key string,
) (bool, error) {
	aerospikeConfig := aerospikeConfigSpec.Value
	if len(aerospikeConfig) == 0 {
		return false, fmt.Errorf("missing aerospike configuration in cluster state")
	}

	confMap, err := GetConfigContext(aerospikeConfigSpec, context)
	if err != nil {
		return false, err
	}

	enabled, err := GetBoolConfig(confMap, key)
	if err != nil {
		return false, fmt.Errorf(
			"invalid aerospike.%s conf. %s", context, err.Error(),
		)
	}

	return enabled, nil
}

func GetConfigContext(
	aerospikeConfigSpec *AerospikeConfigSpec, context string,
) (map[string]interface{}, error) {
	aerospikeConfig := aerospikeConfigSpec.Value
	if len(aerospikeConfig) == 0 {
		return nil, fmt.Errorf("missing aerospike configuration in cluster state")
	}

	if contextConfigMap, ok := aerospikeConfig[context]; ok {
		if validConfigMap, ok := contextConfigMap.(map[string]interface{}); ok {
			return validConfigMap, nil
		}

		return nil, fmt.Errorf(
			"invalid aerospike.%s conf. %w", context,
			internalerrors.ErrInvalidOrEmpty,
		)
	}

	return nil, fmt.Errorf(
		"context %s was %w", context, internalerrors.ErrNotFound,
	)
}

func GetBoolConfig(configMap map[string]interface{}, key string) (bool, error) {
	if enabled, ok := configMap[key]; ok {
		if value, ok := enabled.(bool); ok {
			return value, nil
		}

		return false, fmt.Errorf("%s: not valid", key)
	}

	return false, fmt.Errorf("%s: not present", key)
}

// IsAerospikeNamespacePresent indicates if the namespace is present in aerospikeConfig.
// Assumes the namespace section is validated.
func IsAerospikeNamespacePresent(
	aerospikeConfigSpec AerospikeConfigSpec, namespaceName string,
) bool {
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

func ReadTLSAuthenticateClient(serviceConf map[string]interface{}) (
	[]string, error,
) {
	tlsAuthenticateClientConfig, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		return nil, nil
	}

	switch value := tlsAuthenticateClientConfig.(type) {
	case string:
		return []string{value}, nil
	case []interface{}:
		tlsAuthenticateClientDomains := make([]string, len(value))

		for i := 0; i < len(value); i++ {
			item, ok := value[i].(string)
			if !ok {
				return nil, fmt.Errorf("invalid configuration element")
			}

			tlsAuthenticateClientDomains[i] = item
		}

		return tlsAuthenticateClientDomains, nil
	}

	return nil, fmt.Errorf("invalid configuration")
}

// GetDigestLogFile returns the xdr digest file path if configured.
func GetDigestLogFile(aerospikeConfigSpec AerospikeConfigSpec) (
	*string, error,
) {
	aerospikeConfig := aerospikeConfigSpec.Value

	if xdrConfTmp, ok := aerospikeConfig[confKeyXdr]; ok {
		xdrConf, ok := xdrConfTmp.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(
				"aerospikeConfig.xdr not a valid map %v",
				aerospikeConfig[confKeyXdr],
			)
		}

		dgLog, ok := xdrConf[confKeyXdrDlogPath]
		if !ok {
			return nil, fmt.Errorf(
				"%s is missing in aerospikeConfig.xdr %v", confKeyXdrDlogPath,
				xdrConf,
			)
		}

		if _, ok := dgLog.(string); !ok {
			return nil, fmt.Errorf(
				"%s is not a valid string in aerospikeConfig.xdr %v",
				confKeyXdrDlogPath, xdrConf,
			)
		}

		// "/opt/aerospike/xdr/digestlog 100G"
		if len(strings.Fields(dgLog.(string))) != 2 {
			return nil, fmt.Errorf(
				"%s is not in valid format (/opt/aerospike/xdr/digestlog 100G) in aerospikeConfig.xdr %v",
				confKeyXdrDlogPath, xdrConf,
			)
		}

		return &strings.Fields(dgLog.(string))[0], nil
	}

	return nil, fmt.Errorf("xdr not configured")
}

func GetServiceTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int) {
	return GetTLSNameAndPort(aeroConf, confKeyService)
}

func GetHeartbeatTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int) {
	return GetTLSNameAndPort(aeroConf, confKeyNetworkHeartbeat)
}

func GetFabricTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int) {
	return GetTLSNameAndPort(aeroConf, confKeyNetworkFabric)
}

func GetTLSNameAndPort(
	aeroConf *AerospikeConfigSpec, connectionType string,
) (tlsName string, port *int) {
	if networkConfTmp, ok := aeroConf.Value[confKeyNetwork]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		serviceConf := networkConf[connectionType].(map[string]interface{})

		if tlsName, ok := serviceConf["tls-name"]; ok {
			if tlsPort, portConfigured := serviceConf["tls-port"]; portConfigured {
				intPort := int(tlsPort.(float64))
				return tlsName.(string), &intPort
			}

			return tlsName.(string), nil
		}
	}

	return "", nil
}

func GetServicePort(aeroConf *AerospikeConfigSpec) *int {
	return GetPortFromConfig(aeroConf, confKeyNetworkService, "port")
}

func GetHeartbeatPort(aeroConf *AerospikeConfigSpec) *int {
	return GetPortFromConfig(aeroConf, confKeyNetworkHeartbeat, "port")
}

func GetFabricPort(aeroConf *AerospikeConfigSpec) *int {
	return GetPortFromConfig(aeroConf, confKeyNetworkFabric, "port")
}

func GetPortFromConfig(
	aeroConf *AerospikeConfigSpec, connectionType string, paramName string,
) *int {
	if networkConf, ok := aeroConf.Value[confKeyNetwork]; ok {
		if connectionConfig, ok := networkConf.(map[string]interface{})[connectionType]; ok {
			if port, ok := connectionConfig.(map[string]interface{})[paramName]; ok {
				intPort := int(port.(float64))
				return &intPort
			}
		}
	}

	return nil
}

// GetIntType typecasts the numeric value to the supported type
func GetIntType(value interface{}) (int, error) {
	switch val := value.(type) {
	case int64:
		return int(val), nil
	case int:
		return val, nil
	case float64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("value %v not valid int, int64 or float64", val)
	}
}

// GetMigrateFillDelay returns the migrate-fill-delay from the Aerospike configuration
func GetMigrateFillDelay(asConfig *AerospikeConfigSpec) (int, error) {
	serviceConfig := asConfig.Value["service"].(map[string]interface{})

	fillDelayIFace, exists := serviceConfig["migrate-fill-delay"]
	if !exists {
		return 0, nil
	}

	fillDelay, err := GetIntType(fillDelayIFace)
	if err != nil {
		return 0, fmt.Errorf("migrate-fill-delay %v", err)
	}

	return fillDelay, nil
}

// IsClusterSCEnabled returns true if cluster has a sc namespace
func IsClusterSCEnabled(aeroCluster *AerospikeCluster) bool {
	// Look inside only 1st rack. SC namespaces should be same across all the racks
	rack := aeroCluster.Spec.RackConfig.Racks[0]

	nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})
	for _, nsConfInterface := range nsList {
		isEnabled := isNSSCEnabled(nsConfInterface.(map[string]interface{}))
		if isEnabled {
			return true
		}
	}

	return false
}

func getContainerNames(containers []v1.Container) []string {
	containerNames := make([]string, 0, len(containers))

	for idx := range containers {
		containerNames = append(containerNames, containers[idx].Name)
	}

	return containerNames
}
