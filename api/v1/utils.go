package v1

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	internalerrors "github.com/aerospike/aerospike-kubernetes-operator/v4/errors"
)

var versionRegex = regexp.MustCompile(`(\d+(\.\d+)+)`)

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

	AdminTLSPortName = "tls-admin"
	AdminPortName    = "admin"

	InfoPortName = "info"
)

const (
	// Namespace keys.
	ConfKeyNamespace     = "namespaces"
	ConfKeyStorageEngine = "storage-engine"

	// Network section keys.
	ConfKeyNetwork          = "network"
	ConfKeyNetworkService   = "service"
	ConfKeyNetworkHeartbeat = "heartbeat"
	ConfKeyNetworkFabric    = "fabric"
	ConfKeyNetworkAdmin     = "admin"
	ConfKeyNetworkInfo      = "info"

	// Ports and TLS keys.
	ConfKeyTLSName = "tls-name"
	ConfKeyTLSPort = "tls-port"
	ConfKeyPort    = "port"

	// XDR keys.
	confKeyXdr         = "xdr"
	confKeyXdrDlogPath = "xdr-digestlog-path"

	// Security keys.
	ConfKeySecurity                    = "security"
	confKeySecurityDefaultPasswordFile = "default-password-file"

	// Service section keys.
	ConfKeyService       = "service"
	confKeyWorkDirectory = "work-directory"

	// Defaults.
	DefaultWorkDirectory = "/opt/aerospike"
)

const (
	AerospikeServerContainerName                   = "aerospike-server"
	AerospikeInitContainerName                     = "aerospike-init"
	AerospikeInitContainerRegistryEnvVar           = "AEROSPIKE_KUBERNETES_INIT_REGISTRY"
	AerospikeInitContainerRegistryNamespaceEnvVar  = "AEROSPIKE_KUBERNETES_INIT_REGISTRY_NAMESPACE"
	AerospikeInitContainerNameTagEnvVar            = "AEROSPIKE_KUBERNETES_INIT_NAME_TAG"
	AerospikeInitContainerDefaultRegistry          = "docker.io"
	AerospikeInitContainerDefaultRegistryNamespace = "aerospike"
	AerospikeInitContainerDefaultNameAndTag        = "aerospike-kubernetes-init:2.4.0-dev1"
	AerospikeAppLabel                              = "app"
	AerospikeAppLabelValue                         = "aerospike-cluster"
	AerospikeCustomResourceLabel                   = "aerospike.com/cr"
	AerospikeRackIDLabel                           = "aerospike.com/rack-id"
	AerospikeRackRevisionLabel                     = "aerospike.com/rack-revision"
	AerospikeAPIVersionLabel                       = "aerospike.com/api-version"
	AerospikeAPIVersion                            = "v1"
)

// GetConfiguredWorkDirectory returns the Aerospike work directory configured in aerospikeConfig.
func GetConfiguredWorkDirectory(aerospikeConfigSpec AerospikeConfigSpec) string {
	// Get namespace config.
	aerospikeConfig := aerospikeConfigSpec.Value

	serviceTmp := aerospikeConfig[ConfKeyService]
	if serviceTmp != nil {
		serviceConf := serviceTmp.(map[string]interface{})

		workDir, ok := serviceConf[confKeyWorkDirectory]
		if ok {
			return workDir.(string)
		}
	}

	return ""
}

// GetWorkDirectory returns the Aerospike work directory to be used for aerospikeConfig.
func GetWorkDirectory(aerospikeConfigSpec AerospikeConfigSpec) string {
	workDir := GetConfiguredWorkDirectory(aerospikeConfigSpec)
	if workDir != "" {
		return workDir
	}

	return DefaultWorkDirectory
}

func getInitContainerImage(registry, namespace, repoAndTag string) string {
	return fmt.Sprintf(
		"%s/%s/%s", strings.TrimSuffix(registry, "/"),
		strings.TrimSuffix(namespace, "/"),
		repoAndTag,
	)
}

func GetAerospikeInitContainerImage(aeroCluster *AerospikeCluster) string {
	registry := getInitContainerImageValue(
		aeroCluster, AerospikeInitContainerRegistryEnvVar,
		AerospikeInitContainerDefaultRegistry,
	)
	namespace := getInitContainerImageRegistryNamespace(aeroCluster)
	repoAndTag := getInitContainerImageValue(
		aeroCluster, AerospikeInitContainerNameTagEnvVar,
		AerospikeInitContainerDefaultNameAndTag,
	)

	return getInitContainerImage(registry, namespace, repoAndTag)
}

