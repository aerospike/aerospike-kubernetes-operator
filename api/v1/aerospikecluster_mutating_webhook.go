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

package v1

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"gomodules.xyz/jsonpatch/v2"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/merge"
)

//nolint:lll // for readability
// +kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1-aerospikecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1,name=maerospikecluster.kb.io,admissionReviewVersions={v1}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (c *AerospikeCluster) Default(operation v1.Operation) admission.Response {
	asLog := logf.Log.WithName(ClusterNamespacedName(c))

	asLog.Info(
		"Setting defaults for aerospikeCluster", "aerospikecluster.Spec",
		c.Spec,
	)

	if err := c.setDefaults(asLog); err != nil {
		asLog.Error(err, "Mutate AerospikeCluster create failed")
		return webhook.Denied(err.Error())
	}

	asLog.Info("Setting defaults for aerospikeCluster completed")

	asLog.Info(
		"Added defaults for aerospikeCluster", "aerospikecluster.Spec", c.Spec,
	)

	var patches []jsonpatch.JsonPatchOperation
	patches = append(patches, webhook.JSONPatchOp{Operation: "replace", Path: "/spec", Value: c.Spec})

	if operation == v1.Create {
		patches = append(patches, webhook.JSONPatchOp{Operation: "replace", Path: "/metadata/labels", Value: c.Labels})
	}

	return webhook.Patched(
		"Patched aerospike spec with defaults",
		patches...,
	)
}

func (c *AerospikeCluster) setDefaults(asLog logr.Logger) error {
	// Set network defaults
	c.Spec.AerospikeNetworkPolicy.setDefaults(c.ObjectMeta.Namespace)

	// Set common storage defaults.
	c.Spec.Storage.SetDefaults()

	// Add default rackConfig if not already given. Disallow use of defaultRackID by user.
	// Need to set before setting defaults in aerospikeConfig.
	// aerospikeConfig.namespace checks for racks
	if err := c.setDefaultRackConf(asLog); err != nil {
		return err
	}

	// cluster level aerospike config may be empty and
	if c.Spec.AerospikeConfig != nil {
		// Set common aerospikeConfig defaults
		// Update configMap
		if err := c.setDefaultAerospikeConfigs(
			asLog, *c.Spec.AerospikeConfig,
		); err != nil {
			return err
		}
	}

	// Update racks configuration using global values where required.
	if err := c.updateRacks(asLog); err != nil {
		return err
	}

	// Set defaults for pod spec
	if err := c.Spec.PodSpec.SetDefaults(); err != nil {
		return err
	}

	// Validation policy
	if c.Spec.ValidationPolicy == nil {
		validationPolicy := ValidationPolicySpec{}

		asLog.Info(
			"Set default validation policy", "validationPolicy",
			validationPolicy,
		)

		c.Spec.ValidationPolicy = &validationPolicy
	}

	// Update rosterNodeBlockList
	for idx, nodeID := range c.Spec.RosterNodeBlockList {
		c.Spec.RosterNodeBlockList[idx] = strings.TrimLeft(strings.ToUpper(nodeID), "0")
	}

	if _, ok := c.Labels[AerospikeAPIVersionLabel]; !ok {
		if c.Labels == nil {
			c.Labels = make(map[string]string)
		}

		c.Labels[AerospikeAPIVersionLabel] = AerospikeAPIVersion
	}

	return nil
}

// SetDefaults applies defaults to the pod spec.
func (p *AerospikePodSpec) SetDefaults() error {
	var groupID int64

	if p.InputDNSPolicy == nil {
		if p.HostNetwork {
			p.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		} else {
			p.DNSPolicy = corev1.DNSClusterFirst
		}
	} else {
		p.DNSPolicy = *p.InputDNSPolicy
	}

	if p.SecurityContext != nil {
		if p.SecurityContext.FSGroup == nil {
			p.SecurityContext.FSGroup = &groupID
		}
	} else {
		SecurityContext := &corev1.PodSecurityContext{
			FSGroup: &groupID,
		}
		p.SecurityContext = SecurityContext
	}

	return nil
}

