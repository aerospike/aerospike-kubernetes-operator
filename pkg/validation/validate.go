package validation

import (
	"fmt"
	"reflect"
	"strings"

	validate "github.com/asaskevich/govalidator"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var networkConnectionTypes = []string{asdbv1.ConfKeyNetworkService, asdbv1.ConfKeyNetworkHeartbeat,
	asdbv1.ConfKeyNetworkFabric, asdbv1.ConfKeyNetworkAdmin}

// ValidateAerospikeConfig validates the aerospikeConfig.
// It validates the schema, service, network, logging and namespace configurations.
func ValidateAerospikeConfig(
	aslog logr.Logger, version string, config map[string]interface{}, clSize int,
) error {
	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}

	if err := ValidateAerospikeConfigSchema(aslog, version, config); err != nil {
		return fmt.Errorf("aerospikeConfig not valid: %v", err)
	}

	// service conf
	serviceConf, ok := config[asdbv1.ConfKeyService].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.service not a valid map %v", config[asdbv1.ConfKeyService],
		)
	}

	if val, exists := serviceConf["advertise-ipv6"]; exists && val.(bool) {
		return fmt.Errorf("advertise-ipv6 is not supported")
	}

	// TODO: Shouldn't be set by user, confirm this
	if _, ok = serviceConf["cluster-name"]; !ok {
		return fmt.Errorf("aerospikeCluster name not found in config. Looks like object is not mutated by webhook")
	}

	// network conf
	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", config["network"],
		)
	}

	if err := validateNetworkConfig(networkConf); err != nil {
		return err
	}

	// namespace conf
	nsListInterface, ok := config["namespaces"]
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.namespace not a present. aerospikeConfig %v",
			config,
		)
	} else if nsListInterface == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	}

	if nsList, ok1 := nsListInterface.([]interface{}); !ok1 {
		return fmt.Errorf(
			"aerospikeConfig.namespace not valid namespace list %v",
			nsListInterface,
		)
	} else if err := validateNamespaceConfig(
		nsList, clSize,
	); err != nil {
		return err
	}

	loggingConfList, ok := config["logging"].([]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.logging not a valid list %v", config["logging"],
		)
	}

	return validateLoggingConf(loggingConfList)
}

func validateLoggingConf(loggingConfList []interface{}) error {
	syslogParams := []string{"facility", "path", "tag"}

	for idx := range loggingConfList {
		logConf, ok := loggingConfList[idx].(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"aerospikeConfig.logging not a list of valid map %v", logConf,
			)
		}

		if logConf["name"] != "syslog" {
			for _, param := range syslogParams {
				if _, ok := logConf[param]; ok {
					return fmt.Errorf("can use %s only with `syslog` in aerospikeConfig.logging %v", param, logConf)
				}
			}
		}
	}

	return nil
}

func validateNetworkConfig(networkConf map[string]interface{}) error {
	serviceConf, serviceExist := networkConf[asdbv1.ConfKeyNetworkService]
	if !serviceExist {
		return fmt.Errorf("network.service section not found in config")
	}

	tlsNames := sets.Set[string]{}
	// network.tls conf
	if _, ok := networkConf["tls"]; ok {
		tlsConfList := networkConf["tls"].([]interface{})
		for _, tlsConfInt := range tlsConfList {
			tlsConf := tlsConfInt.(map[string]interface{})
			if tlsName, ok := tlsConf["name"]; ok {
				tlsNames.Insert(tlsName.(string))
			}

			if _, ok := tlsConf["ca-path"]; ok {
				if _, ok1 := tlsConf["ca-file"]; ok1 {
					return fmt.Errorf(
						"both `ca-path` and `ca-file` cannot be set in `tls`. tlsConf %v",
						tlsConf,
					)
				}
			}
		}
	}

	if _, err := ValidateTLSAuthenticateClient(
		serviceConf.(map[string]interface{}),
	); err != nil {
		return err
	}

	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnection(
			networkConf, tlsNames, connectionType,
		); err != nil {
			return err
		}
	}

	return nil
}

