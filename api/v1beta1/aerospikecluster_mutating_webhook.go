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

package v1beta1

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/merge"
	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1beta1-aerospikecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1beta1,name=maerospikecluster.kb.io,admissionReviewVersions={v1,v1beta1}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *AerospikeCluster) Default() admission.Response {
	aslog := logf.Log.WithName(ClusterNamespacedName(r))

	aslog.Info("Setting defaults for aerospikeCluster", "aerospikecluster.Spec", r.Spec)

	if err := r.setDefaults(aslog); err != nil {
		aslog.Error(err, "Mutate AerospikeCluster create failed")
		return webhook.Denied(err.Error())
	}

	aslog.Info("Setting defaults for aerospikeCluster completed")

	aslog.Info("Added defaults for aerospikeCluster", "aerospikecluster.Spec", r.Spec)

	return webhook.Patched("Patched aerospike spec with defaults", webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: r.Spec})
}

func (r *AerospikeCluster) setDefaults(aslog logr.Logger) error {

	aslog.Info("Set defaults for AerospikeCluster", "obj.Spec", r.Spec)

	// Set network defaults
	r.Spec.AerospikeNetworkPolicy.SetDefaults()

	// Set common storage defaults.
	r.Spec.Storage.SetDefaults()

	// Add default rackConfig if not already given. Disallow use of defautRackID by user.
	// Need to set before setting defaults in aerospikeConfig
	// aerospikeConfig.namespace checks for racks
	if err := r.setDefaultRackConf(aslog); err != nil {
		return err
	}

	// Set common aerospikeConfig defaults
	// Update configMap
	if err := r.setDefaultAerospikeConfigs(aslog, *r.Spec.AerospikeConfig); err != nil {
		return err
	}

	// Update racks configuration using global values where required.
	if err := r.updateRacks(aslog); err != nil {
		return err
	}

	// Set defaults for pod spec
	if err := r.Spec.PodSpec.SetDefaults(); err != nil {
		return err
	}

	// Validation policy
	if r.Spec.ValidationPolicy == nil {
		validationPolicy := ValidationPolicySpec{}

		aslog.Info("Set default validation policy", "validationPolicy", validationPolicy)
		r.Spec.ValidationPolicy = &validationPolicy
	}

	return nil
}

// setDefaultRackConf create the default rack if the spec has no racks configured.
func (r *AerospikeCluster) setDefaultRackConf(aslog logr.Logger) error {
	if len(r.Spec.RackConfig.Racks) == 0 {
		r.Spec.RackConfig.Racks = append(r.Spec.RackConfig.Racks, Rack{ID: DefaultRackID})
		aslog.Info("No rack given. Added default rack-id for all nodes", "racks", r.Spec.RackConfig, "DefaultRackID", DefaultRackID)
	} else {
		for _, rack := range r.Spec.RackConfig.Racks {
			if rack.ID == DefaultRackID {
				// User has modified defaultRackConfig or used defaultRackID
				if len(r.Spec.RackConfig.Racks) > 1 ||
					rack.Zone != "" || rack.Region != "" || rack.RackLabel != "" || rack.NodeName != "" ||
					rack.InputAerospikeConfig != nil || rack.InputStorage != nil {
					return fmt.Errorf("invalid RackConfig %v. RackID %d is reserved", r.Spec.RackConfig, DefaultRackID)
				}
			}
		}
	}
	return nil
}

func (r *AerospikeCluster) updateRacks(aslog logr.Logger) error {
	err := r.updateRacksStorageFromGlobal(aslog)

	if err != nil {
		return fmt.Errorf("error updating rack storage: %v", err)
	}

	err = r.updateRacksAerospikeConfigFromGlobal(aslog)

	if err != nil {
		return fmt.Errorf("error updating rack aerospike config: %v", err)
	}

	return nil
}