func getInitContainerImageRegistryNamespace(aeroCluster *AerospikeCluster) string {
	// Given in CR
	var namespace *string
	if aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		namespace = aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistryNamespace
	}

	if namespace == nil {
		// Given in EnvVar
		envRegistryNamespace, found := os.LookupEnv(AerospikeInitContainerRegistryNamespaceEnvVar)
		if found {
			namespace = &envRegistryNamespace
		}
	}

	if namespace == nil {
		return AerospikeInitContainerDefaultRegistryNamespace
	}

	return *namespace
}

func getInitContainerImageValue(aeroCluster *AerospikeCluster, envVar, defaultValue string) string {
	var value string

	// Check in CR based on the valueType
	if aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		switch envVar {
		case AerospikeInitContainerRegistryEnvVar:
			value = aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageRegistry
		case AerospikeInitContainerNameTagEnvVar:
			value = aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.ImageNameAndTag
		}
	}

	// Check in EnvVar if not found in CR
	if value == "" {
		envVal, found := os.LookupEnv(envVar)
		if found {
			value = envVal
		}
	}

	// Return default values if still not found
	if value == "" {
		return defaultValue
	}

	return value
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
	if networkConfInterface, ok := aerospikeConfig[ConfKeyNetwork]; ok {
		if networkConf, ok := networkConfInterface.(map[string]interface{}); ok {
			if serviceConfInterface, ok := networkConf[ConfKeyNetworkService]; ok {
				if serviceConf, ok := serviceConfInterface.(map[string]interface{}); ok {
					if _, ok := serviceConf[ConfKeyTLSName]; ok {
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
func IsSecurityEnabled(aerospikeConfig map[string]interface{}) (bool, error) {
	if _, err := GetConfigContext(aerospikeConfig, "security"); err != nil {
		if errors.Is(err, internalerrors.ErrNotFound) {
			return false, nil
		}

		if errors.Is(err, internalerrors.ErrInvalidOrEmpty) {
			return true, nil
		}

		return false, err
	}

	return true, nil
}

func IsAttributeEnabled(
	aerospikeConfig map[string]interface{}, context, key string,
) (bool, error) {
	confMap, err := GetConfigContext(aerospikeConfig, context)
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
	aerospikeConfig map[string]interface{}, context string,
) (map[string]interface{}, error) {
	if aerospikeConfig == nil {
		return nil, fmt.Errorf("missing aerospike configuration")
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
	if confs, ok := aerospikeConfig[ConfKeyNamespace].([]interface{}); ok {
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

func GetServiceTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int32) {
	return GetTLSNameAndPort(aeroConf, ConfKeyNetworkService)
}

func GetHeartbeatTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int32) {
	return GetTLSNameAndPort(aeroConf, ConfKeyNetworkHeartbeat)
}

func GetFabricTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int32) {
	return GetTLSNameAndPort(aeroConf, ConfKeyNetworkFabric)
}

func GetAdminTLSNameAndPort(aeroConf *AerospikeConfigSpec) (tlsName string, port *int32) {
	return GetTLSNameAndPort(aeroConf, ConfKeyNetworkAdmin)
}

func GetTLSNameAndPort(
	aeroConf *AerospikeConfigSpec, connectionType string,
) (tlsName string, port *int32) {
	if networkConfTmp, ok := aeroConf.Value[ConfKeyNetwork]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		if _, ok := networkConf[connectionType]; !ok {
			return tlsName, port
		}

		connConf := networkConf[connectionType].(map[string]interface{})
		if name, ok := connConf[ConfKeyTLSName]; ok {
			tlsName = name.(string)

			if tlsPort, tlsPortConfigured := connConf[ConfKeyTLSPort]; tlsPortConfigured {
				intPort := int32(tlsPort.(float64))
				port = &intPort
			}
		}
	}

	return tlsName, port
}

func GetServicePort(aeroConf *AerospikeConfigSpec) *int32 {
	return GetPortFromConfig(aeroConf, ConfKeyNetworkService, ConfKeyPort)
}

func GetHeartbeatPort(aeroConf *AerospikeConfigSpec) *int32 {
	return GetPortFromConfig(aeroConf, ConfKeyNetworkHeartbeat, ConfKeyPort)
}

func GetFabricPort(aeroConf *AerospikeConfigSpec) *int32 {
	return GetPortFromConfig(aeroConf, ConfKeyNetworkFabric, ConfKeyPort)
}

func GetAdminPort(aeroConf *AerospikeConfigSpec) *int32 {
	return GetPortFromConfig(aeroConf, ConfKeyNetworkAdmin, ConfKeyPort)
}

func GetPortFromConfig(
	aeroConf *AerospikeConfigSpec, connectionType string, paramName string,
) *int32 {
	if networkConf, ok := aeroConf.Value[ConfKeyNetwork]; ok {
		if connectionConfig, ok := networkConf.(map[string]interface{})[connectionType]; ok {
			if port, ok := connectionConfig.(map[string]interface{})[paramName]; ok {
				intPort := int32(port.(float64))
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
	serviceConfig := asConfig.Value[ConfKeyService].(map[string]interface{})

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
		isEnabled := IsNSSCEnabled(nsConfInterface.(map[string]interface{}))
		if isEnabled {
			return true
		}
	}

	return false
}

func IsNSSCEnabled(nsConf map[string]interface{}) bool {
	scEnabled, ok := nsConf["strong-consistency"]
	if !ok {
		return false
	}

	return scEnabled.(bool)
}

// GetBool returns the value of the given bool pointer. If the pointer is nil, it returns false.
func GetBool(boolPtr *bool) bool {
	return ptr.Deref(boolPtr, false)
}

// GetDefaultPasswordFilePath returns the default-password-fille path if configured.
func GetDefaultPasswordFilePath(aerospikeConfigSpec *AerospikeConfigSpec) *string {
	aerospikeConfig := aerospikeConfigSpec.Value

	// Get security config.
	securityConfTmp, ok := aerospikeConfig[ConfKeySecurity]
	if !ok {
		return nil
	}

	securityConf, ok := securityConfTmp.(map[string]interface{})
	if !ok {
		// Should never happen.
		return nil
	}

	// Get password file.
	passFileTmp, ok := securityConf[confKeySecurityDefaultPasswordFile]
	if !ok {
		return nil
	}

	passFile := passFileTmp.(string)

	return &passFile
}

func DistributeItems(totalItems, totalGroups int32) []int32 {
	itemsPerGroup, extraItems := totalItems/totalGroups, totalItems%totalGroups

	// Distributing nodes in given racks
	var topology []int32

	for groupIdx := int32(0); groupIdx < totalGroups; groupIdx++ {
		itemsForThisGroup := itemsPerGroup
		if groupIdx < extraItems {
			itemsForThisGroup++
		}

		topology = append(topology, itemsForThisGroup)
	}

	return topology
}

func GetAllPodNames(pods map[string]AerospikePodStatus) sets.Set[string] {
	podNames := make(sets.Set[string])

	for podName := range pods {
		podNames.Insert(podName)
	}

	return podNames
}

// GetImageVersion extracts the Aerospike version from a container image.
// The implementation extracts the image tag and find the longest string from
// it that is a version string.
// Note: The behaviour should match the operator's python implementation in
// init container extracting version.
func GetImageVersion(imageStr string) (string, error) {
	_, _, version := ParseDockerImageTag(imageStr)

	if version == "" || strings.EqualFold(version, "latest") {
		return "", fmt.Errorf(
			"image version is mandatory for image: %v", imageStr,
		)
	}

	// Ignore special prefixes and suffixes.
	matches := versionRegex.FindAllString(version, -1)
	if matches == nil || len(matches) < 1 {
		return "", fmt.Errorf(
			"invalid image version format: %v", version,
		)
	}

	longest := 0

	for i := range matches {
		if len(matches[i]) >= len(matches[longest]) {
			longest = i
		}
	}

	return matches[longest], nil
}

func IsClientCertConfigured(certSpec *AerospikeOperatorClientCertSpec) bool {
	return (certSpec.SecretCertSource != nil && certSpec.SecretCertSource.ClientCertFilename != "") ||
		(certSpec.CertPathInOperator != nil && certSpec.CertPathInOperator.ClientCertPath != "")
}

// GetVolumeForAerospikePath returns volume defined for given path for Aerospike server container.
func GetVolumeForAerospikePath(storage *AerospikeStorageSpec, path string) *VolumeSpec {
	var matchedVolume *VolumeSpec

	for idx := range storage.Volumes {
		volume := &storage.Volumes[idx]
		if volume.Aerospike != nil && IsPathParentOrSame(
			volume.Aerospike.Path, path,
		) {
			if matchedVolume == nil || matchedVolume.Aerospike.Path < volume.Aerospike.Path {
				matchedVolume = volume
			}
		}
	}

	return matchedVolume
}

// IsPathParentOrSame indicates if dir1 is a parent or same as dir2.
func IsPathParentOrSame(dir1, dir2 string) bool {
	if relPath, err := filepath.Rel(dir1, dir2); err == nil {
		// If dir1 is not a parent directory then relative path will have to climb up directory hierarchy of dir1.
		return !strings.HasPrefix(relPath, "..")
	}

	// Paths are unrelated.
	return false
}
