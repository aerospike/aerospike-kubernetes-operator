/*
Copyright 2024.

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
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/merge"
	lib "github.com/aerospike/aerospike-management-lib"
)

// +kubebuilder:object:generate=false
// Above marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type AerospikeClusterCustomDefaulter struct {
	// Default values for various AerospikeCluster fields
}

// Implemented webhook.CustomDefaulter interface for future reference
var _ webhook.CustomDefaulter = &AerospikeClusterCustomDefaulter{}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/mutate-asdb-aerospike-com-v1-aerospikecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1,name=maerospikecluster.kb.io,admissionReviewVersions={v1}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (acd *AerospikeClusterCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	aerospikeCluster, ok := obj.(*asdbv1.AerospikeCluster)
	if !ok {
		return fmt.Errorf("expected AerospikeCluster, got %T", obj)
	}

	asLog := logf.Log.WithName(asdbv1.ClusterNamespacedName(aerospikeCluster))

	asLog.Info(
		"Setting defaults for aerospikeCluster", "aerospikecluster.Spec",
		aerospikeCluster.Spec,
	)

	if err := acd.setDefaults(asLog, aerospikeCluster); err != nil {
		asLog.Error(err, "Mutate AerospikeCluster create failed")
		return err
	}

	asLog.Info("Setting defaults for aerospikeCluster completed")

	asLog.Info(
		"Added defaults for aerospikeCluster", "aerospikecluster.Spec", aerospikeCluster.Spec,
	)

	return nil
}

func (acd *AerospikeClusterCustomDefaulter) setDefaults(asLog logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	// If PDB is disabled, set maxUnavailable to nil
	if asdbv1.GetBool(cluster.Spec.DisablePDB) {
		cluster.Spec.MaxUnavailable = nil
	} else if cluster.Spec.MaxUnavailable == nil {
		// Set default maxUnavailable if not set
		maxUnavailable := intstr.FromInt32(1)
		cluster.Spec.MaxUnavailable = &maxUnavailable
	}

	// Set network defaults
	setNetworkPolicyDefaults(&cluster.Spec.AerospikeNetworkPolicy, cluster.Namespace)

	// Set common storage defaults.
	setStorageDefaults(&cluster.Spec.Storage)

	// Add default rackConfig if not already given. Disallow use of defaultRackID by user.
	// Need to set before setting defaults in aerospikeConfig.
	// aerospikeConfig.namespace checks for racks
	if err := setDefaultRackConf(asLog, &cluster.Spec.RackConfig); err != nil {
		return err
	}

	if cluster.Spec.AerospikeConfig == nil {
		return fmt.Errorf("spec.aerospikeConfig cannot be nil")
	}

	// Set common aerospikeConfig defaults
	// Update configMap
	if err := setDefaultAerospikeConfigs(asLog, *cluster.Spec.AerospikeConfig, nil, cluster); err != nil {
		return err
	}

	// Update racks configuration using global values where required.
	if err := updateRacks(asLog, cluster); err != nil {
		return err
	}

	// Set defaults for pod spec
	setPodSpecDefaults(&cluster.Spec.PodSpec)

	// Validation policy
	if cluster.Spec.ValidationPolicy == nil {
		validationPolicy := asdbv1.ValidationPolicySpec{}

		asLog.Info(
			"Set default validation policy", "validationPolicy",
			validationPolicy,
		)

		cluster.Spec.ValidationPolicy = &validationPolicy
	}

	// Update rosterNodeBlockList
	for idx, nodeID := range cluster.Spec.RosterNodeBlockList {
		cluster.Spec.RosterNodeBlockList[idx] = strings.TrimLeft(strings.ToUpper(nodeID), "0")
	}

	if _, ok := cluster.Labels[asdbv1.AerospikeAPIVersionLabel]; !ok {
		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}

		cluster.Labels[asdbv1.AerospikeAPIVersionLabel] = asdbv1.AerospikeAPIVersion
	}

	return nil
}

// setPodSpecDefaults applies defaults to the pod spec.
func setPodSpecDefaults(podSpec *asdbv1.AerospikePodSpec) {
	var groupID int64

	if podSpec.InputDNSPolicy == nil {
		if podSpec.HostNetwork {
			podSpec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		} else {
			podSpec.DNSPolicy = corev1.DNSClusterFirst
		}
	} else {
		podSpec.DNSPolicy = *podSpec.InputDNSPolicy
	}

	if podSpec.SecurityContext != nil {
		if podSpec.SecurityContext.FSGroup == nil {
			podSpec.SecurityContext.FSGroup = &groupID
		}
	} else {
		SecurityContext := &corev1.PodSecurityContext{
			FSGroup: &groupID,
		}
		podSpec.SecurityContext = SecurityContext
	}
}

// setDefaultRackConf create the default rack if the spec has no racks configured.
func setDefaultRackConf(asLog logr.Logger, rackConfig *asdbv1.RackConfig) error {
	defaultRack := asdbv1.Rack{ID: asdbv1.DefaultRackID}

	if len(rackConfig.Racks) == 0 {
		rackConfig.Racks = append(rackConfig.Racks, defaultRack)
		asLog.Info(
			"No rack given. Added default rack-id for all nodes", "racks",
			rackConfig, "DefaultRackID", asdbv1.DefaultRackID,
		)
	} else {
		for idx := range rackConfig.Racks {
			rack := &rackConfig.Racks[idx]
			if rack.ID == asdbv1.DefaultRackID {
				// User has modified defaultRackConfig or used defaultRackID
				if len(rackConfig.Racks) > 1 ||
					rack.Zone != "" || rack.Region != "" || rack.RackLabel != "" || rack.NodeName != "" ||
					rack.InputAerospikeConfig != nil || rack.InputStorage != nil || rack.InputPodSpec != nil {
					return fmt.Errorf(
						"invalid RackConfig %v. RackID %d is reserved",
						rackConfig, asdbv1.DefaultRackID,
					)
				}
			}
		}
	}

	return nil
}

func updateRacks(asLog logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	updateRacksStorageFromGlobal(asLog, cluster)

	if err := updateRacksAerospikeConfigFromGlobal(asLog, cluster); err != nil {
		return fmt.Errorf("error updating rack aerospike config: %v", err)
	}

	updateRacksPodSpecFromGlobal(asLog, cluster)

	return nil
}

func updateRacksStorageFromGlobal(asLog logr.Logger, cluster *asdbv1.AerospikeCluster) {
	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]

		if rack.InputStorage == nil {
			rack.Storage = cluster.Spec.Storage

			asLog.V(1).Info(
				"Updated rack storage with global storage", "rack id", rack.ID,
				"storage", rack.Storage,
			)
		} else {
			rack.Storage = *rack.InputStorage
		}

		// Set storage defaults if rack has storage section
		setStorageDefaults(&rack.Storage)
	}
}

func updateRacksPodSpecFromGlobal(asLog logr.Logger, cluster *asdbv1.AerospikeCluster) {
	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]

		if rack.InputPodSpec == nil {
			rack.PodSpec.SchedulingPolicy = cluster.Spec.PodSpec.SchedulingPolicy

			asLog.V(1).Info(
				"Updated rack podSpec with global podSpec", "rack id", rack.ID,
				"podSpec", rack.PodSpec,
			)
		} else {
			rack.PodSpec = *rack.InputPodSpec
		}
	}
}

func updateRacksAerospikeConfigFromGlobal(asLog logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]

		var m map[string]interface{}

		if rack.InputAerospikeConfig != nil {
			// Merge this rack's and global config.
			merged, err := merge.Merge(
				cluster.Spec.AerospikeConfig.Value, rack.InputAerospikeConfig.Value,
			)
			if err != nil {
				return err
			}

			m = lib.DeepCopy(merged).(map[string]interface{})

			asLog.V(1).Info(
				"Merged rack config from global aerospikeConfig", "rack id",
				rack.ID, "rackAerospikeConfig", m, "globalAerospikeConfig",
				cluster.Spec.AerospikeConfig,
			)
		} else {
			// Use the global config.
			m = cluster.Spec.AerospikeConfig.DeepCopy().Value
		}

		asLog.V(1).Info(
			"Update rack aerospikeConfig from default aerospikeConfig",
			"rackAerospikeConfig", m,
		)

		// Set defaults in updated rack config
		// Above merge function may have overwritten defaults.
		if err := setDefaultAerospikeConfigs(asLog, asdbv1.AerospikeConfigSpec{Value: m}, &rack.ID,
			cluster); err != nil {
			return err
		}

		cluster.Spec.RackConfig.Racks[idx].AerospikeConfig.Value = m
	}

	return nil
}

func setDefaultAerospikeConfigs(asLog logr.Logger,
	configSpec asdbv1.AerospikeConfigSpec, rackID *int, cluster *asdbv1.AerospikeCluster) error {
	config := configSpec.Value

	// namespace conf
	if err := setDefaultNsConf(asLog, configSpec, cluster.Spec.RackConfig.Namespaces, rackID); err != nil {
		return err
	}

	// service conf
	if err := setDefaultServiceConf(asLog, configSpec, cluster.Name); err != nil {
		return err
	}

	// network conf
	if err := setDefaultNetworkConf(
		asLog, &configSpec, cluster.Spec.OperatorClientCertSpec,
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
func setNetworkPolicyDefaults(networkPolicy *asdbv1.AerospikeNetworkPolicy, namespace string) {
	if networkPolicy.AccessType == asdbv1.AerospikeNetworkTypeUnspecified {
		networkPolicy.AccessType = asdbv1.AerospikeNetworkTypeHostInternal
	}

	if networkPolicy.AlternateAccessType == asdbv1.AerospikeNetworkTypeUnspecified {
		networkPolicy.AlternateAccessType = asdbv1.AerospikeNetworkTypeHostExternal
	}

	if networkPolicy.TLSAccessType == asdbv1.AerospikeNetworkTypeUnspecified {
		networkPolicy.TLSAccessType = asdbv1.AerospikeNetworkTypeHostInternal
	}

	if networkPolicy.TLSAlternateAccessType == asdbv1.AerospikeNetworkTypeUnspecified {
		networkPolicy.TLSAlternateAccessType = asdbv1.AerospikeNetworkTypeHostExternal
	}

	// Set network namespace if not present
	setNetworkNamespace(namespace, networkPolicy)
}

func setNetworkNamespace(namespace string, networkPolicy *asdbv1.AerospikeNetworkPolicy) {
	setNamespaceDefault(networkPolicy.CustomAccessNetworkNames, namespace)
	setNamespaceDefault(networkPolicy.CustomAlternateAccessNetworkNames, namespace)
	setNamespaceDefault(networkPolicy.CustomTLSAccessNetworkNames, namespace)
	setNamespaceDefault(networkPolicy.CustomTLSAlternateAccessNetworkNames, namespace)
	setNamespaceDefault(networkPolicy.CustomFabricNetworkNames, namespace)
	setNamespaceDefault(networkPolicy.CustomTLSFabricNetworkNames, namespace)
}

// *****************************************************************************
// Helper
// *****************************************************************************

func setDefaultNsConf(asLog logr.Logger, configSpec asdbv1.AerospikeConfigSpec,
	rackEnabledNsList []string, rackID *int) error {
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

		if nsName, ok := nsMap["name"]; ok {
			if name, ok := nsName.(string); ok {
				if isNameExist(rackEnabledNsList, name) {
					// Add rack-id only for rackEnabled namespaces
					if rackID != nil {
						// Add rack-id only in rack specific config, not in global config
						defaultConfs := map[string]interface{}{"rack-id": *rackID}

						// rack-id was historically set to 0 for all namespaces, but since the AKO 3.3.0, it reflects actual values.
						// During the AKO 3.3.0 upgrade rack-id for namespaces in rack specific config is set to 0.
						// Hence, deleting this 0 rack-id so that correct rack-id will be added.
						if id, ok := nsMap["rack-id"]; ok && id == float64(0) && *rackID != 0 {
							delete(nsMap, "rack-id")
						}

						if err := setDefaultsInConfigMap(
							asLog, nsMap, defaultConfs,
						); err != nil {
							return fmt.Errorf(
								"failed to set default aerospikeConfig.namespaces rack config: %v",
								err,
							)
						}
					} else {
						// Deleting rack-id for namespaces in global config.
						// Correct rack-id will be added in rack specific config.
						delete(nsMap, "rack-id")
					}
				} else {
					// User may have added this key or may have patched object with new smaller rackEnabledNamespace list
					// but left namespace defaults. This key should be removed then only controller will detect
					// that some namespace is removed from rackEnabledNamespace list and cluster needs rolling restart
					asLog.Info(
						"Name aerospikeConfig.namespaces.name not found in rackEnabled namespace list. "+
							"Namespace will not have any rackID",
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
	asLog logr.Logger, configSpec asdbv1.AerospikeConfigSpec, crObjName string,
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
	asLog logr.Logger, configSpec *asdbv1.AerospikeConfigSpec,
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	config := configSpec.Value

	// Network section
	if _, ok := config["network"]; !ok {
		return fmt.Errorf("aerospikeConfig.network cannot be nil")
	}

	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", config["network"],
		)
	}

	// Service section
	if _, ok = networkConf["service"]; !ok {
		return fmt.Errorf("aerospikeConfig.network.service cannot be nil")
	}

	serviceConf, ok := networkConf["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network.service not a valid map %v",
			networkConf["service"],
		)
	}
	// Override these sections
	// TODO: These values lines will be replaced with runtime info by akoinit binary in init-container
	// See if we can get better way to make template
	serviceDefaults := map[string]interface{}{}
	srvPort := asdbv1.GetServicePort(configSpec)

	if srvPort != nil {
		serviceDefaults["port"] = *srvPort
		serviceDefaults["access-port"] = *srvPort
		serviceDefaults["access-addresses"] = []string{"<access-address>"}
		serviceDefaults["alternate-access-port"] = *srvPort
		serviceDefaults["alternate-access-addresses"] = []string{"<alternate-access-address>"}
	} else {
		delete(serviceConf, "access-port")
		delete(serviceConf, "access-addresses")
		delete(serviceConf, "alternate-access-addresses")
		delete(serviceConf, "alternate-access-port")
	}

	if tlsName, tlsPort := asdbv1.GetServiceTLSNameAndPort(configSpec); tlsName != "" && tlsPort != nil {
		serviceDefaults["tls-port"] = *tlsPort
		serviceDefaults["tls-access-port"] = *tlsPort
		serviceDefaults["tls-access-addresses"] = []string{"<tls-access-address>"}
		serviceDefaults["tls-alternate-access-port"] = *tlsPort
		serviceDefaults["tls-alternate-access-addresses"] = []string{"<tls-alternate-access-address>"}
	} else {
		delete(serviceConf, "tls-access-port")
		delete(serviceConf, "tls-access-addresses")
		delete(serviceConf, "tls-alternate-access-addresses")
		delete(serviceConf, "tls-alternate-access-port")
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
		return fmt.Errorf("aerospikeConfig.network.heartbeat cannot be nil")
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
		return fmt.Errorf("aerospikeConfig.network.fabric cannot be nil")
	}

	return addOperatorClientNameIfNeeded(
		asLog, serviceConf, configSpec,
		clientCertSpec,
	)
}

func addOperatorClientNameIfNeeded(
	aslog logr.Logger, serviceConf map[string]interface{},
	configSpec *asdbv1.AerospikeConfigSpec,
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	tlsAuthenticateClientConfig, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		if asdbv1.IsServiceTLSEnabled(configSpec) {
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
	asLog logr.Logger, configSpec asdbv1.AerospikeConfigSpec,
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
	_ logr.Logger, _ asdbv1.AerospikeConfigSpec,
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
func escapeLDAPConfiguration(configSpec asdbv1.AerospikeConfigSpec) error {
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