// ValidateTLSAuthenticateClient validate the tls-authenticate-client field in the service configuration.
func ValidateTLSAuthenticateClient(serviceConf map[string]interface{}) (
	[]string, error,
) {
	config, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		return []string{}, nil
	}

	switch value := config.(type) {
	case string:
		if value == "any" || value == "false" {
			return []string{}, nil
		}

		return nil, fmt.Errorf(
			"tls-authenticate-client contains invalid value: %s", value,
		)
	case bool:
		if !value {
			return []string{}, nil
		}

		return nil, fmt.Errorf(
			"tls-authenticate-client contains invalid value: %t", value,
		)
	case []interface{}:
		dnsNames := make([]string, len(value))

		for i := 0; i < len(value); i++ {
			dnsName, ok := value[i].(string)
			if !ok {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid type value: %v",
					value,
				)
			}

			if !validate.IsDNSName(dnsName) {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid dns-name: %v",
					dnsName,
				)
			}

			dnsNames[i] = dnsName
		}

		return dnsNames, nil
	}

	return nil, fmt.Errorf(
		"tls-authenticate-client contains invalid type value: %v", config,
	)
}

func validateNetworkConnection(
	networkConf map[string]interface{}, tlsNames sets.Set[string],
	connectionType string,
) error {
	if connectionConfig, exists := networkConf[connectionType]; exists {
		connectionConfigMap := connectionConfig.(map[string]interface{})
		if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
			if _, tlsPortExist := connectionConfigMap["tls-port"]; !tlsPortExist {
				return fmt.Errorf(
					"you can't specify tls-name for network.%s without specifying tls-port",
					connectionType,
				)
			}

			if !tlsNames.Has(tlsName.(string)) {
				return fmt.Errorf("tls-name '%s' is not configured", tlsName)
			}
		} else {
			for param := range connectionConfigMap {
				if strings.HasPrefix(param, "tls-") {
					return fmt.Errorf(
						"you can't specify %s for network.%s without specifying tls-name",
						param, connectionType,
					)
				}
			}
		}
	}

	return nil
}

