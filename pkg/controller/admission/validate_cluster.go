package admission

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/deployment"
	log "github.com/inconshreveable/log15"
	av1beta1 "k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ClusterValidatingAdmissionWebhook admission validation webhook
type ClusterValidatingAdmissionWebhook struct {
	obj aerospikev1alpha1.AerospikeCluster
}

// After 4.0, before 31
const maxCommunityClusterSz = 8

// TODO: This should be version specific and part of management lib.
const maxEnterpriseClusterSz = 128

// ValidateAerospikeCluster validate cluster operation
func ValidateAerospikeCluster(req webhook.AdmissionRequest) webhook.AdmissionResponse {

	decoder, _ := admission.NewDecoder(scheme)

	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.Object, newAeroCluster)

	oldAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.OldObject, oldAeroCluster)

	s := ClusterValidatingAdmissionWebhook{
		obj: *newAeroCluster,
	}

	// validate the new AerospikeCluster
	if req.Operation == av1beta1.Create {
		err := s.ValidateCreate()
		if err != nil {
			log.Error("Validate AerospikeCluster create failed", log.Ctx{"err": err})
			return webhook.Denied(err.Error())
		}
	}

	// if this is an update, validate that the transition from old to new
	if req.Operation == av1beta1.Update {
		err := s.ValidateUpdate(*oldAeroCluster)
		if err != nil {
			log.Error("Validate AerospikeCluster update failed", log.Ctx{"err": err})
			return webhook.Denied(err.Error())
		}
	}
	return webhook.Allowed("Validation passed. No create or update")
}

// ValidateCreate validate create
func (s *ClusterValidatingAdmissionWebhook) ValidateCreate() error {
	log.Info("Validate AerospikeCluster create")

	return s.validate()
}

// ValidateUpdate validate update
func (s *ClusterValidatingAdmissionWebhook) ValidateUpdate(old aerospikev1alpha1.AerospikeCluster) error {
	log.Info("Validate AerospikeCluster update")
	if err := s.validate(); err != nil {
		return err
	}

	// Jump version should not be allowed
	newVersion := strings.Split(s.obj.Spec.Build, ":")[1]
	oldVersion := strings.Split(old.Spec.Build, ":")[1]
	if err := deployment.IsValidUpgrade(oldVersion, newVersion); err != nil {
		return fmt.Errorf("Failed to start upgrade: %v", err)
	}

	if !reflect.DeepEqual(s.obj.Spec.Storage, old.Spec.Storage) {
		return fmt.Errorf("Storage config cannot be updated. Old %v, new %v", old.Spec.Storage, s.obj.Spec.Storage)
	}
	if s.obj.Spec.MultiPodPerHost != old.Spec.MultiPodPerHost {
		return fmt.Errorf("Cannot update MultiPodPerHost setting")
	}

	// TODO: How to enable dynamic security update, need to pass policy for individual nodes.
	// auth-enabled and auth-disabled node can co-exist
	oldSec, ok1 := old.Spec.AerospikeConfig["security"]
	newSec, ok2 := s.obj.Spec.AerospikeConfig["security"]
	if ok1 != ok2 ||
		ok1 && ok2 && (!reflect.DeepEqual(oldSec, newSec)) {
		return fmt.Errorf("Cannot update cluster security config")
	}
	// TODO: How to enable dynamic tls update, need to pass policy for individual nodes.
	oldtls, ok11 := old.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"]
	newtls, ok22 := s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"]
	if ok11 != ok22 ||
		ok11 && ok22 && (!reflect.DeepEqual(oldtls, newtls)) {
		return fmt.Errorf("Cannot update cluster network.tls config")
	}

	// network.service
	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.service")
	}
	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-authenticate-client") {
		return fmt.Errorf("Cannot update tls-authenticate-client for network.service")
	}
	// network.heartbeat
	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.heartbeat")
	}
	// network.fabric
	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["fabric"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["fabric"].(map[string]interface{}), "tls-name") {
		return fmt.Errorf("Cannot update tls-name for network.fabric")
	}

	if err := s.validateNsConfUpdate(old); err != nil {
		return err
	}
	return nil
}

