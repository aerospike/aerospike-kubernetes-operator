package admission

import (
	"fmt"
	"path/filepath"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/asconfig"
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

// // ValidateUpdate validate update
// func (s *ClusterValidatingAdmissionWebhook) ValidateUpdate(old aerospikev1alpha1.AerospikeCluster) error {
// 	log.Info("Validate AerospikeCluster update")
// 	if err := s.validate(); err != nil {
// 		return err
// 	}

// 	// Jump version should not be allowed
// 	newVersion := strings.Split(s.obj.Spec.Build, ":")[1]
// 	oldVersion := strings.Split(old.Spec.Build, ":")[1]
// 	if err := deployment.IsValidUpgrade(oldVersion, newVersion); err != nil {
// 		return fmt.Errorf("Failed to start upgrade: %v", err)
// 	}

// 	// Volume storage update is not allowed but cascadeDelete policy is allowed
// 	if err := old.Spec.Storage.ValidateStorageSpecChange(s.obj.Spec.Storage); err != nil {
// 		return fmt.Errorf("Storage config cannot be updated: %v", err)
// 	}

// 	// MultiPodPerHost can not be updated
// 	if s.obj.Spec.MultiPodPerHost != old.Spec.MultiPodPerHost {
// 		return fmt.Errorf("Cannot update MultiPodPerHost setting")
// 	}

// 	// Security can not be updated dynamically
// 	// TODO: How to enable dynamic security update, need to pass policy for individual nodes.
// 	// auth-enabled and auth-disabled node can co-exist
// 	oldSec, ok1 := old.Spec.AerospikeConfig["security"]
// 	newSec, ok2 := s.obj.Spec.AerospikeConfig["security"]
// 	if ok1 != ok2 ||
// 		ok1 && ok2 && (!reflect.DeepEqual(oldSec, newSec)) {
// 		return fmt.Errorf("Cannot update cluster security config")
// 	}

// 	// TLS can not be updated dynamically
// 	// TODO: How to enable dynamic tls update, need to pass policy for individual nodes.
// 	oldtls, ok11 := old.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"]
// 	newtls, ok22 := s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"]
// 	if ok11 != ok22 ||
// 		ok11 && ok22 && (!reflect.DeepEqual(oldtls, newtls)) {
// 		return fmt.Errorf("Cannot update cluster network.tls config")
// 	}

// 	// network.service
// 	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-name") {
// 		return fmt.Errorf("Cannot update tls-name for network.service")
// 	}
// 	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["service"].(map[string]interface{}), "tls-authenticate-client") {
// 		return fmt.Errorf("Cannot update tls-authenticate-client for network.service")
// 	}
// 	// network.heartbeat
// 	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["heartbeat"].(map[string]interface{}), "tls-name") {
// 		return fmt.Errorf("Cannot update tls-name for network.heartbeat")
// 	}
// 	// network.fabric
// 	if isValueUpdated(old.Spec.AerospikeConfig["network"].(map[string]interface{})["fabric"].(map[string]interface{}), s.obj.Spec.AerospikeConfig["network"].(map[string]interface{})["fabric"].(map[string]interface{}), "tls-name") {
// 		return fmt.Errorf("Cannot update tls-name for network.fabric")
// 	}

// 	if err := s.validateNsConfUpdate(old); err != nil {
// 		return err
// 	}

// 	if err := s.validateRackUpdate(old); err != nil {
// 		return err
// 	}
// 	return nil
// }

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
	version, err := getBuildVersion(s.obj.Spec.Build)
	if err != nil {
		return err
	}

	val, err := asconfig.CompareVersions(version, baseVersion)
	if err != nil {
		return fmt.Errorf("Failed to check build version: %v", err)
	}
	if val < 0 {
		return fmt.Errorf("Build version %s not supported. Base version %s", version, baseVersion)
	}

	// Validate common aerospike config
	aeroConfig := s.obj.Spec.AerospikeConfig
	if err := s.validateAerospikeConfig(aeroConfig); err != nil {
		return err
	}

	// Check if passed aerospikeConfig is valid or not
	if err := validateAerospikeConfigSchema(version, s.obj.Spec.AerospikeConfig); err != nil {
		return fmt.Errorf("AerospikeConfig not valid %v", err)
	}

	// Validate resource and limit
	if err := s.validateResourceAndLimits(); err != nil {
		return err
	}

	_, fileStorageList, err := s.getStorageList()
	if err != nil {
		return err
	}
	err = s.validateRequiredFileStorage(fileStorageList, version)
	if err != nil {
		return err
	}

	// Validate access control
	if err := s.validateAccessControl(s.obj); err != nil {
		return err
	}

	// Validate rackConfig
	if err := s.validateRackConfig(); err != nil {
		return err
	}
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) getStorageList() (blockStorageDeviceList []string, fileStorageList []string, err error) {
	// Get list of all devices used in namespace. match it with namespace device list
	storagePaths := map[string]int{}

	for _, volume := range s.obj.Spec.Storage.Volumes {
		if volume.StorageClass == "" {
			return nil, nil, fmt.Errorf("Mising storage class. Invalid volume: %v", volume)
		}

		if !filepath.IsAbs(volume.Path) {
			return nil, nil, fmt.Errorf("Volume path should be absolute: %s", volume.Path)
		}

		if _, ok := storagePaths[volume.Path]; ok {
			return nil, nil, fmt.Errorf("Duplicate volume path %s", volume.Path)
		}

		storagePaths[volume.Path] = 1

		if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeBlock {
			if volume.InitMethod == nil || *volume.InitMethod == aerospikev1alpha1.AerospikeVolumeInitMethodDeleteFiles {
				return nil, nil, fmt.Errorf("Invalid init method %v for block volume: %v", *volume.InitMethod, volume)
			}

			blockStorageDeviceList = append(blockStorageDeviceList, volume.Path)
		} else {
			if *volume.InitMethod != aerospikev1alpha1.AerospikeVolumeInitMethodNone && *volume.InitMethod != aerospikev1alpha1.AerospikeVolumeInitMethodDeleteFiles {
				return nil, nil, fmt.Errorf("Invalid init method %v for filesystem volume: %v2", *volume.InitMethod, volume)
			}
			fileStorageList = append(fileStorageList, volume.Path)
		}
	}
	return blockStorageDeviceList, fileStorageList, nil
}

