package admission

import (
	"fmt"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	log "github.com/inconshreveable/log15"
	av1beta1 "k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ClusterMutatingAdmissionWebhook admission mutation webhook
type ClusterMutatingAdmissionWebhook struct {
	obj    aerospikev1alpha1.AerospikeCluster
	logger log.Logger
}

// MutateAerospikeCluster mutate cluster operation
func MutateAerospikeCluster(req webhook.AdmissionRequest) webhook.AdmissionResponse {

	decoder, _ := admission.NewDecoder(scheme)

	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.Object, newAeroCluster)

	oldAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.OldObject, oldAeroCluster)

	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(newAeroCluster)})

	s := ClusterMutatingAdmissionWebhook{
		obj:    *newAeroCluster,
		logger: logger,
	}

	// mutate the new AerospikeCluster
	if req.Operation == av1beta1.Create {
		return s.MutateCreate()
	}

	// if this is an update, mutate that the transition from old to new
	if req.Operation == av1beta1.Update {
		return s.MutateUpdate(*oldAeroCluster)
	}
	return webhook.Allowed("Mutation passed, No update or create")
}

// MutateCreate mutate create
// Add storage policy defaults
// Add validation policy defaults
// Add required config in AerospikeConfig object if not give by user
//
// Required network, namespace
// network (Required service, heartbeat, fabric)
// 	service
// 		port
// 	heartbeat
// 		mode
// 		port
// 	fabric
// 		port
// namespace (Required storage-engine)
// 	storage-engine
// 		memory or
// 		{device,file}
//
// Xdr (Required xdr-digestlog-path)
// 	xdr-digestlog-path
func (s *ClusterMutatingAdmissionWebhook) MutateCreate() webhook.AdmissionResponse {
	s.logger.Info("Mutate AerospikeCluster create")

	if err := s.setDefaults(); err != nil {
		s.logger.Error("Mutate AerospikeCluster create failed", log.Ctx{"err": err})
		return webhook.Denied(err.Error())
	}

	return webhook.Patched("Patched aerospike spec with defaults", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: s.obj.Spec})
}

// MutateUpdate mutate update
func (s *ClusterMutatingAdmissionWebhook) MutateUpdate(old aerospikev1alpha1.AerospikeCluster) webhook.AdmissionResponse {
	s.logger.Info("Mutate AerospikeCluster update")

	// This will insert the defaults also
	if err := s.setDefaults(); err != nil {
		s.logger.Error("Mutate AerospikeCluster update failed", log.Ctx{"err": err})
		return webhook.Denied(err.Error())
	}

	return webhook.Patched("Patched aerospike spec with updated spec", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: s.obj.Spec})
}

func (s *ClusterMutatingAdmissionWebhook) setDefaults() error {
	s.logger.Info("Set defaults for AerospikeCluster", log.Ctx{"obj.Spec": s.obj.Spec})

	// Set common storage defaults.
	s.obj.Spec.Storage.SetDefaults()

	// Add default rackConfig if not already given. Disallow use of defautRackID by user.
	// Need to set before setting defaults in aerospikeConfig
	// aerospikeConfig.namespace checks for racks
	if err := s.setDefaultRackConf(); err != nil {
		return err
	}

	// Set common aerospikeConfig defaults
	config := s.obj.Spec.AerospikeConfig
	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}
	if err := s.setDefaultAerospikeConfigs(config); err != nil {
		return err
	}

	// Update racks aerospikeConfig after setting common aerospikeConfig defaults
	if err := s.updateRacksAerospikeConfigFromDefault(); err != nil {
		return err
	}

	// validation policy
	if s.obj.Spec.ValidationPolicy == nil {
		validationPolicy := aerospikev1alpha1.ValidationPolicySpec{}

		s.logger.Info("Set default validation policy", log.Ctx{"validationPolicy": validationPolicy})
		s.obj.Spec.ValidationPolicy = &validationPolicy
	}

	return nil
}

func (s *ClusterMutatingAdmissionWebhook) setDefaultRackConf() error {
	if len(s.obj.Spec.RackConfig.Racks) == 0 {
		s.obj.Spec.RackConfig = aerospikev1alpha1.RackConfig{
			Racks: []aerospikev1alpha1.Rack{{ID: utils.DefaultRackID}},
		}
		s.logger.Info("No rack given. Added default rack-id for all nodes", log.Ctx{"racks": s.obj.Spec.RackConfig, "DefaultRackID": utils.DefaultRackID})
	} else {
		for _, rack := range s.obj.Spec.RackConfig.Racks {
			if rack.ID == utils.DefaultRackID {
				// User has modified defaultRackConfig or used defaultRackID
				if len(s.obj.Spec.RackConfig.Racks) > 1 ||
					rack.Zone != "" || rack.Region != "" || rack.RackLabel != "" || rack.NodeName != "" ||
					rack.AerospikeConfig != nil {
					return fmt.Errorf("Invalid RackConfig %v. RackID %d is reserved", s.obj.Spec.RackConfig, utils.DefaultRackID)
				}
			}
			if len(rack.Storage.Volumes) != 0 {
				// Set storage defaults if rack has storage section
				rack.Storage.SetDefaults()
			}
		}
	}
	return nil
}

func (s *ClusterMutatingAdmissionWebhook) updateRacksAerospikeConfigFromDefault() error {
	for i, rack := range s.obj.Spec.RackConfig.Racks {
		if len(rack.AerospikeConfig) != 0 {
			m, err := merge(s.obj.Spec.AerospikeConfig, rack.AerospikeConfig)
			if err != nil {
				return err
			}
			// Set defaults in updated rack config
			// Above merge function may have overwritten defaults
			if err := s.setDefaultAerospikeConfigs(m); err != nil {
				return err
			}
			s.obj.Spec.RackConfig.Racks[i].AerospikeConfig = m

			s.logger.Debug("Update rack aerospikeConfig from default aerospikeConfig", log.Ctx{"rackAerospikeConfig": m})
		}
	}
	return nil
}

func (s *ClusterMutatingAdmissionWebhook) setDefaultAerospikeConfigs(config aerospikev1alpha1.Values) error {

	// namespace conf
	if err := setDefaultNsConf(s.logger, config, s.obj.Spec.RackConfig.Namespaces); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(s.logger, config, s.obj.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(s.logger, config); err != nil {
		return err
	}

	// logging conf
	if err := setDefaultLoggingConf(s.logger, config); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := setDefaultXDRConf(s.logger, config); err != nil {
			return err
		}
	}

	return nil
}
