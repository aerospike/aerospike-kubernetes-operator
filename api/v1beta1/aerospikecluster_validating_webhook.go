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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/util/intstr"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	internalerrors "github.com/aerospike/aerospike-kubernetes-operator/errors"
	//"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	"github.com/aerospike/aerospike-management-lib/deployment"
	validate "github.com/asaskevich/govalidator"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var networkConnectionTypes = []string{"service", "heartbeat", "fabric"}
var immutableNetworkParams = []string{
	"tls-name", "port", "access-port",
	"alternate-access-port", "tls-port", "tls-access-port",
	"tls-alternate-access-port",
}

var versionRegex = regexp.MustCompile(`([0-9]+(\.[0-9]+)+)`)

// +kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1beta1-aerospikecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1beta1,name=vaerospikecluster.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &AerospikeCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (c *AerospikeCluster) ValidateCreate() error {
	aslog := logf.Log.WithName(ClusterNamespacedName(c))

	aslog.Info("Validate create")

	return c.validate(aslog)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (c *AerospikeCluster) ValidateDelete() error {
	aslog := logf.Log.WithName(ClusterNamespacedName(c))

	aslog.Info("Validate delete")

	return nil
}

// ValidateUpdate validate update
func (c *AerospikeCluster) ValidateUpdate(oldObj runtime.Object) error {
	aslog := logf.Log.WithName(ClusterNamespacedName(c))

	aslog.Info("Validate update")

	old := oldObj.(*AerospikeCluster)
	if err := c.validate(aslog); err != nil {
		return err
	}

	outgoingVersion, err := GetImageVersion(old.Spec.Image)
	if err != nil {
		return err
	}

	incomingVersion, err := GetImageVersion(c.Spec.Image)
	if err != nil {
		return err
	}

	if err := deployment.IsValidUpgrade(
		outgoingVersion, incomingVersion,
	); err != nil {
		return fmt.Errorf("failed to start upgrade: %v", err)
	}

	// Volume storage update is not allowed but cascadeDelete policy is allowed
	if err := old.Spec.Storage.ValidateStorageSpecChange(c.Spec.Storage); err != nil {
		return fmt.Errorf("storage config cannot be updated: %v", err)
	}

	// MultiPodPerHost can not be updated
	if c.Spec.PodSpec.MultiPodPerHost != old.Spec.PodSpec.MultiPodPerHost {
		return fmt.Errorf("cannot update MultiPodPerHost setting")
	}

	// Validate AerospikeConfig update
	if err := validateAerospikeConfigUpdate(
		aslog, incomingVersion, outgoingVersion,
		c.Spec.AerospikeConfig, old.Spec.AerospikeConfig,
	); err != nil {
		return err
	}

	// Validate Load Balancer update
	if err := validateLoadBalancerUpdate(
		aslog, c.Spec.SeedsFinderServices.LoadBalancer,
		old.Spec.SeedsFinderServices.LoadBalancer,
	); err != nil {
		return err
	}

	// Validate RackConfig update
	if err := c.validateRackUpdate(aslog, old); err != nil {
		return err
	}

	// Validate changes to pod spec
	if err := old.Spec.PodSpec.ValidatePodSpecChange(c.Spec.PodSpec); err != nil {
		return err
	}

	return nil
}

func (c *AerospikeCluster) validate(aslog logr.Logger) error {
	aslog.V(1).Info("Validate AerospikeCluster spec", "obj.Spec", c.Spec)

	// Validate obj name
	if c.Name == "" {
		return fmt.Errorf("aerospikeCluster name cannot be empty")
	}
	if strings.Contains(c.Name, " ") {
		// Few parsing logic depend on this
		return fmt.Errorf("aerospikeCluster name cannot have spaces")
	}

	// Validate obj namespace
	if c.Namespace == "" {
		return fmt.Errorf("aerospikeCluster namespace name cannot be empty")
	}
	if strings.Contains(c.Namespace, " ") {
		// Few parsing logic depend on this
		return fmt.Errorf("aerospikeCluster name cannot have spaces")
	}

	// Validate image type. Only enterprise image allowed for now
	if !isEnterprise(c.Spec.Image) {
		return fmt.Errorf("CommunityEdition Cluster not supported")
	}

	// Validate size
	if c.Spec.Size == 0 {
		return fmt.Errorf("invalid cluster size 0")
	}

	// Validate Image version
	version, err := GetImageVersion(c.Spec.Image)
	if err != nil {
		return err
	}

	val, err := asconfig.CompareVersions(version, baseVersion)
	if err != nil {
		return fmt.Errorf("failed to check image version: %v", err)
	}
	if val < 0 {
		return fmt.Errorf(
			"image version %s not supported. Base version %s", version,
			baseVersion,
		)
	}

	err = validateClusterSize(aslog, version, int(c.Spec.Size))
	if err != nil {
		return err
	}

	// Storage should be validated before validating aerospikeConfig and fileStorage
	if err := validateStorage(&c.Spec.Storage, &c.Spec.PodSpec); err != nil {
		return err
	}

	for _, rack := range c.Spec.RackConfig.Racks {
		// Storage should be validated before validating aerospikeConfig and fileStorage
		if err := validateStorage(&rack.Storage, &c.Spec.PodSpec); err != nil {
			return err
		}

		// Validate if passed aerospikeConfig
		if err := validateAerospikeConfigSchema(
			aslog, version, rack.AerospikeConfig,
		); err != nil {
			return fmt.Errorf("aerospikeConfig not valid: %v", err)
		}

		// Validate common aerospike config
		if err := c.validateAerospikeConfig(
			&rack.AerospikeConfig, &rack.Storage, int(c.Spec.Size),
		); err != nil {
			return err
		}

		if err := validateRequiredFileStorageForMetadata(
			rack.AerospikeConfig, &rack.Storage, c.Spec.ValidationPolicy, version,
		); err != nil {
			return err
		}

		if err := validateRequiredFileStorageForFeatureConf(
			rack.AerospikeConfig, &rack.Storage,
		); err != nil {
			return err
		}
	}

	// Validate resource and limit
	if err := c.validatePodSpecResourceAndLimits(aslog); err != nil {
		return err
	}

	// Validate access control
	if err := c.validateAccessControl(aslog); err != nil {
		return err
	}

	// Validate rackConfig
	if err := c.validateRackConfig(aslog); err != nil {
		return err
	}

	if err := validateClientCertSpec(
		c.Spec.OperatorClientCertSpec, c.Spec.AerospikeConfig,
	); err != nil {
		return err
	}

	// Validate Sidecars
	if err := c.validatePodSpec(); err != nil {
		return err
	}

	return nil
}

