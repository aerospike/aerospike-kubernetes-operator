package cluster

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	sigsyaml "sigs.k8s.io/yaml"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/validation"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var pkgLog = ctrl.Log.WithName("lib.asconfig")

const (
	// aerospikeTemplateYAMLFileName is the primary config template stored in
	// the ConfigMap.  The init container for Aerospike Server >= 8.1.1 reads
	// this file, substitutes per-pod placeholders, and writes aerospike.yaml.
	aerospikeTemplateYAMLFileName = "aerospike.template.yaml"

	// aerospikeTemplateConfFileName is the legacy config template stored in the
	// ConfigMap for backward compatibility.  Init containers that predate YAML
	// support read this file and produce aerospike.conf.  It is present in the
	// ConfigMap only while at least one pod is still running an init image that
	// predates YAML support (< 2.6.0).
	aerospikeTemplateConfFileName = "aerospike.template.conf"

	// minYAMLInitVersion is the first aerospike-kubernetes-init image version
	// that can read aerospike.template.yaml instead of aerospike.template.conf.
	// Pods whose init image is at or above this version do NOT need the legacy
	// aerospike.template.conf in the ConfigMap.
	minYAMLInitVersion = "2.6.0"

	// networkPolicyHashFileName stores the network policy hash
	networkPolicyHashFileName = "networkPolicyHash"

	// podSpecHashFileName stores the pod spec hash
	podSpecHashFileName = "podSpecHash"

	// aerospikeConfHashFileName stores the Aerospike config hash
	aerospikeConfHashFileName = "aerospikeConfHash"
)

type initializeTemplateInput struct {
	WorkDir          string
	NetworkPolicy    asdbv1.AerospikeNetworkPolicy
	FabricPort       int32
	PodPort          int32
	PodTLSPort       int32
	HeartBeatPort    int32
	HeartBeatTLSPort int32
	FabricTLSPort    int32
	MultiPodPerHost  bool
	HostNetwork      bool
}

//go:embed scripts
var scripts embed.FS

// A map from script name to its template.
var scriptTemplates = make(map[string]*template.Template)

func init() {
	err := fs.WalkDir(
		scripts, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() {
				content, err := fs.ReadFile(scripts, path)
				if err != nil {
					return err
				}

				evaluated, err := template.New(path).Parse(string(content))
				if err != nil {
					return err
				}

				key := filepath.Base(path)
				scriptTemplates[key] = evaluated
			}

			return nil
		},
	)
	if err != nil {
		// Error reading embedded script templates.
		panic(fmt.Sprintf("error reading embedded script templates: %v", err))
	}
}