func (r *AerospikeCluster) updateRacksStorageFromGlobal(aslog logr.Logger) error {
	for i, rack := range r.Spec.RackConfig.Racks {
		if rack.InputStorage == nil {
			rack.Storage = r.Spec.Storage
			aslog.V(1).Info("Updated rack storage with global storage", "rack id", rack.ID, "storage", rack.Storage)
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

func (r *AerospikeCluster) updateRacksAerospikeConfigFromGlobal(aslog logr.Logger) error {
	for i, rack := range r.Spec.RackConfig.Racks {
		var m map[string]interface{}
		var err error
		if rack.InputAerospikeConfig != nil {
			// Merge this rack's and global config.
			m, err = merge.Merge(r.Spec.AerospikeConfig.Value, rack.InputAerospikeConfig.Value)
			aslog.V(1).Info("Merged rack config from global aerospikeConfig", "rack id", rack.ID, "rackAerospikeConfig", m, "globalAerospikeConfig", r.Spec.AerospikeConfig)
			if err != nil {
				return err
			}
		} else {
			// Use the global config.
			m = r.Spec.AerospikeConfig.Value
		}

		aslog.V(1).Info("Update rack aerospikeConfig from default aerospikeConfig", "rackAerospikeConfig", m)
		// Set defaults in updated rack config
		// Above merge function may have overwritten defaults.
		if err := r.setDefaultAerospikeConfigs(aslog, AerospikeConfigSpec{Value: m}); err != nil {
			return err
		}
		r.Spec.RackConfig.Racks[i].AerospikeConfig.Value = m
	}
	return nil
}

func (r *AerospikeCluster) setDefaultAerospikeConfigs(aslog logr.Logger, configSpec AerospikeConfigSpec) error {
	config := configSpec.Value

	// namespace conf
	if err := setDefaultNsConf(aslog, configSpec, r.Spec.RackConfig.Namespaces); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(aslog, configSpec, r.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(aslog, &configSpec, r.Spec.OperatorClientCertSpec); err != nil {
		return err
	}

	// logging conf
	if err := setDefaultLoggingConf(aslog, configSpec); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := setDefaultXDRConf(aslog, configSpec); err != nil {
			return err
		}
	}

	return nil
}

//*****************************************************************************
// Helper
//*****************************************************************************

func setDefaultNsConf(aslog logr.Logger, configSpec AerospikeConfigSpec, rackEnabledNsList []string) error {
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
					if err := setDefaultsInConfigMap(aslog, nsMap, defaultConfs); err != nil {
						return fmt.Errorf("failed to set default aerospikeConfig.namespaces rack config: %v", err)
					}
				} else {
					// User may have added this key or may have patched object with new smaller rackEnabledNamespace list
					// but left namespace defaults. This key should be removed then only controller will detect
					// that some namespace is removed from rackEnabledNamespace list and cluster needs rolling restart
					aslog.Info("aerospikeConfig.namespaces.name not found in rackEnabled namespace list. Namespace will not have defaultRackID", "nsName", nsName, "rackEnabledNamespaces", rackEnabledNsList)

					delete(nsMap, "rack-id")
				}
			}
		}
		// If namespace map doesn't have valid name, it will fail in validation layer
	}
	aslog.Info("Set default template values in aerospikeConfig.namespace")

	return nil
}

func setDefaultServiceConf(aslog logr.Logger, configSpec AerospikeConfigSpec, crObjName string) error {
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

	if err := setDefaultsInConfigMap(aslog, serviceConf, defaultConfs); err != nil {
		return fmt.Errorf("failed to set default aerospikeConfig.service config: %v", err)
	}

	aslog.Info("Set default template values in aerospikeConfig.service", "aerospikeConfig.service", serviceConf)

	return nil
}