func validateClientCertSpec(
	clientCertSpec *AerospikeOperatorClientCertSpec,
	configSpec *AerospikeConfigSpec,
) error {
	networkConf, networkConfExist := configSpec.Value[confKeyNetwork]
	if !networkConfExist {
		return nil
	}
	serviceConf, serviceConfExists := networkConf.(map[string]interface{})[confKeyNetworkService]
	if !serviceConfExists {
		return nil
	}
	tlsAuthenticateClientConfig, ok := serviceConf.(map[string]interface{})["tls-authenticate-client"]
	if !ok {
		return nil
	}

	switch {
	case reflect.DeepEqual("false", tlsAuthenticateClientConfig):
		return nil
	case reflect.DeepEqual("any", tlsAuthenticateClientConfig):
		if clientCertSpec == nil {
			return fmt.Errorf("operator client cert is not specified")
		}
		if !clientCertSpec.IsClientCertConfigured() {
			return fmt.Errorf("operator client cert is not configured")
		}
		return nil
	default:
		if clientCertSpec == nil {
			return fmt.Errorf("operator client cert is not specified")
		}
		if clientCertSpec.TLSClientName == "" {
			return fmt.Errorf("operator TLSClientName is not specified")
		}
		if err := clientCertSpec.validate(); err != nil {
			return err
		}

	}

	return nil
}

func (c *AerospikeCluster) validateRackUpdate(
	aslog logr.Logger, old *AerospikeCluster,
) error {
	if reflect.DeepEqual(c.Spec.RackConfig, old.Spec.RackConfig) {
		return nil
	}

	outgoingVersion, err := GetImageVersion(old.Spec.Image)
	if err != nil {
		return err
	}
	incomingVersion, err := GetImageVersion(c.Spec.Image)
	if err != nil {
		return err
	}
	// Old racks can not be updated
	// Also need to exclude a default rack with default rack ID. No need to check here, user should not provide or update default rackID
	// Also when user add new rackIDs old default will be removed by reconciler.
	for _, oldRack := range old.Spec.RackConfig.Racks {
		for _, newRack := range c.Spec.RackConfig.Racks {

			if oldRack.ID == newRack.ID {

				if oldRack.NodeName != newRack.NodeName ||
					oldRack.RackLabel != newRack.RackLabel ||
					oldRack.Region != newRack.Region ||
					oldRack.Zone != newRack.Zone {

					return fmt.Errorf(
						"old RackConfig (NodeName, RackLabel, Region, Zone) can not be updated. Old rack %v, new rack %v",
						oldRack, newRack,
					)
				}

				if len(oldRack.AerospikeConfig.Value) != 0 || len(newRack.AerospikeConfig.Value) != 0 {
					// Validate aerospikeConfig update
					if err := validateAerospikeConfigUpdate(
						aslog, incomingVersion, outgoingVersion,
						&newRack.AerospikeConfig, &oldRack.AerospikeConfig,
					); err != nil {
						return fmt.Errorf(
							"invalid update in Rack(ID: %d) aerospikeConfig: %v",
							oldRack.ID, err,
						)
					}
				}

				if len(oldRack.Storage.Volumes) != 0 || len(newRack.Storage.Volumes) != 0 {
					// Storage might have changed
					oldStorage := oldRack.Storage
					newStorage := newRack.Storage
					// Volume storage update is not allowed but cascadeDelete policy is allowed
					if err := oldStorage.ValidateStorageSpecChange(newStorage); err != nil {
						return fmt.Errorf(
							"rack storage config cannot be updated: %v", err,
						)
					}
				}

				break
			}
		}
	}
	return nil
}

// TODO: FIX
func (c *AerospikeCluster) validateAccessControl(_ logr.Logger) error {
	_, err := IsAerospikeAccessControlValid(&c.Spec)
	return err
}

func (c *AerospikeCluster) validatePodSpecResourceAndLimits(_ logr.Logger) error {

	checkResourcesLimits := false

	if err := c.validateResourceAndLimits(c.Spec.PodSpec.AerospikeContainerSpec.Resources, checkResourcesLimits); err != nil {
		return err
	}

	for _, rack := range c.Spec.RackConfig.Racks {
		if rack.Storage.CleanupThreads != AerospikeVolumeSingleCleanupThread {
			checkResourcesLimits = true
			break
		}
	}

	if checkResourcesLimits && c.Spec.PodSpec.AerospikeInitContainerSpec == nil {
		return fmt.Errorf(
			"init container spec should have resources.Limits set if CleanupThreads is more than 1",
		)
	}
	if c.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		return c.validateResourceAndLimits(c.Spec.PodSpec.AerospikeInitContainerSpec.Resources, checkResourcesLimits)
	}
	return nil
}

