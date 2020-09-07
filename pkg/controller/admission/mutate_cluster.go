package admission

import (
	"fmt"
	"reflect"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	log "github.com/inconshreveable/log15"
	av1beta1 "k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ClusterMutatingAdmissionWebhook admission mutation webhook
type ClusterMutatingAdmissionWebhook struct {
	obj aerospikev1alpha1.AerospikeCluster
}

// MutateAerospikeCluster mutate cluster operation
func MutateAerospikeCluster(req webhook.AdmissionRequest) webhook.AdmissionResponse {

	decoder, _ := admission.NewDecoder(scheme)

	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.Object, newAeroCluster)

	oldAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	decoder.DecodeRaw(req.OldObject, oldAeroCluster)

	s := ClusterMutatingAdmissionWebhook{
		obj: *newAeroCluster,
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
	log.Info("Mutate AerospikeCluster create")

	if err := s.setDefaults(); err != nil {
		log.Error("Mutate AerospikeCluster create failed", log.Ctx{"err": err})
		return webhook.Denied(err.Error())
	}

	return webhook.Patched("Patched aerospike spec with defaults", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: s.obj.Spec})
}

// MutateUpdate mutate update
func (s *ClusterMutatingAdmissionWebhook) MutateUpdate(old aerospikev1alpha1.AerospikeCluster) webhook.AdmissionResponse {
	log.Info("Mutate AerospikeCluster update")

	// This will insert the defaults also
	if err := s.setDefaults(); err != nil {
		log.Error("Mutate AerospikeCluster update failed", log.Ctx{"err": err})
		return webhook.Denied(err.Error())
	}

	return webhook.Patched("Patched aerospike spec with updated spec", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: s.obj.Spec})
}

func (s *ClusterMutatingAdmissionWebhook) setDefaults() error {
	log.Info("Set defaults for AerospikeCluster", log.Ctx{"obj.Spec": s.obj.Spec})

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

		log.Info("Set default validation policy", log.Ctx{"validationPolicy": validationPolicy})
		s.obj.Spec.ValidationPolicy = &validationPolicy
	}

	return nil
}

func (s *ClusterMutatingAdmissionWebhook) setDefaultRackConf() error {
	if len(s.obj.Spec.RackConfig.Racks) == 0 {
		s.obj.Spec.RackConfig = aerospikev1alpha1.RackConfig{
			Racks: []aerospikev1alpha1.Rack{{ID: utils.DefaultRackID}},
		}
		log.Info("No rack given. Added default rack-id for all nodes", log.Ctx{"racks": s.obj.Spec.RackConfig, "DefaultRackID": utils.DefaultRackID})
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

			log.Debug("Update rack aerospikeConfig from default aerospikeConfig", log.Ctx{"rackAerospikeConfig": m})
		}
	}
	return nil
}

func (s *ClusterMutatingAdmissionWebhook) setDefaultAerospikeConfigs(config aerospikev1alpha1.Values) error {

	// namespace conf
	if err := setDefaultNsConf(config, s.obj.Spec.RackConfig.Namespaces); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(config, s.obj.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(config); err != nil {
		return err
	}

	// logging conf
	if err := setDefaultLoggingConf(config); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := setDefaultXDRConf(config); err != nil {
			return err
		}
	}

	return nil
}

