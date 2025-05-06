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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/asaskevich/govalidator"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var networkConnectionTypes = []string{"service", "heartbeat", "fabric"}

// +kubebuilder:object:generate=false
type AerospikeClusterCustomValidator struct {
}

//nolint:lll // for readability
// +kubebuilder:webhook:path=/validate-asdb-aerospike-com-v1-aerospikecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=create;update,versions=v1,name=vaerospikecluster.kb.io,admissionReviewVersions={v1}

var _ webhook.CustomValidator = &AerospikeClusterCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (acv *AerospikeClusterCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	aerospikeCluster, ok := obj.(*asdbv1.AerospikeCluster)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeCluster, got %T", obj)
	}

	aslog := logf.Log.WithName(asdbv1.ClusterNamespacedName(aerospikeCluster))

	aslog.Info("Validate create")

	return validate(aslog, aerospikeCluster)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (acv *AerospikeClusterCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object,
) (admission.Warnings, error) {
	aerospikeCluster, ok := obj.(*asdbv1.AerospikeCluster)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeCluster, got %T", obj)
	}

	aslog := logf.Log.WithName(asdbv1.ClusterNamespacedName(aerospikeCluster))

	aslog.Info("Validate delete")

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (acv *AerospikeClusterCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object,
) (admission.Warnings, error) {
	aerospikeCluster, ok := newObj.(*asdbv1.AerospikeCluster)
	if !ok {
		return nil, fmt.Errorf("expected AerospikeCluster, got %T", newObj)
	}

	aslog := logf.Log.WithName(asdbv1.ClusterNamespacedName(aerospikeCluster))

	aslog.Info("Validate update")

	oldObject := oldObj.(*asdbv1.AerospikeCluster)

	var warnings admission.Warnings

	warns, vErr := validate(aslog, aerospikeCluster)
	warnings = append(warnings, warns...)

	if vErr != nil {
		return warnings, vErr
	}

	if err := validateEnableDynamicConfigUpdate(aerospikeCluster); err != nil {
		return warnings, err
	}

	outgoingVersion, err := asdbv1.GetImageVersion(oldObject.Spec.Image)
	if err != nil {
		return warnings, err
	}

	incomingVersion, err := asdbv1.GetImageVersion(aerospikeCluster.Spec.Image)
	if err != nil {
		return warnings, err
	}

	if err := asconfig.IsValidUpgrade(
		outgoingVersion, incomingVersion,
	); err != nil {
		return warnings, fmt.Errorf("failed to start upgrade: %v", err)
	}

	// Volume storage update is not allowed but cascadeDelete policy is allowed
	if err := validateStorageSpecChange(&oldObject.Spec.Storage, &aerospikeCluster.Spec.Storage); err != nil {
		return warnings, fmt.Errorf("storage config cannot be updated: %v", err)
	}

	// MultiPodPerHost cannot be updated
	if asdbv1.GetBool(aerospikeCluster.Spec.PodSpec.MultiPodPerHost) !=
		asdbv1.GetBool(oldObject.Spec.PodSpec.MultiPodPerHost) {
		return warnings, fmt.Errorf("cannot update MultiPodPerHost setting")
	}

	if err := validateNetworkPolicyUpdate(
		&oldObject.Spec.AerospikeNetworkPolicy, &aerospikeCluster.Spec.AerospikeNetworkPolicy,
	); err != nil {
		return warnings, err
	}

	if err := validateOperationUpdate(
		&oldObject.Spec, &aerospikeCluster.Spec, &aerospikeCluster.Status,
	); err != nil {
		return warnings, err
	}

	// Validate AerospikeConfig update
	if err := validateAerospikeConfigUpdate(
		aslog, aerospikeCluster.Spec.AerospikeConfig, oldObject.Spec.AerospikeConfig,
		aerospikeCluster.Status.AerospikeConfig,
	); err != nil {
		return warnings, err
	}

	// Validate RackConfig update
	return warnings, validateRackUpdate(aslog, oldObject, aerospikeCluster)
}

func validate(aslog logr.Logger, cluster *asdbv1.AerospikeCluster) (admission.Warnings, error) {
	aslog.V(1).Info("Validate AerospikeCluster spec", "obj.Spec", cluster.Spec)

	var warnings admission.Warnings

	// Validate obj name
	if cluster.Name == "" {
		return warnings, fmt.Errorf("aerospikeCluster name cannot be empty")
	}

	if strings.Contains(cluster.Name, " ") {
		// Few parsing logic depend on this
		return warnings, fmt.Errorf("aerospikeCluster name cannot have spaces")
	}

	// Validate obj namespace
	if cluster.Namespace == "" {
		return warnings, fmt.Errorf("aerospikeCluster namespace name cannot be empty")
	}

	if strings.Contains(cluster.Namespace, " ") {
		// Few parsing logic depend on this
		return warnings, fmt.Errorf("aerospikeCluster name cannot have spaces")
	}

	// Validate image type. Only enterprise image allowed for now
	if !isEnterprise(cluster.Spec.Image) {
		return warnings, fmt.Errorf("CommunityEdition Cluster not supported")
	}

	// Validate size
	if cluster.Spec.Size == 0 {
		return warnings, fmt.Errorf("invalid cluster size 0")
	}

	// Validate MaxUnavailable for PodDisruptionBudget
	warns, err := validateMaxUnavailable(cluster)
	warnings = append(warnings, warns...)

	if err != nil {
		return warnings, err
	}

	// Validate Image version
	version, err := asdbv1.GetImageVersion(cluster.Spec.Image)
	if err != nil {
		return warnings, err
	}

	val, err := lib.CompareVersions(version, baseVersion)
	if err != nil {
		return warnings, fmt.Errorf("failed to check image version: %v", err)
	}

	if val < 0 {
		return warnings, fmt.Errorf(
			"image version %s not supported. Base version %s", version,
			baseVersion,
		)
	}

	initVersion, err := asdbv1.GetImageVersion(asdbv1.GetAerospikeInitContainerImage(cluster))
	if err != nil {
		return warnings, err
	}

	if asdbv1.GetBool(cluster.Spec.EnableDynamicConfigUpdate) {
		val, err = lib.CompareVersions(initVersion, minInitVersionForDynamicConf)
		if err != nil {
			return warnings, fmt.Errorf("failed to check init image version: %v", err)
		}

		if val < 0 {
			return warnings, fmt.Errorf("cannot set enableDynamicConfigUpdate flag, init container version is less"+
				" than %s. Please visit https://aerospike.com/docs/cloud/kubernetes/operator/Cluster-configuration-settings#spec"+
				" for more details about enableDynamicConfigUpdate flag",
				minInitVersionForDynamicConf)
		}
	}

	err = validateClusterSize(aslog, int(cluster.Spec.Size))
	if err != nil {
		return warnings, err
	}

	if err := validateOperation(cluster); err != nil {
		return warnings, err
	}

	// Storage should be validated before validating aerospikeConfig and fileStorage
	if err := validateStorage(&cluster.Spec.Storage, &cluster.Spec.PodSpec); err != nil {
		return warnings, err
	}

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		// Storage should be validated before validating aerospikeConfig and fileStorage
		if err := validateStorage(&rack.Storage, &cluster.Spec.PodSpec); err != nil {
			return warnings, err
		}

		// Validate if passed aerospikeConfig
		if err := validateAerospikeConfigSchema(
			aslog, version, rack.AerospikeConfig,
		); err != nil {
			return warnings, fmt.Errorf("aerospikeConfig not valid: %v", err)
		}

		// Validate common aerospike config
		if err := validateAerospikeConfig(
			&rack.AerospikeConfig, &rack.Storage, int(cluster.Spec.Size),
			cluster.Spec.OperatorClientCertSpec,
		); err != nil {
			return warnings, err
		}

		if err := validateRequiredFileStorageForMetadata(
			rack.AerospikeConfig, &rack.Storage, cluster.Spec.ValidationPolicy,
		); err != nil {
			return warnings, err
		}

		if err := validateRequiredFileStorageForAerospikeConfig(
			rack.AerospikeConfig, &rack.Storage,
		); err != nil {
			return warnings, err
		}
	}

	// Validate resource and limit
	if err := validatePodSpecResourceAndLimits(aslog, cluster); err != nil {
		return warnings, err
	}

	// Validate access control
	if err := validateAccessControl(aslog, cluster); err != nil {
		return warnings, err
	}

	// Validate rackConfig
	if err := validateRackConfig(aslog, cluster); err != nil {
		return warnings, err
	}

	if err := validateClientCertSpec(cluster); err != nil {
		return warnings, err
	}

	if err := validateNetworkPolicy(cluster); err != nil {
		return warnings, err
	}

	// Validate Sidecars
	if err := validatePodSpec(cluster); err != nil {
		return warnings, err
	}

	return warnings, validateSCNamespaces(cluster)
}