//nolint:gocyclo // for readability
func validateNamespaceConfig(
	nsConfInterfaceList []interface{},
	clSize int,
) error {
	if len(nsConfInterfaceList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace list cannot be empty")
	}

	for _, nsConfInterface := range nsConfInterfaceList {
		// Validate new namespace conf
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", nsConfInterface,
			)
		}

		if nErr := validateNamespaceReplicationFactor(
			nsConf, clSize,
		); nErr != nil {
			return nErr
		}

		if mErr := validateMRTFields(nsConf); mErr != nil {
			return mErr
		}

		if nsStorage, ok := nsConf[asdbv1.ConfKeyStorageEngine]; ok {
			if nsStorage == nil {
				return fmt.Errorf(
					"%s cannot be nil for namespace %v", asdbv1.ConfKeyStorageEngine, nsConf,
				)
			}

			if IsInMemoryNamespace(nsConf) {
				// storage-engine memory
				continue
			}

			if !IsDeviceOrPmemNamespace(nsConf) {
				return fmt.Errorf(
					"%s not supported for namespace %v", asdbv1.ConfKeyStorageEngine, nsConf,
				)
			}

			if devices, ok := nsStorage.(map[string]interface{})["devices"]; ok {
				if devices == nil {
					return fmt.Errorf(
						"namespace storage devices cannot be nil %v", nsStorage,
					)
				}

				if _, ok := devices.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace storage device format not valid %v",
						nsStorage,
					)
				}

				if len(devices.([]interface{})) == 0 {
					return fmt.Errorf(
						"no devices for namespace storage %v", nsStorage,
					)
				}

				for _, device := range devices.([]interface{}) {
					if _, ok := device.(string); !ok {
						return fmt.Errorf(
							"namespace storage device not valid string %v",
							device,
						)
					}

					device = strings.TrimSpace(device.(string))

					// device list Fields cannot be more than 2 in single line. Two in shadow device case. Validate.
					if len(strings.Fields(device.(string))) > 2 {
						return fmt.Errorf(
							"invalid device name %v. Max 2 device can be mentioned in single line (Shadow device config)",
							device,
						)
					}
				}
			}

			if files, ok := nsStorage.(map[string]interface{})["files"]; ok {
				if files == nil {
					return fmt.Errorf(
						"namespace storage files cannot be nil %v", nsStorage,
					)
				}

				if _, ok := files.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace storage files format not valid %v",
						nsStorage,
					)
				}

				if len(files.([]interface{})) == 0 {
					return fmt.Errorf(
						"no files for namespace storage %v", nsStorage,
					)
				}

				for _, file := range files.([]interface{}) {
					if _, ok := file.(string); !ok {
						return fmt.Errorf(
							"namespace storage file not valid string %v", file,
						)
					}

					file = strings.TrimSpace(file.(string))

					// File list Fields cannot be more than 2 in single line. Two in shadow device case. Validate.
					if len(strings.Fields(file.(string))) > 2 {
						return fmt.Errorf(
							"invalid file name %v. Max 2 file can be mentioned in single line (Shadow file config)",
							file,
						)
					}
				}
			}
		} else {
			return fmt.Errorf("storage-engine config is required for namespace")
		}
	}

	if _, _, err := ValidateStorageEngineDeviceList(nsConfInterfaceList); err != nil {
		return err
	}

	// Validate index-type
	for _, nsConfInterface := range nsConfInterfaceList {
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", nsConfInterface,
			)
		}

		if IsShMemIndexTypeNamespace(nsConf) {
			continue
		}

		if nsIndexStorage, ok := nsConf["index-type"]; ok {
			if mounts, ok := nsIndexStorage.(map[string]interface{})["mounts"]; ok {
				if mounts == nil {
					return fmt.Errorf(
						"namespace index-type mounts cannot be nil %v",
						nsIndexStorage,
					)
				}

				if _, ok := mounts.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace index-type mounts format not valid %v",
						nsIndexStorage,
					)
				}

				if len(mounts.([]interface{})) == 0 {
					return fmt.Errorf(
						"no mounts for namespace index-type %v", nsIndexStorage,
					)
				}

				for _, mount := range mounts.([]interface{}) {
					if _, ok := mount.(string); !ok {
						return fmt.Errorf(
							"namespace index-type mount not valid string %v",
							mount,
						)
					}
				}
			}
		}
	}

	return nil
}

func validateMRTFields(nsConf map[string]interface{}) error {
	mrtField := isMRTFieldSet(nsConf)
	scEnabled := asdbv1.IsNSSCEnabled(nsConf)

	if !scEnabled && mrtField {
		return fmt.Errorf("MRT fields are not allowed in non-SC namespace %v", nsConf)
	}

	return nil
}

func isMRTFieldSet(nsConf map[string]interface{}) bool {
	mrtFields := []string{"mrt-duration", "disable-mrt-writes"}

	for _, field := range mrtFields {
		if _, exists := nsConf[field]; exists {
			return true
		}
	}

	return false
}

func validateNamespaceReplicationFactor(
	nsConf map[string]interface{}, clSize int,
) error {
	rf, err := GetNamespaceReplicationFactor(nsConf)
	if err != nil {
		return err
	}

	scEnabled := asdbv1.IsNSSCEnabled(nsConf)

	// clSize < rf is allowed in AP mode but not in sc mode
	if scEnabled && (clSize < rf) {
		return fmt.Errorf(
			"strong-consistency namespace replication-factor %v cannot be more than cluster size %d", rf, clSize,
		)
	}

	return nil
}

// GetNamespaceReplicationFactor returns the replication factor for the namespace.
func GetNamespaceReplicationFactor(nsConf map[string]interface{}) (int, error) {
	rfInterface, ok := nsConf["replication-factor"]
	if !ok {
		rfInterface = 2 // default replication-factor
	}

	rf, err := GetIntType(rfInterface)
	if err != nil {
		return 0, fmt.Errorf("namespace replication-factor %v", err)
	}

	return rf, nil
}