// func (s *ClusterValidatingAdmissionWebhook) validateAerospikeConfig() error {
// 	config := s.obj.Spec.AerospikeConfig
// 	if config == nil {
// 		return fmt.Errorf("aerospikeConfig cannot be empty")
// 	}
// 	// namespace conf
// 	nsConf, ok := config["namespace"]
// 	if !ok {
// 		return fmt.Errorf("aerospikeConfig.namespace not a present. aerospikeConfig %v", config)
// 	} else if nsConf == nil {
// 		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
// 	} else if nsList, ok := nsConf.([]interface{}); !ok {
// 		return fmt.Errorf("aerospikeConfig.namespace not valid namespace list %v", nsConf)
// 	} else if len(nsList) == 0 {
// 		return fmt.Errorf("aerospikeConfig.namespace cannot be empty. aerospikeConfig %v", config)
// 	}

// 	// service conf
// 	serviceConf, ok := config["service"].(map[string]interface{})
// 	if !ok {
// 		return fmt.Errorf("aerospikeConfig.service not a valid map %v", config["service"])
// 	}
// 	if _, ok := serviceConf["cluster-name"]; !ok {
// 		return fmt.Errorf("AerospikeCluster name not found in config. Looks like object is not mutated by webhook")
// 	}

// 	// network conf
// 	networkConf, ok := config["network"].(map[string]interface{})
// 	if !ok {
// 		return fmt.Errorf("aerospikeConfig.network not a valid map %v", config["network"])
// 	}
// 	if _, ok := networkConf["service"]; !ok {
// 		return fmt.Errorf("Network.service section not found in config. Looks like object is not mutated by webhook")
// 	}

// 	// network.tls conf
// 	if _, ok := networkConf["tls"]; ok {
// 		tlsConfList := networkConf["tls"].([]interface{})
// 		for _, tlsConfInt := range tlsConfList {
// 			tlsConf := tlsConfInt.(map[string]interface{})
// 			if _, ok := tlsConf["ca-path"]; ok {
// 				return fmt.Errorf("ca-path not allowed, please use ca-file. tlsConf %v", tlsConf)
// 			}
// 		}
// 	}
// 	return nil
// }

func (s *ClusterValidatingAdmissionWebhook) validateRequiredFileStorage(fileStorageList []string, version string) error {
	config := s.obj.Spec.AerospikeConfig

	// Validate work directory.
	if !s.obj.Spec.ValidationPolicy.SkipWorkDirValidate {
		workDirPath := utils.GetWorkDirectory(config)

		if !filepath.IsAbs(workDirPath) {
			return fmt.Errorf("Aerospike work directory path %s must be absolute in storage config %v", workDirPath, s.obj.Spec.Storage)
		}

		if !isFileStorageConfiguredForDir(fileStorageList, workDirPath) {
			return fmt.Errorf("Aerospike work directory path %s not mounted on a filesystem in storage config %v", workDirPath, s.obj.Spec.Storage)
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
					return fmt.Errorf("xdr digestlog path %v must be absolute in storage config %v", dglogFilePath, s.obj.Spec.Storage)
				}

				dglogDirPath := filepath.Dir(*dglogFilePath)

				if !isFileStorageConfiguredForDir(fileStorageList, dglogDirPath) {
					return fmt.Errorf("xdr digestlog path %v not mounted in Storage config %v", dglogFilePath, s.obj.Spec.Storage)
				}
			}
		}
	}

	return nil
}

// func (s *ClusterValidatingAdmissionWebhook) validateNsConfUpdate(old aerospikev1alpha1.AerospikeCluster) error {