// setDefaultRackConf create the default rack if the spec has no racks configured.
func (c *AerospikeCluster) setDefaultRackConf(asLog logr.Logger) error {
	defaultRack := Rack{ID: DefaultRackID}

	if len(c.Spec.RackConfig.Racks) == 0 {
		c.Spec.RackConfig.Racks = append(c.Spec.RackConfig.Racks, defaultRack)
		asLog.Info(
			"No rack given. Added default rack-id for all nodes", "racks",
			c.Spec.RackConfig, "DefaultRackID", DefaultRackID,
		)
	} else {
		for idx := range c.Spec.RackConfig.Racks {
			rack := &c.Spec.RackConfig.Racks[idx]
			if rack.ID == DefaultRackID {
				// User has modified defaultRackConfig or used defaultRackID
				if len(c.Spec.RackConfig.Racks) > 1 ||
					rack.Zone != "" || rack.Region != "" || rack.RackLabel != "" || rack.NodeName != "" ||
					rack.InputAerospikeConfig != nil || rack.InputStorage != nil || rack.InputPodSpec != nil {
					return fmt.Errorf(
						"invalid RackConfig %v. RackID %d is reserved",
						c.Spec.RackConfig, DefaultRackID,
					)
				}
			}
		}
	}

	return nil
}

func (c *AerospikeCluster) updateRacks(asLog logr.Logger) error {
	c.updateRacksStorageFromGlobal(asLog)

	if err := c.updateRacksAerospikeConfigFromGlobal(asLog); err != nil {
		return fmt.Errorf("error updating rack aerospike config: %v", err)
	}

	c.updateRacksPodSpecFromGlobal(asLog)

	return nil
}

func (c *AerospikeCluster) updateRacksStorageFromGlobal(asLog logr.Logger) {
	for idx := range c.Spec.RackConfig.Racks {
		rack := &c.Spec.RackConfig.Racks[idx]

		if rack.InputStorage == nil {
			rack.Storage = c.Spec.Storage

			asLog.V(1).Info(
				"Updated rack storage with global storage", "rack id", rack.ID,
				"storage", rack.Storage,
			)
		} else {
			rack.Storage = *rack.InputStorage
		}

		// Set storage defaults if rack has storage section
		rack.Storage.SetDefaults()
	}
}

func (c *AerospikeCluster) updateRacksPodSpecFromGlobal(asLog logr.Logger) {
	for idx := range c.Spec.RackConfig.Racks {
		rack := &c.Spec.RackConfig.Racks[idx]

		if rack.InputPodSpec == nil {
			rack.PodSpec.SchedulingPolicy = c.Spec.PodSpec.SchedulingPolicy

			asLog.V(1).Info(
				"Updated rack podSpec with global podSpec", "rack id", rack.ID,
				"podSpec", rack.PodSpec,
			)
		} else {
			rack.PodSpec = *rack.InputPodSpec
		}
	}
}

func (c *AerospikeCluster) updateRacksAerospikeConfigFromGlobal(asLog logr.Logger) error {
	for idx := range c.Spec.RackConfig.Racks {
		rack := &c.Spec.RackConfig.Racks[idx]

		var (
			m   map[string]interface{}
			err error
		)

		if rack.InputAerospikeConfig != nil {
			// Merge this rack's and global config.
			m, err = merge.Merge(
				c.Spec.AerospikeConfig.Value, rack.InputAerospikeConfig.Value,
			)

			asLog.V(1).Info(
				"Merged rack config from global aerospikeConfig", "rack id",
				rack.ID, "rackAerospikeConfig", m, "globalAerospikeConfig",
				c.Spec.AerospikeConfig,
			)

			if err != nil {
				return err
			}
		} else {
			// Use the global config.
			m = c.Spec.AerospikeConfig.Value
		}

		asLog.V(1).Info(
			"Update rack aerospikeConfig from default aerospikeConfig",
			"rackAerospikeConfig", m,
		)

		// Set defaults in updated rack config
		// Above merge function may have overwritten defaults.
		if err := c.setDefaultAerospikeConfigs(
			asLog, AerospikeConfigSpec{Value: m},
		); err != nil {
			return err
		}

		c.Spec.RackConfig.Racks[idx].AerospikeConfig.Value = m
	}

	return nil
}

