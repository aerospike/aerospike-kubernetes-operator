package admission

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	log "github.com/inconshreveable/log15"
)

// After 4.0, before 31
const maxCommunityClusterSz = 8

// TODO: This should be version specific and part of management lib.
// max cluster size for pre-5.0 cluster
const maxEnterpriseClusterSzLT5_0 = 128

// max cluster size for 5.0+ cluster
const maxEnterpriseClusterSzGT5_0 = 256

const versionForSzCheck = "5.0.0"

func validateClusterSize(version string, sz int) error {
	val, err := asconfig.CompareVersions(version, versionForSzCheck)
	if err != nil {
		return fmt.Errorf("Failed to validate cluster size limit from version: %v", err)
	}
	if val < 0 && sz > maxEnterpriseClusterSzLT5_0 {
		return fmt.Errorf("Cluster size cannot be more than %d", maxEnterpriseClusterSzLT5_0)
	}
	if val > 0 && sz > maxEnterpriseClusterSzGT5_0 {
		return fmt.Errorf("Cluster size cannot be more than %d", maxEnterpriseClusterSzGT5_0)
	}
	return nil
}

func validateAerospikeConfig(logger log.Logger, config v1alpha1.Values, storage *aerospikev1alpha1.AerospikeStorageSpec, clSize int) error {
	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}

	// service conf
	serviceConf, ok := config["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.service not a valid map %v", config["service"])
	}
	if _, ok := serviceConf["cluster-name"]; !ok {
		return fmt.Errorf("AerospikeCluster name not found in config. Looks like object is not mutated by webhook")
	}

	// network conf
	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network not a valid map %v", config["network"])
	}
	if _, ok := networkConf["service"]; !ok {
		return fmt.Errorf("Network.service section not found in config. Looks like object is not mutated by webhook")
	}

	// network.tls conf
	if _, ok := networkConf["tls"]; ok {
		tlsConfList := networkConf["tls"].([]interface{})
		for _, tlsConfInt := range tlsConfList {
			tlsConf := tlsConfInt.(map[string]interface{})
			if _, ok := tlsConf["ca-path"]; ok {
				return fmt.Errorf("ca-path not allowed, please use ca-file. tlsConf %v", tlsConf)
			}
		}
	}

	// namespace conf
	nsListInterface, ok := config["namespaces"]
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not a present. aerospikeConfig %v", config)
	} else if nsListInterface == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	}
	if nsList, ok := nsListInterface.([]interface{}); !ok {
		return fmt.Errorf("aerospikeConfig.namespace not valid namespace list %v", nsListInterface)
	} else if err := validateNamespaceConfig(logger, nsList, storage, clSize); err != nil {
		return err
	}

	return nil
}