// 	nsConf := s.obj.Spec.AerospikeConfig["namespace"].([]interface{})
// 	for _, singleConfInterface := range nsConf {
// 		// Validate new namespaceonf
// 		singleConf, ok := singleConfInterface.(map[string]interface{})
// 		if !ok {
// 			return fmt.Errorf("Namespace conf not in valid format %v", singleConfInterface)
// 		}

// 		// Validate new namespace conf from old namespace conf. Few filds cannot be updated
// 		var found bool
// 		oldNsConf := old.Spec.AerospikeConfig["namespace"].([]interface{})
// 		for _, oldSingleConfInterface := range oldNsConf {
// 			oldSingleConf, ok := oldSingleConfInterface.(map[string]interface{})
// 			if !ok {
// 				return fmt.Errorf("Namespace conf not in valid format %v", oldSingleConfInterface)
// 			}
// 			if singleConf["name"] == oldSingleConf["name"] {
// 				found = true
// 				// replication-factor update not allowed
// 				if isValueUpdated(oldSingleConf, singleConf, "replication-factor") {
// 					return fmt.Errorf("replication-factor cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
// 				}
// 				if isValueUpdated(oldSingleConf, singleConf, "tls-name") {
// 					return fmt.Errorf("tls-name cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
// 				}
// 				if isValueUpdated(oldSingleConf, singleConf, "tls-authenticate-client") {
// 					return fmt.Errorf("tls-authenticate-client cannot be update. old nsconf %v, new nsconf %v", oldSingleConf, singleConf)
// 				}
// 				// storage-engine update not allowed for now
// 				storage, ok1 := singleConf["storage-engine"]
// 				oldStorage, ok2 := oldSingleConf["storage-engine"]
// 				if ok1 && !ok2 || !ok1 && ok2 {
// 					return fmt.Errorf("storage-engine config cannot be added or removed from existing cluster. Old namespace config %v, new namespace config %v", oldSingleConf, singleConf)
// 				}
// 				if ok1 && ok2 && !reflect.DeepEqual(storage, oldStorage) {
// 					return fmt.Errorf("storage-engine config cannot be changed. Old namespace config %v, new namespace config %v", oldSingleConf, singleConf)
// 				}
// 			}

// 		}

// 		// New namespace not allowed to add
// 		if !found && !isInMemoryNamespace(singleConf) {
// 			return fmt.Errorf("New persistent storage namespace %s cannot be added. Old namespace list %v, new namespace list %v", singleConf["name"], oldNsConf, nsConf)
// 		}
// 	}
// 	// Check for namespace name len
// 	return nil
// }

func (s *ClusterValidatingAdmissionWebhook) validateAccessControl(aeroCluster aerospikev1alpha1.AerospikeCluster) error {
	_, err := accessControl.IsAerospikeAccessControlValid(&aeroCluster.Spec)
	return err
}

// func (s *ClusterValidatingAdmissionWebhook) validateRackUpdate(old aerospikev1alpha1.AerospikeCluster) error {
// 	if reflect.DeepEqual(s.obj.Spec.RackConfig, old.Spec.RackConfig) {
// 		return nil
// 	}
// 	for _, newRack := range s.obj.Spec.RackConfig.Racks {
// 		// Check for defaultRackID in mutate.
// 		if newRack.ID < 1 || newRack.ID > 1000000 {
// 			return fmt.Errorf("Invalid rackID. RackID range (1, 1000000)")
// 		}
// 	}
// 	// Old racks can not be updated
// 	// Also need to exclude a default rack with default rack ID. No need to check here, user should not provide or update default rackID
// 	// Also when user add new rackIDs old default will be removed by reconciler.
// 	for _, oldRack := range old.Spec.RackConfig.Racks {
// 		for _, newRack := range s.obj.Spec.RackConfig.Racks {
// 			//	if oldRack.ID == newRack.ID && !reflect.DeepEqual(oldRack, newRack) {
// 			if oldRack.ID == newRack.ID {
// 				if oldRack.NodeName != newRack.NodeName ||
// 					oldRack.RackLabel != newRack.RackLabel ||
// 					oldRack.Region != newRack.Region ||
// 					oldRack.Zone != newRack.Zone {

// 					return fmt.Errorf("Old RackConfig (NodeName, RackLabel, Region, Zone) can not be updated. Old rack %v, new rack %v", oldRack, newRack)
// 				}
// 				// TODO: Check aerospikeConfig update
// 			}
// 		}
// 	}
// 	return nil
// }

// isInMemoryNamespace returns true if this nameapce config uses memory for storage.
func isInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	return !ok || storage == "memory"
}

// func rackListToMap(rackList []aerospikev1alpha1.RackConfig) map[int]aerospikev1alpha1.RackConfig {
// 	rackMap := map[int]aerospikev1alpha1.RackConfig{}
// 	for _, rack := range rackList {
// 		rackMap[rack.ID] = rack
// 	}
// 	return rackMap
// }

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
