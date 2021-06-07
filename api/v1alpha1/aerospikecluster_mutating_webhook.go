/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/merge"
)

// +kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1alpha1-aerospikecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1alpha1,name=maerospikecluster.kb.io,admissionReviewVersions={v1,v1beta1}

// var _ webhook.Defaulter = &AerospikeCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeCluster) Default() admission.Response {
	aerospikeclusterlog.Info("Setting defaults for aerospikeCluster", "name", r.Name, "aerospikecluster.Spec", r.Spec)

	// TODO(user): fill in your defaulting logic.
	// r.// // logger.Info("Mutate AerospikeCluster create")

	// r.setDefaults()

	// webhook.AdmissionResponse = admission.Patched("Patched aerospike spec with updated spec", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: r.Spec})

	if err := r.setDefaults(); err != nil {
		aerospikeclusterlog.Error(err, "Mutate AerospikeCluster create failed")
		return webhook.Denied(err.Error())
	}
	aerospikeclusterlog.Info("Setting defaults for aerospikeCluster completed", "name", r.Name)

	aerospikeclusterlog.Info("Added defaults for aerospikeCluster", "name", r.Name, "aerospikecluster.Spec", r.Spec)

	return webhook.Patched("Patched aerospike spec with defaults", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: r.Spec})
}

func (r *AerospikeCluster) setDefaults() error {
	// r.// logger.Info("Set defaults for AerospikeCluster", log.Ctx{"obj.Spec": r.Spec})

	// Set network defaults
	r.Spec.AerospikeNetworkPolicy.SetDefaults()

	// Set common storage defaults.
	r.Spec.Storage.SetDefaults()

	// Add default rackConfig if not already given. Disallow use of defautRackID by user.
	// Need to set before setting defaults in aerospikeConfig
	// aerospikeConfig.namespace checks for racks
	if err := r.setDefaultRackConf(); err != nil {
		return err
	}

	// Set common aerospikeConfig defaults
	// Update configMap
	if err := r.setDefaultAerospikeConfigs(*r.Spec.AerospikeConfig); err != nil {
		return err
	}

	// Update racks configuration using global values where required.
	if err := r.updateRacks(); err != nil {
		return err
	}

	// Validation policy
	if r.Spec.ValidationPolicy == nil {
		validationPolicy := ValidationPolicySpec{}

		// r.// logger.Info("Set default validation policy", log.Ctx{"validationPolicy": validationPolicy})
		r.Spec.ValidationPolicy = &validationPolicy
	}

	return nil
}

// setDefaultRackConf create the default rack if the spec has no racks configured.
func (r *AerospikeCluster) setDefaultRackConf() error {
	if len(r.Spec.RackConfig.Racks) == 0 {
		r.Spec.RackConfig.Racks = append(r.Spec.RackConfig.Racks, Rack{ID: DefaultRackID})
		// r.// logger.Info("No rack given. Added default rack-id for all nodes", log.Ctx{"racks": r.Spec.RackConfig, "DefaultRackID": DefaultRackID})
	} else {
		for _, rack := range r.Spec.RackConfig.Racks {
			if rack.ID == DefaultRackID {
				// User has modified defaultRackConfig or used defaultRackID
				if len(r.Spec.RackConfig.Racks) > 1 ||
					rack.Zone != "" || rack.Region != "" || rack.RackLabel != "" || rack.NodeName != "" ||
					rack.InputAerospikeConfig != nil || rack.InputStorage != nil {
					return fmt.Errorf("Invalid RackConfig %v. RackID %d is reserved", r.Spec.RackConfig, DefaultRackID)
				}
			}
		}
	}
	return nil
}

func (r *AerospikeCluster) updateRacks() error {
	err := r.updateRacksStorageFromGlobal()

	if err != nil {
		return fmt.Errorf("Error updating rack storage: %v", err)
	}

	err = r.updateRacksAerospikeConfigFromGlobal()

	if err != nil {
		return fmt.Errorf("Error updating rack aerospike config: %v", err)
	}

	return nil
}