// createConfigMapData creates ConfigMap data for the given rack.
//
// The config is always serialized as YAML and stored under
// aerospikeTemplateYAMLFileName ("aerospike.template.yaml"). Configs provided
// in the legacy list format (pre-8.1.1) are first normalized to the new
// map-keyed YAML format before serialization.
//
// When includeLegacyConf is true the legacy aerospike.template.conf is also
// written into the ConfigMap.  This is needed during rolling upgrades from
// server versions < 8.1.1: if a pod crashes before its rolling restart it
// will be restarted on the old server image and the old init container needs
// aerospike.template.conf to be present.
//
// Hash selection:
//   - While legacy pods still exist (includeLegacyConf=true), the config hash
//     is derived from the .conf template.  This preserves continuity with the
//     hash already stored in pod annotations from before the AKO upgrade,
//     preventing a spurious rolling restart of pods whose config has not
//     actually changed.
//   - Once all pods have upgraded their init image (includeLegacyConf=false),
//     the hash is derived from the YAML template.  The one-time hash change
//     that occurs at this transition is intentional and expected.
func (r *SingleClusterReconciler) createConfigMapData(rack *asdbv1.Rack, includeLegacyConf bool) (
	map[string]string, error,
) {
	// Build the per-rack config template as YAML (always).
	yamlTemp, err := r.buildYAMLTemplate(rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build YAML config template: %v", err)
	}

	// Build base conf data (scripts, peers, etc.).
	confData, err := r.getBaseConfData(rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build base conf data: %v", err)
	}

	// Store YAML template. Init containers for server >= 8.1.1 read this.
	confData[aerospikeTemplateYAMLFileName] = yamlTemp

	// hashSource is the config content used to compute aerospikeConfHash.
	// Default to the YAML template; overridden below when legacy conf is used.
	hashSource := yamlTemp

	// Optionally include the legacy .conf template for pods still running an
	// init image that predates YAML support.
	if includeLegacyConf {
		confTemp, legacyErr := r.buildLegacyConfTemplate(rack)
		if legacyErr != nil {
			r.Log.Error(legacyErr, "Failed to build legacy .conf template; omitting from ConfigMap")
		} else {
			confData[aerospikeTemplateConfFileName] = confTemp
			// Use the .conf content for the hash while legacy pods exist.
			// Pod annotations already carry a .conf-based hash; keeping the
			// same hash source here avoids a spurious rolling restart on AKO
			// upgrade when no config has actually changed.
			hashSource = confTemp
		}
	}

	// Config hash — derived from hashSource (see comment above).
	confHash, err := utils.GetHash(hashSource)
	if err != nil {
		return nil, err
	}

	confData[aerospikeConfHashFileName] = confHash

	// Network policy hash.
	policy := r.aeroCluster.Spec.AerospikeNetworkPolicy

	policyStr, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}

	policyHash, err := utils.GetHash(string(policyStr))
	if err != nil {
		return nil, err
	}

	confData[networkPolicyHashFileName] = policyHash

	// Pod spec hash.
	podSpec := createPodSpecForRack(r.aeroCluster, rack)

	podSpecStr, err := json.Marshal(podSpec)
	if err != nil {
		return nil, err
	}

	// [Backward compatibility fix for AKO 2.1.0 upgrade]
	// aerospikeInitContainer is a newly introduced field in 2.1.0.
	// Ignore empty value from hash computation so that on upgrade clusters are
	// not rolling restarted.
	podSpecStr = []byte(strings.ReplaceAll(
		string(podSpecStr), "\"aerospikeInitContainer\":{},", "",
	))

	// [Backward compatibility fix for AKO 3.3.0 upgrade]
	// multiPodPerHost changed from bool to *bool in 3.3.0.
	// Ignore false value from hash computation so that on upgrade clusters are
	// not rolling restarted.
	podSpecStr = []byte(strings.ReplaceAll(
		string(podSpecStr), "\"multiPodPerHost\":false,", "",
	))

	podSpecHash, err := utils.GetHash(string(podSpecStr))
	if err != nil {
		return nil, err
	}

	confData[podSpecHashFileName] = podSpecHash

	return confData, nil
}

func createPodSpecForRack(
	aeroCluster *asdbv1.AerospikeCluster, rack *asdbv1.Rack,
) *asdbv1.AerospikePodSpec {
	rackFullPodSpec := lib.DeepCopy(
		&aeroCluster.Spec.PodSpec,
	).(*asdbv1.AerospikePodSpec)

	rackFullPodSpec.Affinity = rack.PodSpec.Affinity
	rackFullPodSpec.Tolerations = rack.PodSpec.Tolerations
	rackFullPodSpec.NodeSelector = rack.PodSpec.NodeSelector

	return rackFullPodSpec
}

// buildYAMLTemplate serializes rack.AerospikeConfig into a YAML template
// string suitable for storage as "aerospike.template.yaml".
//
// Configs provided in the legacy list format (pre-8.1.1) are first normalized
// to the new map-keyed format before marshalling, so the output is always in
// the YAML format that Aerospike Server 8.1.1+ expects.
func (r *SingleClusterReconciler) buildYAMLTemplate(rack *asdbv1.Rack) (
	string, error,
) {
	log := pkgLog.WithValues(
		"aerospikecluster", utils.ClusterNamespacedName(r.aeroCluster),
	)

	// Deep-copy so normalization does not mutate the in-memory rack config.
	configMap := validation.DeepCopyConfig(rack.AerospikeConfig.Value)
	log.V(1).Info(
		"AerospikeConfig", "config", configMap, "image", r.aeroCluster.Spec.Image,
	)

	// Normalize legacy list-format fields (namespaces, network.tls, xdr.dcs,
	// etc.) to the new map-keyed format. This is a no-op for configs that are
	// already in the new format.
	validation.NormalizeConfigFormat(configMap)

	yamlBytes, err := sigsyaml.Marshal(configMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal aerospikeConfig to YAML: %v", err)
	}

	confFile := string(yamlBytes)
	log.V(1).Info("AerospikeConfig YAML template", "yaml", confFile)

	return confFile, nil
}