func validateNamespaceConfig(logger log.Logger, nsConfInterfaceList []interface{}, storage *aerospikev1alpha1.AerospikeStorageSpec, clSize int) error {
	if len(nsConfInterfaceList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace list cannot be empty")
	}

	// Get list of all devices used in namespace. match it with namespace device list
	blockStorageDeviceList, fileStorageList, err := storage.GetStorageList()
	if err != nil {
		return err
	}

	for _, nsConfInterface := range nsConfInterfaceList {
		// Validate new namespace conf
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("namespace conf not in valid format %v", nsConfInterface)
		}

		if err := validateNamespaceReplicationFactor(logger, nsConf, clSize); err != nil {
			return err
		}

		if nsStorage, ok := nsConf["storage-engine"]; ok {
			if nsStorage == nil {
				return fmt.Errorf("storage-engine cannot be nil for namespace %v", nsConf)
			}

			if isInMemoryNamespace(nsConf) {
				// storage-engine memory
				continue
			}

			if !isDeviceNamespace(nsConf) {
				// storage-engine pmem
				return fmt.Errorf("storage-engine not supported for namespace %v", nsConf)
			}

			// TODO: worry about pmem.
			if devices, ok := nsStorage.(map[string]interface{})["devices"]; ok {
				if devices == nil {
					return fmt.Errorf("namespace storage devices cannot be nil %v", nsStorage)
				}

				if _, ok := devices.([]interface{}); !ok {
					return fmt.Errorf("namespace storage device format not valid %v", nsStorage)
				}

				if len(devices.([]interface{})) == 0 {
					return fmt.Errorf("No devices for namespace storage %v", nsStorage)
				}

				for _, device := range devices.([]interface{}) {
					if _, ok := device.(string); !ok {
						return fmt.Errorf("namespace storage device not valid string %v", device)
					}

					// device list Fields cannot be more that 2 in single line. Two in shadow device case. validate.
					if len(strings.Fields(device.(string))) > 2 {
						return fmt.Errorf("Invalid device name %v. Max 2 device can be mentioned in single line (Shadow device config)", device)
					}

					dList := strings.Fields(device.(string))
					for _, dev := range dList {
						// Namespace device should be present in BlockStorage config section
						if !utils.ContainsString(blockStorageDeviceList, dev) {
							return fmt.Errorf("Namespace storage device related devicePath %v not found in Storage config %v", dev, storage)
						}
					}
				}
			}

			if files, ok := nsStorage.(map[string]interface{})["files"]; ok {
				if files == nil {
					return fmt.Errorf("namespace storage files cannot be nil %v", nsStorage)
				}

				if _, ok := files.([]interface{}); !ok {
					return fmt.Errorf("namespace storage files format not valid %v", nsStorage)
				}

				if len(files.([]interface{})) == 0 {
					return fmt.Errorf("No files for namespace storage %v", nsStorage)
				}

				for _, file := range files.([]interface{}) {
					if _, ok := file.(string); !ok {
						return fmt.Errorf("namespace storage file not valid string %v", file)
					}

					dirPath := filepath.Dir(file.(string))
					if !isFileStorageConfiguredForDir(fileStorageList, dirPath) {
						return fmt.Errorf("Namespace storage file related mountPath %v not found in storage config %v", dirPath, storage)
					}
				}
			}
		} else {
			return fmt.Errorf("storage-engine config is required for namespace")
		}
	}

	// Vaidate index-type
	for _, nsConfInterface := range nsConfInterfaceList {
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("namespace conf not in valid format %v", nsConfInterface)
		}

		if isShmemIndexTypeNamespace(nsConf) {
			continue
		}

		if nsIndexStorage, ok := nsConf["index-type"]; ok {
			if mounts, ok := nsIndexStorage.(map[string]interface{})["mounts"]; ok {
				if mounts == nil {
					return fmt.Errorf("namespace index-type mounts cannot be nil %v", nsIndexStorage)
				}

				if _, ok := mounts.([]interface{}); !ok {
					return fmt.Errorf("namespace index-type mounts format not valid %v", nsIndexStorage)
				}

				if len(mounts.([]interface{})) == 0 {
					return fmt.Errorf("No mounts for namespace index-type %v", nsIndexStorage)
				}

				for _, mount := range mounts.([]interface{}) {
					if _, ok := mount.(string); !ok {
						return fmt.Errorf("namespace index-type mount not valid string %v", mount)
					}

					// Namespace index-type mount should be present in filesystem config section
					if !utils.ContainsString(fileStorageList, mount.(string)) {
						return fmt.Errorf("Namespace index-type mount %v not found in Storage config %v", mount, storage)
					}
				}
			}
		}
	}

	return nil
}

func validateNamespaceReplicationFactor(logger log.Logger, nsConf map[string]interface{}, clSize int) error {
	// Validate replication-factor with cluster size only at the time of deployment
	rfInterface, ok := nsConf["replication-factor"]
	if !ok {
		rfInterface = 2 // default replication-factor
	}

	if rf, ok := rfInterface.(int64); ok {
		if int64(clSize) < rf {
			return fmt.Errorf("namespace replication-factor %v cannot be more than cluster size %d", rf, clSize)
		}
	} else if rf, ok := rfInterface.(int); ok {
		if clSize < rf {
			return fmt.Errorf("namespace replication-factor %v cannot be more than cluster size %d", rf, clSize)
		}
	} else {
		return fmt.Errorf("namespace replication-factor %v not valid int or int64", rfInterface)
	}

	return nil
}