func validateOperation(cluster *asdbv1.AerospikeCluster) error {
	// Nothing to validate if no operation
	if len(cluster.Spec.Operations) == 0 {
		return nil
	}

	if cluster.Status.AerospikeConfig == nil {
		return fmt.Errorf("operation cannot be added during aerospike cluster creation")
	}

	return nil
}

func validateSCNamespaces(cluster *asdbv1.AerospikeCluster) error {
	scNamespaceSet := sets.NewString()

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		tmpSCNamespaceSet := sets.NewString()

		nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})
		for _, nsConfInterface := range nsList {
			nsConf := nsConfInterface.(map[string]interface{})

			isEnabled := asdbv1.IsNSSCEnabled(nsConf)
			if isEnabled {
				tmpSCNamespaceSet.Insert(nsConf["name"].(string))

				if isInMemoryNamespace(nsConf) {
					return fmt.Errorf("in-memory SC namespace is not supported, namespace %v", nsConf["name"])
				}
			}
		}

		if idx == 0 {
			scNamespaceSet = tmpSCNamespaceSet
			continue
		}

		if !scNamespaceSet.Equal(tmpSCNamespaceSet) {
			return fmt.Errorf("SC namespaces list is different for different racks. All racks should have same SC namespaces")
		}
	}

	return nil
}

func validateOperatorClientCert(clientCert *asdbv1.AerospikeOperatorClientCertSpec) error {
	if (clientCert.SecretCertSource == nil) == (clientCert.CertPathInOperator == nil) {
		return fmt.Errorf(
			"either `secretCertSource` or `certPathInOperator` must be set in `operatorClientCertSpec` but not"+
				" both: %+v",
			clientCert,
		)
	}

	if clientCert.SecretCertSource != nil {
		if (clientCert.SecretCertSource.ClientCertFilename == "") != (clientCert.SecretCertSource.ClientKeyFilename == "") {
			return fmt.Errorf(
				"both `clientCertFilename` and `clientKeyFilename` should be either set or not set in"+
					" `secretCertSource`: %+v",
				clientCert.SecretCertSource,
			)
		}

		if (clientCert.SecretCertSource.CaCertsFilename != "") && (clientCert.SecretCertSource.CaCertsSource != nil) {
			return fmt.Errorf(
				"both `caCertsFilename` or `caCertsSource` cannot be set in `secretCertSource`: %+v",
				clientCert.SecretCertSource,
			)
		}
	}

	if clientCert.CertPathInOperator != nil &&
		(clientCert.CertPathInOperator.ClientCertPath == "") != (clientCert.CertPathInOperator.ClientKeyPath == "") {
		return fmt.Errorf(
			"both `clientCertPath` and `clientKeyPath` should be either set or not set in `certPathInOperator"+
				"`: %+v",
			clientCert.CertPathInOperator,
		)
	}

	if !asdbv1.IsClientCertConfigured(clientCert) {
		return fmt.Errorf("operator client cert is not configured")
	}

	return nil
}

func validateClientCertSpec(cluster *asdbv1.AerospikeCluster) error {
	networkConf, networkConfExist := cluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork]
	if !networkConfExist {
		return nil
	}

	serviceConf, serviceConfExists := networkConf.(map[string]interface{})[asdbv1.ConfKeyNetworkService]
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
		if cluster.Spec.OperatorClientCertSpec == nil {
			return fmt.Errorf("operator client cert is not specified")
		}

		if err := validateOperatorClientCert(cluster.Spec.OperatorClientCertSpec); err != nil {
			return err
		}

		return nil
	default:
		if cluster.Spec.OperatorClientCertSpec == nil {
			return fmt.Errorf("operator client cert is not specified")
		}

		if cluster.Spec.OperatorClientCertSpec.TLSClientName == "" {
			return fmt.Errorf("operator TLSClientName is not specified")
		}

		if err := validateOperatorClientCert(cluster.Spec.OperatorClientCertSpec); err != nil {
			return err
		}
	}

	return nil
}