// buildLegacyConfTemplate generates the legacy aerospike.template.conf content
// from rack.AerospikeConfig using the management-lib's ToConfFile serializer.
//
// If the rack config is already in the new YAML map-keyed format (server >=
// 8.1.1) it is first denormalized back to the legacy list format so that
// asconfig.NewMapAsConfig can process it correctly.
func (r *SingleClusterReconciler) buildLegacyConfTemplate(rack *asdbv1.Rack) (
	string, error,
) {
	log := pkgLog.WithValues(
		"aerospikecluster", utils.ClusterNamespacedName(r.aeroCluster),
	)

	// Deep-copy to avoid mutating the in-memory rack config.
	configMap := validation.DeepCopyConfig(rack.AerospikeConfig.Value)

	// If the config is in the new YAML map format, convert it back to the
	// legacy list format that asconfig.NewMapAsConfig expects.
	if configIsYAMLMapFormat(configMap) {
		configMap = denormalizeForConf(configMap)
	}

	asConf, err := asconfig.NewMapAsConfig(log, configMap)
	if err != nil {
		return "", fmt.Errorf("failed to load config map by lib: %v", err)
	}

	confFile := asConf.ToConfFile()
	log.V(1).Info("AerospikeConfig legacy .conf template", "conf", confFile)

	// [Backward compatibility fix for AKO 3.3.0 upgrade]
	// rack-id was historically set to 0 for all namespaces, but since AKO 3.3.0
	// it reflects actual values.  Replace rack-id with 0 in hash calculations
	// to avoid triggering unnecessary warm restarts during AKO upgrades.
	re := regexp.MustCompile(`rack-id.*\d+`)
	if rackStr := re.FindString(confFile); rackStr != "" {
		confFile = strings.ReplaceAll(confFile, rackStr, "rack-id    0")
	}

	// [Backward Compatibility Fix for AKO benchmark config changes]
	// Convert "enable-benchmarks-read true" → "enable-benchmarks-read" for hash
	// stability across AKO versions.
	for _, benchmarkConfig := range asconfig.BenchmarkConfigs {
		re := regexp.MustCompile(fmt.Sprintf(`%s.*true`, benchmarkConfig))
		if benchmarkStr := re.FindString(confFile); benchmarkStr != "" {
			confFile = strings.ReplaceAll(confFile, benchmarkStr, benchmarkConfig)
		}
	}

	return confFile, nil
}

// configIsYAMLMapFormat returns true when the aerospikeConfig uses the new
// YAML map-keyed format, detected by checking whether the namespaces field is
// a map rather than a list.
func configIsYAMLMapFormat(config map[string]interface{}) bool {
	ns := config[asdbv1.ConfKeyNamespace]
	if ns == nil {
		return false
	}

	_, isMap := ns.(map[string]interface{})

	return isMap
}

