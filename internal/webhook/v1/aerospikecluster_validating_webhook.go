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
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/validation"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

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

	warns, vErr := validate(aslog, aerospikeCluster)
	if vErr != nil {
		return warns, vErr
	}

	if err := validateAccessControlCreate(&aerospikeCluster.Spec); err != nil {
		return warns, err
	}

	return warns, nil
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

	if err := validateImageUpdate(oldObject.Spec.Image, aerospikeCluster.Spec.Image); err != nil {
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

	if err := validateAccessControlUpdate(&oldObject.Spec, &aerospikeCluster.Spec); err != nil {
		return warnings, err
	}

	// Block single-step TLS + PKIOnly update
	if err := validateTLSAndPKIOnlyUpdate(
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
	if err := validateRackUpdate(aslog, oldObject, aerospikeCluster); err != nil {
		return warnings, err
	}

	// Validate concurrent rack revisions limit
	if err := validateConcurrentRackRevisions(oldObject, aerospikeCluster); err != nil {
		return warnings, err
	}

	// Validate replication-factor update restrictions
	// Check both: spec-to-spec changes and status-to-spec changes
	if err := validateReplicationFactorUpdateRestrictions(oldObject, aerospikeCluster); err != nil {
		return warnings, err
	}

	return warnings, nil
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
		return warnings, fmt.Errorf("aerospikeCluster namespace cannot have spaces")
	}

	if err := validateImage(&cluster.Spec); err != nil {
		return warnings, err
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

	err = validateOperation(cluster)
	if err != nil {
		return warnings, err
	}

	// Storage should be validated before validating aerospikeConfig and fileStorage
	warns, err = validateStorage(&cluster.Spec.Storage, &cluster.Spec.PodSpec)
	warnings = append(warnings, warns...)

	if err != nil {
		return warnings, err
	}

	// Validate rackConfig
	warns, err = validateRackConfig(aslog, cluster)

	warnings = append(warnings, warns...)
	if err != nil {
		return warnings, err
	}

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		// Storage should be validated before validating aerospikeConfig and fileStorage
		warns, err = validateStorage(&rack.Storage, &cluster.Spec.PodSpec)
		warnings = append(warnings, warns...)

		if err != nil {
			return warnings, err
		}

		// Validate common aerospike config schema and fields
		err = validateAerospikeConfig(aslog, version,
			&rack.AerospikeConfig, &rack.Storage, int(cluster.Spec.Size),
			cluster.Spec.OperatorClientCertSpec,
		)
		if err != nil {
			return warnings, err
		}

		err = validateRequiredFileStorageForMetadata(
			rack.AerospikeConfig, &rack.Storage, cluster.Spec.ValidationPolicy,
		)
		if err != nil {
			return warnings, err
		}

		err = validateRequiredFileStorageForAerospikeConfig(
			rack.AerospikeConfig, &rack.Storage,
		)
		if err != nil {
			return warnings, err
		}
	}

	// Validate resource and limit
	err = validatePodSpecResourceAndLimits(aslog, cluster)
	if err != nil {
		return warnings, err
	}

	// Validate access control
	err = validateAccessControl(aslog, cluster)
	if err != nil {
		return warnings, err
	}

	if err := validateClientCertSpec(cluster); err != nil {
		return warnings, err
	}

	warns = validateDeprecatedFields(cluster)
	warnings = append(warnings, warns...)

	if err := validateNetworkPolicy(cluster); err != nil {
		return warnings, err
	}

	// Validate Sidecars
	if err := validatePodSpec(cluster); err != nil {
		return warnings, err
	}

	return warnings, validateSCNamespaces(cluster)
}

func validateAccessControlCreate(
	aerospikeClusterSpec *asdbv1.AerospikeClusterSpec,
) error {
	enabled, err := asdbv1.IsSecurityEnabled(aerospikeClusterSpec.AerospikeConfig.Value)
	if err != nil {
		return err
	}

	if !enabled && aerospikeClusterSpec.AerospikeAccessControl != nil {
		// Security is disabled however access control is specified.
		return fmt.Errorf("security is disabled but access control is specified")
	}

	return nil
}

func validateAccessControlUpdate(
	oldSpec *asdbv1.AerospikeClusterSpec,
	newSpec *asdbv1.AerospikeClusterSpec,
) error {
	if reflect.DeepEqual(oldSpec.AerospikeAccessControl, newSpec.AerospikeAccessControl) {
		return nil
	}

	if newSpec.AerospikeAccessControl == nil && oldSpec.AerospikeAccessControl != nil {
		return fmt.Errorf("aerospikeAccessControl cannot be removed once set")
	}

	desiredSecurityEnabled, err := asdbv1.IsSecurityEnabled(newSpec.AerospikeConfig.Value)
	if err != nil {
		return err
	}

	// ACL changes are only allowed when security is enabled
	if !desiredSecurityEnabled {
		return fmt.Errorf("aerospikeAccessControl cannot be updated when security is disabled")
	}

	var oldUsers, newUsers []asdbv1.AerospikeUserSpec
	if oldSpec.AerospikeAccessControl != nil {
		oldUsers = oldSpec.AerospikeAccessControl.Users
	}

	if newSpec.AerospikeAccessControl != nil {
		newUsers = newSpec.AerospikeAccessControl.Users
	}

	return validateUsersAuthModeUpdate(oldUsers, newUsers)
}