// ValidateCreate validate create
func (s *ClusterValidatingAdmissionWebhook) validate() error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(&s.obj)})

	logger.Debug("Validate AerospikeCluster spec", log.Ctx{"obj.Spec": s.obj.Spec})
	if s.obj.Name == "" {
		return fmt.Errorf("AerospikeCluster name cannot be empty")
	}
	if strings.Contains(s.obj.Name, "-") {
		// Few parsing logic depend on this
		return fmt.Errorf("AerospikeCluster name cannot have char '-'")
	}

	if s.obj.Namespace == "" {
		return fmt.Errorf("AerospikeCluster namespace name cannot be empty")
	}

	// TODO: comment or uncomment for community support
	if !isEnterprise(s.obj.Spec.Build) {
		return fmt.Errorf("CommunityEdition Cluster not supported")
	}

	// Check size
	if s.obj.Spec.Size == 0 {
		return fmt.Errorf("Invalid cluster size 0")
	}

	if s.obj.Spec.Size > maxEnterpriseClusterSz {
		return fmt.Errorf("Cluster size cannot be more than %d", maxEnterpriseClusterSz)
	}

	// Check if multiPodPerHost is false then number of kubernetes host should be >= size

	// Check for AerospikeConfigSecret.
	// TODO: Should we validate mount path also. Config has tls info at different paths, fetching and validating that may be little complex
	if isSecretNeeded(s.obj.Spec.AerospikeConfig) && s.obj.Spec.AerospikeConfigSecret.SecretName == "" {
		return fmt.Errorf("aerospikeConfig has feature-key-file path or tls paths. User need to create a secret for these and provide its info in `aerospikeConfigSecret` field")
	}

	// Check Build
	build := strings.Split(s.obj.Spec.Build, ":")
	if len(build) != 2 {
		return fmt.Errorf("Build name %s not valid. Should be in the format of repo:version", s.obj.Spec.Build)
	}
	version := build[1]
	val, err := asconfig.CompareVersions(version, baseVersion)
	if err != nil {
		return fmt.Errorf("Failed to check build version: %v", err)
	}
	if val < 0 {
		return fmt.Errorf("Build version %s not supported. Base version %s", version, baseVersion)
	}

	// Get list of all devices used in namespace. match it with namespace device list
	storagePaths := map[string]int{}
	var blockStorageDeviceList []string
	var fileStorageList []string

	for _, volume := range s.obj.Spec.Storage.Volumes {
		if volume.StorageClass == "" {
			return fmt.Errorf("Mising storage class. Invalid volume: %v", volume)
		}

		if _, ok := storagePaths[volume.Path]; ok {
			return fmt.Errorf("Invalid volume. Path %s, is repeated", volume.Path)
		}

		storagePaths[volume.Path] = 1

		if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeBlock {
			blockStorageDeviceList = append(blockStorageDeviceList, volume.Path)
		} else {
			fileStorageList = append(fileStorageList, volume.Path)
		}
	}

	if err := s.validateAerospikeConfig(); err != nil {
		return err
	}

	if nsConf, ok := s.obj.Spec.AerospikeConfig["namespace"].([]interface{}); ok {
		if len(nsConf) == 0 {
			return fmt.Errorf("aerospikeConfig.namespace list cannot be empty")
		}
		for _, singleConfInterface := range nsConf {
			// Validate new namespace conf
			singleConf, ok := singleConfInterface.(map[string]interface{})
			if !ok {
				return fmt.Errorf("namespace conf not in valid format %v", singleConfInterface)
			}
			// Validate replication-factor with cluster size only at the time of deployment
			rfInterface, ok := singleConf["replication-factor"]
			if !ok {
				rfInterface = 2 // default replication-factor
			}
			if rf, ok := rfInterface.(int64); ok {
				if int64(s.obj.Spec.Size) < rf {
					return fmt.Errorf("namespace replication-factor %v cannot be more than cluster size %d", rf, s.obj.Spec.Size)
				}
			} else if rf, ok := rfInterface.(int); ok {
				if int(s.obj.Spec.Size) < rf {
					return fmt.Errorf("namespace replication-factor %v cannot be more than cluster size %d", rf, s.obj.Spec.Size)
				}
			} else {
				return fmt.Errorf("namespace replication-factor %v not valid int or int64", rfInterface)
			}

			if storage, ok := singleConf["storage-engine"]; ok {
				if storage == nil {
					// TODO: Should it be error
					return fmt.Errorf("storage-engine cannot be nil for namespace %v", singleConf)
				}
				if _, ok := storage.(string); ok {
					// storage-engine memory
					continue
				}
				if devices, ok := storage.(map[string]interface{})["device"]; ok {
					if devices == nil {
						return fmt.Errorf("namespace storage devices cannot be nil %v", singleConf)
					}
					if _, ok := devices.([]interface{}); !ok {
						return fmt.Errorf("namespace storage device format not valid %v", singleConf)
					}
					if len(devices.([]interface{})) == 0 {
						return fmt.Errorf("No devices for namespace storage %v", singleConf)
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
								return fmt.Errorf("Namespace storage device related devicePath %v not found in Storage config %v", dev, s.obj.Spec.Storage)
							}
						}
						logger.Debug("Valid namespace storage device", log.Ctx{"device": device})
					}
				}
				if files, ok := storage.(map[string]interface{})["file"]; ok {
					if files == nil {
						return fmt.Errorf("namespace storage files cannot be nil %v", singleConf)
					}
					if _, ok := files.([]interface{}); !ok {
						return fmt.Errorf("namespace storage files format not valid %v", singleConf)
					}
					if len(files.([]interface{})) == 0 {
						return fmt.Errorf("No files for namespace storage %v", singleConf)
					}
					for _, file := range files.([]interface{}) {
						if _, ok := file.(string); !ok {
							return fmt.Errorf("namespace storage file not valid string %v", file)
						}
						dirPath := filepath.Dir(file.(string))
						if !isFileStorageConfiguredForDir(fileStorageList, dirPath) {
							return fmt.Errorf("Namespace storage file related mountPath %v not found in storage config %v", dirPath, s.obj.Spec.Storage)
						}
						logger.Debug("Valid namespace storage file", log.Ctx{"file": file})
					}
				}
			} else {
				return fmt.Errorf("storage-engine config is required for namespace")
			}
		}
	}

	// Check if passed aerospikeConfig is valid or not
	config := s.obj.Spec.AerospikeConfig
	asConf, err := asconfig.NewMapAsConfig(version, config)
	if err != nil {
		return fmt.Errorf("Failed to load config map by lib: %v", err)
	}
	valid, validationErr, err := asConf.IsValid(version)
	if !valid {
		for _, e := range validationErr {
			logger.Info("validation failed", log.Ctx{"err": *e})
		}
		return fmt.Errorf("Generated config not valid for version %s: %v", version, err)
	}

	// Validate resource and limit
	res := s.obj.Spec.Resources
	if res == nil || res.Requests == nil {
		return fmt.Errorf("Resources or Resources.Requests cannot be nil")
	}
	if res.Requests.Memory().IsZero() || res.Requests.Cpu().IsZero() {
		return fmt.Errorf("Resources.Requests.Memory or Resources.Requests.Cpu cannot be zero")
	}
	if res.Limits != nil &&
		((res.Limits.Cpu().Cmp(*res.Requests.Cpu()) < 0) ||
			(res.Limits.Memory().Cmp(*res.Requests.Memory()) < 0)) {
		return fmt.Errorf("Resource.Limits cannot be less than Resource.Requests. Resources %v", res)
	}

	err = s.validateRequiredFileStorage(fileStorageList, version)
	if err != nil {
		return err
	}

	// Validate access control
	err = s.validateAccessControl(s.obj)
	return err
}