func (r *AerospikeCluster) updateRacksStorageFromGlobal() error {
	for i, rack := range r.Spec.RackConfig.Racks {
		if rack.InputStorage == nil {
			rack.Storage = r.Spec.Storage
			// r.// logger.Debug("Updated rack storage with global storage", log.Ctx{"rack id": rack.ID, "storage": rack.Storage})
		} else {
			rack.Storage = *rack.InputStorage
		}

		// Set storage defaults if rack has storage section
		rack.Storage.SetDefaults()

		// Copy over to the actual slice.
		r.Spec.RackConfig.Racks[i].Storage = rack.Storage
	}
	return nil
}

func (r *AerospikeCluster) updateRacksAerospikeConfigFromGlobal() error {
	for i, rack := range r.Spec.RackConfig.Racks {
		var m map[string]interface{}
		var err error
		if rack.InputAerospikeConfig != nil {
			// Merge this rack's and global config.
			m, err = merge.Merge(r.Spec.AerospikeConfig.Value, rack.InputAerospikeConfig.Value)
			// s.logger.Debug("Merged rack config from global aerospikeConfig", log.Ctx{"rack id": rack.ID, "rackAerospikeConfig": m, "globalAerospikeConfig": r.Spec.AerospikeConfig})
			if err != nil {
				return err
			}
		} else {
			// Use the global config.
			m = r.Spec.AerospikeConfig.Value
		}

		// s.logger.Debug("Update rack aerospikeConfig from default aerospikeConfig", log.Ctx{"rackAerospikeConfig": m})
		// Set defaults in updated rack config
		// Above merge function may have overwritten defaults.
		if err := r.setDefaultAerospikeConfigs(AerospikeConfigSpec{Value: m}); err != nil {
			return err
		}
		r.Spec.RackConfig.Racks[i].AerospikeConfig.Value = m
	}
	return nil
}

func (r *AerospikeCluster) setDefaultAerospikeConfigs(configSpec AerospikeConfigSpec) error {
	config := configSpec.Value

	// namespace conf
	if err := setDefaultNsConf(configSpec, r.Spec.RackConfig.Namespaces); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(configSpec, r.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(configSpec); err != nil {
		return err
	}

	// logging conf
	if err := setDefaultLoggingConf(configSpec); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := setDefaultXDRConf(configSpec); err != nil {
			return err
		}
	}

	return nil
}

//*****************************************************************************
// Helper
//*****************************************************************************

func setDefaultNsConf(configSpec AerospikeConfigSpec, rackEnabledNsList []string) error {
	config := configSpec.Value
	// namespace conf
	nsConf, ok := config["namespaces"]
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespaces not present. aerospikeConfig %v", config)
	} else if nsConf == nil {
		return fmt.Errorf("aerospikeConfig.namespaces cannot be nil")
	}

	nsList, ok := nsConf.([]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.namespaces not valid namespace list %v", nsConf)
	} else if len(nsList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespaces cannot be empty. aerospikeConfig %v", config)
	}

	for _, nsInt := range nsList {
		nsMap, ok := nsInt.(map[string]interface{})
		if !ok {
			return fmt.Errorf("aerospikeConfig.namespaces does not have valid namespace map. nsMap %v", nsInt)
		}

		// Add dummy rack-id only for rackEnabled namespaces
		defaultConfs := map[string]interface{}{"rack-id": DefaultRackID}
		if nsName, ok := nsMap["name"]; ok {
			if _, ok := nsName.(string); ok {
				if isNameExist(rackEnabledNsList, nsName.(string)) {
					// Add dummy rack-id, should be replaced with actual rack-id by init-container script
					if err := setDefaultsInConfigMap(nsMap, defaultConfs); err != nil {
						return fmt.Errorf("Failed to set default aerospikeConfig.namespaces rack config: %v", err)
					}
				} else {
					// User may have added this key or may have patched object with new smaller rackEnabledNamespace list
					// but left namespace defaults. This key should be removed then only controller will detect
					// that some namespace is removed from rackEnabledNamespace list and cluster needs rolling restart
					// logger.Info("aerospikeConfig.namespaces.name not found in rackEnabled namespace list. Namespace will not have defaultRackID", log.Ctx{"nsName": nsName, "rackEnabledNamespaces": rackEnabledNsList})

					delete(nsMap, "rack-id")
				}
			}
		}
		// If namespace map doesn't have valid name, it will fail in validation layer
	}
	// logger.Info("Set default template values in aerospikeConfig.namespace")

	return nil
}