func setDefaultNsConf(config aerospikev1alpha1.Values, rackEnabledNsList []string) error {
	// namespace conf
	nsConf, ok := config["namespace"]
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not a present. aerospikeConfig %v", config)
	} else if nsConf == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	}

	nsList, ok := nsConf.([]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespace not valid namespace list %v", nsConf)
	} else if len(nsList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace cannot be empty. aerospikeConfig %v", config)
	}

	for _, nsInt := range nsList {
		nsMap, ok := nsInt.(map[string]interface{})
		if !ok {
			return fmt.Errorf("aerospikeConfig.namespace does not have valid namespace map. nsMap %v", nsInt)
		}

		// Add dummy rack-id only for rackEnabled namespaces
		defaultConfs := map[string]interface{}{"rack-id": utils.DefaultRackID}
		if nsName, ok := nsMap["name"]; ok {
			if _, ok := nsName.(string); ok {
				if isNameExist(rackEnabledNsList, nsName.(string)) {
					// Add dummy rack-id, should be replaced with actual rack-id by init-container script
					if err := setDefaultsInConfigMap(nsMap, defaultConfs); err != nil {
						return fmt.Errorf("Failed to set default aerospikeConfig.namespace rack config: %v", err)
					}
				} else {
					// User may have added this key or may have patched object with new smaller rackEnabledNamespace list
					// but left namespace defaults. This key should be removed then only controller will detect
					// that some namespace is removed from rackEnabledNamespace list and cluster needs rolling restart
					log.Info("aerospikeConfig.namespace.name not found in rackEnabled namespace list. Namespace will not have defaultRackID", log.Ctx{"nsName": nsName, "rackEnabledNamespaces": rackEnabledNsList})

					delete(nsMap, "rack-id")
				}
			}
		}
		// If namespace map doesn't have valid name, it will fail in validation layer
	}
	log.Info("Set default template values in aerospikeConfig.namespace")

	return nil
}

func setDefaultServiceConf(config aerospikev1alpha1.Values, crObjName string) error {
	if _, ok := config["service"]; !ok {
		config["service"] = map[string]interface{}{}
	}
	serviceConf, ok := config["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.service not a valid map %v", config["service"])
	}

	defaultConfs := map[string]interface{}{
		"node-id":      "ENV_NODE_ID",
		"cluster-name": crObjName,
	}

	if err := setDefaultsInConfigMap(serviceConf, defaultConfs); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.service config: %v", err)
	}

	log.Info("Set default template values in aerospikeConfig.service", log.Ctx{"aerospikeConfig.service": serviceConf})

	return nil
}

func setDefaultNetworkConf(config aerospikev1alpha1.Values) error {
	// Network section
	if _, ok := config["network"]; !ok {
		config["network"] = map[string]interface{}{}
	}
	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network not a valid map %v", config["network"])
	}

	// Service section
	if _, ok := networkConf["service"]; !ok {
		networkConf["service"] = map[string]interface{}{}
	}
	serviceConf, ok := networkConf["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network.service not a valid map %v", networkConf["service"])
	}
	// Override these sections
	// TODO: These values lines will be replaces with runtime info by script in init-container
	// See if we can get better way to make template
	serviceDefaults := map[string]interface{}{}
	serviceDefaults["port"] = utils.ServicePort
	serviceDefaults["access-port"] = utils.ServicePort // must be greater that or equal to 1024
	serviceDefaults["access-address"] = []string{"<access_address>"}
	serviceDefaults["alternate-access-port"] = utils.ServicePort // must be greater that or equal to 1024,
	serviceDefaults["alternate-access-address"] = []string{"<alternate_access_address>"}
	if _, ok := serviceConf["tls-name"]; ok {
		serviceDefaults["tls-port"] = utils.ServiceTLSPort
		serviceDefaults["tls-access-port"] = utils.ServiceTLSPort
		serviceDefaults["tls-access-address"] = []string{"<tls-access-address>"}
		serviceDefaults["tls-alternate-access-port"] = utils.ServiceTLSPort // must be greater that or equal to 1024,
		serviceDefaults["tls-alternate-access-address"] = []string{"<tls-alternate-access-address>"}
	}

	if err := setDefaultsInConfigMap(serviceConf, serviceDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.service config: %v", err)
	}

	log.Info("Set default template values in aerospikeConfig.network.service", log.Ctx{"aerospikeConfig.network.service": serviceConf})

	// Heartbeat section
	if _, ok := networkConf["heartbeat"]; !ok {
		networkConf["heartbeat"] = map[string]interface{}{}
	}
	heartbeatConf, ok := networkConf["heartbeat"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network.heartbeat not a valid map %v", networkConf["heartbeat"])
	}

	hbDefaults := map[string]interface{}{}
	hbDefaults["mode"] = "mesh"
	hbDefaults["port"] = utils.HeartbeatPort
	if _, ok := heartbeatConf["tls-name"]; ok {
		hbDefaults["tls-port"] = utils.HeartbeatTLSPort
	}

	if err := setDefaultsInConfigMap(heartbeatConf, hbDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.heartbeat config: %v", err)
	}

	log.Info("Set default template values in aerospikeConfig.network.heartbeat", log.Ctx{"aerospikeConfig.network.heartbeat": heartbeatConf})

	// Fabric section
	if _, ok := networkConf["fabric"]; !ok {
		networkConf["fabric"] = map[string]interface{}{}
	}
	fabricConf, ok := networkConf["fabric"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network.fabric not a valid map %v", networkConf["fabric"])
	}

	fabricDefaults := map[string]interface{}{}
	fabricDefaults["port"] = utils.FabricPort
	if _, ok := fabricConf["tls-name"]; ok {
		fabricDefaults["tls-port"] = utils.FabricTLSPort
	}

	if err := setDefaultsInConfigMap(fabricConf, fabricDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.fabric config: %v", err)
	}

	log.Info("Set default template values in aerospikeConfig.network.fabric", log.Ctx{"aerospikeConfig.network.fabric": fabricConf})

	return nil
}