func (c *AerospikeCluster) validateResourceAndLimits(resources *v1.ResourceRequirements, checkResourcesLimits bool) error {

	if checkResourcesLimits {
		if resources == nil || resources.Limits == nil {
			return fmt.Errorf(
				"resources.Limits for init container cannot be empty if CleanupThreads is being set more than 1",
			)
		}
	} else if resources == nil {
		return nil
	}

	if resources.Limits != nil && resources.Requests != nil &&
		((resources.Limits.Cpu().Cmp(*resources.Requests.Cpu()) < 0) ||
			(resources.Limits.Memory().Cmp(*resources.Requests.Memory()) < 0)) {
		return fmt.Errorf(
			"resources.Limits cannot be less than resource.Requests. Resources %v",
			resources,
		)
	}

	return nil
}

func (c *AerospikeCluster) validateRackConfig(aslog logr.Logger) error {
	// Validate namespace names
	// TODO: Add more validation for namespace name
	for _, nsName := range c.Spec.RackConfig.Namespaces {
		if strings.Contains(nsName, " ") {
			return fmt.Errorf(
				"namespace name `%s` cannot have spaces, Namespaces %v", nsName,
				c.Spec.RackConfig.Namespaces,
			)
		}
	}

	rackMap := map[int]bool{}
	for _, rack := range c.Spec.RackConfig.Racks {
		// Check for duplicate
		if _, ok := rackMap[rack.ID]; ok {
			return fmt.Errorf(
				"duplicate rackID %d not allowed, racks %v", rack.ID,
				c.Spec.RackConfig.Racks,
			)
		}
		rackMap[rack.ID] = true

		// Check out of range rackID
		// Check for defaultRackID is in mutate (user can not use defaultRackID).
		// Allow DefaultRackID
		if rack.ID > MaxRackID {
			return fmt.Errorf(
				"invalid rackID. RackID range (%d, %d)", MinRackID, MaxRackID,
			)
		}

		if rack.InputAerospikeConfig != nil {
			_, inputRackNetwork := rack.InputAerospikeConfig.Value["network"]
			_, inputRackSecurity := rack.InputAerospikeConfig.Value["security"]
			if inputRackNetwork || inputRackSecurity {
				// Aerospike K8s Operator doesn't support different network configurations for different racks.
				// I.e.
				//    - the same heartbeat port (taken from current node) is used for all peers regardless to racks.
				//    - a single target port is used in headless service and LB.
				//    - we need to refactor how connection is created to AS to take into account rack's network config.
				// So, just reject rack specific network connections for now.
				return fmt.Errorf(
					"you can't specify network or security configuration for rack %d (network and security should be the same for all racks)",
					rack.ID,
				)
			}
		}
	}

	// Validate batch upgrade/restart param
	if c.Spec.RackConfig.RollingUpdateBatchSize != nil {

		// Just validate if RollingUpdateBatchSize is valid number or string.
		randomNumber := 100
		count, err := intstr.GetScaledValueFromIntOrPercent(c.Spec.RackConfig.RollingUpdateBatchSize, randomNumber, false)
		if err != nil {
			return err
		}
		// Only negative is not allowed. Any big number can be given.
		if count < 0 {
			return fmt.Errorf("can not use negative rackConfig.RollingUpdateBatchSize  %s", c.Spec.RackConfig.RollingUpdateBatchSize.String())
		}

		if len(c.Spec.RackConfig.Racks) < 2 {
			return fmt.Errorf("can not use rackConfig.RollingUpdateBatchSize when number of racks is less than two")
		}

		nsConfsNamespaces := c.getNsConfsForNamespaces()
		for ns, nsConf := range nsConfsNamespaces {
			if !isNameExist(c.Spec.RackConfig.Namespaces, ns) {
				return fmt.Errorf("can not use rackConfig.RollingUpdateBatchSize when there is any non-rack enabled namespace %s", ns)
			}

			if nsConf.noOfRacksForNamespaces <= 1 {
				return fmt.Errorf("can not use rackConfig.RollingUpdateBatchSize when namespace `%s` is configured in only one rack", ns)
			}
			if nsConf.replicationFactor <= 1 {
				return fmt.Errorf("can not use rackConfig.RollingUpdateBatchSize when namespace `%s` is configured with replication-factor 1", ns)
			}
		}
	}

	// TODO: should not use batch if racks are less than replication-factor
	return nil
}

type nsConf struct {
	noOfRacksForNamespaces int
	replicationFactor      int
}

func (c *AerospikeCluster) getNsConfsForNamespaces() map[string]nsConf {
	nsConfs := map[string]nsConf{}
	for _, rack := range c.Spec.RackConfig.Racks {
		nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})
		for _, nsInterface := range nsList {
			nsName := nsInterface.(map[string]interface{})["name"].(string)

			var noOfRacksForNamespaces int
			if _, ok := nsConfs[nsName]; !ok {
				noOfRacksForNamespaces = 1
			} else {
				noOfRacksForNamespaces = nsConfs[nsName].noOfRacksForNamespaces + 1
			}

			rf, _ := getNamespaceReplicationFactor(nsInterface.(map[string]interface{}))
			nsConfs[nsName] = nsConf{
				noOfRacksForNamespaces: noOfRacksForNamespaces,
				replicationFactor:      rf,
			}
		}
	}
	return nsConfs
}

//******************************************************************************
// Helper
//******************************************************************************

// TODO: This should be version specific and part of management lib.
// max cluster size for pre-5.0 cluster
const maxEnterpriseClusterSzLt5_0 = 128

// max cluster size for 5.0+ cluster
const maxEnterpriseClusterSzGt5_0 = 256

