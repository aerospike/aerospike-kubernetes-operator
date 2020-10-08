package admission

import (
	"fmt"
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
	obj    aerospikev1alpha1.AerospikeCluster
	logger log.Logger
}

// ValidateAerospikeCluster validate cluster operation
func ValidateAerospikeCluster(req webhook.AdmissionRequest) webhook.AdmissionResponse {

	decoder, _ := admission.NewDecoder(scheme)

	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.Object, newAeroCluster)

	oldAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.OldObject, oldAeroCluster)

	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(newAeroCluster)})

	s := ClusterValidatingAdmissionWebhook{
		obj:    *newAeroCluster,
		logger: logger,
	}

	// validate the new AerospikeCluster
	if req.Operation == av1beta1.Create {
		err := s.ValidateCreate()
		if err != nil {
			s.logger.Error("Validate AerospikeCluster create failed", log.Ctx{"err": err})
			return webhook.Denied(err.Error())
		}
	}

	// if this is an update, validate that the transition from old to new
	if req.Operation == av1beta1.Update {
		err := s.ValidateUpdate(*oldAeroCluster)
		if err != nil {
			s.logger.Error("Validate AerospikeCluster update failed", log.Ctx{"err": err})
			return webhook.Denied(err.Error())
		}
	}
	return webhook.Allowed("Validation passed. No create or update")
}

// ValidateCreate validate create
func (s *ClusterValidatingAdmissionWebhook) ValidateCreate() error {
	s.logger.Info("Validate AerospikeCluster create")

	return s.validate()
}

// ValidateUpdate validate update
func (s *ClusterValidatingAdmissionWebhook) ValidateUpdate(old aerospikev1alpha1.AerospikeCluster) error {
	s.logger.Info("Validate AerospikeCluster update")
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
	if err := validateAerospikeConfigUpdate(s.logger, s.obj.Spec.AerospikeConfig, old.Spec.AerospikeConfig); err != nil {
		return err
	}

	// Validate RackConfig update
	if err := s.validateRackUpdate(old); err != nil {
		return err
	}
	return nil
}

func (s *ClusterValidatingAdmissionWebhook) validate() error {
	s.logger.Debug("Validate AerospikeCluster spec", log.Ctx{"obj.Spec": s.obj.Spec})

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
	if err := validateAerospikeConfig(s.logger, aeroConfig, &s.obj.Spec.Storage, int(s.obj.Spec.Size)); err != nil {
		return err
	}

	// Validate if passed aerospikeConfig
	if err := validateAerospikeConfigSchema(s.logger, version, s.obj.Spec.AerospikeConfig); err != nil {
		return fmt.Errorf("AerospikeConfig not valid %v", err)
	}

	err = validateRequiredFileStorage(s.logger, aeroConfig, &s.obj.Spec.Storage, s.obj.Spec.ValidationPolicy, version)
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
	s.logger.Info("Validate rack update")

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
					if err := validateAerospikeConfigUpdate(s.logger, newConf, oldConf); err != nil {
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
			if err := validateAerospikeConfig(s.logger, config, &storage, int(s.obj.Spec.Size)); err != nil {
				return err
			}
		}

		// Validate rack aerospike config
		if len(rack.AerospikeConfig) != 0 {
			if err := validateAerospikeConfigSchema(s.logger, version, rack.AerospikeConfig); err != nil {
				return fmt.Errorf("AerospikeConfig not valid for rack %v", rack)
			}
		}
	}

	return nil
}