func (c *AerospikeCluster) setDefaultAerospikeConfigs(
	asLog logr.Logger, configSpec AerospikeConfigSpec,
) error {
	config := configSpec.Value

	// namespace conf
	if err := setDefaultNsConf(
		asLog, configSpec, c.Spec.RackConfig.Namespaces,
	); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(asLog, configSpec, c.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(
		asLog, &configSpec, c.Spec.OperatorClientCertSpec,
	); err != nil {
		return err
	}

	// logging conf
	if err := setDefaultLoggingConf(asLog, configSpec); err != nil {
		return err
	}

	// xdr conf
	if _, ok := config["xdr"]; ok {
		if err := setDefaultXDRConf(asLog, configSpec); err != nil {
			return err
		}
	}

	// escape LDAP configuration
	return escapeLDAPConfiguration(configSpec)
}

// setDefaults applies default to unspecified fields on the network policy.
func (n *AerospikeNetworkPolicy) setDefaults(namespace string) {
	if n.AccessType == AerospikeNetworkTypeUnspecified {
		n.AccessType = AerospikeNetworkTypeHostInternal
	}

	if n.AlternateAccessType == AerospikeNetworkTypeUnspecified {
		n.AlternateAccessType = AerospikeNetworkTypeHostExternal
	}

	if n.TLSAccessType == AerospikeNetworkTypeUnspecified {
		n.TLSAccessType = AerospikeNetworkTypeHostInternal
	}

	if n.TLSAlternateAccessType == AerospikeNetworkTypeUnspecified {
		n.TLSAlternateAccessType = AerospikeNetworkTypeHostExternal
	}

	// Set network namespace if not present
	n.setNetworkNamespace(namespace)
}

func (n *AerospikeNetworkPolicy) setNetworkNamespace(namespace string) {
	setNamespaceDefault(n.CustomAccessNetworkNames, namespace)
	setNamespaceDefault(n.CustomAlternateAccessNetworkNames, namespace)
	setNamespaceDefault(n.CustomTLSAccessNetworkNames, namespace)
	setNamespaceDefault(n.CustomTLSAlternateAccessNetworkNames, namespace)
	setNamespaceDefault(n.CustomFabricNetworkNames, namespace)
	setNamespaceDefault(n.CustomTLSFabricNetworkNames, namespace)
}

// *****************************************************************************
// Helper
// *****************************************************************************

func setDefaultNsConf(
	asLog logr.Logger, configSpec AerospikeConfigSpec,
	rackEnabledNsList []string,
) error {
	config := configSpec.Value
	// namespace conf
	nsConf, ok := config["namespaces"]
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.namespaces not present. aerospikeConfig %v",
			config,
		)
	} else if nsConf == nil {
		return fmt.Errorf("aerospikeConfig.namespaces cannot be nil")
	}

	nsList, ok := nsConf.([]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.namespaces not valid namespace list %v", nsConf,
		)
	} else if len(nsList) == 0 {
		return fmt.Errorf(
			"aerospikeConfig.namespaces cannot be empty. aerospikeConfig %v",
			config,
		)
	}

	for _, nsInt := range nsList {
		nsMap, ok := nsInt.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"aerospikeConfig.namespaces does not have valid namespace map. nsMap %v",
				nsInt,
			)
		}

		// Add dummy rack-id only for rackEnabled namespaces
		defaultConfs := map[string]interface{}{"rack-id": DefaultRackID}

		if nsName, ok := nsMap["name"]; ok {
			if _, ok := nsName.(string); ok {
				if isNameExist(rackEnabledNsList, nsName.(string)) {
					// Add dummy rack-id, should be replaced with actual rack-id by init-container script
					if err := setDefaultsInConfigMap(
						asLog, nsMap, defaultConfs,
					); err != nil {
						return fmt.Errorf(
							"failed to set default aerospikeConfig.namespaces rack config: %v",
							err,
						)
					}
				} else {
					// User may have added this key or may have patched object with new smaller rackEnabledNamespace list
					// but left namespace defaults. This key should be removed then only controller will detect
					// that some namespace is removed from rackEnabledNamespace list and cluster needs rolling restart
					asLog.Info(
						"Name aerospikeConfig.namespaces.name not found in rackEnabled namespace list. "+
							"Namespace will not have defaultRackID",
						"nsName", nsName, "rackEnabledNamespaces",
						rackEnabledNsList,
					)

					delete(nsMap, "rack-id")
				}
			}
		}
	}

	asLog.Info("Set default template values in aerospikeConfig.namespace")

	return nil
}

func setDefaultServiceConf(
	asLog logr.Logger, configSpec AerospikeConfigSpec, crObjName string,
) error {
	config := configSpec.Value

	if _, ok := config["service"]; !ok {
		config["service"] = map[string]interface{}{}
	}

	serviceConf, ok := config["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.service not a valid map %v", config["service"],
		)
	}

	defaultConfs := map[string]interface{}{
		"node-id":      "ENV_NODE_ID",
		"cluster-name": crObjName,
	}

	if err := setDefaultsInConfigMap(
		asLog, serviceConf, defaultConfs,
	); err != nil {
		return fmt.Errorf(
			"failed to set default aerospikeConfig.service config: %v", err,
		)
	}

	asLog.Info(
		"Set default template values in aerospikeConfig.service",
		"aerospikeConfig.service", serviceConf,
	)

	return nil
}