func setDefaultLoggingConf(config aerospikev1alpha1.Values) error {
	if _, ok := config["logging"]; !ok {
		config["logging"] = []interface{}{}
	}
	loggingConfList, ok := config["logging"].([]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.logging not a valid list %v", config["logging"])
	}

	var found bool
	for _, conf := range loggingConfList {
		logConf, ok := conf.(map[string]interface{})
		if !ok {
			return fmt.Errorf("aerospikeConfig.logging not a list of valid map %v", logConf)
		}
		if logConf["name"] == "console" {
			found = true
			break
		}
	}
	if !found {
		loggingConfList = append(loggingConfList, map[string]interface{}{
			"name": "console",
			"any":  "info",
		})
	}

	log.Info("Set default template values in aerospikeConfig.logging", log.Ctx{"aerospikeConfig.logging": loggingConfList})

	config["logging"] = loggingConfList

	return nil
}

func setDefaultXDRConf(config aerospikev1alpha1.Values) error {
	// Nothing to update for now

	return nil
}

func setDefaultsInConfigMap(baseConfigs, defaultConfigs map[string]interface{}) error {
	for k, v := range defaultConfigs {
		// Special handling.
		// Older baseValues are parsed to int64 but defaults are in int
		if newv, ok := v.(int); ok {
			v = int64(newv)
		}

		// Older baseValues are parsed to []interface{} but defaults are in []string
		// Can make default as []interface{} but then we have to remember it there.
		// []string looks make natural there. So lets handle it here only
		if newv, ok := v.([]string); ok {
			v = toInterfaceList(newv)
		}

		if bv, ok := baseConfigs[k]; ok &&
			!reflect.DeepEqual(bv, v) {
			return fmt.Errorf("Config %s can not have non-default value (%T %v). It will be set internally (%T %v)", k, bv, bv, v, v)
		}
		baseConfigs[k] = v
	}
	return nil
}

func toInterfaceList(list []string) []interface{} {
	var ilist []interface{}
	for _, e := range list {
		ilist = append(ilist, e)
	}
	return ilist
}

func isValueUpdated(m1, m2 map[string]interface{}, key string) bool {
	val1, _ := m1[key]
	val2, _ := m2[key]
	return val1 != val2
}

func isNameExist(names []string, name string) bool {
	for _, lName := range names {
		if lName == name {
			return true
		}
	}
	return false
}