func ValidateStorageEngineDeviceList(nsConfList []interface{}) (deviceList, fileList map[string]string, err error) {
	deviceList = map[string]string{}
	fileList = map[string]string{}

	// build a map device -> namespace
	for _, nsConfInterface := range nsConfList {
		nsConf := nsConfInterface.(map[string]interface{})
		namespace := nsConf["name"].(string)
		storage := nsConf["storage-engine"].(map[string]interface{})

		if devices, ok := storage["devices"]; ok {
			for _, d := range devices.([]interface{}) {
				device := d.(string)

				previousNamespace, exists := deviceList[device]
				if exists {
					return nil, nil, fmt.Errorf(
						"device %s is already being referenced in multiple namespaces (%s, %s)",
						device, previousNamespace, namespace,
					)
				}

				deviceList[device] = namespace
			}
		}

		if files, ok := storage["files"]; ok {
			for _, d := range files.([]interface{}) {
				file := d.(string)

				previousNamespace, exists := fileList[file]
				if exists {
					return nil, nil, fmt.Errorf(
						"file %s is already being referenced in multiple namespaces (%s, %s)",
						file, previousNamespace, namespace,
					)
				}

				fileList[file] = namespace
			}
		}
	}

	return deviceList, fileList, nil
}

// IsInMemoryNamespace returns true if this namespace config uses memory for storage.
func IsInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "memory"
}

// IsDeviceOrPmemNamespace returns true if this namespace config uses device for storage.
func IsDeviceOrPmemNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && (typeStr == "device" || typeStr == "pmem")
}