func (s *ClusterValidatingAdmissionWebhook) validateAerospikeConfig() error {
	config := s.obj.Spec.AerospikeConfig
	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}
	// namespace conf
	nsConf, ok := config["namespace"]
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not a present. aerospikeConfig %v", config)
	} else if nsConf == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	} else if nsList, ok := nsConf.([]interface{}); !ok {
		return fmt.Errorf("aerospikeConfig.namespace not valid namespace list %v", nsConf)
	} else if len(nsList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace cannot be empty. aerospikeConfig %v", config)
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
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validateRequiredFileStorage(fileStorageList []string, version string) error {
	config := s.obj.Spec.AerospikeConfig

	// Validate work directory.
	if !s.obj.Spec.ValidationPolicy.SkipWorkDirValidate {
		workDirPath := utils.GetWorkDirectory(config)

		if !filepath.IsAbs(workDirPath) {
			return fmt.Errorf("Aerospike work directory path %v must be absolute", workDirPath, s.obj.Spec.Storage)
		}

		if !isFileStorageConfiguredForDir(fileStorageList, workDirPath) {
			return fmt.Errorf("Aerospike work directory path %v not mounted in storage config %v", workDirPath, s.obj.Spec.Storage)
		}
	}

	if !s.obj.Spec.ValidationPolicy.SkipXdrDlogFileValidate {
		val, err := asconfig.CompareVersions(version, "5.0.0")
		if err != nil {
			return fmt.Errorf("Failed to check build version: %v", err)
		}
		if val < 0 {
			// Validate xdr-digestlog-path for pre-5.0.0 versions.
			if utils.IsXdrEnabled(config) {
				dglogFilePath, err := utils.GetDigestLogFile(config)
				if err != nil {
					return err
				}

				if !filepath.IsAbs(*dglogFilePath) {
					return fmt.Errorf("xdr digestlog path %v must be absolute", dglogFilePath, s.obj.Spec.Storage)
				}

				dglogDirPath := filepath.Dir(*dglogFilePath)

				if !isFileStorageConfiguredForDir(fileStorageList, dglogDirPath) {
					return fmt.Errorf("xdr digestlog path %v not mounted in fileStorage config %v", dglogFilePath, s.obj.Spec.Storage)
				}
			}
		}
	}

	return nil
}

// isInMemoryNamespace returns true if this nameapce config uses memory for storage.
func isInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	return !ok || storage == "memory"
}

func (s *ClusterValidatingAdmissionWebhook) validateNsConfUpdate(old aerospikev1alpha1.AerospikeCluster) error {

	nsConf := s.obj.Spec.AerospikeConfig["namespace"].([]interface{})
	for _, singleConfInterface := range nsConf {
		// Validate new namespaceonf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("Namespace conf not in valid format %v", singleConfInterface)
		}

		// Validate new namespace conf from old namespace conf. Few filds cannot be updated
		var found bool
		oldNsConf := old.Spec.AerospikeConfig["namespace"].([]interface{})
		for _, oldSingleConfInterface := range oldNsConf {
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

		// New namespace not allowed to add
		if !found && !isInMemoryNamespace(singleConf) {
			return fmt.Errorf("New persistent storage namespace %s cannot be added. Old namespace list %v, new namespace list %v", singleConf["name"], oldNsConf, nsConf)
		}
	}
	// Check for namespace name len
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validateAccessControl(aeroCluster aerospikev1alpha1.AerospikeCluster) error {
	_, err := accessControl.IsAerospikeAccessControlValid(&aeroCluster.Spec)
	return err
}

func isEnterprise(build string) bool {
	return strings.Contains(strings.ToLower(build), "enterprise")
}

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