func validateRackUpdate(
	aslog logr.Logger, oldObj, newObj *asdbv1.AerospikeCluster,
) error {
	if reflect.DeepEqual(newObj.Spec.RackConfig, oldObj.Spec.RackConfig) {
		return nil
	}

	// Old racks cannot be updated
	// Also need to exclude a default rack with default rack ID. No need to check here,
	// user should not provide or update default rackID
	// Also when user add new rackIDs old default will be removed by reconciler.
	for rackIdx := range oldObj.Spec.RackConfig.Racks {
		oldRack := oldObj.Spec.RackConfig.Racks[rackIdx]

		for specIdx := range newObj.Spec.RackConfig.Racks {
			newRack := newObj.Spec.RackConfig.Racks[specIdx]
			if oldRack.ID == newRack.ID {
				if oldRack.NodeName != newRack.NodeName ||
					oldRack.RackLabel != newRack.RackLabel ||
					oldRack.Region != newRack.Region ||
					oldRack.Zone != newRack.Zone {
					return fmt.Errorf(
						"old RackConfig (NodeName, RackLabel, Region, Zone) cannot be updated. Old rack %v, new rack %v",
						oldRack, newRack,
					)
				}

				if len(oldRack.AerospikeConfig.Value) != 0 || len(newRack.AerospikeConfig.Value) != 0 {
					var rackStatusConfig *asdbv1.AerospikeConfigSpec

					for statusIdx := range newObj.Status.RackConfig.Racks {
						statusRack := newObj.Status.RackConfig.Racks[statusIdx]
						if statusRack.ID == newRack.ID {
							rackStatusConfig = &statusRack.AerospikeConfig
							break
						}
					}

					// Validate aerospikeConfig update
					if err := validateAerospikeConfigUpdate(
						aslog, &newRack.AerospikeConfig, &oldRack.AerospikeConfig,
						rackStatusConfig,
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
					if err := validateStorageSpecChange(&oldStorage, &newStorage); err != nil {
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
func validateAccessControl(_ logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	_, err := asdbv1.IsAerospikeAccessControlValid(&cluster.Spec)
	return err
}

func validatePodSpecResourceAndLimits(_ logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	checkResourcesLimits := false

	if err := validateResourceAndLimits(
		cluster.Spec.PodSpec.AerospikeContainerSpec.Resources, false,
	); err != nil {
		return err
	}

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		if rack.Storage.CleanupThreads != asdbv1.AerospikeVolumeSingleCleanupThread {
			checkResourcesLimits = true
			break
		}
	}

	if checkResourcesLimits && cluster.Spec.PodSpec.AerospikeInitContainerSpec == nil {
		return fmt.Errorf(
			"init container spec should have resources.Limits set if CleanupThreads is more than 1",
		)
	}

	if cluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		return validateResourceAndLimits(cluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources, checkResourcesLimits)
	}

	return nil
}

func validateResourceAndLimits(
	resources *v1.ResourceRequirements, checkResourcesLimits bool,
) error {
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

func validateRackConfig(_ logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	// Validate namespace names
	// TODO: Add more validation for namespace name
	for _, nsName := range cluster.Spec.RackConfig.Namespaces {
		if strings.Contains(nsName, " ") {
			return fmt.Errorf(
				"namespace name `%s` cannot have spaces, Namespaces %v", nsName,
				cluster.Spec.RackConfig.Namespaces,
			)
		}
	}

	rackMap := map[int]bool{}
	migrateFillDelaySet := sets.Set[int]{}

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		// Check for duplicate
		if _, ok := rackMap[rack.ID]; ok {
			return fmt.Errorf(
				"duplicate rackID %d not allowed, racks %v", rack.ID,
				cluster.Spec.RackConfig.Racks,
			)
		}

		rackMap[rack.ID] = true

		// Check out of range rackID
		// Check for defaultRackID is in mutate (user can not use defaultRackID).
		// Allow DefaultRackID
		if rack.ID > asdbv1.MaxRackID {
			return fmt.Errorf(
				"invalid rackID. RackID range (%d, %d)", asdbv1.MinRackID, asdbv1.MaxRackID,
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
					"you can't specify network or security configuration for rack %d ("+
						"network and security should be the same for all racks)",
					rack.ID,
				)
			}
		}

		migrateFillDelay, err := asdbv1.GetMigrateFillDelay(&rack.AerospikeConfig)
		if err != nil {
			return err
		}

		migrateFillDelaySet.Insert(migrateFillDelay)
	}

	// If len of migrateFillDelaySet is more than 1, it means that different migrate-fill-delay is set across racks
	if migrateFillDelaySet.Len() > 1 {
		return fmt.Errorf("migrate-fill-delay value should be same across all racks")
	}

	// Validate batch upgrade/restart param
	if err := validateBatchSize(cluster.Spec.RackConfig.RollingUpdateBatchSize, true, cluster); err != nil {
		return err
	}

	// Validate batch scaleDown param
	if err := validateBatchSize(cluster.Spec.RackConfig.ScaleDownBatchSize, false, cluster); err != nil {
		return err
	}

	// Validate MaxIgnorablePods param
	if cluster.Spec.RackConfig.MaxIgnorablePods != nil {
		if err := validateIntOrStringField(cluster.Spec.RackConfig.MaxIgnorablePods,
			"spec.rackConfig.maxIgnorablePods"); err != nil {
			return err
		}
	}
	// TODO: should not use batch if racks are less than replication-factor
	return nil
}

type nsConf struct {
	noOfRacksForNamespaces int
	replicationFactor      int
	scEnabled              bool
}

func getNsConfForNamespaces(rackConfig asdbv1.RackConfig) map[string]nsConf {
	nsConfs := map[string]nsConf{}

	for idx := range rackConfig.Racks {
		rack := &rackConfig.Racks[idx]
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

			ns := nsInterface.(map[string]interface{})
			scEnabled := asdbv1.IsNSSCEnabled(ns)
			nsConfs[nsName] = nsConf{
				noOfRacksForNamespaces: noOfRacksForNamespaces,
				replicationFactor:      rf,
				scEnabled:              scEnabled,
			}
		}
	}

	return nsConfs
}

// ******************************************************************************
// Helper
// ******************************************************************************

// TODO: This should be version specific and part of management lib.
// max cluster size for 5.0+ cluster
const maxEnterpriseClusterSize = 256

func validateClusterSize(_ logr.Logger, sz int) error {
	if sz > maxEnterpriseClusterSize {
		return fmt.Errorf(
			"cluster size cannot be more than %d", maxEnterpriseClusterSize,
		)
	}

	return nil
}

func validateAerospikeConfig(
	configSpec *asdbv1.AerospikeConfigSpec, storage *asdbv1.AerospikeStorageSpec, clSize int,
	clientCert *asdbv1.AerospikeOperatorClientCertSpec,
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

	if val, exists := serviceConf["advertise-ipv6"]; exists && val.(bool) {
		return fmt.Errorf("advertise-ipv6 is not supported")
	}

	if _, ok = serviceConf["cluster-name"]; !ok {
		return fmt.Errorf("aerospikeCluster name not found in config. Looks like object is not mutated by webhook")
	}

	// network conf
	networkConf, ok := config["network"].(map[string]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.network not a valid map %v", config["network"],
		)
	}

	if err := validateNetworkConfig(networkConf, clientCert); err != nil {
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

	if nsList, ok1 := nsListInterface.([]interface{}); !ok1 {
		return fmt.Errorf(
			"aerospikeConfig.namespace not valid namespace list %v",
			nsListInterface,
		)
	} else if err := validateNamespaceConfig(
		nsList, storage, clSize,
	); err != nil {
		return err
	}

	// logging conf
	// config["logging"] is added in mutating webhook
	loggingConfList, ok := config["logging"].([]interface{})
	if !ok {
		return fmt.Errorf(
			"aerospikeConfig.logging not a valid list %v", config["logging"],
		)
	}

	return validateLoggingConf(loggingConfList)
}

func validateLoggingConf(loggingConfList []interface{}) error {
	syslogParams := []string{"facility", "path", "tag"}

	for idx := range loggingConfList {
		logConf, ok := loggingConfList[idx].(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"aerospikeConfig.logging not a list of valid map %v", logConf,
			)
		}

		if logConf["name"] != "syslog" {
			for _, param := range syslogParams {
				if _, ok := logConf[param]; ok {
					return fmt.Errorf("can use %s only with `syslog` in aerospikeConfig.logging %v", param, logConf)
				}
			}
		}
	}

	return nil
}

func validateNetworkConfig(networkConf map[string]interface{}, clientCert *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	serviceConf, serviceExist := networkConf["service"]
	if !serviceExist {
		return fmt.Errorf("network.service section not found in config")
	}

	tlsNames := sets.Set[string]{}
	// network.tls conf
	if _, ok := networkConf["tls"]; ok {
		tlsConfList := networkConf["tls"].([]interface{})
		for _, tlsConfInt := range tlsConfList {
			tlsConf := tlsConfInt.(map[string]interface{})
			if tlsName, ok := tlsConf["name"]; ok {
				tlsNames.Insert(tlsName.(string))
			}

			if _, ok := tlsConf["ca-path"]; ok {
				if _, ok1 := tlsConf["ca-file"]; ok1 {
					return fmt.Errorf(
						"both `ca-path` and `ca-file` cannot be set in `tls`. tlsConf %v",
						tlsConf,
					)
				}
			}
		}
	}

	if err := validateTLSClientNames(
		serviceConf.(map[string]interface{}), clientCert,
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
		if !value {
			return []string{}, nil
		}

		return nil, fmt.Errorf(
			"tls-authenticate-client contains invalid value: %t", value,
		)
	case []interface{}:
		dnsNames := make([]string, len(value))

		for i := 0; i < len(value); i++ {
			dnsName, ok := value[i].(string)
			if !ok {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid type value: %v",
					value,
				)
			}

			if !govalidator.IsDNSName(dnsName) {
				return nil, fmt.Errorf(
					"tls-authenticate-client contains invalid dns-name: %v",
					dnsName,
				)
			}

			dnsNames[i] = dnsName
		}

		return dnsNames, nil
	}

	return nil, fmt.Errorf(
		"tls-authenticate-client contains invalid type value: %v", config,
	)
}

func validateTLSClientNames(
	serviceConf map[string]interface{},
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	dnsNames, err := ValidateTLSAuthenticateClient(serviceConf)
	if err != nil {
		return err
	}

	if len(dnsNames) == 0 {
		return nil
	}

	localCertNames, err := readNamesFromLocalCertificate(clientCertSpec)
	if err != nil {
		return err
	}

	if !containsAnyName(dnsNames, localCertNames) && len(localCertNames) > 0 {
		return fmt.Errorf(
			"tls-authenticate-client (%+v) doesn't contain name from Operator's certificate (%+v), "+
				"configure OperatorClientCertSpec.TLSClientName properly",
			dnsNames, localCertNames,
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

func readNamesFromLocalCertificate(clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec) (
	map[string]struct{}, error,
) {
	result := make(map[string]struct{})
	if clientCertSpec == nil || clientCertSpec.CertPathInOperator == nil ||
		clientCertSpec.CertPathInOperator.ClientCertPath == "" {
		return result, nil
	}

	r, err := os.ReadFile(clientCertSpec.CertPathInOperator.ClientCertPath)
	if err != nil {
		return result, err
	}

	block, _ := pem.Decode(r)

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return result, err
	}

	if cert.Subject.CommonName != "" {
		result[cert.Subject.CommonName] = struct{}{}
	}

	for _, dns := range cert.DNSNames {
		result[dns] = struct{}{}
	}

	return result, nil
}

func validateNetworkConnection(
	networkConf map[string]interface{}, tlsNames sets.Set[string],
	connectionType string,
) error {
	if connectionConfig, exists := networkConf[connectionType]; exists {
		connectionConfigMap := connectionConfig.(map[string]interface{})
		if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
			if _, tlsPortExist := connectionConfigMap["tls-port"]; !tlsPortExist {
				return fmt.Errorf(
					"you can't specify tls-name for network.%s without specifying tls-port",
					connectionType,
				)
			}

			if !tlsNames.Has(tlsName.(string)) {
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

//nolint:gocyclo // for readability
func validateNamespaceConfig(
	nsConfInterfaceList []interface{}, storage *asdbv1.AerospikeStorageSpec,
	clSize int,
) error {
	if len(nsConfInterfaceList) == 0 {
		return fmt.Errorf("aerospikeConfig.namespace list cannot be empty")
	}

	// Get list of all devices used in namespace. match it with a namespace device list
	blockStorageDeviceList, fileStorageList, err := getAerospikeStorageList(storage, true)
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

		if nErr := validateNamespaceReplicationFactor(
			nsConf, clSize,
		); nErr != nil {
			return nErr
		}

		if mErr := validateMRTFields(nsConf); mErr != nil {
			return mErr
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

					// device list Fields cannot be more than 2 in single line. Two in shadow device case. Validate.
					if len(strings.Fields(device.(string))) > 2 {
						return fmt.Errorf(
							"invalid device name %v. Max 2 device can be mentioned in single line (Shadow device config)",
							device,
						)
					}

					dList := strings.Fields(device.(string))
					for _, dev := range dList {
						// Namespace device should be present in BlockStorage config section
						if !asdbv1.ContainsString(blockStorageDeviceList, dev) {
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

	_, _, err = validateStorageEngineDeviceList(nsConfInterfaceList)
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

		if isShMemIndexTypeNamespace(nsConf) {
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
					if !asdbv1.ContainsString(fileStorageList, mount.(string)) {
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

func validateMRTFields(nsConf map[string]interface{}) error {
	mrtField := isMRTFieldSet(nsConf)
	scEnabled := asdbv1.IsNSSCEnabled(nsConf)

	if !scEnabled && mrtField {
		return fmt.Errorf("MRT fields are not allowed in non-SC namespace %v", nsConf)
	}

	return nil
}

func isMRTFieldSet(nsConf map[string]interface{}) bool {
	mrtFields := []string{"mrt-duration", "disable-mrt-writes"}

	for _, field := range mrtFields {
		if _, exists := nsConf[field]; exists {
			return true
		}
	}

	return false
}

func validateNamespaceReplicationFactor(
	nsConf map[string]interface{}, clSize int,
) error {
	rf, err := getNamespaceReplicationFactor(nsConf)
	if err != nil {
		return err
	}

	scEnabled := asdbv1.IsNSSCEnabled(nsConf)

	// clSize < rf is allowed in AP mode but not in sc mode
	if scEnabled && (clSize < rf) {
		return fmt.Errorf(
			"strong-consistency namespace replication-factor %v cannot be more than cluster size %d", rf, clSize,
		)
	}

	return nil
}

func getNamespaceReplicationFactor(nsConf map[string]interface{}) (int, error) {
	rfInterface, ok := nsConf["replication-factor"]
	if !ok {
		rfInterface = 2 // default replication-factor
	}

	rf, err := asdbv1.GetIntType(rfInterface)
	if err != nil {
		return 0, fmt.Errorf("namespace replication-factor %v", err)
	}

	return rf, nil
}

func validateSecurityConfigUpdate(newSpec, oldSpec, currentStatus *asdbv1.AerospikeConfigSpec) error {
	if currentStatus != nil {
		currentSecurityEnabled, err := asdbv1.IsSecurityEnabled(currentStatus)
		if err != nil {
			return err
		}

		desiredSecurityEnabled, err := asdbv1.IsSecurityEnabled(newSpec)
		if err != nil {
			return err
		}

		if currentSecurityEnabled && !desiredSecurityEnabled {
			return fmt.Errorf("cannot disable cluster security in running cluster")
		}
	}

	return validateSecurityContext(newSpec, oldSpec)
}

func validateSecurityContext(newSpec, oldSpec *asdbv1.AerospikeConfigSpec) error {
	ovflag, err := asdbv1.IsSecurityEnabled(oldSpec)
	if err != nil {
		return fmt.Errorf(
			"failed to validate Security context of old aerospike conf: %w", err,
		)
	}

	ivflag, err := asdbv1.IsSecurityEnabled(newSpec)
	if err != nil {
		return fmt.Errorf(
			"failed to validate Security context of new aerospike conf: %w", err,
		)
	}

	if !ivflag && ovflag {
		return fmt.Errorf("cannot disable cluster security in running cluster")
	}

	return nil
}

func validateAerospikeConfigUpdate(
	aslog logr.Logger,
	incomingSpec, outgoingSpec, currentStatus *asdbv1.AerospikeConfigSpec,
) error {
	aslog.Info("Validate AerospikeConfig update")

	if err := validateSecurityConfigUpdate(incomingSpec, outgoingSpec, currentStatus); err != nil {
		return err
	}

	newConf := incomingSpec.Value
	oldConf := outgoingSpec.Value

	if err := validateTLSUpdate(oldConf, newConf); err != nil {
		return err
	}

	for _, connectionType := range networkConnectionTypes {
		if err := validateNetworkConnectionUpdate(
			newConf, oldConf, connectionType,
		); err != nil {
			return err
		}
	}

	return validateNsConfUpdate(incomingSpec, outgoingSpec, currentStatus)
}

func validateTLSUpdate(oldConf, newConf map[string]interface{}) error {
	oldTLS, oldExists := oldConf["network"].(map[string]interface{})["tls"]
	newTLS, newExists := newConf["network"].(map[string]interface{})["tls"]

	if oldExists && newExists && (!reflect.DeepEqual(oldTLS, newTLS)) {
		oldTLSCAFileMap := make(map[string]string)
		oldTLSCAPathMap := make(map[string]string)
		newUsedTLS := sets.NewString()
		oldUsedTLS := sets.NewString()

		// fetching names of TLS configurations used in connections
		for _, connectionType := range networkConnectionTypes {
			if connectionConfig, exists := newConf["network"].(map[string]interface{})[connectionType]; exists {
				connectionConfigMap := connectionConfig.(map[string]interface{})
				if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
					newUsedTLS.Insert(tlsName.(string))
				}
			}
		}

		// fetching names of TLS configurations used in old connections configurations
		for _, connectionType := range networkConnectionTypes {
			if connectionConfig, exists := oldConf["network"].(map[string]interface{})[connectionType]; exists {
				connectionConfigMap := connectionConfig.(map[string]interface{})
				if tlsName, ok := connectionConfigMap[asdbv1.ConfKeyTLSName]; ok {
					oldUsedTLS.Insert(tlsName.(string))
				}
			}
		}

		for _, tls := range oldTLS.([]interface{}) {
			tlsMap := tls.(map[string]interface{})
			if !oldUsedTLS.Has(tlsMap["name"].(string)) {
				continue
			}

			oldCAFile, oldCAFileOK := tlsMap["ca-file"]
			if oldCAFileOK {
				oldTLSCAFileMap[tlsMap["name"].(string)] = oldCAFile.(string)
			}

			oldCAPath, oldCAPathOK := tlsMap["ca-path"]
			if oldCAPathOK {
				oldTLSCAPathMap[tlsMap["name"].(string)] = oldCAPath.(string)
			}
		}

		for _, tls := range newTLS.([]interface{}) {
			tlsMap := tls.(map[string]interface{})
			if !newUsedTLS.Has(tlsMap["name"].(string)) {
				continue
			}

			_, newCAPathOK := tlsMap["ca-path"]
			newCAFile, newCAFileOK := tlsMap["ca-file"]

			oldCAFile, oldCAFileOK := oldTLSCAFileMap[tlsMap["name"].(string)]
			_, oldCAPathOK := oldTLSCAPathMap[tlsMap["name"].(string)]

			if (oldCAFileOK || oldCAPathOK) && !(newCAPathOK || newCAFileOK) {
				return fmt.Errorf(
					"cannot remove used `ca-file` or `ca-path` from tls",
				)
			}

			if oldCAFileOK && newCAFileOK && newCAFile.(string) != oldCAFile {
				return fmt.Errorf("cannot change ca-file of used tls")
			}
		}
	}

	return nil
}

func validateNetworkConnectionUpdate(
	newConf, oldConf map[string]interface{}, connectionType string,
) error {
	var networkPorts = []string{
		"port", "access-port",
		"alternate-access-port",
	}

	oldConnectionConfig := oldConf["network"].(map[string]interface{})[connectionType].(map[string]interface{})
	newConnectionConfig := newConf["network"].(map[string]interface{})[connectionType].(map[string]interface{})

	oldTLSName, oldTLSNameOk := oldConnectionConfig["tls-name"]
	newTLSName, newTLSNameOk := newConnectionConfig["tls-name"]

	if oldTLSNameOk && newTLSNameOk {
		if !reflect.DeepEqual(oldTLSName, newTLSName) {
			return fmt.Errorf("cannot modify tls name")
		}
	}

	for _, port := range networkPorts {
		if err := validateNetworkPortUpdate(oldConnectionConfig, newConnectionConfig, port); err != nil {
			return err
		}
	}

	return nil
}

func validateNetworkPortUpdate(oldConnectionConfig, newConnectionConfig map[string]interface{}, port string) error {
	oldPort, oldPortOk := oldConnectionConfig[port]
	newPort, newPortOk := newConnectionConfig[port]
	tlsPort := "tls-" + port

	if oldPortOk && newPortOk {
		if !reflect.DeepEqual(oldPort, newPort) {
			return fmt.Errorf("cannot modify %s number: old value %v, new value %v", port, oldPort, newPort)
		}
	}

	oldTLSPort, oldTLSPortOk := oldConnectionConfig[tlsPort]
	newTLSPort, newTLSPortOk := newConnectionConfig[tlsPort]

	if oldTLSPortOk && newTLSPortOk {
		if !reflect.DeepEqual(oldTLSPort, newTLSPort) {
			return fmt.Errorf(
				"cannot modify %s number: old value %v, new value %v", tlsPort, oldTLSPort, newTLSPort)
		}
	}

	if (!newTLSPortOk && oldTLSPortOk) || (!newPortOk && oldPortOk) {
		if !(oldPortOk && oldTLSPortOk) {
			return fmt.Errorf("cannot remove tls or non-tls configurations unless both configurations have been set initially")
		}
	}

	return nil
}

func validateNetworkPolicyUpdate(oldPolicy, newPolicy *asdbv1.AerospikeNetworkPolicy) error {
	if oldPolicy.FabricType != newPolicy.FabricType {
		return fmt.Errorf("cannot update fabric type")
	}

	if oldPolicy.TLSFabricType != newPolicy.TLSFabricType {
		return fmt.Errorf("cannot update tlsFabric type")
	}

	if newPolicy.FabricType == asdbv1.AerospikeNetworkTypeCustomInterface &&
		!reflect.DeepEqual(oldPolicy.CustomFabricNetworkNames, newPolicy.CustomFabricNetworkNames) {
		return fmt.Errorf("cannot change/update customFabricNetworkNames")
	}

	if newPolicy.TLSFabricType == asdbv1.AerospikeNetworkTypeCustomInterface &&
		!reflect.DeepEqual(oldPolicy.CustomTLSFabricNetworkNames, newPolicy.CustomTLSFabricNetworkNames) {
		return fmt.Errorf("cannot change/update customTLSFabricNetworkNames")
	}

	return nil
}

func validateNsConfUpdate(newConfSpec, oldConfSpec, currentStatus *asdbv1.AerospikeConfigSpec) error {
	newConf := newConfSpec.Value
	oldConf := oldConfSpec.Value

	var statusNsConfList []interface{}

	if currentStatus != nil && len(currentStatus.Value) != 0 {
		statusConf := currentStatus.Value
		statusNsConfList = statusConf["namespaces"].([]interface{})
	}

	newNsConfList := newConf["namespaces"].([]interface{})
	oldNsConfList := oldConf["namespaces"].([]interface{})

	for _, singleConfInterface := range newNsConfList {
		// Validate new namespaceconf
		singleConf, ok := singleConfInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"namespace conf not in valid format %v", singleConfInterface,
			)
		}

		// Validate new namespace conf from old namespace conf. Few fields cannot be updated
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
						"replication-factor cannot be updated. old nsconf %v, new nsconf %v",
						oldSingleConf, singleConf,
					)
				}

				// strong-consistency update not allowed
				if isValueUpdated(
					oldSingleConf, singleConf, "strong-consistency",
				) {
					return fmt.Errorf(
						"strong-consistency cannot be updated. old nsconf %v, new nsconf %v",
						oldSingleConf, singleConf,
					)
				}
			}
		}
	}

	err := validateStorageEngineDeviceListUpdate(newNsConfList, statusNsConfList)
	if err != nil {
		return err
	}

	// Check for namespace name len
	return nil
}

func validateStorageEngineDeviceList(nsConfList []interface{}) (deviceList, fileList map[string]string, err error) {
	deviceList = map[string]string{}
	fileList = map[string]string{}

	// build a map device -> namespace
	for _, nsConfInterface := range nsConfList {
		nsConf := nsConfInterface.(map[string]interface{})
		namespace := nsConf["name"].(string)
		storage := nsConf["storage-engine"].(map[string]interface{})

		if devices, ok := storage["devices"]; ok {
			for _, d := range devices.([]interface{}) {
				device := d.(string)

				previousNamespace, exists := deviceList[device]
				if exists {
					return nil, nil, fmt.Errorf(
						"device %s is already being referenced in multiple namespaces (%s, %s)",
						device, previousNamespace, namespace,
					)
				}

				deviceList[device] = namespace
			}
		}

		if files, ok := storage["files"]; ok {
			for _, d := range files.([]interface{}) {
				file := d.(string)

				previousNamespace, exists := fileList[file]
				if exists {
					return nil, nil, fmt.Errorf(
						"file %s is already being referenced in multiple namespaces (%s, %s)",
						file, previousNamespace, namespace,
					)
				}

				fileList[file] = namespace
			}
		}
	}

	return deviceList, fileList, nil
}

func validateStorageEngineDeviceListUpdate(nsConfList, statusNsConfList []interface{}) error {
	deviceList, fileList, err := validateStorageEngineDeviceList(nsConfList)
	if err != nil {
		return err
	}

	for _, statusNsConfInterface := range statusNsConfList {
		nsConf := statusNsConfInterface.(map[string]interface{})
		namespace := nsConf["name"].(string)
		storage := nsConf["storage-engine"].(map[string]interface{})

		if devices, ok := storage["devices"]; ok {
			for _, d := range devices.([]interface{}) {
				device := d.(string)
				if deviceList[device] != "" && deviceList[device] != namespace {
					return fmt.Errorf(
						"device %s can not be removed and re-used in a different namespace at the same time. "+
							"It has to be removed first. currentNamespace `%s`, oldNamespace `%s`",
						device, deviceList[device], namespace,
					)
				}
			}
		}

		if files, ok := storage["files"]; ok {
			for _, d := range files.([]interface{}) {
				file := d.(string)
				if fileList[file] != "" && fileList[file] != namespace {
					return fmt.Errorf(
						"file %s can not be removed and re-used in a different namespace at the same time. "+
							"It has to be removed first. currentNamespace `%s`, oldNamespace `%s`",
						file, fileList[file], namespace,
					)
				}
			}
		}
	}

	return nil
}

func validateAerospikeConfigSchema(
	aslog logr.Logger, version string, configSpec asdbv1.AerospikeConfigSpec,
) error {
	config := configSpec.Value

	asConf, err := asconfig.NewMapAsConfig(aslog, config)
	if err != nil {
		return fmt.Errorf("failed to load config map by lib: %v", err)
	}

	valid, validationErr, err := asConf.IsValid(aslog, version)
	if !valid {
		if len(validationErr) == 0 {
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

func validateWorkDir(workDirPath string, fileStorageList []string) error {
	if !filepath.IsAbs(workDirPath) {
		return fmt.Errorf(
			"aerospike work directory path %s must be absolute",
			workDirPath,
		)
	}

	if !isFileStorageConfiguredForDir(fileStorageList, workDirPath) {
		return fmt.Errorf(
			"aerospike work directory path %s not found in storage volume's aerospike paths %v",
			workDirPath, fileStorageList,
		)
	}

	return nil
}

func validateRequiredFileStorageForMetadata(
	configSpec asdbv1.AerospikeConfigSpec, storage *asdbv1.AerospikeStorageSpec,
	validationPolicy *asdbv1.ValidationPolicySpec,
) error {
	_, onlyPVFileStorageList, err := getAerospikeStorageList(storage, true)
	if err != nil {
		return err
	}

	// Validate work directory.
	if !validationPolicy.SkipWorkDirValidate {
		workDirPath := asdbv1.GetWorkDirectory(configSpec)

		if err := validateWorkDir(workDirPath, onlyPVFileStorageList); err != nil {
			return err
		}
	} else {
		workDirPath := asdbv1.GetConfiguredWorkDirectory(configSpec)

		if workDirPath != "" {
			_, allFileStorageList, err := getAerospikeStorageList(storage, false)
			if err != nil {
				return err
			}

			if err := validateWorkDir(workDirPath, allFileStorageList); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateRequiredFileStorageForAerospikeConfig(
	configSpec asdbv1.AerospikeConfigSpec, storage *asdbv1.AerospikeStorageSpec,
) error {
	featureKeyFilePaths := getFeatureKeyFilePaths(configSpec)
	nonCAPaths, caPaths := getTLSFilePaths(configSpec)
	defaultPassFilePath := asdbv1.GetDefaultPasswordFilePath(&configSpec)

	// TODO: What if default password file is given via Secret Manager?
	// How operator will access that file? Should we allow that?

	var allPaths []string

	for _, path := range featureKeyFilePaths {
		if !isSecretManagerPath(path) {
			allPaths = append(allPaths, path)
		}
	}

	for _, path := range nonCAPaths {
		if !isSecretManagerPath(path) {
			allPaths = append(allPaths, path)
		}
	}

	if defaultPassFilePath != nil {
		if !isSecretManagerPath(*defaultPassFilePath) {
			allPaths = append(allPaths, *defaultPassFilePath)
		} else {
			return fmt.Errorf("default-password-file path doesn't support Secret Manager, path %s", *defaultPassFilePath)
		}
	}

	// CA cert related fields are not supported with Secret Manager, so check their mount volume
	allPaths = append(allPaths, caPaths...)

	for _, path := range allPaths {
		volume := asdbv1.GetVolumeForAerospikePath(storage, filepath.Dir(path))
		if volume == nil {
			return fmt.Errorf(
				"feature-key-file paths or tls paths or default-password-file path "+
					"are not mounted - create an entry for '%s' in 'storage.volumes'",
				path,
			)
		}

		if defaultPassFilePath != nil &&
			(path == *defaultPassFilePath && volume.Source.Secret == nil) {
			return fmt.Errorf(
				"default-password-file path %s volume source should be secret in storage config, volume %v",
				path, volume,
			)
		}
	}

	return nil
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

// isDeviceOrPmemNamespace returns true if this namespace config uses a device for storage.
func isDeviceOrPmemNamespace(namespaceConf map[string]interface{}) bool {
	storage, ok := namespaceConf["storage-engine"]
	if !ok {
		return false
	}

	storageConf := storage.(map[string]interface{})
	typeStr, ok := storageConf["type"]

	return ok && (typeStr == "device" || typeStr == "pmem")
}

// isShMemIndexTypeNamespace returns true if this namespace index type is shmem.
func isShMemIndexTypeNamespace(namespaceConf map[string]interface{}) bool {
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

func getFeatureKeyFilePaths(configSpec asdbv1.AerospikeConfigSpec) []string {
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

func getTLSFilePaths(configSpec asdbv1.AerospikeConfigSpec) (nonCAPaths, caPaths []string) {
	config := configSpec.Value

	// feature-key-file needs secret
	if network, ok := config["network"]; ok {
		if tlsListInterface, ok := network.(map[string]interface{})["tls"]; ok {
			if tlsList, ok := tlsListInterface.([]interface{}); ok {
				for _, tlsInterface := range tlsList {
					if path, ok := tlsInterface.(map[string]interface{})["cert-file"]; ok {
						nonCAPaths = append(nonCAPaths, path.(string))
					}

					if path, ok := tlsInterface.(map[string]interface{})["key-file"]; ok {
						nonCAPaths = append(nonCAPaths, path.(string))
					}

					if path, ok := tlsInterface.(map[string]interface{})["ca-file"]; ok {
						caPaths = append(caPaths, path.(string))
					}

					if path, ok := tlsInterface.(map[string]interface{})["ca-path"]; ok {
						caPaths = append(caPaths, path.(string)+"/")
					}
				}
			}
		}
	}

	return nonCAPaths, caPaths
}

// isSecretManagerPath indicates if the given path is a Secret Manager's unique identifier path
func isSecretManagerPath(path string) bool {
	return strings.HasPrefix(path, "secrets:") || strings.HasPrefix(path, "vault:")
}

// isFileStorageConfiguredForDir indicates if file storage is configured for dir.
func isFileStorageConfiguredForDir(fileStorageList []string, dir string) bool {
	for _, storageMount := range fileStorageList {
		if asdbv1.IsPathParentOrSame(storageMount, dir) {
			return true
		}
	}

	return false
}

func validatePodSpec(cluster *asdbv1.AerospikeCluster) error {
	if cluster.Spec.PodSpec.HostNetwork && asdbv1.GetBool(cluster.Spec.PodSpec.MultiPodPerHost) {
		return fmt.Errorf("host networking cannot be enabled with multi pod per host")
	}

	if err := validateDNS(cluster.Spec.PodSpec.DNSPolicy, cluster.Spec.PodSpec.DNSConfig); err != nil {
		return err
	}

	var allContainers []v1.Container

	allContainers = append(allContainers, cluster.Spec.PodSpec.Sidecars...)
	allContainers = append(allContainers, cluster.Spec.PodSpec.InitContainers...)

	if err := ValidateAerospikeObjectMeta(&cluster.Spec.PodSpec.AerospikeObjectMeta); err != nil {
		return err
	}

	// Duplicate names are not allowed across sidecars and initContainers
	return validatePodSpecContainer(allContainers)
}

func validatePodSpecContainer(containers []v1.Container) error {
	containerNames := map[string]int{}

	for idx := range containers {
		container := &containers[idx]
		// Check for reserved container name
		if container.Name == asdbv1.AerospikeServerContainerName || container.Name == asdbv1.AerospikeInitContainerName {
			return fmt.Errorf(
				"cannot use reserved container name: %v", container.Name,
			)
		}

		// Check for duplicate names
		if _, ok := containerNames[container.Name]; ok {
			return fmt.Errorf(
				"cannot have duplicate names of containers: %v", container.Name,
			)
		}

		containerNames[container.Name] = 1
	}

	return nil
}

func ValidateAerospikeObjectMeta(aerospikeObjectMeta *asdbv1.AerospikeObjectMeta) error {
	for label := range aerospikeObjectMeta.Labels {
		if label == asdbv1.AerospikeAppLabel || label == asdbv1.AerospikeRackIDLabel ||
			label == asdbv1.AerospikeCustomResourceLabel {
			return fmt.Errorf(
				"label: %s is automatically defined by operator and shouldn't be specified by user",
				label,
			)
		}
	}

	return nil
}

func validateDNS(dnsPolicy v1.DNSPolicy, dnsConfig *v1.PodDNSConfig) error {
	if dnsPolicy == v1.DNSDefault {
		return fmt.Errorf("dnsPolicy: Default is not supported")
	}

	if dnsPolicy == v1.DNSNone && dnsConfig == nil {
		return fmt.Errorf("dnsConfig is required field when dnsPolicy is set to None")
	}

	return nil
}

func validateNetworkPolicy(cluster *asdbv1.AerospikeCluster) error {
	networkPolicy := &cluster.Spec.AerospikeNetworkPolicy

	annotations := cluster.Spec.PodSpec.AerospikeObjectMeta.Annotations
	networks := annotations["k8s.v1.cni.cncf.io/networks"]
	networkList := strings.Split(networks, ",")
	networkSet := sets.NewString()

	setNamespaceDefault(networkList, cluster.Namespace)

	networkSet.Insert(networkList...)

	validateNetworkList := func(netList []string, addressType asdbv1.AerospikeNetworkType, listName string) error {
		if netList == nil {
			return fmt.Errorf("%s is required with 'customInterface' %s type", listName, addressType)
		}

		if cluster.Spec.PodSpec.HostNetwork {
			return fmt.Errorf("hostNetwork is not allowed with 'customInterface' network type")
		}

		if !networkSet.HasAll(netList...) {
			return fmt.Errorf(
				"required networks %v not present in pod metadata annotations key `k8s.v1.cni.cncf.io/networks`",
				netList,
			)
		}

		return nil
	}

	if networkPolicy.AccessType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomAccessNetworkNames,
			"access", "customAccessNetworkNames",
		); err != nil {
			return err
		}
	}

	if networkPolicy.AlternateAccessType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomAlternateAccessNetworkNames,
			"alternateAccess", "customAlternateAccessNetworkNames",
		); err != nil {
			return err
		}
	}

	if networkPolicy.TLSAccessType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomTLSAccessNetworkNames,
			"tlsAccess", "customTLSAccessNetworkNames",
		); err != nil {
			return err
		}
	}

	if networkPolicy.TLSAlternateAccessType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomTLSAlternateAccessNetworkNames,
			"tlsAlternateAccess", "customTLSAlternateAccessNetworkNames",
		); err != nil {
			return err
		}
	}

	if networkPolicy.FabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomFabricNetworkNames,
			"fabric", "customFabricNetworkNames",
		); err != nil {
			return err
		}
	}

	if networkPolicy.TLSFabricType == asdbv1.AerospikeNetworkTypeCustomInterface {
		if err := validateNetworkList(
			networkPolicy.CustomTLSFabricNetworkNames,
			"tlsFabric", "customTLSFabricNetworkNames",
		); err != nil {
			return err
		}
	}

	return nil
}

// validateBatchSize validates the batch size for the following types:
// - rollingUpdateBatchSize: Rolling update batch size
// - scaleDownBatchSize: Scale down batch size
func validateBatchSize(batchSize *intstr.IntOrString, rollingUpdateBatch bool, cluster *asdbv1.AerospikeCluster) error {
	var fieldPath string

	if batchSize == nil {
		return nil
	}

	if rollingUpdateBatch {
		fieldPath = "spec.rackConfig.rollingUpdateBatchSize"
	} else {
		fieldPath = "spec.rackConfig.scaleDownBatchSize"
	}

	if err := validateIntOrStringField(batchSize, fieldPath); err != nil {
		return err
	}

	validateRacksForBatchSize := func(rackConfig asdbv1.RackConfig) error {
		if len(rackConfig.Racks) < 2 {
			return fmt.Errorf("can not use %s when number of racks is less than two", fieldPath)
		}

		nsConfsNamespaces := getNsConfForNamespaces(rackConfig)
		for ns, nsConf := range nsConfsNamespaces {
			if !isNameExist(rackConfig.Namespaces, ns) {
				return fmt.Errorf(
					"can not use %s when there is any non-rack enabled namespace %s", fieldPath, ns,
				)
			}

			if nsConf.noOfRacksForNamespaces <= 1 {
				return fmt.Errorf(
					"can not use %s when namespace `%s` is configured in only one rack", fieldPath, ns,
				)
			}

			if nsConf.replicationFactor <= 1 {
				return fmt.Errorf(
					"can not use %s when namespace `%s` is configured with replication-factor 1", fieldPath,
					ns,
				)
			}

			// If Strong Consistency is enabled, then scaleDownBatchSize can't be used
			if !rollingUpdateBatch && nsConf.scEnabled {
				return fmt.Errorf(
					"can not use %s when namespace `%s` is configured with Strong Consistency", fieldPath,
					ns,
				)
			}
		}

		return nil
	}

	// validate rackConf from spec
	if err := validateRacksForBatchSize(cluster.Spec.RackConfig); err != nil {
		return err
	}

	// If the status is not nil, validate rackConf from status to restrict batch-size update
	// when old rackConfig is not valid for batch-size
	if cluster.Status.AerospikeConfig != nil {
		if err := validateRacksForBatchSize(cluster.Status.RackConfig); err != nil {
			return fmt.Errorf("status invalid for %s: update, %v", fieldPath, err)
		}
	}

	return nil
}

func validateIntOrStringField(value *intstr.IntOrString, fieldPath string) error {
	randomNumber := 100
	// Just validate if value is valid number or string.
	count, err := intstr.GetScaledValueFromIntOrPercent(value, randomNumber, false)
	if err != nil {
		return err
	}

	// Only negative is not allowed. Any big number can be given.
	if count < 0 {
		return fmt.Errorf("can not use negative %s: %s", fieldPath, value.String())
	}

	if value.Type == intstr.String && count > 100 {
		return fmt.Errorf("%s: %s must not be greater than 100 percent", fieldPath, value.String())
	}

	return nil
}

func validateMaxUnavailable(cluster *asdbv1.AerospikeCluster) (admission.Warnings, error) {
	var warnings admission.Warnings

	if asdbv1.GetBool(cluster.Spec.DisablePDB) {
		warnings = append(warnings, fmt.Sprintf("Spec field 'spec.maxUnavailable' will be omitted from Custom Resource (CR) "+
			"because 'spec.disablePDB' is true."))

		// PDB is disabled, no further validation required
		return warnings, nil
	}

	if err := validateIntOrStringField(cluster.Spec.MaxUnavailable, "spec.maxUnavailable"); err != nil {
		return warnings, err
	}

	safeMaxUnavailable := int(cluster.Spec.Size)

	// If Size is 1, then ignore it for maxUnavailable calculation as it will anyway result in data loss
	if safeMaxUnavailable == 1 {
		return warnings, nil
	}

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})

		for _, nsInterface := range nsList {
			rfInterface, exists := nsInterface.(map[string]interface{})["replication-factor"]
			if !exists {
				// Default RF is 2 if not given
				safeMaxUnavailable = 2
				continue
			}

			rf, err := asdbv1.GetIntType(rfInterface)
			if err != nil {
				return warnings, fmt.Errorf("namespace replication-factor %v", err)
			}

			// If RF is 1, then ignore it for maxUnavailable calculation as it will anyway result in data loss
			if rf == 1 {
				continue
			}

			if rf < safeMaxUnavailable {
				safeMaxUnavailable = rf
			}
		}
	}

	if cluster.Spec.MaxUnavailable.IntValue() >= safeMaxUnavailable {
		return warnings, fmt.Errorf("maxUnavailable %s cannot be greater than or equal to %v as it may result in "+
			"data loss. Set it to a lower value",
			cluster.Spec.MaxUnavailable.String(), safeMaxUnavailable)
	}

	return warnings, nil
}

func validateEnableDynamicConfigUpdate(cluster *asdbv1.AerospikeCluster) error {
	if !asdbv1.GetBool(cluster.Spec.EnableDynamicConfigUpdate) {
		return nil
	}

	if len(cluster.Status.Pods) == 0 {
		return nil
	}

	minInitVersion, err := getMinRunningInitVersion(cluster.Status.Pods)
	if err != nil {
		return err
	}

	val, err := lib.CompareVersions(minInitVersion, minInitVersionForDynamicConf)
	if err != nil {
		return fmt.Errorf("failed to check image version: %v", err)
	}

	if val < 0 {
		return fmt.Errorf("cannot enable enableDynamicConfigUpdate flag, some init containers are running version less"+
			" than %s. Please visit https://aerospike.com/docs/cloud/kubernetes/operator/Cluster-configuration-settings#spec"+
			" for more details about enableDynamicConfigUpdate flag",
			minInitVersionForDynamicConf)
	}

	return nil
}

func validateOperationUpdate(oldSpec, newSpec *asdbv1.AerospikeClusterSpec,
	status *asdbv1.AerospikeClusterStatus) error {
	if len(newSpec.Operations) == 0 {
		return nil
	}

	newOp := &newSpec.Operations[0]

	var oldOp *asdbv1.OperationSpec

	if len(oldSpec.Operations) != 0 {
		oldOp = &oldSpec.Operations[0]
	}

	if oldOp != nil && oldOp.ID == newOp.ID && !reflect.DeepEqual(oldOp, newOp) {
		return fmt.Errorf("operation %s cannot be updated", newOp.ID)
	}

	allPodNames := asdbv1.GetAllPodNames(status.Pods)

	podSet := sets.New(newSpec.Operations[0].PodList...)
	if !allPodNames.IsSuperset(podSet) {
		return fmt.Errorf("invalid pod names in operation %v", podSet.Difference(allPodNames).UnsortedList())
	}

	// Don't allow any on-demand operation along with these cluster change:
	// 1- scale up
	// 2- racks added or removed
	// 3- image update
	// New pods won't be available for operation
	if !reflect.DeepEqual(newSpec.Operations, status.Operations) {
		switch {
		case newSpec.Size > status.Size:
			return fmt.Errorf("cannot change Spec.Operations along with cluster scale-up")
		case len(newSpec.RackConfig.Racks) != len(status.RackConfig.Racks) ||
			len(newSpec.RackConfig.Racks) != len(oldSpec.RackConfig.Racks):
			return fmt.Errorf("cannot change Spec.Operations along with rack addition/removal")
		case newSpec.Image != status.Image || newSpec.Image != oldSpec.Image:
			return fmt.Errorf("cannot change Spec.Operations along with image update")
		}
	}

	return nil
}

func getMinRunningInitVersion(pods map[string]asdbv1.AerospikePodStatus) (string, error) {
	minVersion := ""

	for idx := range pods {
		if pods[idx].InitImage != "" {
			version, err := asdbv1.GetImageVersion(pods[idx].InitImage)
			if err != nil {
				return "", err
			}

			if minVersion == "" {
				minVersion = version
				continue
			}

			val, err := lib.CompareVersions(version, minVersion)
			if err != nil {
				return "", fmt.Errorf("failed to check image version: %v", err)
			}

			if val < 0 {
				minVersion = version
			}
		} else {
			return baseInitVersion, nil
		}
	}

	return minVersion, nil
}