func setDefaultNetworkConf(
	asLog logr.Logger, configSpec *AerospikeConfigSpec,
	clientCertSpec *AerospikeOperatorClientCertSpec,
) error {
	config := configSpec.Value

	// Network section
	if _, ok := config["network"]; !ok {
		config["network"] = map[string]interface{}{}
	}

	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", config["network"],
		)
	}

	// Service section
	if _, ok = networkConf["service"]; !ok {
		networkConf["service"] = map[string]interface{}{}
	}

	serviceConf, ok := networkConf["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network.service not a valid map %v",
			networkConf["service"],
		)
	}
	// Override these sections
	// TODO: These values lines will be replaces with runtime info by script in init-container
	// See if we can get better way to make template
	serviceDefaults := map[string]interface{}{}
	srvPort := GetServicePort(configSpec)

	if srvPort != nil {
		serviceDefaults["port"] = *srvPort
		serviceDefaults["access-port"] = *srvPort
		serviceDefaults["access-addresses"] = []string{"<access-address>"}
		serviceDefaults["alternate-access-port"] = *srvPort
		serviceDefaults["alternate-access-addresses"] = []string{"<alternate-access-address>"}
	}

	if tlsName, tlsPort := GetServiceTLSNameAndPort(configSpec); tlsName != "" && tlsPort != nil {
		serviceDefaults["tls-port"] = *tlsPort
		serviceDefaults["tls-access-port"] = *tlsPort
		serviceDefaults["tls-access-addresses"] = []string{"<tls-access-address>"}
		serviceDefaults["tls-alternate-access-port"] = *tlsPort
		serviceDefaults["tls-alternate-access-addresses"] = []string{"<tls-alternate-access-address>"}
	}

	if err := setDefaultsInConfigMap(
		asLog, serviceConf, serviceDefaults,
	); err != nil {
		return fmt.Errorf(
			"failed to set default aerospikeConfig.network.service config: %v",
			err,
		)
	}

	asLog.Info(
		"Set default template values in aerospikeConfig.network.service",
		"aerospikeConfig.network.service", serviceConf,
	)

	// Heartbeat section
	if _, ok = networkConf["heartbeat"]; !ok {
		networkConf["heartbeat"] = map[string]interface{}{}
	}

	heartbeatConf, ok := networkConf["heartbeat"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network.heartbeat not a valid map %v",
			networkConf["heartbeat"],
		)
	}

	hbDefaults := map[string]interface{}{}
	hbDefaults["mode"] = "mesh"

	if err := setDefaultsInConfigMap(
		asLog, heartbeatConf, hbDefaults,
	); err != nil {
		return fmt.Errorf(
			"failed to set default aerospikeConfig.network.heartbeat config: %v",
			err,
		)
	}

	asLog.Info(
		"Set default template values in aerospikeConfig.network.heartbeat",
		"aerospikeConfig.network.heartbeat", heartbeatConf,
	)

	// Fabric section
	if _, ok := networkConf["fabric"]; !ok {
		networkConf["fabric"] = map[string]interface{}{}
	}

	return addOperatorClientNameIfNeeded(
		asLog, serviceConf, configSpec,
		clientCertSpec,
	)
}

