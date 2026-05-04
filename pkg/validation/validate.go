package validation

import (
	"fmt"
	"reflect"
	"strings"

	validate "github.com/asaskevich/govalidator"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var networkConnectionTypes = []string{asdbv1.ConfKeyNetworkService, asdbv1.ConfKeyNetworkHeartbeat,
	asdbv1.ConfKeyNetworkFabric, asdbv1.ConfKeyNetworkAdmin}

// ValidateAerospikeConfig validates the aerospikeConfig.
// It validates the schema, service, network, logging and namespace configurations.
//
// Format handling: schema validation runs on the original config so the correct
// versioned schema is selected. All structural checks then run on a normalized
// deep copy (legacy list fields converted to the new YAML map format) so every
// sub-validator operates on a single, consistent representation regardless of
// whether the user supplied the old or new format.
func ValidateAerospikeConfig(
	aslog logr.Logger, version string, config map[string]interface{}, clSize int,
) error {
	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}

	// Schema validation on the original config (the schema path is format-aware).
	if err := ValidateAerospikeConfigSchema(aslog, version, config); err != nil {
		return fmt.Errorf("aerospikeConfig not valid: %v", err)
	}

	return ValidateAerospikeConfigStructural(aslog, version, config, clSize)
}

// ValidateAerospikeConfigStructural validates the structural (non-schema) aspects
// of the aerospikeConfig: service invariants, network topology, namespace basic
// validity (storage-engine, RF, MRT), and logging format.
//
// It operates on the original config but normalizes a deep copy internally so
// callers need not pre-normalize.  Schema validation is intentionally excluded;
// use ValidateAerospikeConfig or call ValidateAerospikeConfigSchema separately
// before normalization when format-aware schema selection is required.
func ValidateAerospikeConfigStructural(
	_ logr.Logger, _ string, config map[string]interface{}, clSize int,
) error {
	// Deep copy + normalize: all structural checks below work on the new map format.
	cfg := NormalizeConfigFormat(DeepCopyConfig(config))

	// service conf (format-agnostic; always a map)
	serviceConf, ok := cfg[asdbv1.ConfKeyService].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.service not a valid map %v", cfg[asdbv1.ConfKeyService],
		)
	}

	if val, exists := serviceConf["advertise-ipv6"]; exists && val.(bool) {
		return fmt.Errorf("advertise-ipv6 is not supported")
	}

	if _, ok = serviceConf["cluster-name"]; !ok {
		return fmt.Errorf("aerospikeCluster name not found in config. Looks like object is not mutated by webhook")
	}

	// network conf (TLS is now always a map after normalization)
	networkConf, ok := cfg["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", cfg["network"],
		)
	}

	if err := validateNetworkConfig(networkConf); err != nil {
		return err
	}

	// namespace conf (always a map after normalization)
	nsVal, ok := cfg[asdbv1.ConfKeyNamespace]
	if !ok || nsVal == nil {
		return fmt.Errorf(
			"aerospikeConfig.namespace not present or nil. aerospikeConfig %v", cfg,
		)
	}

	nsMap, ok := nsVal.(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not a valid map after normalization %v", nsVal)
	}

	if err := validateNamespaceConfigFromMap(nsMap, clSize); err != nil {
		return err
	}

	// logging (remains a list in both formats; sink-type key may differ)
	loggingConfList, ok := cfg["logging"].([]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.logging not a valid list %v", cfg["logging"],
		)
	}

	return validateLoggingConf(loggingConfList)
}

// validateLoggingConf checks that syslog-specific parameters (facility, path,
// tag) are not used for non-syslog log sinks.
//
// Logging remains a list in both the legacy format (sink identified by "name")
// and the new YAML format (sink identified by "type").  We check both keys so
// that no separate normalization step is required for the logging section.
func validateLoggingConf(loggingConfList []interface{}) error {
	syslogParams := []string{"facility", "path", "tag"}

	for idx := range loggingConfList {
		logConf, ok := loggingConfList[idx].(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"aerospikeConfig.logging not a list of valid map %v", logConf,
			)
		}

		// Accept either "name" (legacy) or "type" (new format) for the sink identifier.
		sinkType := logConf["name"]
		if sinkType == nil {
			sinkType = logConf["type"]
		}

		if sinkType != "syslog" {
			for _, param := range syslogParams {
				if _, ok := logConf[param]; ok {
					return fmt.Errorf("can use %s only with `syslog` in aerospikeConfig.logging %v", param, logConf)
				}
			}
		}
	}

	return nil
}