func setDefaultNetworkConf(aslog logr.Logger, configSpec *AerospikeConfigSpec, clientCertSpec *AerospikeOperatorClientCertSpec) error {
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
	srvPort := GetServicePort(configSpec)
	serviceDefaults["port"] = srvPort
	// Here all access ports are explicitly set to hardcoded constant 65535. These values will
	// be replaced by aerospike-init container with an appropriate port in accordance to
	// MultiPodPerHost flag (NodePort of Service or Host Port of Pod).
	// Alternatively, we can set all access ports to srvPort, but in this case if user changes srvPort in CR
	// we will get a confusing error for the rack scope config. Rack scope config will have merged global network config
	// (thus have all access ports set to service.port value) and got confusing error message that "non-default values
	// can't be set". In order to avoid this confusing message we are going to use hardcoded constant 65535 as a placeholder.
	serviceDefaults["access-port"] = 65535 // must be greater that or equal to 1024,
	serviceDefaults["access-addresses"] = []string{"<access-address>"}
	serviceDefaults["alternate-access-port"] = 65535 // must be greater that or equal to 1024,
	serviceDefaults["alternate-access-addresses"] = []string{"<alternate-access-address>"}
	if tlsName, tlsPort := GetServiceTLSNameAndPort(configSpec); tlsName != "" {
		serviceDefaults["tls-port"] = tlsPort
		serviceDefaults["tls-access-port"] = 65535 // must be greater that or equal to 1024,
		serviceDefaults["tls-access-addresses"] = []string{"<tls-access-address>"}
		serviceDefaults["tls-alternate-access-port"] = 65535 // must be greater that or equal to 1024,
		serviceDefaults["tls-alternate-access-addresses"] = []string{"<tls-alternate-access-address>"}
	}

	if err := setDefaultsInConfigMap(aslog, serviceConf, serviceDefaults); err != nil {
		return fmt.Errorf("failed to set default aerospikeConfig.network.service config: %v", err)
	}

	aslog.Info("Set default template values in aerospikeConfig.network.service", "aerospikeConfig.network.service", serviceConf)

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
	hbDefaults["port"] = GetHeartbeatPort(configSpec)
	if _, ok := heartbeatConf["tls-name"]; ok {
		hbDefaults["tls-port"] = GetHeartbeatTLSPort(configSpec)
	}

	if err := setDefaultsInConfigMap(aslog, heartbeatConf, hbDefaults); err != nil {
		return fmt.Errorf("failed to set default aerospikeConfig.network.heartbeat config: %v", err)
	}

	aslog.Info("Set default template values in aerospikeConfig.network.heartbeat", "aerospikeConfig.network.heartbeat", heartbeatConf)

	// Fabric section
	if _, ok := networkConf["fabric"]; !ok {
		networkConf["fabric"] = map[string]interface{}{}
	}
	fabricConf, ok := networkConf["fabric"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aerospikeConfig.network.fabric not a valid map %v", networkConf["fabric"])
	}

	fabricDefaults := map[string]interface{}{}
	fabricDefaults["port"] = GetFabricPort(configSpec)
	if _, ok := fabricConf["tls-name"]; ok {
		fabricDefaults["tls-port"] = GetFabricTLSPort(configSpec)
	}

	if err := setDefaultsInConfigMap(aslog, fabricConf, fabricDefaults); err != nil {
		return fmt.Errorf("failed to set default aerospikeConfig.network.fabric config: %v", err)
	}

	aslog.Info("Set default template values in aerospikeConfig.network.fabric", "aerospikeConfig.network.fabric", fabricConf)

	if err := addOperatorClientNameIfNeeded(aslog, serviceConf, configSpec, clientCertSpec); err != nil {
		return err
	}

	return nil
}

func addOperatorClientNameIfNeeded(aslog logr.Logger, serviceConf map[string]interface{}, configSpec *AerospikeConfigSpec, clientCertSpec *AerospikeOperatorClientCertSpec) error {
	if clientCertSpec == nil || clientCertSpec.TLSClientName == "" {
		aslog.Info("OperatorClientCertSpec or its TLSClientName is not configured. Skipping setting tls-authenticate-client.")
		return nil
	}
	tlsAuthenticateClientConfig, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		if IsTLS(configSpec) {
			serviceConf["tls-authenticate-client"] = "any"
		}
		return nil
	}

	if value, ok := tlsAuthenticateClientConfig.([]interface{}); ok {
		if !reflect.DeepEqual("any", value) && !reflect.DeepEqual(value, "false") {
			if !func() bool {
				for i := 0; i < len(value); i++ {
					if reflect.DeepEqual(value[i], clientCertSpec.TLSClientName) {
						return true
					}
				}
				return false
			}() {
				value = append(value, clientCertSpec.TLSClientName)
				serviceConf["tls-authenticate-client"] = value
			}
		}
	}
	return nil
}

func setDefaultLoggingConf(aslog logr.Logger, configSpec AerospikeConfigSpec) error {
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

	aslog.Info("Set default template values in aerospikeConfig.logging", "aerospikeConfig.logging", loggingConfList)

	config["logging"] = loggingConfList

	return nil
}

func setDefaultXDRConf(aslog logr.Logger, configSpec AerospikeConfigSpec) error {
	// Nothing to update for now

	return nil
}

func setDefaultsInConfigMap(aslog logr.Logger, baseConfigs, defaultConfigs map[string]interface{}) error {
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
			return fmt.Errorf("config %s can not have non-default value (%T %v). It will be set internally (%T %v)", k, bv, bv, v, v)
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
	val1, ok1 := m1[key]
	val2, ok2 := m2[key]
	if ok1 != ok2 {
		return true
	}
	return !reflect.DeepEqual(val1, val2)
}

func isNameExist(names []string, name string) bool {
	for _, lName := range names {
		if lName == name {
			return true
		}
	}
	return false
}