// denormalizeForConf converts the YAML map-keyed aerospikeConfig fields back
// to the legacy list-of-maps format expected by asconfig.NewMapAsConfig.
// The following fields are converted (if present):
//   - namespaces                 map → []{"name":key, ...rest}
//   - network.tls                map → []{"name":key, ...rest}
//   - xdr.dcs                    map → []{"name":key, ...rest}
//   - xdr.dcs.*.namespaces       map → []{"name":key, ...rest}
//
// The function operates on the map in-place and also returns it for
// convenience.  Order of list items is not guaranteed (map iteration).
func denormalizeForConf(config map[string]interface{}) map[string]interface{} {
	// namespaces: map → list
	if nsMap, ok := config[asdbv1.ConfKeyNamespace].(map[string]interface{}); ok {
		nsList := make([]interface{}, 0, len(nsMap))

		for name, nsVal := range nsMap {
			if nsConf, ok := nsVal.(map[string]interface{}); ok {
				nsCopy := make(map[string]interface{}, len(nsConf)+1)
				for k, v := range nsConf {
					nsCopy[k] = v
				}

				nsCopy[asdbv1.ConfKeyName] = name
				nsList = append(nsList, nsCopy)
			}
		}

		config[asdbv1.ConfKeyNamespace] = nsList
	}

	// network.tls: map → list
	if network, ok := config["network"].(map[string]interface{}); ok {
		if tlsMap, ok := network["tls"].(map[string]interface{}); ok {
			tlsList := make([]interface{}, 0, len(tlsMap))

			for name, tlsVal := range tlsMap {
				if tlsConf, ok := tlsVal.(map[string]interface{}); ok {
					tlsCopy := make(map[string]interface{}, len(tlsConf)+1)
					for k, v := range tlsConf {
						tlsCopy[k] = v
					}

					tlsCopy[asdbv1.ConfKeyName] = name
					tlsList = append(tlsList, tlsCopy)
				}
			}

			network["tls"] = tlsList
		}
	}

	// xdr.dcs: map → list, with nested namespaces also converted
	if xdr, ok := config["xdr"].(map[string]interface{}); ok {
		if dcsMap, ok := xdr["dcs"].(map[string]interface{}); ok {
			dcsList := make([]interface{}, 0, len(dcsMap))

			for name, dcVal := range dcsMap {
				if dcConf, ok := dcVal.(map[string]interface{}); ok {
					dcCopy := make(map[string]interface{}, len(dcConf)+1)
					for k, v := range dcConf {
						dcCopy[k] = v
					}

					dcCopy[asdbv1.ConfKeyName] = name

					// xdr.dcs.*.namespaces: map → list
					if nsMap, ok := dcCopy[asdbv1.ConfKeyNamespace].(map[string]interface{}); ok {
						nsList := make([]interface{}, 0, len(nsMap))

						for nsName, nsVal := range nsMap {
							if nsConf, ok := nsVal.(map[string]interface{}); ok {
								nsCopy := make(map[string]interface{}, len(nsConf)+1)
								for k, v := range nsConf {
									nsCopy[k] = v
								}

								nsCopy[asdbv1.ConfKeyName] = nsName
								nsList = append(nsList, nsCopy)
							}
						}

						dcCopy[asdbv1.ConfKeyNamespace] = nsList
					}

					dcsList = append(dcsList, dcCopy)
				}
			}

			xdr["dcs"] = dcsList
		}
	}

	return config
}