const versionForSzCheck = "5.0.0"

func validateClusterSize(_ logr.Logger, version string, sz int) error {
	val, err := asconfig.CompareVersions(version, versionForSzCheck)
	if err != nil {
		return fmt.Errorf(
			"failed to validate cluster size limit from version: %v", err,
		)
	}
	if val < 0 && sz > maxEnterpriseClusterSzLt5_0 {
		return fmt.Errorf(
			"cluster size cannot be more than %d", maxEnterpriseClusterSzLt5_0,
		)
	}
	if val > 0 && sz > maxEnterpriseClusterSzGt5_0 {
		return fmt.Errorf(
			"cluster size cannot be more than %d", maxEnterpriseClusterSzGt5_0,
		)
	}
	return nil
}

func (c *AerospikeCluster) validateAerospikeConfig(
	configSpec *AerospikeConfigSpec, storage *AerospikeStorageSpec, clSize int,
) error {
	config := configSpec.Value

	if config == nil {
		return fmt.Errorf("aerospikeConfig cannot be empty")
	}

	// service conf
	serviceConf, ok := config["service"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.service not a valid map %v", config["service"],
		)
	}
	if _, ok := serviceConf["cluster-name"]; !ok {
		return fmt.Errorf("aerospikeCluster name not found in config. Looks like object is not mutated by webhook")
	}

	// network conf
	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", config["network"],
		)
	}
	if err := c.validateNetworkConfig(networkConf); err != nil {
		return err
	}

	// namespace conf
	nsListInterface, ok := config["namespaces"]
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.namespace not a present. aerospikeConfig %v",
			config,
		)
	} else if nsListInterface == nil {
		return fmt.Errorf("aerospikeConfig.namespace cannot be nil")
	}
	if nsList, ok := nsListInterface.([]interface{}); !ok {
		return fmt.Errorf(
			"aerospikeConfig.namespace not valid namespace list %v",
			nsListInterface,
		)
	} else if err := validateNamespaceConfig(
		nsList, storage, clSize,
	); err != nil {
		return err
	}

	return nil
}

func (c *AerospikeCluster) validateNetworkConfig(networkConf map[string]interface{}) error {
	serviceConf, serviceExist := networkConf["service"]
	if !serviceExist {
		return fmt.Errorf("network.service section not found in config. Looks like object is not mutated by webhook")
	}

	tlsNames := make(map[string]struct{})
	// network.tls conf
	if _, ok := networkConf["tls"]; ok {
		tlsConfList := networkConf["tls"].([]interface{})
		for _, tlsConfInt := range tlsConfList {
			tlsConf := tlsConfInt.(map[string]interface{})
			if tlsName, ok := tlsConf["name"]; ok {
				tlsNames[tlsName.(string)] = struct{}{}
			}
			if _, ok := tlsConf["ca-path"]; ok {
				return fmt.Errorf(
					"ca-path not allowed, please use ca-file. tlsConf %v",
					tlsConf,
				)
			}
		}
	}
	if err := validateTlsClientNames(
		serviceConf.(map[string]interface{}), c.Spec.OperatorClientCertSpec,
	); err != nil {
		return err
	}
	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnection(
			networkConf, tlsNames, connectionType,
		); err != nil {
			return err
		}
	}

	return nil
}

// ValidateTLSAuthenticateClient validate the tls-authenticate-client field in the service configuration.
func ValidateTLSAuthenticateClient(serviceConf map[string]interface{}) (
	[]string, error,
) {
	config, ok := serviceConf["tls-authenticate-client"]
	if !ok {
		return []string{}, nil
	}
	switch value := config.(type) {
	case string:
		if value == "any" || value == "false" {
			return []string{}, nil
		}
		return nil, fmt.Errorf(
			"tls-authenticate-client contains invalid value: %s", value,
		)
	case bool:
		if value == false {
			return []string{}, nil
		}
		return nil, fmt.Errorf(
			"tls-authenticate-client contains invalid value: %t", value,
		)
	case []interface{}:
		dnsnames := make([]string, len(value))
		for i := 0; i < len(value); i++ {
			dnsname, ok := value[i].(string)
			if !ok {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid type value: %v",
					value,
				)
			}
			if !validate.IsDNSName(dnsname) {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid dns-name: %v",
					dnsname,
				)
			}
			dnsnames[i] = dnsname
		}
		return dnsnames, nil
	}
	return nil, fmt.Errorf(
		"tls-authenticate-client contains invalid type value: %v", config,
	)
}

func validateTlsClientNames(
	serviceConf map[string]interface{},
	clientCertSpec *AerospikeOperatorClientCertSpec,
) error {
	dnsnames, err := ValidateTLSAuthenticateClient(serviceConf)
	if err != nil {
		return err
	}
	if len(dnsnames) == 0 {
		return nil
	}

	localCertNames, err := readNamesFromLocalCertificate(clientCertSpec)
	if err != nil {
		return err
	}
	if !containsAnyName(dnsnames, localCertNames) && len(localCertNames) > 0 {
		return fmt.Errorf(
			"tls-authenticate-client (%+v) doesn't contain name from Operator's certificate (%+v), configure OperatorClientCertSpec.TLSClientName properly",
			dnsnames, localCertNames,
		)
	}
	return nil

}

func containsAnyName(
	clientNames []string, namesToFind map[string]struct{},
) bool {
	for _, clientName := range clientNames {
		if _, exists := namesToFind[clientName]; exists {
			return true
		}
	}
	return false
}