// validateNetworkConfig validates network section contents.
// It expects the normalized (new YAML map) format: network.tls is a
// map[tlsName]→{ca-file, ca-path, ...} keyed by TLS name.
func validateNetworkConfig(networkConf map[string]interface{}) error {
	serviceConf, serviceExist := networkConf[asdbv1.ConfKeyNetworkService]
	if !serviceExist {
		return fmt.Errorf("network.service section not found in config")
	}

	tlsNames := sets.Set[string]{}

	if tlsVal, ok := networkConf["tls"]; ok {
		tlsConfMap, ok := tlsVal.(map[string]interface{})
		if !ok {
			return fmt.Errorf("network.tls not a valid map %v", tlsVal)
		}

		for tlsName, tlsConfInt := range tlsConfMap {
			tlsNames.Insert(tlsName)

			tlsConf, ok := tlsConfInt.(map[string]interface{})
			if !ok {
				continue
			}

			if _, ok := tlsConf["ca-path"]; ok {
				if _, ok1 := tlsConf["ca-file"]; ok1 {
					return fmt.Errorf(
						"both `ca-path` and `ca-file` cannot be set in `tls`. tlsName %v tlsConf %v",
						tlsName, tlsConf,
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

// validateNamespaceConfigFromMap validates the namespace section when it uses
// the new YAML map format (server >= 9.0), where each namespace is a key in
// the map and the namespace name is the map key (not a "name" field inside the
// map value).  It converts each entry into the list-of-maps representation
// (injecting a synthetic "name" field equal to the map key) so that the
// shared validateNamespaceConfig logic can be reused without modification.
func validateNamespaceConfigFromMap(nsConfMap map[string]interface{}, clSize int) error {
	if len(nsConfMap) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace map cannot be empty")
	}

	nsList := make([]interface{}, 0, len(nsConfMap))

	for nsName, nsVal := range nsConfMap {
		nsCfg, ok := nsVal.(map[string]interface{})
		if !ok {
			return fmt.Errorf("namespace conf not in valid format for namespace %q: %v", nsName, nsVal)
		}

		// Shallow-copy and inject the name so existing validators can locate it.
		merged := make(map[string]interface{}, len(nsCfg)+1)
		for k, v := range nsCfg {
			merged[k] = v
		}

		merged[asdbv1.ConfKeyName] = nsName
		nsList = append(nsList, merged)
	}

	return validateNamespaceConfig(nsList, clSize)
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

		if _, ok := nsConf[asdbv1.ConfKeyName]; !ok {
			return fmt.Errorf("namespace name not found in namespace config %v", nsConf)
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
	rfInterface, ok := nsConf[asdbv1.ConfKeyReplicationFactor]
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
		namespace := nsConf[asdbv1.ConfKeyName].(string)
		storage := nsConf[asdbv1.ConfKeyStorageEngine].(map[string]interface{})

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

// ValidateStorageEngineDeviceListFromMap validates that no storage device or
// file is shared across multiple namespaces when the namespace section is in
// the new YAML map format (server >= 9.0).  The namespace name is the map key;
// it is NOT expected to be present as a "name" field inside the value map.
func ValidateStorageEngineDeviceListFromMap(nsConfMap map[string]interface{}) (deviceList, fileList map[string]string, err error) {
	deviceList = map[string]string{}
	fileList = map[string]string{}

	for nsName, nsVal := range nsConfMap {
		nsCfg, ok := nsVal.(map[string]interface{})
		if !ok {
			continue
		}

		storageVal, ok := nsCfg[asdbv1.ConfKeyStorageEngine]
		if !ok {
			continue
		}

		storage, ok := storageVal.(map[string]interface{})
		if !ok {
			continue
		}

		if devices, ok := storage["devices"]; ok {
			for _, d := range devices.([]interface{}) {
				device := d.(string)

				if prev, exists := deviceList[device]; exists {
					return nil, nil, fmt.Errorf(
						"device %s is already being referenced in multiple namespaces (%s, %s)",
						device, prev, nsName,
					)
				}

				deviceList[device] = nsName
			}
		}

		if files, ok := storage["files"]; ok {
			for _, d := range files.([]interface{}) {
				file := d.(string)

				if prev, exists := fileList[file]; exists {
					return nil, nil, fmt.Errorf(
						"file %s is already being referenced in multiple namespaces (%s, %s)",
						file, prev, nsName,
					)
				}

				fileList[file] = nsName
			}
		}
	}

	return deviceList, fileList, nil
}

// IsInMemoryNamespace returns true if this namespace config uses memory for storage.
func IsInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf[asdbv1.ConfKeyStorageEngine]
	if !ok || storage == nil {
		return false
	}

	storageConf, ok := storage.(map[string]interface{})
	if !ok {
		return false
	}

	typeStr, ok := storageConf["type"]

	return ok && typeStr == "memory"
}

// IsDeviceOrPmemNamespace returns true if this namespace config uses device for storage.
func IsDeviceOrPmemNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf[asdbv1.ConfKeyStorageEngine]
	if !ok || storage == nil {
		return false
	}

	storageConf, ok := storage.(map[string]interface{})
	if !ok {
		return false
	}

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

	cmp, err := lib.CompareVersions(version, "8.1.1")
	if err != nil {
		return fmt.Errorf("failed to parse server version %q: %v", version, err)
	}

	if cmp >= 0 {
		// New YAML map format (server >= 8.1.1): validate directly against the
		// versioned JSON schema via asconfig.ValidateConfig.
		// Signature: ValidateConfig(configMap map[string]interface{}, ver string)
		//            (bool, []*ValidationErr, error)
		valid, validationErr, err := asconfig.ValidateConfig(config, version)
		if !valid {
			if len(validationErr) == 0 {
				return fmt.Errorf(
					"failed to validate config for version %s: %v", version, err,
				)
			}

			errStrings := make([]string, 0, len(validationErr))
			for _, e := range validationErr {
				errStrings = append(errStrings, fmt.Sprintf("\t%v\n", *e))
			}

			return fmt.Errorf(
				"config not valid for version %s: %v %v", version, err, errStrings,
			)
		}

		return nil
	}

	// Legacy .conf format (server < 8.1.1): the aerospike-server/ schema
	// directory only covers versions >= 8.1.1. For older versions that have no
	// schema, skip schema validation rather than hard-failing.
	asConf, err := asconfig.NewMapAsConfig(aslog, config)
	if err != nil {
		return fmt.Errorf("failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(aslog, version)
	if !valid {
		if len(validationErr) == 0 {
			// No schema available for this version — skip validation.
			if err != nil && strings.Contains(err.Error(), "unsupported version") {
				return nil
			}

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

// ValidateAerospikeConfigUpdate validates the update of aerospikeConfig.
// It validates the schema, security, tls, network and namespace configurations for the newConfig
// It also validates the update of tls, network and namespace configurations.
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
// It validates the update of tls, network and namespace configurations.
//
// Both configs are normalized to the new map format before comparison so that
// upgrade/downgrade scenarios (old format → new format or vice versa) are
// handled correctly without separate code paths.
func ValidateAerospikeConfigUpdateWithoutSchema(oldConfig, newConfig map[string]interface{}) error {
	// Normalize deep copies so all comparisons operate on a single format.
	oldNorm := NormalizeConfigFormat(DeepCopyConfig(oldConfig))
	newNorm := NormalizeConfigFormat(DeepCopyConfig(newConfig))

	if err := validateTLSUpdate(oldNorm, newNorm); err != nil {
		return err
	}

	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnectionUpdate(oldNorm, newNorm, connectionType); err != nil {
			return err
		}
	}

	return validateNsConfUpdate(oldNorm, newNorm)
}

// validateTLSUpdate checks TLS immutability constraints.
// Both configs are expected to be already normalized (network.tls is a map
// keyed by TLS name).
func validateTLSUpdate(oldConf, newConf map[string]interface{}) error {
	if newConf == nil || oldConf == nil {
		return fmt.Errorf("config cannot be nil")
	}

	oldTLS, oldExists := oldConf["network"].(map[string]interface{})["tls"]
	newTLS, newExists := newConf["network"].(map[string]interface{})["tls"]

	if !oldExists || !newExists || reflect.DeepEqual(oldTLS, newTLS) {
		return nil
	}

	oldTLSMap, _ := oldTLS.(map[string]interface{})
	newTLSMap, _ := newTLS.(map[string]interface{})

	// Collect TLS names referenced by network connection sub-sections.
	oldUsedTLS := collectUsedTLSNames(oldConf)
	newUsedTLS := collectUsedTLSNames(newConf)

	// Build a snapshot of old CA file/path info for used TLS entries.
	oldTLSCAFileMap := make(map[string]string)
	oldTLSCAPathMap := make(map[string]string)

	for name, tlsVal := range oldTLSMap {
		if !oldUsedTLS.Has(name) {
			continue
		}

		tlsCfg, ok := tlsVal.(map[string]interface{})
		if !ok {
			continue
		}

		if caFile, ok := tlsCfg["ca-file"]; ok {
			oldTLSCAFileMap[name] = caFile.(string)
		}

		if _, ok := tlsCfg["ca-path"]; ok {
			oldTLSCAPathMap[name] = ""
		}
	}

	// Validate that used TLS entries have not had their CA certs removed or changed.
	for name, tlsVal := range newTLSMap {
		if !newUsedTLS.Has(name) {
			continue
		}

		tlsCfg, ok := tlsVal.(map[string]interface{})
		if !ok {
			continue
		}

		_, newCAPathOK := tlsCfg["ca-path"]
		newCAFile, newCAFileOK := tlsCfg["ca-file"]

		oldCAFile, oldCAFileOK := oldTLSCAFileMap[name]
		_, oldCAPathOK := oldTLSCAPathMap[name]

		if (oldCAFileOK || oldCAPathOK) && !newCAPathOK && !newCAFileOK {
			return fmt.Errorf("cannot remove used `ca-file` or `ca-path` from tls")
		}

		if oldCAFileOK && newCAFileOK && newCAFile.(string) != oldCAFile {
			return fmt.Errorf("cannot change ca-file of used tls")
		}
	}

	return nil
}

// collectUsedTLSNames returns the set of TLS names referenced in the network
// connection subsections (service, heartbeat, fabric, admin).
func collectUsedTLSNames(conf map[string]interface{}) sets.String {
	used := sets.NewString()

	networkConf, ok := conf["network"].(map[string]interface{})
	if !ok {
		return used
	}

	for _, connectionType := range networkConnectionTypes {
		if connCfg, exists := networkConf[connectionType]; exists {
			if connMap, ok := connCfg.(map[string]interface{}); ok {
				if tlsName, ok := connMap[asdbv1.ConfKeyTLSName]; ok {
					used.Insert(tlsName.(string))
				}
			}
		}
	}

	return used
}

// validateNsConfUpdate validates namespace immutability constraints.
// Both configs are expected to be already normalized (namespaces is a map
// keyed by namespace name).
func validateNsConfUpdate(oldConf, newConf map[string]interface{}) error {
	if newConf == nil || oldConf == nil {
		return fmt.Errorf("namespace conf cannot be nil")
	}

	newNsMap, ok := newConf[asdbv1.ConfKeyNamespace].(map[string]interface{})
	if !ok {
		return fmt.Errorf("namespace conf not a valid map in new config")
	}

	oldNsMap, _ := oldConf[asdbv1.ConfKeyNamespace].(map[string]interface{})

	for nsName, newNsVal := range newNsMap {
		newNsCfg, ok := newNsVal.(map[string]interface{})
		if !ok {
			return fmt.Errorf("namespace conf not in valid format for namespace %q: %v", nsName, newNsVal)
		}

		oldNsVal, exists := oldNsMap[nsName]
		if !exists {
			continue
		}

		oldNsCfg, ok := oldNsVal.(map[string]interface{})
		if !ok {
			continue
		}

		if isValueUpdated(oldNsCfg, newNsCfg, "replication-factor") &&
			asdbv1.IsNSSCEnabled(newNsCfg) {
			return fmt.Errorf(
				"replication-factor cannot be updated for SC namespaces. old nsconf %v, new nsconf %v",
				oldNsCfg, newNsCfg,
			)
		}

		if isValueUpdated(oldNsCfg, newNsCfg, asdbv1.ConfKeyStrongConsistency) {
			return fmt.Errorf(
				"strong-consistency cannot be updated. old nsconf %v, new nsconf %v",
				oldNsCfg, newNsCfg,
			)
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
		if !oldPortOk || !oldTLSPortOk {
			return fmt.Errorf("cannot remove tls or non-tls configurations unless both configurations have been set initially")
		}
	}

	return nil
}