func setDefaultServiceConf(configSpec AerospikeConfigSpec, crObjName string) error {
	config := configSpec.Value

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

	// logger.Info("Set default template values in aerospikeConfig.service", log.Ctx{"aerospikeConfig.service": serviceConf})

	return nil
}

func setDefaultNetworkConf(configSpec AerospikeConfigSpec) error {
	config := configSpec.Value

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
	serviceDefaults["port"] = ServicePort
	serviceDefaults["access-port"] = ServicePort // must be greater that or equal to 1024
	serviceDefaults["access-addresses"] = []string{"<access-address>"}
	serviceDefaults["alternate-access-port"] = ServicePort // must be greater that or equal to 1024,
	serviceDefaults["alternate-access-addresses"] = []string{"<alternate-access-address>"}
	if _, ok := serviceConf["tls-name"]; ok {
		serviceDefaults["tls-port"] = ServiceTLSPort
		serviceDefaults["tls-access-port"] = ServiceTLSPort
		serviceDefaults["tls-access-addresses"] = []string{"<tls-access-address>"}
		serviceDefaults["tls-alternate-access-port"] = ServiceTLSPort // must be greater that or equal to 1024,
		serviceDefaults["tls-alternate-access-addresses"] = []string{"<tls-alternate-access-address>"}
	}

	if err := setDefaultsInConfigMap(serviceConf, serviceDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.service config: %v", err)
	}

	// logger.Info("Set default template values in aerospikeConfig.network.service", log.Ctx{"aerospikeConfig.network.service": serviceConf})

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
	hbDefaults["port"] = HeartbeatPort
	if _, ok := heartbeatConf["tls-name"]; ok {
		hbDefaults["tls-port"] = HeartbeatTLSPort
	}

	if err := setDefaultsInConfigMap(heartbeatConf, hbDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.heartbeat config: %v", err)
	}

	// logger.Info("Set default template values in aerospikeConfig.network.heartbeat", log.Ctx{"aerospikeConfig.network.heartbeat": heartbeatConf})

	// Fabric section
	if _, ok := networkConf["fabric"]; !ok {
		networkConf["fabric"] = map[string]interface{}{}
	}
	fabricConf, ok := networkConf["fabric"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network.fabric not a valid map %v", networkConf["fabric"])
	}

	fabricDefaults := map[string]interface{}{}
	fabricDefaults["port"] = FabricPort
	if _, ok := fabricConf["tls-name"]; ok {
		fabricDefaults["tls-port"] = FabricTLSPort
	}

	if err := setDefaultsInConfigMap(fabricConf, fabricDefaults); err != nil {
		return fmt.Errorf("Failed to set default aerospikeConfig.network.fabric config: %v", err)
	}

	// logger.Info("Set default template values in aerospikeConfig.network.fabric", log.Ctx{"aerospikeConfig.network.fabric": fabricConf})

	return nil
}

func setDefaultLoggingConf(configSpec AerospikeConfigSpec) error {
	config := configSpec.Value

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

	// logger.Info("Set default template values in aerospikeConfig.logging", log.Ctx{"aerospikeConfig.logging": loggingConfList})

	config["logging"] = loggingConfList

	return nil
}

func setDefaultXDRConf(configSpec AerospikeConfigSpec) error {
	// Nothing to update for now

	return nil
}

func setDefaultsInConfigMap(baseConfigs, defaultConfigs map[string]interface{}) error {
	for k, v := range defaultConfigs {
		// Special handling.
		// Older baseValues are parsed to int64 but defaults are in int
		if newv, ok := v.(int); ok {
			// TODO: verify this: Looks like, in new openapi schema, values are parsed in float64
			v = float64(newv)
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