func addOperatorClientNameIfNeeded(
	aslog logr.Logger, serviceConf map[string]interface{},
	configSpec *AerospikeConfigSpec,
	clientCertSpec *AerospikeOperatorClientCertSpec,
) error {
	tlsAuthenticateClientConfig, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		if IsServiceTLSEnabled(configSpec) {
			serviceConf["tls-authenticate-client"] = "any"
		}

		return nil
	}

	if clientCertSpec == nil || clientCertSpec.TLSClientName == "" {
		aslog.Info(
			"OperatorClientCertSpec or its TLSClientName is not" +
				" configured. Skipping setting tls-authenticate-client.",
		)

		return nil
	}

	if value, ok := tlsAuthenticateClientConfig.([]interface{}); ok {
		if !reflect.DeepEqual("any", value) && !reflect.DeepEqual(
			value, "false",
		) {
			if !func() bool {
				for i := 0; i < len(value); i++ {
					if reflect.DeepEqual(
						value[i], clientCertSpec.TLSClientName,
					) {
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

func setDefaultLoggingConf(
	asLog logr.Logger, configSpec AerospikeConfigSpec,
) error {
	config := configSpec.Value

	if _, ok := config["logging"]; !ok {
		config["logging"] = []interface{}{}
	}

	loggingConfList, ok := config["logging"].([]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.logging not a valid list %v", config["logging"],
		)
	}

	var found bool

	for _, conf := range loggingConfList {
		logConf, ok := conf.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"aerospikeConfig.logging not a list of valid map %v", logConf,
			)
		}

		if logConf["name"] == "console" {
			found = true
			break
		}
	}

	if !found {
		loggingConfList = append(
			loggingConfList, map[string]interface{}{
				"name": "console",
				"any":  "info",
			},
		)
	}

	asLog.Info(
		"Set default template values in aerospikeConfig.logging",
		"aerospikeConfig.logging", loggingConfList,
	)

	config["logging"] = loggingConfList

	return nil
}

func setDefaultXDRConf(
	_ logr.Logger, _ AerospikeConfigSpec,
) error {
	// Nothing to update for now
	return nil
}

func setDefaultsInConfigMap(
	_ logr.Logger, baseConfigs, defaultConfigs map[string]interface{},
) error {
	for k, v := range defaultConfigs {
		// Special handling.
		// Older baseValues are parsed to int64 but defaults are in int
		if newV, ok := v.(int); ok {
			// TODO: verify this: Looks like, in new openapi schema, values are parsed in float64
			v = float64(newV)
		}

		// Older baseValues are parsed to []interface{} but defaults are in []string
		// Can make default as []interface{} but then we have to remember it there.
		// []string looks make natural there. So lets handle it here only
		if newV, ok := v.([]string); ok {
			v = toInterfaceList(newV)
		}

		if bv, ok := baseConfigs[k]; ok &&
			!reflect.DeepEqual(bv, v) {
			return fmt.Errorf(
				"config %s can not have non-default value (%T %v). It will be set internally (%T %v)",
				k, bv, bv, v, v,
			)
		}

		baseConfigs[k] = v
	}

	return nil
}

func toInterfaceList(list []string) []interface{} {
	iList := make([]interface{}, 0, len(list))

	for _, e := range list {
		iList = append(iList, e)
	}

	return iList
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

// escapeLDAPConfiguration escapes LDAP variables ${un} and ${dn} to
// $${_DNE}{un} and $${_DNE}{dn} to prevent aerospike container images
// template expansion from messing up the LDAP section.
func escapeLDAPConfiguration(configSpec AerospikeConfigSpec) error {
	config := configSpec.Value

	if _, ok := config["security"]; ok {
		security, ok := config["security"].(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"security conf not in valid format %v", config["security"],
			)
		}

		if _, ok := security["ldap"]; ok {
			security["ldap"] = escapeValue(security["ldap"])
		}
	}

	return nil
}

func escapeValue(valueGeneric interface{}) interface{} {
	switch value := valueGeneric.(type) {
	case string:
		return escapeString(value)
	case []interface{}:
		var modifiedSlice []interface{}

		for _, item := range value {
			modifiedSlice = append(modifiedSlice, escapeValue(item))
		}

		return modifiedSlice
	case []string:
		var modifiedSlice []string

		for _, item := range value {
			modifiedSlice = append(modifiedSlice, escapeString(item))
		}

		return modifiedSlice
	case map[string]interface{}:
		modifiedMap := map[string]interface{}{}

		for key, mapValue := range value {
			modifiedMap[escapeString(key)] = escapeValue(mapValue)
		}

		return modifiedMap
	case map[string]string:
		modifiedMap := map[string]string{}

		for key, mapValue := range value {
			modifiedMap[escapeString(key)] = escapeString(mapValue)
		}

		return modifiedMap
	default:
		return value
	}
}

func escapeString(str string) string {
	str = strings.ReplaceAll(str, "${un}", "$${_DNE}{un}")
	str = strings.ReplaceAll(str, "${dn}", "$${_DNE}{dn}")

	return str
}

func setNamespaceDefault(networks []string, namespace string) {
	for idx := range networks {
		netName := strings.TrimSpace(networks[idx])
		namespacedName := strings.Split(netName, "/")

		if len(namespacedName) == 1 {
			netName = namespace + "/" + netName
		}

		networks[idx] = netName
	}
}
