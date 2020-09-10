package admission

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
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
// max cluster size for pre-5.0 cluster
const maxEnterpriseClusterSzLT5_0 = 128

// max cluster size for 5.0+ cluster
const maxEnterpriseClusterSzGT5_0 = 256

const versionForSzCheck = "5.0.0"

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

	// Volume storage update is not allowed but cascadeDelete policy is allowed
	if err := old.Spec.Storage.ValidateStorageSpecChange(s.obj.Spec.Storage); err != nil {
		return fmt.Errorf("Storage config cannot be updated: %v", err)
	}

	// MultiPodPerHost can not be updated
	if s.obj.Spec.MultiPodPerHost != old.Spec.MultiPodPerHost {
		return fmt.Errorf("Cannot update MultiPodPerHost setting")
	}

	// Validate AerospikeConfig update
	if err := validateAerospikeConfigUpdate(s.obj.Spec.AerospikeConfig, old.Spec.AerospikeConfig); err != nil {
		return err
	}

	// Validate RackConfig update
	if err := s.validateRackUpdate(old); err != nil {
		return err
	}
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validate() error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(&s.obj)})

	logger.Debug("Validate AerospikeCluster spec", log.Ctx{"obj.Spec": s.obj.Spec})

	// Validate obj name
	if s.obj.Name == "" {
		return fmt.Errorf("AerospikeCluster name cannot be empty")
	}
	if strings.Contains(s.obj.Name, "-") {
		// Few parsing logic depend on this
		return fmt.Errorf("AerospikeCluster name cannot have char '-'")
	}
	if strings.Contains(s.obj.Name, " ") {
		// Few parsing logic depend on this
		return fmt.Errorf("AerospikeCluster name cannot have spaces")
	}

	// Validate obj namespace
	if s.obj.Namespace == "" {
		return fmt.Errorf("AerospikeCluster namespace name cannot be empty")
	}
	if strings.Contains(s.obj.Namespace, " ") {
		// Few parsing logic depend on this
		return fmt.Errorf("AerospikeCluster name cannot have spaces")
	}

	// Validate build type. Only enterprise build allowed for now
	if !isEnterprise(s.obj.Spec.Build) {
		return fmt.Errorf("CommunityEdition Cluster not supported")
	}

	// Validate size
	if s.obj.Spec.Size == 0 {
		return fmt.Errorf("Invalid cluster size 0")
	}

	// TODO: Validate if multiPodPerHost is false then number of kubernetes host should be >= size

	// Validate for AerospikeConfigSecret.
	// TODO: Should we validate mount path also. Config has tls info at different paths, fetching and validating that may be little complex
	if isSecretNeeded(s.obj.Spec.AerospikeConfig) && s.obj.Spec.AerospikeConfigSecret.SecretName == "" {
		return fmt.Errorf("aerospikeConfig has feature-key-file path or tls paths. User need to create a secret for these and provide its info in `aerospikeConfigSecret` field")
	}

	// Validate Build version
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

	err = validateClusterSize(version, int(s.obj.Spec.Size))
	if err != nil {
		return err
	}

	// Validate common aerospike config
	aeroConfig := s.obj.Spec.AerospikeConfig
	if err := validateAerospikeConfig(aeroConfig, &s.obj.Spec.Storage, int(s.obj.Spec.Size)); err != nil {
		return err
	}

	// Validate if passed aerospikeConfig
	if err := validateAerospikeConfigSchema(version, s.obj.Spec.AerospikeConfig); err != nil {
		return fmt.Errorf("AerospikeConfig not valid %v", err)
	}

	err = validateRequiredFileStorage(aeroConfig, &s.obj.Spec.Storage, s.obj.Spec.ValidationPolicy, version)
	if err != nil {
		return err
	}

	// Validate resource and limit
	if err := s.validateResourceAndLimits(); err != nil {
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

func (s *ClusterValidatingAdmissionWebhook) validateRackUpdate(old aerospikev1alpha1.AerospikeCluster) error {
	log.Info("Validate rack update")

	if reflect.DeepEqual(s.obj.Spec.RackConfig, old.Spec.RackConfig) {
		return nil
	}

	// Allow updating namespace list to dynamically enable, disable rack on namespaces
	// if !reflect.DeepEqual(s.obj.Spec.RackConfig.Namespaces, old.Spec.RackConfig.Namespaces) {
	// 	return fmt.Errorf("Rack namespaces cannot be updated. Old %v, new %v", old.Spec.RackConfig.Namespaces, s.obj.Spec.RackConfig.Namespaces)
	// }

	// Old racks can not be updated
	// Also need to exclude a default rack with default rack ID. No need to check here, user should not provide or update default rackID
	// Also when user add new rackIDs old default will be removed by reconciler.
	for _, oldRack := range old.Spec.RackConfig.Racks {
		for _, newRack := range s.obj.Spec.RackConfig.Racks {

			if oldRack.ID == newRack.ID {

				if oldRack.NodeName != newRack.NodeName ||
					oldRack.RackLabel != newRack.RackLabel ||
					oldRack.Region != newRack.Region ||
					oldRack.Zone != newRack.Zone {

					return fmt.Errorf("Old RackConfig (NodeName, RackLabel, Region, Zone) can not be updated. Old rack %v, new rack %v", oldRack, newRack)
				}

				if len(oldRack.AerospikeConfig) != 0 || len(newRack.AerospikeConfig) != 0 {
					// Config might have changed
					newConf := utils.GetRackAerospikeConfig(&s.obj, newRack)
					oldConf := utils.GetRackAerospikeConfig(&old, oldRack)
					// Validate aerospikeConfig update
					if err := validateAerospikeConfigUpdate(newConf, oldConf); err != nil {
						return fmt.Errorf("Invalid update in Rack(ID: %d) aerospikeConfig: %v", oldRack.ID, err)
					}
				}

				if len(oldRack.Storage.Volumes) != 0 || len(newRack.Storage.Volumes) != 0 {
					// Storage might have changed
					oldStorage := utils.GetRackStorage(&old, oldRack)
					newStorage := utils.GetRackStorage(&s.obj, newRack)
					// Volume storage update is not allowed but cascadeDelete policy is allowed
					if err := oldStorage.ValidateStorageSpecChange(newStorage); err != nil {
						return fmt.Errorf("Rack storage config cannot be updated: %v", err)
					}
				}

				break
			}
		}
	}
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validateAccessControl(aeroCluster aerospikev1alpha1.AerospikeCluster) error {
	_, err := accessControl.IsAerospikeAccessControlValid(&aeroCluster.Spec)
	return err
}

func (s *ClusterValidatingAdmissionWebhook) validateResourceAndLimits() error {
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

	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validateRackConfig() error {
	if len(s.obj.Spec.RackConfig.Racks) != 0 && (int(s.obj.Spec.Size) < len(s.obj.Spec.RackConfig.Racks)) {
		return fmt.Errorf("Cluster size can not be less than number of Racks")
	}

	// Validate namespace names
	// TODO: Add more validation for namespace name
	for _, nsName := range s.obj.Spec.RackConfig.Namespaces {
		if strings.Contains(nsName, " ") {
			return fmt.Errorf("Namespace name `%s` cannot have spaces, Namespaces %v", nsName, s.obj.Spec.RackConfig.Namespaces)
		}
	}

	version, err := getBuildVersion(s.obj.Spec.Build)
	if err != nil {
		return err
	}

	rackMap := map[int]bool{}
	for _, rack := range s.obj.Spec.RackConfig.Racks {
		// Check for duplicate
		if _, ok := rackMap[rack.ID]; ok {
			return fmt.Errorf("Duplicate rackID %d not allowed, racks %v", rack.ID, s.obj.Spec.RackConfig.Racks)
		}
		rackMap[rack.ID] = true

		// Check out of range rackID
		// Check for defaultRackID is in mutate (user can not use defaultRackID).
		// Allow utils.DefaultRackID
		if rack.ID > utils.MaxRackID {
			return fmt.Errorf("Invalid rackID. RackID range (%d, %d)", utils.MinRackID, utils.MaxRackID)
		}

		if len(rack.AerospikeConfig) != 0 || len(rack.Storage.Volumes) != 0 {
			config := utils.GetRackAerospikeConfig(&s.obj, rack)
			// TODO:
			// Replication-factor in rack and commonConfig can not be different
			storage := utils.GetRackStorage(&s.obj, rack)
			if err := validateAerospikeConfig(config, &storage, int(s.obj.Spec.Size)); err != nil {
				return err
			}
		}

		// Validate rack aerospike config
		if len(rack.AerospikeConfig) != 0 {
			if err := validateAerospikeConfigSchema(version, rack.AerospikeConfig); err != nil {
				return fmt.Errorf("AerospikeConfig not valid for rack %v", rack)
			}
		}
	}

	return nil
}

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

func validateAerospikeConfig(config v1alpha1.Values, storage *aerospikev1alpha1.AerospikeStorageSpec, clSize int) error {
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
	nsListInterface, ok := config["namespace"]
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not a present. aerospikeConfig %v", config)
	} else if nsListInterface == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	}
	if nsList, ok := nsListInterface.([]interface{}); !ok {
		return fmt.Errorf("aerospikeConfig.namespace not valid namespace list %v", nsListInterface)
	} else if err := validateNamespaceConfig(nsList, storage, clSize); err != nil {
		return err
	}

	return nil
}

func validateNamespaceConfig(nsConfInterfaceList []interface{}, storage *aerospikev1alpha1.AerospikeStorageSpec, clSize int) error {
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

		if err := validateNamespaceReplicationFactor(nsConf, clSize); err != nil {
			return err
		}

		if storage, ok := nsConf["storage-engine"]; ok {
			if storage == nil {
				// TODO: Should it be error
				return fmt.Errorf("storage-engine cannot be nil for namespace %v", storage)
			}

			if _, ok := storage.(string); ok {
				// storage-engine memory
				continue
			}

			if devices, ok := storage.(map[string]interface{})["device"]; ok {
				if devices == nil {
					return fmt.Errorf("namespace storage devices cannot be nil %v", storage)
				}

				if _, ok := devices.([]interface{}); !ok {
					return fmt.Errorf("namespace storage device format not valid %v", storage)
				}

				if len(devices.([]interface{})) == 0 {
					return fmt.Errorf("No devices for namespace storage %v", storage)
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

			if files, ok := storage.(map[string]interface{})["file"]; ok {
				if files == nil {
					return fmt.Errorf("namespace storage files cannot be nil %v", storage)
				}

				if _, ok := files.([]interface{}); !ok {
					return fmt.Errorf("namespace storage files format not valid %v", storage)
				}

				if len(files.([]interface{})) == 0 {
					return fmt.Errorf("No files for namespace storage %v", storage)
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

	return nil
}

func validateNamespaceReplicationFactor(nsConf map[string]interface{}, clSize int) error {
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

func validateAerospikeConfigUpdate(newConf, oldConf aerospikev1alpha1.Values) error {
	log.Info("Validate AerospikeConfig update")

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

	if err := validateNsConfUpdate(newConf, oldConf); err != nil {
		return err
	}

	return nil
}

func validateNsConfUpdate(newConf, oldConf aerospikev1alpha1.Values) error {

	newNsConfList := newConf["namespace"].([]interface{})

	for _, singleConfInterface := range newNsConfList {
		// Validate new namespaceonf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf("Namespace conf not in valid format %v", singleConfInterface)
		}

		// Validate new namespace conf from old namespace conf. Few filds cannot be updated
		var found bool
		oldNsConfList := oldConf["namespace"].([]interface{})

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

		// New namespace not allowed to add
		if !found && !isInMemoryNamespace(singleConf) {
			return fmt.Errorf("New persistent storage namespace %s cannot be added. Old namespace list %v, new namespace list %v", singleConf["name"], oldNsConfList, newNsConfList)
		}
	}
	// Check for namespace name len
	return nil
}

func validateAerospikeConfigSchema(version string, config aerospikev1alpha1.Values) error {
	logger := pkglog.New(log.Ctx{"version": version})

	asConf, err := asconfig.NewMapAsConfig(version, config)
	if err != nil {
		return fmt.Errorf("Failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(version)
	if !valid {
		for _, e := range validationErr {
			logger.Info("Validation failed for aerospikeConfig", log.Ctx{"err": *e})
		}
		return fmt.Errorf("Generated config not valid for version %s: %v", version, err)
	}

	return nil
}

func validateRequiredFileStorage(config aerospikev1alpha1.Values, storage *aerospikev1alpha1.AerospikeStorageSpec, validationPolicy *aerospikev1alpha1.ValidationPolicySpec, version string) error {

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

func getBuildVersion(buildStr string) (string, error) {
	build := strings.Split(buildStr, ":")
	if len(build) != 2 {
		return "", fmt.Errorf("Build name %s not valid. Should be in the format of repo:version", buildStr)
	}

	version := build[1]

	return version, nil
}

// isInMemoryNamespace returns true if this nameapce config uses memory for storage.
func isInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	return !ok || storage == "memory"
}

// isEnterprise indicates if aerospike build is enterprise
func isEnterprise(build string) bool {
	return strings.Contains(strings.ToLower(build), "enterprise")
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