func validateImageUpdate(oldImage, newImage string) error {
	if isEnterprise(oldImage) && asdbv1.IsFederal(newImage) {
		return fmt.Errorf("enterprise to federal edition upgrade is not supported")
	}

	if asdbv1.IsFederal(oldImage) && isEnterprise(newImage) {
		return fmt.Errorf("federal to enterprise edition upgrade is not supported")
	}

	return nil
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

		nsList := rack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
		for _, nsConfInterface := range nsList {
			nsConf := nsConfInterface.(map[string]interface{})

			isEnabled := asdbv1.IsNSSCEnabled(nsConf)
			if isEnabled {
				tmpSCNamespaceSet.Insert(nsConf[asdbv1.ConfKeyName].(string))

				if validation.IsInMemoryNamespace(nsConf) {
					return fmt.Errorf("in-memory SC namespace is not supported, namespace %v", nsConf[asdbv1.ConfKeyName])
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

// warnOperatorClientCertSecretNamespace returns admission warnings when the deprecated
// secretNamespace field is used in operator client cert or CA cert source.
func warnOperatorClientCertSecretNamespace(spec *asdbv1.AerospikeOperatorClientCertSpec) admission.Warnings {
	if spec == nil || spec.SecretCertSource == nil {
		return nil
	}

	var warnings admission.Warnings

	//nolint:staticcheck // SA1019: intentionally read deprecated SecretNamespace to emit admission warning
	if spec.SecretCertSource.SecretNamespace != "" {
		warnings = append(warnings,
			"operatorClientCert.secretCertSource.secretNamespace is deprecated: use secrets in the same"+
				" namespace as the AerospikeCluster. Omit this field. This will be removed in future versions.")
	}

	//nolint:staticcheck // SA1019: intentionally read deprecated SecretNamespace to emit admission warning
	if spec.SecretCertSource.CaCertsSource != nil && spec.SecretCertSource.CaCertsSource.SecretNamespace != "" {
		warnings = append(warnings,
			"operatorClientCert.secretCertSource.caCertsSource.secretNamespace is deprecated: use secrets in"+
				" the same namespace as the AerospikeCluster. Omit this field. This will be blocked in future versions.")
	}

	return warnings
}

// warnValidationPolicySkipXdrDlogFileValidate returns an admission warning when the deprecated
// skipXdrDlogFileValidate field is set. This field is no longer in use and will be removed in future versions.
func warnValidationPolicySkipXdrDlogFileValidate(validationPolicy *asdbv1.ValidationPolicySpec) admission.Warnings {
	//nolint:staticcheck // SA1019: intentionally read deprecated SkipXdrDlogFileValidate to emit admission warning
	if validationPolicy == nil || !validationPolicy.SkipXdrDlogFileValidate {
		return nil
	}

	return admission.Warnings{
		"validationPolicy.skipXdrDlogFileValidate is deprecated: this field is no longer in use. " +
			"This will be removed in future versions.",
	}
}

func validateDeprecatedFields(cluster *asdbv1.AerospikeCluster) (warnings admission.Warnings) {
	warns := warnOperatorClientCertSecretNamespace(cluster.Spec.OperatorClientCertSpec)
	warnings = append(warnings, warns...)

	warns = warnValidationPolicySkipXdrDlogFileValidate(cluster.Spec.ValidationPolicy)
	warnings = append(warnings, warns...)

	return warnings
}

func validateClientCertSpec(cluster *asdbv1.AerospikeCluster) error {
	networkConf, networkConfExist := cluster.Spec.AerospikeConfig.Value[asdbv1.ConfKeyNetwork]
	if !networkConfExist {
		return fmt.Errorf("aerospikeConfig.network cannot be nil")
	}

	serviceConf, serviceConfExists := networkConf.(map[string]interface{})[asdbv1.ConfKeyNetworkService]
	if !serviceConfExists {
		return fmt.Errorf("aerospikeConfig.network.service cannot be nil")
	}

	serviceConfMap := serviceConf.(map[string]interface{})

	isPKIOnlyEnabled := hasPKIOnlyUser(&cluster.Spec)

	// PKIOnly mode requires mTLS configuration
	if isPKIOnlyEnabled {
		// Validate that service TLS is enabled (tls-name must be set)
		if _, tlsEnabled := serviceConfMap[asdbv1.ConfKeyTLSName]; !tlsEnabled {
			return fmt.Errorf("PKIOnly authMode requires Aerospike cluster to be mTLS enabled")
		}
	}

	tlsAuthenticateClientConfig, ok := serviceConfMap["tls-authenticate-client"]
	if !ok {
		// tls-authenticate-client not set - mutation webhook will default to "any" if TLS is enabled
		// If any user has PKIOnly mode and TLS is not enabled, we already returned error above
		return nil
	}

	switch {
	case reflect.DeepEqual("false", tlsAuthenticateClientConfig):
		// tls-authenticate-client: false is not allowed when any user has PKIOnly mode
		if isPKIOnlyEnabled {
			return fmt.Errorf("tls-authenticate-client cannot be 'false' when any user has PKIOnly authMode," +
				" it should be 'any' or a list of specific client names")
		}

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
		// tls-authenticate-client is a list of specific client names
		if cluster.Spec.OperatorClientCertSpec == nil {
			return fmt.Errorf("operator client cert is not specified")
		}

		if cluster.Spec.OperatorClientCertSpec.TLSClientName == "" {
			return fmt.Errorf("operator TLSClientName is not specified")
		}

		// When PKIOnly is used with tls-authenticate-client as a list,
		// TLSClientName must be "admin" for the operator to authenticate as admin user
		if isPKIOnlyEnabled && cluster.Spec.OperatorClientCertSpec.TLSClientName != asdbv1.AdminUsername {
			return fmt.Errorf("PKIOnly authMode with tls-authenticate-client as a list requires "+
				"operatorClientCertSpec.tlsClientName to be '%s'", asdbv1.AdminUsername)
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

	forceBlockFromRosterChanged := false

	// Need to exclude a default rack with default rack ID. No need to check here,
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

					// Storage update is allowed only when rack revision is changed.
					// In case of the same rack revision, check for storage update
					if oldRack.Revision == newRack.Revision {
						if err := validateStorageSpecChange(&oldStorage, &newStorage); err != nil {
							return fmt.Errorf(
								"rack storage config cannot be updated: %v", err,
							)
						}
					} else {
						// Even if revision changed, validate against current status storage
						// for the same rack ID to prevent "revision bump and rollback" bypass
						var statusStorage *asdbv1.AerospikeStorageSpec

						for idx := range oldObj.Status.RackConfig.Racks {
							if oldObj.Status.RackConfig.Racks[idx].ID == newRack.ID &&
								oldObj.Status.RackConfig.Racks[idx].Revision == newRack.Revision {
								statusStorage = &oldObj.Status.RackConfig.Racks[idx].Storage
								break
							}
						}

						if statusStorage != nil {
							if err := validateStorageSpecChange(statusStorage, &newStorage); err != nil {
								return fmt.Errorf(
									"old rack with same revision %s already exists with different storage "+
										"config: %v", newRack.Revision, err)
							}
						}
					}
				}

				if oldRack.ForceBlockFromRoster != newRack.ForceBlockFromRoster {
					forceBlockFromRosterChanged = true
				}

				break
			}
		}
	}

	if forceBlockFromRosterChanged {
		return validateForceBlockFromRosterUpdate(newObj)
	}

	return nil
}

func validateForceBlockFromRosterUpdate(newObj *asdbv1.AerospikeCluster) error {
	if newObj.Status.AerospikeConfig == nil {
		return fmt.Errorf("status is not updated yet, cannot change forceBlockFromRoster in rack")
	}

	racksBlockedFromRosterInSpec := sets.New[int]()
	racksBlockedFromRosterInStatus := sets.New[int]()

	for idx := range newObj.Spec.RackConfig.Racks {
		rack := newObj.Spec.RackConfig.Racks[idx]
		if asdbv1.GetBool(rack.ForceBlockFromRoster) {
			racksBlockedFromRosterInSpec.Insert(rack.ID)
		}
	}

	for idx := range newObj.Status.RackConfig.Racks {
		rack := newObj.Status.RackConfig.Racks[idx]
		if asdbv1.GetBool(rack.ForceBlockFromRoster) {
			racksBlockedFromRosterInStatus.Insert(rack.ID)
		}
	}

	remainingRacks := len(newObj.Status.RackConfig.Racks) - len(racksBlockedFromRosterInSpec)
	if err := validateRackCountConstraints(remainingRacks, &newObj.Spec.RackConfig); err != nil {
		return err
	}

	desiredRacksBlockedFromRoster := racksBlockedFromRosterInSpec.Difference(racksBlockedFromRosterInStatus)
	if len(desiredRacksBlockedFromRoster) > 1 {
		return fmt.Errorf("the forceBlockFromRoster flag can be applied to only one rack at a time")
	}

	return nil
}

func validateConcurrentRackRevisions(oldObj, newObj *asdbv1.AerospikeCluster) error {
	// Group racks by ID to check for concurrent revisions across all three sources:
	// 1. Old Status
	// 2. Old Spec
	// 3. New Spec
	rackRevisions := make(map[int]sets.Set[string])

	addRackRevision := func(racks []asdbv1.Rack) {
		for idx := range racks {
			if rackRevisions[racks[idx].ID] == nil {
				rackRevisions[racks[idx].ID] = sets.New[string]()
			}

			rackRevisions[racks[idx].ID] = rackRevisions[racks[idx].ID].Insert(racks[idx].Revision)
		}
	}

	addRackRevision(oldObj.Status.RackConfig.Racks)
	addRackRevision(oldObj.Spec.RackConfig.Racks)
	addRackRevision(newObj.Spec.RackConfig.Racks)

	// Check if any rack has more than 2 concurrent revisions
	for rackID, revisions := range rackRevisions {
		if len(revisions) > 2 {
			return fmt.Errorf("rack %d has %d concurrent revisions (%v). "+
				"Maximum allowed is 2 (old + new). "+
				"Wait for the current migration to complete before starting a new one",
				rackID, len(revisions), revisions.UnsortedList())
		}
	}

	return nil
}

// TODO: FIX
func validateAccessControl(_ logr.Logger, cluster *asdbv1.AerospikeCluster) error {
	if _, err := asdbv1.IsAerospikeAccessControlValid(&cluster.Spec); err != nil {
		return err
	}

	return validatePKIAuthSupportForEE(&cluster.Spec)
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

func validateRackConfig(_ logr.Logger, cluster *asdbv1.AerospikeCluster) (admission.Warnings, error) {
	var warnings admission.Warnings

	// If EnableRackIDOverride is enabled, only single rack is allowed
	if asdbv1.GetBool(cluster.Spec.EnableRackIDOverride) {
		rackCount := len(cluster.Spec.RackConfig.Racks)
		if rackCount > 1 {
			return warnings, fmt.Errorf(
				"enableRackIDOverride requires a single rack configuration, but %d racks are specified",
				rackCount,
			)
		}

		// Warn if namespaces is empty when EnableRackIDOverride is set
		if len(cluster.Spec.RackConfig.Namespaces) == 0 {
			warnings = append(warnings,
				"enableRackIDOverride is set but rackConfig.namespaces is empty. "+
					"Consider specifying namespaces for namespace rack-aware setup.",
			)
		}
	}

	// Validate namespace names
	// TODO: Add more validation for namespace name
	for _, nsName := range cluster.Spec.RackConfig.Namespaces {
		if strings.Contains(nsName, " ") {
			return warnings, fmt.Errorf(
				"namespace name `%s` cannot have spaces, Namespaces %v", nsName,
				cluster.Spec.RackConfig.Namespaces,
			)
		}
	}

	rackMap := map[int]bool{}
	migrateFillDelaySet := sets.Set[int]{}

	var racksBlockedFromRoster int

	for idx := range cluster.Spec.RackConfig.Racks {
		rack := &cluster.Spec.RackConfig.Racks[idx]
		// Check for duplicate
		if _, ok := rackMap[rack.ID]; ok {
			return warnings, fmt.Errorf(
				"duplicate rackID %d not allowed, racks %v", rack.ID,
				cluster.Spec.RackConfig.Racks,
			)
		}

		rackMap[rack.ID] = true

		// Check out of range rackID
		// Check for defaultRackID is in mutate (user can not use defaultRackID).
		// Allow DefaultRackID
		if rack.ID > asdbv1.MaxRackID {
			return warnings, fmt.Errorf(
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
				return warnings, fmt.Errorf(
					"you can't specify network or security configuration for rack %d ("+
						"network and security should be the same for all racks)",
					rack.ID,
				)
			}
		}

		migrateFillDelay, err := asdbv1.GetMigrateFillDelay(&rack.AerospikeConfig)
		if err != nil {
			return warnings, err
		}

		migrateFillDelaySet.Insert(migrateFillDelay)

		if asdbv1.GetBool(rack.ForceBlockFromRoster) {
			racksBlockedFromRoster++
		}
	}

	if err := validateRackBlockedFromRoster(racksBlockedFromRoster, cluster); err != nil {
		return warnings, err
	}

	// If len of migrateFillDelaySet is more than 1, it means that different migrate-fill-delay is set across racks
	if migrateFillDelaySet.Len() > 1 {
		return warnings, fmt.Errorf("migrate-fill-delay value should be same across all racks")
	}

	// Validate batch upgrade/restart param
	if err := validateBatchSize(cluster.Spec.RackConfig.RollingUpdateBatchSize, true, cluster); err != nil {
		return warnings, err
	}

	// Validate batch scaleDown param
	if err := validateBatchSize(cluster.Spec.RackConfig.ScaleDownBatchSize, false, cluster); err != nil {
		return warnings, err
	}

	// Validate MaxIgnorablePods param
	if cluster.Spec.RackConfig.MaxIgnorablePods != nil {
		if err := validateIntOrStringField(cluster.Spec.RackConfig.MaxIgnorablePods,
			"spec.rackConfig.maxIgnorablePods"); err != nil {
			return warnings, err
		}
	}

	if err := validateReplicationFactorConsistencyAcrossRacks(cluster.Spec.RackConfig.Racks); err != nil {
		return err
	}

	// TODO: should not use batch if racks are less than replication-factor
	return warnings, nil
}

func validateRackBlockedFromRoster(racksBlockedFromRoster int, cluster *asdbv1.AerospikeCluster) error {
	if racksBlockedFromRoster > 0 {
		if cluster.Spec.RackConfig.MaxIgnorablePods != nil {
			return fmt.Errorf("forceBlockFromRoster cannot be used together with maxIgnorablePods")
		}

		if len(cluster.Spec.RosterNodeBlockList) > 0 {
			return fmt.Errorf("forceBlockFromRoster cannot be used together with RosterNodeBlockList")
		}

		remainingRacks := len(cluster.Spec.RackConfig.Racks) - racksBlockedFromRoster
		if err := validateRackCountConstraints(remainingRacks, &cluster.Spec.RackConfig); err != nil {
			return err
		}
	}

	return nil
}

func validateRackCountConstraints(remainingRacks int, rackConfig *asdbv1.RackConfig) error {
	if remainingRacks <= 0 {
		return fmt.Errorf("all racks cannot have forceBlockFromRoster enabled. At least one rack must remain in the roster")
	}

	if remainingRacks == 1 &&
		(rackConfig.RollingUpdateBatchSize != nil || rackConfig.ScaleDownBatchSize != nil) {
		return fmt.Errorf("with only one rack in roster, cannot use rollingUpdateBatchSize or scaleDownBatchSize")
	}

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
		nsList := rack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})

		for _, nsInterface := range nsList {
			nsName := nsInterface.(map[string]interface{})[asdbv1.ConfKeyName].(string)

			var noOfRacksForNamespaces int
			if _, ok := nsConfs[nsName]; !ok {
				noOfRacksForNamespaces = 1
			} else {
				noOfRacksForNamespaces = nsConfs[nsName].noOfRacksForNamespaces + 1
			}

			rf, _ := validation.GetNamespaceReplicationFactor(nsInterface.(map[string]interface{}))

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
	aslog logr.Logger, version string, configSpec *asdbv1.AerospikeConfigSpec,
	storage *asdbv1.AerospikeStorageSpec, clSize int,
	clientCert *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	// It validates the aerospikeConfig schema and generic aerospikeConfig fields
	if err := validation.ValidateAerospikeConfig(
		aslog, version, configSpec.Value, clSize,
	); err != nil {
		return err
	}

	if err := validateNetworkConfig(configSpec.Value, clientCert); err != nil {
		return err
	}

	return validateNamespaceConfig(configSpec.Value, storage)
}

func validateNetworkConfig(config map[string]interface{},
	operatorClientCert *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	// network conf
	networkConf := config["network"].(map[string]interface{})
	serviceConf := networkConf[asdbv1.ConfKeyNetworkService]

	return validateTLSClientNames(
		serviceConf.(map[string]interface{}), operatorClientCert,
	)
}

func validateTLSClientNames(
	serviceConf map[string]interface{},
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
) error {
	dnsNames, err := validation.ValidateTLSAuthenticateClient(serviceConf)
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

// validateNamespaceConfig validates namespace config based on storage config
// It validates namespace storage-engine devices and files against storage config
// It validates namespace index-type mounts against storage config
// It does not do any basic aerospikeConfig related validation like format, required fields etc. as it is already done
// as part of ValidateAerospikeConfig
func validateNamespaceConfig(
	config map[string]interface{}, storage *asdbv1.AerospikeStorageSpec,
) error {
	nsConfInterfaceList := config[asdbv1.ConfKeyNamespace].([]interface{})

	// Get list of all devices used in namespace. match it with namespace device list
	blockStorageDeviceList, fileStorageList, err := getAerospikeStorageList(storage, true)
	if err != nil {
		return err
	}

	for _, nsConfInterface := range nsConfInterfaceList {
		// Validate new namespace conf
		nsConf := nsConfInterface.(map[string]interface{})

		if nsStorage, ok := nsConf["storage-engine"]; ok {
			if validation.IsInMemoryNamespace(nsConf) {
				// storage-engine memory
				continue
			}

			if !validation.IsDeviceOrPmemNamespace(nsConf) {
				return fmt.Errorf(
					"storage-engine not supported for namespace %v", nsConf,
				)
			}

			if devices, ok := nsStorage.(map[string]interface{})["devices"]; ok {
				for _, device := range devices.([]interface{}) {
					device = strings.TrimSpace(device.(string))

					dList := strings.Fields(device.(string))
					for _, dev := range dList {
						// Namespace device should be present in BlockStorage config section
						if !utils.ContainsString(blockStorageDeviceList, dev) {
							return fmt.Errorf(
								"namespace storage device related devicePath %v not found in Storage config %v",
								dev, storage,
							)
						}
					}
				}
			}

			if files, ok := nsStorage.(map[string]interface{})["files"]; ok {
				for _, file := range files.([]interface{}) {
					file = strings.TrimSpace(file.(string))

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

	// Validate index-type
	for _, nsConfInterface := range nsConfInterfaceList {
		nsConf := nsConfInterface.(map[string]interface{})

		if validation.IsShMemIndexTypeNamespace(nsConf) {
			continue
		}

		if nsIndexStorage, ok := nsConf["index-type"]; ok {
			if mounts, ok := nsIndexStorage.(map[string]interface{})["mounts"]; ok {
				for _, mount := range mounts.([]interface{}) {
					// Namespace index-type mount should be present in filesystem config section
					if !utils.ContainsString(fileStorageList, mount.(string)) {
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

func validateAerospikeConfigUpdate(
	aslog logr.Logger,
	incomingSpec, outgoingSpec, currentStatus *asdbv1.AerospikeConfigSpec,
) error {
	aslog.Info("Validate AerospikeConfig update")

	newConf := incomingSpec.Value
	oldConf := outgoingSpec.Value

	if err := validation.ValidateAerospikeConfigUpdateWithoutSchema(oldConf, newConf); err != nil {
		return err
	}

	return validateNsConfUpdateFromStatus(incomingSpec, currentStatus)
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

func validateNsConfUpdateFromStatus(newConfSpec, currentStatus *asdbv1.AerospikeConfigSpec) error {
	var statusNsConfList []interface{}

	if currentStatus != nil && len(currentStatus.Value) != 0 {
		statusConf := currentStatus.Value
		statusNsConfList = statusConf[asdbv1.ConfKeyNamespace].([]interface{})
	}

	newConf := newConfSpec.Value
	newNsConfList := newConf[asdbv1.ConfKeyNamespace].([]interface{})

	return validateStorageEngineDeviceListUpdate(newNsConfList, statusNsConfList)
}

func validateStorageEngineDeviceListUpdate(nsConfList, statusNsConfList []interface{}) error {
	deviceList, fileList, err := validation.ValidateStorageEngineDeviceList(nsConfList)
	if err != nil {
		return err
	}

	for _, statusNsConfInterface := range statusNsConfList {
		nsConf := statusNsConfInterface.(map[string]interface{})
		namespace := nsConf[asdbv1.ConfKeyName].(string)
		storage := nsConf[asdbv1.ConfKeyStorageEngine].(map[string]interface{})

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

// isEnterprise indicates if aerospike image is enterprise
func isEnterprise(image string) bool {
	return strings.Contains(strings.ToLower(image), "enterprise")
}

func validateImage(spec *asdbv1.AerospikeClusterSpec) error {
	// Validate image type. Only enterprise and federal image allowed for now.
	if !isEnterprise(spec.Image) && !asdbv1.IsFederal(spec.Image) {
		return fmt.Errorf("CommunityEdition Cluster not supported")
	}

	return nil
}

func getFeatureKeyFilePaths(configSpec asdbv1.AerospikeConfigSpec) []string {
	config := configSpec.Value

	// feature-key-file needs secret
	if svc, ok := config[asdbv1.ConfKeyService]; ok {
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
		if container.Name == asdbv1.AerospikeServerContainerName {
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

	for annotation := range aerospikeObjectMeta.Annotations {
		if annotation == asdbv1.EvictionBlockedAnnotation {
			return fmt.Errorf(
				"annotation: %s is reserved by operator and shouldn't be specified by user",
				annotation,
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
		nsList := rack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})

		for _, nsInterface := range nsList {
			rfInterface, exists := nsInterface.(map[string]interface{})[asdbv1.ConfKeyReplicationFactor]
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

func validateUsersAuthModeUpdate(oldUsers, newUsers []asdbv1.AerospikeUserSpec) error {
	if len(oldUsers) == 0 {
		return nil
	}

	oldUsersMap := make(map[string]asdbv1.AerospikeUserSpec)
	for _, userSpec := range oldUsers {
		oldUsersMap[userSpec.Name] = userSpec
	}

	for _, userSpec := range newUsers {
		if oldUserSpec, found := oldUsersMap[userSpec.Name]; found {
			if oldUserSpec.AuthMode == asdbv1.AerospikeAuthModePKIOnly && asdbv1.IsAuthModeInternal(userSpec.AuthMode) {
				return fmt.Errorf("user %s is not allowed to update authMode from PKI to Internal", userSpec.Name)
			}
		}
	}

	return nil
}

func validatePKIAuthSupportForEE(spec *asdbv1.AerospikeClusterSpec) error {
	if !isEnterprise(spec.Image) {
		return nil
	}

	if !hasPKIOnlyUser(spec) {
		return nil
	}

	version, err := asdbv1.GetImageVersion(spec.Image)
	if err != nil {
		return fmt.Errorf("failed to get Aerospike server version: %w", err)
	}

	val, err := lib.CompareVersions(version, minVersionForEnterprisePKIOnlyAuthMode)
	if err != nil {
		return fmt.Errorf("failed to compare Aerospike server version: %w", err)
	}

	if val < 0 {
		return fmt.Errorf(
			"PKIOnly authMode requires Enterprise Edition version %s or later (found version %s)",
			minVersionForEnterprisePKIOnlyAuthMode, version,
		)
	}

	return nil
}

// hasPKIOnlyUser checks if any user in the spec has PKIOnly authentication mode.
func hasPKIOnlyUser(spec *asdbv1.AerospikeClusterSpec) bool {
	if spec.AerospikeAccessControl == nil {
		return false
	}

	for _, user := range spec.AerospikeAccessControl.Users {
		if user.AuthMode == asdbv1.AerospikeAuthModePKIOnly {
			return true
		}
	}

	return false
}

// validateTLSAndPKIOnlyUpdate blocks upgrading to TLS and PKIOnly in a single update.
// When enabling TLS with PKIOnly, ACL reconciliation happens during rolling restart
// before all pods have TLS. If admin is set to PKIOnly (nopassword), the operator cannot connect
// to non-TLS pods because PKI auth requires TLS certificates.
func validateTLSAndPKIOnlyUpdate(oldSpec, newSpec *asdbv1.AerospikeClusterSpec,
	status *asdbv1.AerospikeClusterStatus) error {
	oldTLSName, _ := asdbv1.GetServiceTLSNameAndPort(oldSpec.AerospikeConfig)
	newTLSName, _ := asdbv1.GetServiceTLSNameAndPort(newSpec.AerospikeConfig)
	hasPKIUser := hasPKIOnlyUser(newSpec)

	// Check if TLS is being enabled
	if oldTLSName == "" && newTLSName != "" {
		if hasPKIUser {
			return fmt.Errorf("cannot enable TLS and PKIOnly authMode in a single update; " +
				"first enable TLS, then switch to PKIOnly auth mode")
		}
	}

	// Block PKIOnly if TLS rollout is still in progress
	if hasPKIUser && status != nil && status.AerospikeConfig != nil {
		statusTLSName, _ := asdbv1.GetServiceTLSNameAndPort(status.AerospikeConfig)
		// Spec has TLS but status doesn't = TLS rollout in progress
		if newTLSName != "" && statusTLSName == "" {
			return fmt.Errorf("cannot enable PKIOnly authMode while TLS rollout is in progress; " +
				"wait for TLS to be fully deployed first")
		}
	}

	return nil
}

// validateReplicationFactorUpdateRestrictions validates that if replication-factor is modified for any namespace,
// then:
// 1. Only one namespace's replication-factor can be changed at a time
// 2. No other fields in the entire aerospikecluster spec can be modified
// It performs two checks:
// - oldSpec vs newSpec: ensures user isn't making invalid changes in a single update
// - status vs newSpec: ensures the desired state change is valid (prevents drift from status)
func validateReplicationFactorUpdateRestrictions(oldObj, newObj *asdbv1.AerospikeCluster) error {
	// First check: Validate changes from old spec to new spec
	if err := validateReplicationFactorUpdateComparison(&oldObj.Spec, newObj); err != nil {
		return err
	}

	// Second check: Validate changes from status to new spec (if status exists)
	// This ensures the desired state is valid even if there's drift between status and spec
	if newObj.Status.AerospikeConfig != nil {
		statusToSpec, err := asdbv1.CopyStatusToSpec(&newObj.Status.AerospikeClusterStatusSpec)
		if err != nil {
			return fmt.Errorf("failed to copy status to spec for comparison: %v", err)
		}

		if err := validateReplicationFactorUpdateComparison(statusToSpec, newObj); err != nil {
			return err
		}
	}

	return nil
}

// validateReplicationFactorUpdateComparison validates replication-factor update restrictions
// by comparing two specs (either oldSpec vs newSpec, or statusSpec vs newSpec)
func validateReplicationFactorUpdateComparison(oldSpec *asdbv1.AerospikeClusterSpec,
	newObj *asdbv1.AerospikeCluster) error {
	// Create JSON patch to see all changes
	oldJSON, err := json.Marshal(oldSpec)
	if err != nil {
		return fmt.Errorf("failed to marshal old spec: %v", err)
	}

	newJSON, err := json.Marshal(newObj.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal new spec: %v", err)
	}

	patch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("failed to create JSON patch: %v", err)
	}

	// Filter only spec changes (ignore status and metadata changes)
	var (
		otherSpecChanged bool
	)

	replicationFactorPatches := sets.NewString()

	for _, op := range patch {
		// Check if this is a replication-factor change
		if strings.HasSuffix(op.Path, "/"+asdbv1.ConfKeyReplicationFactor) {
			if strings.Contains(op.Path, "/effectiveAerospikeConfig/") {
				// Extract namespace info from path
				// Path format: /rackConfig/racks/1/effectiveAerospikeConfig/namespaces/1/replication-factor
				nsName, scEnabled := extractNamespaceInfoFromPath(op.Path, newObj)
				if scEnabled {
					return fmt.Errorf(
						"cannot update replication-factor for namespace %s with Strong Consistency enabled",
						nsName,
					)
				}

				if nsName != "" {
					replicationFactorPatches.Insert(nsName)
				}
			}
		} else {
			if !strings.HasSuffix(op.Path, "/enableDynamicConfigUpdate") {
				otherSpecChanged = true
			}
		}
	}

	// If no replication-factor changes, no need to validate further
	if len(replicationFactorPatches) == 0 {
		return nil
	}

	if otherSpecChanged {
		// There are other spec changes along with replication-factor changes
		return fmt.Errorf(
			"when updating replication-factor for namespace, no other fields in the " +
				"aerospikecluster spec are allowed to be modified. Only replication-factor for a single " +
				"namespace can be changed at a time",
		)
	}

	if !ptr.Deref(newObj.Spec.EnableDynamicConfigUpdate, false) {
		return fmt.Errorf(
			"replication-factor update requires 'spec.enableDynamicConfigUpdate' to be set to true",
		)
	}

	// Rule: Only one namespace's replication-factor can be changed at a time
	if len(replicationFactorPatches) > 1 {
		return fmt.Errorf(
			"cannot update replication-factor for multiple namespaces at the same time. "+
				"Changed namespaces: %v. Only one namespace replication-factor can be updated at a time",
			replicationFactorPatches.List(),
		)
	}

	return nil
}

// extractNamespaceInfoFromPath extracts the namespace name and SC info from a JSON patch path
func extractNamespaceInfoFromPath(path string, newObj *asdbv1.AerospikeCluster) (string, bool) {
	// Path format examples:
	// /spec/rackConfig/racks/0/aerospikeConfig/namespaces/1/replication-factor
	parts := strings.Split(path, "/")

	var (
		nsIndex   = -1
		rackIndex = -1
		nsName    string
		scEnabled bool
	)

	// Find the namespace index in the path
	for i, part := range parts {
		if part == asdbv1.ConfKeyNamespace && i+1 < len(parts) {
			if i-2 >= 0 {
				if idx, err := strconv.Atoi(parts[i-2]); err == nil {
					rackIndex = idx
				}
			}

			// Extract namespace index
			if idx, err := strconv.Atoi(parts[i+1]); err == nil {
				nsIndex = idx
			}

			break
		}
	}

	if nsIndex < 0 || rackIndex < 0 {
		return nsName, scEnabled
	}

	// Get namespace name from rack config
	if rackIndex < len(newObj.Spec.RackConfig.Racks) {
		rack := &newObj.Spec.RackConfig.Racks[rackIndex]
		if nsList, ok := rack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{}); ok {
			if nsIndex < len(nsList) {
				if ns, ok := nsList[nsIndex].(map[string]interface{}); ok {
					if name, ok := ns[asdbv1.ConfKeyName].(string); ok {
						nsName = name
					}

					if sc, ok := ns[asdbv1.ConfKeyStrongConsistency].(bool); ok {
						scEnabled = sc
					}
				}
			}
		}
	}

	return nsName, scEnabled
}

// validateReplicationFactorConsistencyAcrossRacks validates that the same namespace
// has the same replication-factor value across all racks
func validateReplicationFactorConsistencyAcrossRacks(racks []asdbv1.Rack) error {
	// Map to store replication-factor for each namespace across racks
	// Key: namespace name, Value: replication-factor as int (normalized)
	namespaceReplicationFactors := make(map[string]int)

	// Helper function to get normalized replication-factor
	getReplicationFactor := func(ns map[string]interface{}) int {
		rf, exists := ns[asdbv1.ConfKeyReplicationFactor]
		if !exists {
			// Default replication-factor is 2 if not specified
			return 2
		}

		// Normalize to int for comparison
		rfInt, err := validation.GetIntType(rf)
		if err != nil {
			// If we can't parse it, return 0 to trigger an error
			return 0
		}

		return rfInt
	}

	// Check all racks
	for rackIdx := range racks {
		rack := racks[rackIdx]
		if nsList, ok := rack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{}); ok {
			for _, nsInterface := range nsList {
				var ns map[string]interface{}
				if ns, ok = nsInterface.(map[string]interface{}); !ok {
					continue
				}

				nsName, ok := ns[asdbv1.ConfKeyName].(string)
				if !ok {
					continue
				}

				currentRF := getReplicationFactor(ns)
				if currentRF == 0 {
					return fmt.Errorf("invalid replication-factor value for namespace '%s' in rack %d", nsName, rack.ID)
				}

				// Check if this namespace already has a replication-factor set
				if existingRF, exists := namespaceReplicationFactors[nsName]; exists {
					// Compare replication-factor values
					if existingRF != currentRF {
						return fmt.Errorf(
							"namespace '%s' has different replication-factor values across racks. "+
								"Replication-factor must be the same for the same namespace across all racks. "+
								"Found: %d and %d",
							nsName, existingRF, currentRF,
						)
					}
				} else {
					// First time seeing this namespace, store its replication-factor
					namespaceReplicationFactors[nsName] = currentRF
				}
			}
		}
	}

	return nil
}