// IsShMemIndexTypeNamespace returns true if this namespace index type is shmem.
func IsShMemIndexTypeNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["index-type"]
	if !ok {
		// missing index-type assumed to be shmem.
		return true
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "shmem"
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

func ValidateAerospikeConfigSchema(
	aslog logr.Logger, version string, config map[string]interface{},
) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	asConf, err := asconfig.NewMapAsConfig(aslog, config)
	if err != nil {
		return fmt.Errorf("failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(aslog, version)
	if !valid {
		if len(validationErr) == 0 {
			return fmt.Errorf(
				"failed to validate config for the version %s: %v", version,
				err,
			)
		}

		errStrings := make([]string, 0)
		for _, e := range validationErr {
			errStrings = append(errStrings, fmt.Sprintf("\t%v\n", *e))
		}

		return fmt.Errorf(
			"generated config not valid for version %s: %v %v", version, err,
			errStrings,
		)
	}

	return nil
}

func isValueUpdated(m1, m2 map[string]interface{}, key string) bool {
	val1, ok1 := m1[key]
	val2, ok2 := m2[key]

	if ok1 != ok2 {
		return true
	}

	return !reflect.DeepEqual(val1, val2)
}

func validateSecurityConfigUpdate(oldConfig, newConfig map[string]interface{}) error {
	ovflag, err := asdbv1.IsSecurityEnabled(oldConfig)
	if err != nil {
		return fmt.Errorf(
			"failed to validate Security context of old aerospike conf: %w", err,
		)
	}

	ivflag, err := asdbv1.IsSecurityEnabled(newConfig)
	if err != nil {
		return fmt.Errorf(
			"failed to validate Security context of new aerospike conf: %w", err,
		)
	}

	if !ivflag && ovflag {
		return fmt.Errorf("cannot disable cluster security in running cluster")
	}

	return nil
}

// ValidateAerospikeConfigUpdate validates the update of aerospikeConfig.
// It validates the schema, security, tls, network and namespace configurations for the newConfig
// It also validates the update of security, tls, network and namespace configurations.
func ValidateAerospikeConfigUpdate(
	aslog logr.Logger, version string,
	oldConfig, newConfig map[string]interface{}, clSize int,
) error {
	aslog.Info("Validate AerospikeConfig update")

	if err := ValidateAerospikeConfig(aslog, version, newConfig, clSize); err != nil {
		return err
	}

	return ValidateAerospikeConfigUpdateWithoutSchema(oldConfig, newConfig)
}

// ValidateAerospikeConfigUpdateWithoutSchema validates the update of aerospikeConfig except for the schema.
// It validates the update of security, tls, network and namespace configurations.
func ValidateAerospikeConfigUpdateWithoutSchema(oldConfig, newConfig map[string]interface{}) error {
	if err := validateSecurityConfigUpdate(oldConfig, newConfig); err != nil {
		return err
	}

	if err := validateTLSUpdate(oldConfig, newConfig); err != nil {
		return err
	}

	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnectionUpdate(oldConfig, newConfig, connectionType); err != nil {
			return err
		}
	}

	return validateNsConfUpdate(oldConfig, newConfig)
}

func validateTLSUpdate(oldConf, newConf map[string]interface{}) error {
	if newConf == nil || oldConf == nil {
		return fmt.Errorf("config cannot be nil")
	}

	oldTLS, oldExists := oldConf["network"].(map[string]interface{})["tls"]
	newTLS, newExists := newConf["network"].(map[string]interface{})["tls"]

	if oldExists && newExists && (!reflect.DeepEqual(oldTLS, newTLS)) {
		oldTLSCAFileMap := make(map[string]string)
		oldTLSCAPathMap := make(map[string]string)
		newUsedTLS := sets.NewString()
		oldUsedTLS := sets.NewString()

		// fetching names of TLS configurations used in connections
		for _, connectionType := range networkConnectionTypes {
			if connectionConfig, exists := newConf["network"].(map[string]interface{})[connectionType]; exists {
				connectionConfigMap := connectionConfig.(map[string]interface{})
				if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
					newUsedTLS.Insert(tlsName.(string))
				}
			}
		}

		// fetching names of TLS configurations used in old connections configurations
		for _, connectionType := range networkConnectionTypes {
			if connectionConfig, exists := oldConf["network"].(map[string]interface{})[connectionType]; exists {
				connectionConfigMap := connectionConfig.(map[string]interface{})
				if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
					oldUsedTLS.Insert(tlsName.(string))
				}
			}
		}

		for _, tls := range oldTLS.([]interface{}) {
			tlsMap := tls.(map[string]interface{})
			if !oldUsedTLS.Has(tlsMap["name"].(string)) {
				continue
			}

			oldCAFile, oldCAFileOK := tlsMap["ca-file"]
			if oldCAFileOK {
				oldTLSCAFileMap[tlsMap["name"].(string)] = oldCAFile.(string)
			}

			oldCAPath, oldCAPathOK := tlsMap["ca-path"]
			if oldCAPathOK {
				oldTLSCAPathMap[tlsMap["name"].(string)] = oldCAPath.(string)
			}
		}

		for _, tls := range newTLS.([]interface{}) {
			tlsMap := tls.(map[string]interface{})
			if !newUsedTLS.Has(tlsMap["name"].(string)) {
				continue
			}

			_, newCAPathOK := tlsMap["ca-path"]
			newCAFile, newCAFileOK := tlsMap["ca-file"]

			oldCAFile, oldCAFileOK := oldTLSCAFileMap[tlsMap["name"].(string)]
			_, oldCAPathOK := oldTLSCAPathMap[tlsMap["name"].(string)]

			if (oldCAFileOK || oldCAPathOK) && !(newCAPathOK || newCAFileOK) {
				return fmt.Errorf(
					"cannot remove used `ca-file` or `ca-path` from tls",
				)
			}

			if oldCAFileOK && newCAFileOK && newCAFile.(string) != oldCAFile {
				return fmt.Errorf("cannot change ca-file of used tls")
			}
		}
	}

	return nil
}