// getBaseConfData returns the basic data to be used in the config map for input aeroCluster spec.
func (r *SingleClusterReconciler) getBaseConfData(rack *asdbv1.Rack) (map[string]string, error) {
	workDir := asdbv1.GetWorkDirectory(rack.AerospikeConfig)
	volume := asdbv1.GetVolumeForAerospikePath(&rack.Storage, workDir)

	if volume != nil {
		// Init container mounts all volumes by name. Update workdir to reflect that path.
		// For example
		// volume name: aerospike-workdir
		// path: /opt/aerospike
		// config-workdir: /opt/aerospike/workdir/
		// workDir = aerospike-workdir/workdir
		workDir = "/" + volume.Name + "/" + strings.TrimPrefix(
			workDir, volume.Aerospike.Path,
		)
	}

	asConfig := r.aeroCluster.Spec.AerospikeConfig

	var serviceTLSPortParam int32
	if _, serviceTLSPort := asdbv1.GetServiceTLSNameAndPort(asConfig); serviceTLSPort != nil {
		serviceTLSPortParam = *serviceTLSPort
	}

	var servicePortParam int32
	if servicePort := asdbv1.GetServicePort(asConfig); servicePort != nil {
		servicePortParam = *servicePort
	}

	var hbTLSPortParam int32
	if _, hbTLSPort := asdbv1.GetHeartbeatTLSNameAndPort(asConfig); hbTLSPort != nil {
		hbTLSPortParam = *hbTLSPort
	}

	var hbPortParam int32
	if hbPort := asdbv1.GetHeartbeatPort(asConfig); hbPort != nil {
		hbPortParam = *hbPort
	}

	var fabricTLSPortParam int32
	if _, fabricTLSPort := asdbv1.GetFabricTLSNameAndPort(asConfig); fabricTLSPort != nil {
		fabricTLSPortParam = *fabricTLSPort
	}

	var fabricPortParam int32
	if fabricPort := asdbv1.GetFabricPort(asConfig); fabricPort != nil {
		fabricPortParam = *fabricPort
	}

	initTemplateInput := initializeTemplateInput{
		WorkDir:          workDir,
		MultiPodPerHost:  asdbv1.GetBool(r.aeroCluster.Spec.PodSpec.MultiPodPerHost),
		NetworkPolicy:    r.aeroCluster.Spec.AerospikeNetworkPolicy,
		PodPort:          servicePortParam,
		PodTLSPort:       serviceTLSPortParam,
		HeartBeatPort:    hbPortParam,
		HeartBeatTLSPort: hbTLSPortParam,
		FabricPort:       fabricPortParam,
		FabricTLSPort:    fabricTLSPortParam,
		HostNetwork:      r.aeroCluster.Spec.PodSpec.HostNetwork,
	}

	baseConfData := map[string]string{}

	for path, scriptTemplate := range scriptTemplates {
		var script bytes.Buffer

		if err := scriptTemplate.Execute(&script, initTemplateInput); err != nil {
			return nil, err
		}

		baseConfData[path] = script.String()
	}

	// Include peer list.
	peers, err := r.getFQDNsForCluster()
	if err != nil {
		return nil, err
	}

	baseConfData["peers"] = strings.Join(peers, "\n")

	return baseConfData, nil
}

func (r *SingleClusterReconciler) getFQDNsForCluster() ([]string, error) {
	podNameSet := sets.NewString()

	// The default rack is not listed in config during switchover to rack aware state.
	// Use current pod names as well.
	pods, err := r.getClusterPodList()
	if err != nil {
		return nil, err
	}

	for idx := range pods.Items {
		fqdn := getFQDNForPod(r.aeroCluster, pods.Items[idx].Name)
		podNameSet.Insert(fqdn)
	}

	rackStateList := getConfiguredRackStateList(r.aeroCluster)

	// Use all pods running or to be launched for each rack.
	for idx := range rackStateList {
		rackState := &rackStateList[idx]
		size := rackState.Size
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
			utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision))

		for i := int32(0); i < size; i++ {
			fqdn := getFQDNForPod(r.aeroCluster, getSTSPodName(stsName.Name, i))
			podNameSet.Insert(fqdn)
		}
	}

	return podNameSet.List(), nil
}

// allPodsAboveYAMLVersion returns true when every pod recorded in the cluster
// status is running an aerospike-kubernetes-init image >= 2.6.0 (the first
// init image version that reads aerospike.template.yaml instead of
// aerospike.template.conf).  It also returns true when the status has no pods
// yet (e.g. before the first reconcile creates any).
//
// When this returns false at least one pod has an older init image that can
// only read aerospike.template.conf, so the legacy template must be kept in
// the ConfigMap.
func (r *SingleClusterReconciler) allPodsAboveYAMLVersion() bool {
	for _, podStatus := range r.aeroCluster.Status.Pods {
		if podStatus.InitImage == "" {
			// No init image recorded for this pod; assume old init for safety.
			return false
		}

		version, err := asdbv1.GetImageVersion(podStatus.InitImage)
		if err != nil {
			// Cannot determine the version; assume old init for safety.
			return false
		}

		cmp, err := lib.CompareVersions(version, minYAMLInitVersion)
		if err != nil || cmp < 0 {
			return false
		}
	}

	return true
}

func (r *SingleClusterReconciler) deleteRackConfigMap(namespacedName types.NamespacedName) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}

	if err := r.Delete(context.TODO(), configMap); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info(
				"Can't find rack configmap while trying to delete it. Skipping...",
				"configmap", namespacedName.Name,
			)

			return nil
		}

		return fmt.Errorf("failed to delete rack configmap for pod %s: %v", namespacedName.Name, err)
	}

	return nil
}