func validateAerospikeConfigUpdate(logger log.Logger, newConf, oldConf aerospikev1alpha1.Values) error {
	logger.Info("Validate AerospikeConfig update")

	// Security can not be updated dynamically
	// TODO: How to enable dynamic security update, need to pass policy for individual nodes.
	// auth-enabled and auth-disabled node can co-exist
	oldSec, ok1 := oldConf["security"]
	newSec, ok2 := newConf["security"]
	if ok1 != ok2 ||
		ok1 && ok2 && (!reflect.DeepEqual(oldSec, newSec)) {
		return fmt.Errorf("Cannot update cluster security config")
	}

	// TLS can not be updated dynamically
	// TODO: How to enable dynamic tls update, need to pass policy for individual nodes.
	oldtls, ok11 := oldConf["network"].(map[string]interface{})["tls"]
	newtls, ok22 := newConf["network"].(map[string]interface{})["tls"]
	if ok11 != ok22 ||
		ok11 && ok22 && (!reflect.DeepEqual(oldtls, newtls)) {
		return fmt.Errorf("Cannot update cluster network.tls config")
	}

	// network.service
	if isValueUpdated(oldConf["network"].(map[string]interface{})["service"].(map[string]interface{}), newConf["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.service")
	}
	if isValueUpdated(oldConf["network"].(map[string]interface{})["service"].(map[string]interface{}), newConf["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-authenticate-client") {
		return fmt.Errorf("Cannot update tls-authenticate-client for network.service")
	}

	// network.heartbeat
	if isValueUpdated(oldConf["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), newConf["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.heartbeat")
	}

	// network.fabric
	if isValueUpdated(oldConf["network"].(map[string]interface{})["fabric"].(map[string]interface{}), newConf["network"].(map[string]interface{})["fabric"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.fabric")
	}

	if err := validateNsConfUpdate(logger, newConf, oldConf); err != nil {
		return err
	}

	return nil
}

func validateNsConfUpdate(logger log.Logger, newConf, oldConf aerospikev1alpha1.Values) error {

	newNsConfList := newConf["namespaces"].([]interface{})

	for _, singleConfInterface := range newNsConfList {
		// Validate new namespaceonf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("Namespace conf not in valid format %v", singleConfInterface)
		}

		// Validate new namespace conf from old namespace conf. Few filds cannot be updated
		var found bool
		oldNsConfList := oldConf["namespaces"].([]interface{})

		for _, oldSingleConfInterface := range oldNsConfList {

			oldSingleConf, ok := oldSingleConfInterface.(map[string]interface{})
			if !ok {
				return fmt.Errorf("Namespace conf not in valid format %v", oldSingleConfInterface)
			}

			if singleConf["name"] == oldSingleConf["name"] {
				found = true

				// replication-factor update not allowed
				if isValueUpdated(oldSingleConf, singleConf, "replication-factor") {
					return fmt.Errorf("replication-factor cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
				}
				if isValueUpdated(oldSingleConf, singleConf, "tls-name") {
					return fmt.Errorf("tls-name cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
				}
				if isValueUpdated(oldSingleConf, singleConf, "tls-authenticate-client") {
					return fmt.Errorf("tls-authenticate-client cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
				}

				// storage-engine update not allowed for now
				storage, ok1 := singleConf["storage-engine"]
				oldStorage, ok2 := oldSingleConf["storage-engine"]
				if ok1 && !ok2 || !ok1 && ok2 {
					return fmt.Errorf("storage-engine config cannot be added or removed from existing cluster. Old namespace config %v, new namespace config %v", oldSingleConf, singleConf)
				}
				if ok1 && ok2 && !reflect.DeepEqual(storage, oldStorage) {
					return fmt.Errorf("storage-engine config cannot be changed. Old namespace config %v, new namespace config %v", oldSingleConf, singleConf)
				}
			}
		}

		// Cannot add new persistent namespaces.
		if !found && !isInMemoryNamespace(singleConf) {
			return fmt.Errorf("New persistent storage namespace %s cannot be added. Old namespace list %v, new namespace list %v", singleConf["name"], oldNsConfList, newNsConfList)
		}
	}
	// Check for namespace name len
	return nil
}

func validateAerospikeConfigSchema(logger log.Logger, version string, config aerospikev1alpha1.Values) error {
	logger = logger.New(log.Ctx{"version": version})

	asConf, err := asconfig.NewMapAsConfig(version, config)
	if err != nil {
		return fmt.Errorf("Failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(version)
	if !valid {
		errStrs := []string{}
		for _, e := range validationErr {
			errStrs = append(errStrs, fmt.Sprintf("\t%v\n", *e))
		}

		return fmt.Errorf("Generated config not valid for version %s: %v", version, errStrs)
	}

	return nil
}

func validateRequiredFileStorage(logger log.Logger, config aerospikev1alpha1.Values, storage *aerospikev1alpha1.AerospikeStorageSpec, validationPolicy *aerospikev1alpha1.ValidationPolicySpec, version string) error {

	_, fileStorageList, err := storage.GetStorageList()
	if err != nil {
		return err
	}

	// Validate work directory.
	if !validationPolicy.SkipWorkDirValidate {
		workDirPath := utils.GetWorkDirectory(config)

		if !filepath.IsAbs(workDirPath) {
			return fmt.Errorf("Aerospike work directory path %s must be absolute in storage config %v", workDirPath, storage)
		}

		if !isFileStorageConfiguredForDir(fileStorageList, workDirPath) {
			return fmt.Errorf("Aerospike work directory path %s not mounted on a filesystem in storage config %v", workDirPath, storage)
		}
	}

	if !validationPolicy.SkipXdrDlogFileValidate {
		val, err := asconfig.CompareVersions(version, "5.0.0")
		if err != nil {
			return fmt.Errorf("Failed to check image version: %v", err)
		}
		if val < 0 {
			// Validate xdr-digestlog-path for pre-5.0.0 versions.
			if utils.IsXdrEnabled(config) {
				dglogFilePath, err := utils.GetDigestLogFile(config)
				if err != nil {
					return err
				}

				if !filepath.IsAbs(*dglogFilePath) {
					return fmt.Errorf("xdr digestlog path %v must be absolute in storage config %v", dglogFilePath, storage)
				}

				dglogDirPath := filepath.Dir(*dglogFilePath)

				if !isFileStorageConfiguredForDir(fileStorageList, dglogDirPath) {
					return fmt.Errorf("xdr digestlog path %v not mounted in Storage config %v", dglogFilePath, storage)
				}
			}
		}
	}

	return nil
}

func validateConfigMapVolumes(logger log.Logger, config aerospikev1alpha1.Values, storage *aerospikev1alpha1.AerospikeStorageSpec, validationPolicy *aerospikev1alpha1.ValidationPolicySpec, version string) error {
	_, err := storage.GetConfigMaps()
	return err
}

func getImageVersion(imageStr string) (string, error) {
	_, _, version := utils.ParseDockerImageTag(imageStr)

	if version == "" || strings.ToLower(version) == "latest" {
		return "", fmt.Errorf("Image version is mandatory for image: %v", imageStr)
	}

	return version, nil
}

// isInMemoryNamespace returns true if this namespace config uses memory for storage.
func isInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "memory"
}

// isDeviceNamespace returns true if this namespace config uses device for storage.
func isDeviceNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "device"
}

// isShmemIndexTypeNamespace returns true if this namespace index type is shmem.
func isShmemIndexTypeNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["index-type"]
	if !ok {
		// missing index-type assumed to be shmem.
		return true
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "shmem"
}

// isEnterprise indicates if aerospike image is enterprise
func isEnterprise(image string) bool {
	return strings.Contains(strings.ToLower(image), "enterprise")
}

// isSecretNeeded indicates if aerospikeConfig needs secret
func isSecretNeeded(aerospikeConfig aerospikev1alpha1.Values) bool {
	// feature-key-file needs secret
	if svc, ok := aerospikeConfig["service"]; ok {
		if _, ok := svc.(map[string]interface{})["feature-key-file"]; ok {
			return true
		}
	}

	// tls needs secret
	if utils.IsTLS(aerospikeConfig) {
		return true
	}
	return false
}

// isFileStorageConfiguredForDir indicates if file storage is configured for dir.
func isFileStorageConfiguredForDir(fileStorageList []string, dir string) bool {
	for _, storageMount := range fileStorageList {
		if isPathParentOrSame(storageMount, dir) {
			return true
		}
	}

	return false
}

// isPathParentOrSame indicates if dir1 is a parent or same as dir2.
func isPathParentOrSame(dir1 string, dir2 string) bool {
	if relPath, err := filepath.Rel(dir1, dir2); err == nil {
		// If dir1 is not a parent directory then relative path will have to climb up directory hierarchy of dir1.
		return !strings.HasPrefix(relPath, "..")
	}

	// Paths are unrelated.
	return false
}

func (s *ClusterValidatingAdmissionWebhook) validatePodSpec() error {
	sidecarNames := map[string]int{}

	for _, sidecar := range s.obj.Spec.PodSpec.Sidecars {
		// Check for reserved sidecar name
		if sidecar.Name == utils.AerospikeServerContainerName || sidecar.Name == utils.AerospikeServerInitContainerName {
			return fmt.Errorf("Cannot use reserved sidecar name: %v", sidecar.Name)
		}

		// Check for duplicate names
		if _, ok := sidecarNames[sidecar.Name]; ok {
			return fmt.Errorf("Connot have duplicate names of sidecars: %v", sidecar.Name)
		}
		sidecarNames[sidecar.Name] = 1

		_, err := getImageVersion(sidecar.Image)

		if err != nil {
			return err
		}
	}

	return nil
}