func readNamesFromLocalCertificate(clientCertSpec *AerospikeOperatorClientCertSpec) (
	map[string]struct{}, error,
) {
	result := make(map[string]struct{})
	if clientCertSpec == nil || clientCertSpec.CertPathInOperator == nil || clientCertSpec.CertPathInOperator.ClientCertPath == "" {
		return result, nil
	}
	r, err := ioutil.ReadFile(clientCertSpec.CertPathInOperator.ClientCertPath)
	if err != nil {
		return result, err
	}
	block, _ := pem.Decode(r)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return result, err
	}
	if len(cert.Subject.CommonName) > 0 {
		result[cert.Subject.CommonName] = struct{}{}
	}
	for _, dns := range cert.DNSNames {
		result[dns] = struct{}{}
	}
	return result, nil
}

func validateNetworkConnection(
	networkConf map[string]interface{}, tlsNames map[string]struct{},
	connectionType string,
) error {
	if connectionConfig, exists := networkConf[connectionType]; exists {
		connectionConfigMap := connectionConfig.(map[string]interface{})
		if tlsName, ok := connectionConfigMap[confKeyTLSName]; ok {
			if _, exists := tlsNames[tlsName.(string)]; !exists {
				return fmt.Errorf("tls-name '%s' is not configured", tlsName)
			}
		} else {
			for param := range connectionConfigMap {
				if strings.HasPrefix(param, "tls-") {
					return fmt.Errorf(
						"you can't specify %s for network.%s without specifying tls-name",
						param, connectionType,
					)
				}
			}
		}
	}
	return nil
}

func validateNamespaceConfig(
	nsConfInterfaceList []interface{}, storage *AerospikeStorageSpec,
	clSize int,
) error {
	if len(nsConfInterfaceList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace list cannot be empty")
	}

	// Get list of all devices used in namespace. match it with namespace device list
	blockStorageDeviceList, fileStorageList, err := storage.GetAerospikeStorageList()
	if err != nil {
		return err
	}

	for _, nsConfInterface := range nsConfInterfaceList {
		// Validate new namespace conf
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", nsConfInterface,
			)
		}

		if err := validateNamespaceReplicationFactor(
			nsConf, clSize,
		); err != nil {
			return err
		}

		if nsStorage, ok := nsConf["storage-engine"]; ok {
			if nsStorage == nil {
				return fmt.Errorf(
					"storage-engine cannot be nil for namespace %v", nsConf,
				)
			}

			if isInMemoryNamespace(nsConf) {
				// storage-engine memory
				continue
			}

			if !isDeviceOrPmemNamespace(nsConf) {
				return fmt.Errorf(
					"storage-engine not supported for namespace %v", nsConf,
				)
			}

			if devices, ok := nsStorage.(map[string]interface{})["devices"]; ok {
				if devices == nil {
					return fmt.Errorf(
						"namespace storage devices cannot be nil %v", nsStorage,
					)
				}

				if _, ok := devices.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace storage device format not valid %v",
						nsStorage,
					)
				}

				if len(devices.([]interface{})) == 0 {
					return fmt.Errorf(
						"no devices for namespace storage %v", nsStorage,
					)
				}

				for _, device := range devices.([]interface{}) {
					if _, ok := device.(string); !ok {
						return fmt.Errorf(
							"namespace storage device not valid string %v",
							device,
						)
					}

					device = strings.TrimSpace(device.(string))

					// device list Fields cannot be more that 2 in single line. Two in shadow device case. Validate.
					if len(strings.Fields(device.(string))) > 2 {
						return fmt.Errorf(
							"invalid device name %v. Max 2 device can be mentioned in single line (Shadow device config)",
							device,
						)
					}

					dList := strings.Fields(device.(string))
					for _, dev := range dList {
						// Namespace device should be present in BlockStorage config section
						if !ContainsString(blockStorageDeviceList, dev) {
							return fmt.Errorf(
								"namespace storage device related devicePath %v not found in Storage config %v",
								dev, storage,
							)
						}
					}
				}
			}

			if files, ok := nsStorage.(map[string]interface{})["files"]; ok {
				if files == nil {
					return fmt.Errorf(
						"namespace storage files cannot be nil %v", nsStorage,
					)
				}

				if _, ok := files.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace storage files format not valid %v",
						nsStorage,
					)
				}

				if len(files.([]interface{})) == 0 {
					return fmt.Errorf(
						"no files for namespace storage %v", nsStorage,
					)
				}

				for _, file := range files.([]interface{}) {
					if _, ok := file.(string); !ok {
						return fmt.Errorf(
							"namespace storage file not valid string %v", file,
						)
					}

					file = strings.TrimSpace(file.(string))

					// File list Fields cannot be more than 2 in single line. Two in shadow device case. Validate.
					if len(strings.Fields(file.(string))) > 2 {
						return fmt.Errorf(
							"invalid file name %v. Max 2 file can be mentioned in single line (Shadow file config)",
							file,
						)
					}

					fList := strings.Fields(file.(string))
					for _, f := range fList {
						dirPath := filepath.Dir(f)
						if !isFileStorageConfiguredForDir(
							fileStorageList, dirPath,
						) {
							return fmt.Errorf(
								"namespace storage file related mountPath %v not found in storage config %v",
								dirPath, storage,
							)
						}
					}
				}
			}
		} else {
			return fmt.Errorf("storage-engine config is required for namespace")
		}
	}

	err = validateStorageEngineDeviceList(nsConfInterfaceList)
	if err != nil {
		return err
	}

	// Validate index-type
	for _, nsConfInterface := range nsConfInterfaceList {
		nsConf, ok := nsConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", nsConfInterface,
			)
		}

		if isShmemIndexTypeNamespace(nsConf) {
			continue
		}

		if nsIndexStorage, ok := nsConf["index-type"]; ok {
			if mounts, ok := nsIndexStorage.(map[string]interface{})["mounts"]; ok {
				if mounts == nil {
					return fmt.Errorf(
						"namespace index-type mounts cannot be nil %v",
						nsIndexStorage,
					)
				}

				if _, ok := mounts.([]interface{}); !ok {
					return fmt.Errorf(
						"namespace index-type mounts format not valid %v",
						nsIndexStorage,
					)
				}

				if len(mounts.([]interface{})) == 0 {
					return fmt.Errorf(
						"no mounts for namespace index-type %v", nsIndexStorage,
					)
				}

				for _, mount := range mounts.([]interface{}) {
					if _, ok := mount.(string); !ok {
						return fmt.Errorf(
							"namespace index-type mount not valid string %v",
							mount,
						)
					}

					// Namespace index-type mount should be present in filesystem config section
					if !ContainsString(fileStorageList, mount.(string)) {
						return fmt.Errorf(
							"namespace index-type mount %v not found in Storage config %v",
							mount, storage,
						)
					}
				}
			}
		}
	}
	return nil
}