func validateNsConfUpdate(oldConf, newConf map[string]interface{}) error {
	if newConf == nil || oldConf == nil {
		return fmt.Errorf("namespace conf cannot be nil")
	}

	newNsConfList := newConf["namespaces"].([]interface{})
	oldNsConfList := oldConf["namespaces"].([]interface{})

	for _, singleConfInterface := range newNsConfList {
		// Validate new namespaceconf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", singleConfInterface,
			)
		}

		// Validate new namespace conf from old namespace conf. Few fields cannot be updated
		for _, oldSingleConfInterface := range oldNsConfList {
			oldSingleConf, ok := oldSingleConfInterface.(map[string]interface{})
			if !ok {
				return fmt.Errorf(
					"namespace conf not in valid format %v",
					oldSingleConfInterface,
				)
			}

			if singleConf["name"] == oldSingleConf["name"] {
				// replication-factor update not allowed
				if isValueUpdated(
					oldSingleConf, singleConf, "replication-factor",
				) {
					return fmt.Errorf(
						"replication-factor cannot be updated. old nsconf %v, new nsconf %v",
						oldSingleConf, singleConf,
					)
				}

				// strong-consistency update not allowed
				if isValueUpdated(
					oldSingleConf, singleConf, "strong-consistency",
				) {
					return fmt.Errorf(
						"strong-consistency cannot be updated. old nsconf %v, new nsconf %v",
						oldSingleConf, singleConf,
					)
				}
			}
		}
	}

	return nil
}

func validateNetworkConnectionUpdate(oldConf, newConf map[string]interface{}, connectionType string) error {
	var networkPorts = []string{
		"port", "access-port",
		"alternate-access-port"}

	// Extract network configs safely
	oldNetwork, oldOk := oldConf["network"].(map[string]interface{})
	newNetwork, newOk := newConf["network"].(map[string]interface{})

	if !oldOk || !newOk {
		return fmt.Errorf("invalid network configuration structure")
	}

	oldConnectionConfig, oldConnOk := oldNetwork[connectionType].(map[string]interface{})
	newConnectionConfig, newConnOk := newNetwork[connectionType].(map[string]interface{})

	// If the connectionType is missing in either old or new config, assume it's an admin and skip validation.
	// Other connectionType are required fields and their existence is checked in mutation.
	if !oldConnOk || !newConnOk {
		return nil
	}

	oldTLSName, oldTLSNameOk := oldConnectionConfig["tls-name"]
	newTLSName, newTLSNameOk := newConnectionConfig["tls-name"]

	if oldTLSNameOk && newTLSNameOk {
		if !reflect.DeepEqual(oldTLSName, newTLSName) {
			return fmt.Errorf("cannot modify tls name")
		}
	}

	for _, port := range networkPorts {
		if err := validateNetworkPortUpdate(oldConnectionConfig, newConnectionConfig, port); err != nil {
			return err
		}
	}

	return nil
}

func validateNetworkPortUpdate(oldConnectionConfig, newConnectionConfig map[string]interface{}, port string) error {
	oldPort, oldPortOk := oldConnectionConfig[port]
	newPort, newPortOk := newConnectionConfig[port]
	tlsPort := "tls-" + port

	if oldPortOk && newPortOk {
		if !reflect.DeepEqual(oldPort, newPort) {
			return fmt.Errorf("cannot modify %s number: old value %v, new value %v", port, oldPort, newPort)
		}
	}

	oldTLSPort, oldTLSPortOk := oldConnectionConfig[tlsPort]
	newTLSPort, newTLSPortOk := newConnectionConfig[tlsPort]

	if oldTLSPortOk && newTLSPortOk {
		if !reflect.DeepEqual(oldTLSPort, newTLSPort) {
			return fmt.Errorf(
				"cannot modify %s number: old value %v, new value %v", tlsPort, oldTLSPort, newTLSPort)
		}
	}

	if (!newTLSPortOk && oldTLSPortOk) || (!newPortOk && oldPortOk) {
		if !(oldPortOk && oldTLSPortOk) {
			return fmt.Errorf("cannot remove tls or non-tls configurations unless both configurations have been set initially")
		}
	}

	return nil
}
