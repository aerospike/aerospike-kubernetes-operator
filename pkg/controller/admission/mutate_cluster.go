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

	response := webhook.Patched("Patched aerospikeConfig with defaults", webhook.JSONPatchOp{Operation: "replace", Path: "/spec/aerospikeConfig", Value: s.obj.Spec.AerospikeConfig},
		webhook.JSONPatchOp{Operation: "replace", Path: "/spec/validationPolicy", Value: s.obj.Spec.ValidationPolicy},
		webhook.JSONPatchOp{Operation: "replace", Path: "/spec/storage", Value: s.obj.Spec.Storage},
	)

	log.Info("Mutate AerospikeCluster response", log.Ctx{"response": response})
	return response
}

// MutateUpdate mutate update
func (s *ClusterMutatingAdmissionWebhook) MutateUpdate(old aerospikev1alpha1.AerospikeCluster) webhook.AdmissionResponse {
	log.Info("Mutate AerospikeCluster update")

	// This will insert the defaults also
	if err := s.setDefaults(); err != nil {
		log.Error("Mutate AerospikeCluster update failed", log.Ctx{"err": err})
		return webhook.Denied(err.Error())
	}

	return webhook.Patched("Patched aerospikeConfig with updated spec", webhook.JSONPatchOp{Operation: "replace", Path: "/spec/aerospikeConfig", Value: s.obj.Spec.AerospikeConfig},
		webhook.JSONPatchOp{Operation: "replace", Path: "/spec/validationPolicy", Value: s.obj.Spec.ValidationPolicy},
		webhook.JSONPatchOp{Operation: "replace", Path: "/spec/storage", Value: s.obj.Spec.Storage},
	)
}

func (s *ClusterMutatingAdmissionWebhook) setDefaults() error {
	log.Info("Set defaults for AerospikeCluster", log.Ctx{"obj.Spec": s.obj.Spec})

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

	// servic conf
	if err := s.patchServiceConf(); err != nil {
		return err
	}

	// network conf
	if err := s.patchNetworkConf(); err != nil {
		return err
	}

	// logging conf
	if err := s.patchLoggingConf(); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := s.patchXDRConf(); err != nil {
			return err
		}
	}

	// validation policy
	if s.obj.Spec.ValidationPolicy == nil {
		validationPolicy := aerospikev1alpha1.ValidationPolicySpec{}

		log.Info("Set default validation policy", log.Ctx{"validationPolicy": validationPolicy})
		s.obj.Spec.ValidationPolicy = &validationPolicy
	}

	// volume policy defaults.
	setVolumePolicyDefaults(&s.obj.Spec.Storage.AerospikePersistentVolumePolicySpec)
	for i := range s.obj.Spec.Storage.Volumes {
		setVolumePolicyDefaults(&s.obj.Spec.Storage.Volumes[i].AerospikePersistentVolumePolicySpec)
	}

	return nil
}

func setVolumePolicyDefaults(volumepolicy *aerospikev1alpha1.AerospikePersistentVolumePolicySpec) {
	if volumepolicy.InitMethod == nil {
		defaultInitMethod := aerospikev1alpha1.AerospikeVolumeInitMethodNone
		volumepolicy.InitMethod = &defaultInitMethod
	}

	if volumepolicy.CascadeDelete == nil {
		defaultTrue := true
		volumepolicy.CascadeDelete = &defaultTrue
	}
}

func (s *ClusterMutatingAdmissionWebhook) patchServiceConf() error {
	config := s.obj.Spec.AerospikeConfig
	if _, ok := config["service"]; !ok {
		config["service"] = map[string]interface{}{}
	}
	serviceConf, ok := config["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.service not a valid map %v", config["service"])
	}

	serviceConf["node-id"] = "aENV_NODE_ID"
	serviceConf["cluster-name"] = s.obj.Name

	log.Info("Set default template values in aerospikeConfig.service", log.Ctx{"aerospikeConfig.service": serviceConf})

	return nil
}

func (s *ClusterMutatingAdmissionWebhook) patchNetworkConf() error {
	// Network section
	config := s.obj.Spec.AerospikeConfig
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
	serviceConf["port"] = utils.ServicePort
	serviceConf["access-port"] = utils.ServicePort // must be greater that or equal to 1024
	serviceConf["access-address"] = []string{"<access_address>"}
	serviceConf["alternate-access-port"] = utils.ServicePort // must be greater that or equal to 1024,
	serviceConf["alternate-access-address"] = []string{"<alternate_access_address>"}

	if _, ok := serviceConf["tls-name"]; ok {
		serviceConf["tls-port"] = utils.ServiceTlsPort
		serviceConf["tls-access-port"] = utils.ServiceTlsPort
		serviceConf["tls-access-address"] = []string{"<tls-access-address>"}
		serviceConf["tls-alternate-access-port"] = utils.ServiceTlsPort // must be greater that or equal to 1024,
		serviceConf["tls-alternate-access-address"] = []string{"<tls-alternate-access-address>"}
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
	heartbeatConf["mode"] = "mesh"
	heartbeatConf["port"] = utils.HeartbeatPort
	if _, ok := heartbeatConf["tls-name"]; ok {
		heartbeatConf["tls-port"] = utils.HeartbeatTlsPort
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
	fabricConf["port"] = utils.FabricPort
	if _, ok := fabricConf["tls-name"]; ok {
		fabricConf["tls-port"] = utils.FabricTlsPort
	}
	log.Info("Set default template values in aerospikeConfig.network.fabric", log.Ctx{"aerospikeConfig.network.fabric": fabricConf})

	return nil
}

func (s *ClusterMutatingAdmissionWebhook) patchLoggingConf() error {
	config := s.obj.Spec.AerospikeConfig
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

func (s *ClusterMutatingAdmissionWebhook) patchXDRConf() error {
	// Nothing to update for now

	return nil
}

func updateMap(baseMap, opMap map[string]interface{}) map[string]interface{} {
	for k, v := range opMap {
		baseMap[k] = v
	}
	return baseMap
}

func isValueUpdated(m1, m2 map[string]interface{}, key string) bool {
	val1, _ := m1[key]
	val2, _ := m2[key]
	return val1 != val2
}