func validateNamespaceReplicationFactor(
	nsConf map[string]interface{}, clSize int,
) error {
	rf, err := getNamespaceReplicationFactor(nsConf)
	if err != nil {
		return err
	}
	if clSize < rf {
		return fmt.Errorf("namespace replication-factor %v cannot be more than cluster size %d", rf, clSize)
	}

	return nil
}

func getNamespaceReplicationFactor(nsConf map[string]interface{}) (int, error) {
	rfInterface, ok := nsConf["replication-factor"]
	if !ok {
		rfInterface = 2 // default replication-factor
	}

	switch rf := rfInterface.(type) {
	case int64:
		return int(rf), nil
	case int:
		return rf, nil
	case float64:
		return int(rf), nil
	default:
		return 0, fmt.Errorf(
			"namespace replication-factor %v not valid int, int64 or float64",
			rf,
		)
	}
}

func validateLoadBalancerUpdate(
	aslog logr.Logger, newLBSpec, oldLBSpec *LoadBalancerSpec,
) error {
	aslog.Info("Validate LoadBalancer update")

	if oldLBSpec != nil && !reflect.DeepEqual(oldLBSpec, newLBSpec) {
		return fmt.Errorf("cannot update existing LoadBalancer Service")
	}

	return nil
}

func validateSecurityConfigUpdate(
	newVersion, oldVersion string, newSpec, oldSpec *AerospikeConfigSpec,
) error {
	nv, err := asconfig.CompareVersions(newVersion, "5.7.0")
	if err != nil {
		return err
	}
	ov, err := asconfig.CompareVersions(oldVersion, "5.7.0")
	if err != nil {
		return err
	}
	if nv >= 0 || ov >= 0 {
		return validateSecurityContext(newVersion, oldVersion, newSpec, oldSpec)
	}
	return validateEnableSecurityConfig(newSpec, oldSpec)

}

func validateEnableSecurityConfig(newConfSpec, oldConfSpec *AerospikeConfigSpec) error {
	newConf := newConfSpec.Value
	oldConf := oldConfSpec.Value
	// Security can not be updated dynamically
	// TODO: How to enable dynamic security update, need to pass policy for individual nodes.
	// auth-enabled and auth-disabled node can co-exist
	oldSec, oldSecConfFound := oldConf["security"]
	newSec, newSecConfFound := newConf["security"]

	if oldSecConfFound && newSecConfFound {
		oldSecFlag, oldEnableSecurityFlagFound := oldSec.(map[string]interface{})["enable-security"]
		newSecFlag, newEnableSecurityFlagFound := newSec.(map[string]interface{})["enable-security"]
		if oldEnableSecurityFlagFound != newEnableSecurityFlagFound || !reflect.DeepEqual(
			oldSecFlag, newSecFlag,
		) {
			return fmt.Errorf("cannot update cluster security config enable-security was changed")
		}
	}
	return nil
}

func validateSecurityContext(
	newVersion, oldVersion string, newSpec, oldSpec *AerospikeConfigSpec,
) error {
	ovflag, err := IsSecurityEnabled(oldVersion, oldSpec)
	if err != nil {
		if !errors.Is(err, internalerrors.NotFoundError) {
			return fmt.Errorf(
				"validateEnableSecurityConfig got an error - oldVersion: %s: %w",
				oldVersion, err,
			)
		}
	}
	ivflag, err := IsSecurityEnabled(newVersion, newSpec)
	if err != nil {
		if !errors.Is(err, internalerrors.NotFoundError) {
			return fmt.Errorf(
				"validateEnableSecurityConfig got an error: %w", err,
			)
		}
	}
	if ivflag != ovflag {
		return fmt.Errorf("cannot update cluster security config enable-security was changed")
	}
	return nil
}

func validateAerospikeConfigUpdate(
	aslog logr.Logger, incomingVersion, outgoingVersion string,
	incomingSpec, outgoingSpec *AerospikeConfigSpec,
) error {
	aslog.Info("Validate AerospikeConfig update")
	if err := validateSecurityConfigUpdate(
		incomingVersion, outgoingVersion, incomingSpec, outgoingSpec,
	); err != nil {
		return err
	}

	newConf := incomingSpec.Value
	oldConf := outgoingSpec.Value

	// TLS can not be updated dynamically
	// TODO: How to enable dynamic tls update, need to pass policy for individual nodes.
	oldtls, ok11 := oldConf["network"].(map[string]interface{})["tls"]
	newtls, ok22 := newConf["network"].(map[string]interface{})["tls"]
	if ok11 != ok22 ||
		ok11 && ok22 && (!reflect.DeepEqual(oldtls, newtls)) {
		return fmt.Errorf("cannot update cluster network.tls config")
	}

	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnectionUpdate(
			newConf, oldConf, connectionType,
		); err != nil {
			return err
		}
	}

	if err := validateNsConfUpdate(incomingSpec, outgoingSpec); err != nil {
		return err
	}

	return nil
}

func validateNetworkConnectionUpdate(
	newConf, oldConf map[string]interface{}, connectionType string,
) error {
	oldConnectionConfig := oldConf["network"].(map[string]interface{})[connectionType].(map[string]interface{})
	newConnectionConfig := newConf["network"].(map[string]interface{})[connectionType].(map[string]interface{})
	for _, param := range immutableNetworkParams {
		if isValueUpdated(oldConnectionConfig, newConnectionConfig, param) {
			return fmt.Errorf(
				"cannot update %s for network.%s", param, connectionType,
			)
		}
	}
	return nil
}

func validateNsConfUpdate(newConfSpec, oldConfSpec *AerospikeConfigSpec) error {
	newConf := newConfSpec.Value
	oldConf := oldConfSpec.Value

	newNsConfList := newConf["namespaces"].([]interface{})

	for _, singleConfInterface := range newNsConfList {
		// Validate new namespaceconf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", singleConfInterface,
			)
		}

		// Validate new namespace conf from old namespace conf. Few fields cannot be updated
		oldNsConfList := oldConf["namespaces"].([]interface{})

		for _, oldSingleConfInterface := range oldNsConfList {

			oldSingleConf, ok := oldSingleConfInterface.(map[string]interface{})
			if !ok {
				return fmt.Errorf(
					"namespace conf not in valid format %v",
					oldSingleConfInterface,
				)
			}

			if singleConf["name"] == oldSingleConf["name"] {

				// replication-factor update not allowed
				if isValueUpdated(
					oldSingleConf, singleConf, "replication-factor",
				) {
					return fmt.Errorf(
						"replication-factor cannot be update. old nsconf %v, new nsconf %v",
						oldSingleConf, singleConf,
					)
				}
			}
		}
	}

	err := validateStorageEngineDeviceList(newNsConfList)
	if err != nil {
		return err
	}

	// Check for namespace name len
	return nil
}

func validateStorageEngineDeviceList(nsConfList []interface{}) error {
	deviceList := map[string]string{}

	// build a map device -> namespace
	for _, nsConfInterface := range nsConfList {
		nsConf := nsConfInterface.(map[string]interface{})
		namespace := nsConf["name"].(string)
		storage := nsConf["storage-engine"].(map[string]interface{})
		devices, ok := storage["devices"]
		if !ok {
			continue
		}

		for _, d := range devices.([]interface{}) {
			device := d.(string)
			previousNamespace, exists := deviceList[device]
			if exists {
				return fmt.Errorf(
					"device %s is already being referenced in multiple namespaces (%s, %s)",
					device, previousNamespace, namespace,
				)
			} else {
				deviceList[device] = namespace
			}
		}
	}

	return nil
}

func validateAerospikeConfigSchema(
	aslog logr.Logger, version string, configSpec AerospikeConfigSpec,
) error {
	config := configSpec.Value

	asConf, err := asconfig.NewMapAsConfig(aslog, version, config)
	if err != nil {
		return fmt.Errorf("failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(aslog, version)
	if !valid {
		if len(validationErr) <= 0 {
			return fmt.Errorf(
				"failed to validate config for the version %s: %v", version,
				err,
			)
		}

		errStrings := make([]string, 0)
		for _, e := range validationErr {
			errStrings = append(errStrings, fmt.Sprintf("\t%v\n", *e))
		}

		return fmt.Errorf(
			"generated config not valid for version %s: %v %v", version, err,
			errStrings,
		)
	}

	return nil
}

func validateRequiredFileStorageForMetadata(
	configSpec AerospikeConfigSpec, storage *AerospikeStorageSpec,
	validationPolicy *ValidationPolicySpec, version string,
) error {

	_, fileStorageList, err := storage.GetAerospikeStorageList()
	if err != nil {
		return err
	}

	// Validate work directory.
	if !validationPolicy.SkipWorkDirValidate {
		workDirPath := GetWorkDirectory(configSpec)

		if !filepath.IsAbs(workDirPath) {
			return fmt.Errorf(
				"aerospike work directory path %s must be absolute in storage config %v",
				workDirPath, storage,
			)
		}

		if !isFileStorageConfiguredForDir(fileStorageList, workDirPath) {
			return fmt.Errorf(
				"aerospike work directory path %s not mounted on a filesystem in storage config %v",
				workDirPath, storage,
			)
		}
	}

	if !validationPolicy.SkipXdrDlogFileValidate {
		val, err := asconfig.CompareVersions(version, "5.0.0")
		if err != nil {
			return fmt.Errorf("failed to check image version: %v", err)
		}
		if val < 0 {
			// Validate xdr-digestlog-path for pre-5.0.0 versions.
			if IsXdrEnabled(configSpec) {
				dglogFilePath, err := GetDigestLogFile(configSpec)
				if err != nil {
					return err
				}

				if !filepath.IsAbs(*dglogFilePath) {
					return fmt.Errorf(
						"xdr digestlog path %v must be absolute in storage config %v",
						dglogFilePath, storage,
					)
				}

				dglogDirPath := filepath.Dir(*dglogFilePath)

				if !isFileStorageConfiguredForDir(
					fileStorageList, dglogDirPath,
				) {
					return fmt.Errorf(
						"xdr digestlog path %v not mounted in Storage config %v",
						dglogFilePath, storage,
					)
				}
			}
		}
	}

	return nil
}

func validateRequiredFileStorageForFeatureConf(
	configSpec AerospikeConfigSpec, storage *AerospikeStorageSpec,
) error {
	// TODO Add validation for feature key file.
	featureKeyFilePaths := getFeatureKeyFilePaths(configSpec)
	tlsPaths := getTLSFilePaths(configSpec)

	var allPaths []string
	allPaths = append(allPaths, featureKeyFilePaths...)
	allPaths = append(allPaths, tlsPaths...)

	for _, path := range allPaths {
		if !storage.IsVolumePresentForAerospikePath(filepath.Dir(path)) {
			return fmt.Errorf(
				"feature-key-file paths or tls paths are not mounted - create an entry for '%v' in 'storage.volumes'",
				path,
			)
		}
	}
	return nil
}

//
// GetImageVersion extracts the Aerospike version from a container image.
// The implementation extracts the image tag and find the longest string from
// it that is a version string.
// Note: The behaviour should match the operator's python implementation in
// init container extracting version.
func GetImageVersion(imageStr string) (string, error) {
	_, _, version := ParseDockerImageTag(imageStr)

	if version == "" || strings.ToLower(version) == "latest" {
		return "", fmt.Errorf(
			"image version is mandatory for image: %v", imageStr,
		)
	}

	// Ignore special prefixes and suffixes.
	matches := versionRegex.FindAllString(version, -1)
	if matches == nil || len(matches) < 1 {
		return "", fmt.Errorf(
			"invalid image version format: %v", version,
		)
	}

	longest := 0
	for i := range matches {
		if len(matches[i]) >= len(matches[longest]) {
			longest = i
		}
	}

	return matches[longest], nil
}

// isInMemoryNamespace returns true if this namespace config uses memory for storage.
func isInMemoryNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "memory"
}

// isDeviceOrPmemNamespace returns true if this namespace config uses device for storage.
func isDeviceOrPmemNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && (typeStr == "device" || typeStr == "pmem")
}

// isShmemIndexTypeNamespace returns true if this namespace index type is shmem.
func isShmemIndexTypeNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["index-type"]
	if !ok {
		// missing index-type assumed to be shmem.
		return true
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && typeStr == "shmem"
}

// isEnterprise indicates if aerospike image is enterprise
func isEnterprise(image string) bool {
	return strings.Contains(strings.ToLower(image), "enterprise")
}

func getFeatureKeyFilePaths(configSpec AerospikeConfigSpec) []string {
	config := configSpec.Value

	// feature-key-file needs secret
	if svc, ok := config["service"]; ok {
		if path, ok := svc.(map[string]interface{})["feature-key-file"]; ok {
			return []string{path.(string)}

		} else if pathsInterface, ok := svc.(map[string]interface{})["feature-key-files"]; ok {
			if pathsList, ok := pathsInterface.([]interface{}); ok {
				var paths []string
				for _, pathInt := range pathsList {
					paths = append(paths, pathInt.(string))
				}
				return paths
			}
		}
	}

	// TODO: Assert - this should not happen.
	return nil
}

func getTLSFilePaths(configSpec AerospikeConfigSpec) []string {
	config := configSpec.Value

	var paths []string
	// feature-key-file needs secret
	if network, ok := config["network"]; ok {
		if tlsListInterface, ok := network.(map[string]interface{})["tls"]; ok {
			if tlsList, ok := tlsListInterface.([]interface{}); ok {
				for _, tlsInterface := range tlsList {
					if path, ok := tlsInterface.(map[string]interface{})["cert-file"]; ok {
						paths = append(paths, path.(string))
					}
					if path, ok := tlsInterface.(map[string]interface{})["key-file"]; ok {
						paths = append(paths, path.(string))
					}
					if path, ok := tlsInterface.(map[string]interface{})["ca-file"]; ok {
						paths = append(paths, path.(string))
					}
				}
			}
		}
	}
	return paths
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

func (c *AerospikeCluster) validatePodSpec() error {
	if c.Spec.PodSpec.HostNetwork && c.Spec.PodSpec.MultiPodPerHost {
		return fmt.Errorf("host networking cannot be enabled with multi pod per host")
	}

	var allContainers []v1.Container
	allContainers = append(allContainers, c.Spec.PodSpec.Sidecars...)
	allContainers = append(allContainers, c.Spec.PodSpec.InitContainers...)
	if err := ValidateAerospikeObjectMeta(&c.Spec.PodSpec.AerospikeObjectMeta); err != nil {
		return err
	}
	// Duplicate names are not allowed across sidecars and initContainers
	return validatePodSpecContainer(allContainers)
}

func validatePodSpecContainer(containers []v1.Container) error {

	containerNames := map[string]int{}

	for _, container := range containers {
		// Check for reserved container name
		if container.Name == AerospikeServerContainerName || container.Name == AerospikeInitContainerName {
			return fmt.Errorf(
				"cannot use reserved container name: %v", container.Name,
			)
		}

		// Check for duplicate names
		if _, ok := containerNames[container.Name]; ok {
			return fmt.Errorf(
				"connot have duplicate names of containers: %v", container.Name,
			)
		}
		containerNames[container.Name] = 1

		// TODO: do we need this image check for other containers
		//_, err := getImageVersion(container.Image)
		// if err != nil {
		// 	return err
		// }
	}

	return nil
}

func ValidateAerospikeObjectMeta(aerospikeObjectMeta *AerospikeObjectMeta) error {
	for label := range aerospikeObjectMeta.Labels {
		if label == AerospikeAppLabel || label == AerospikeRackIdLabel || label == AerospikeCustomResourceLabel {
			return fmt.Errorf(
				"label: %s is automatically defined by operator and shouldn't be specified by user",
				label,
			)
		}

	}
	return nil
}
