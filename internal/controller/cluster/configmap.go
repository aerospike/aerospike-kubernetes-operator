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

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

var pkgLog = ctrl.Log.WithName("lib.asconfig")

const (
	// aerospikeTemplateConfFileName is the name of the aerospike conf template
	aerospikeTemplateConfFileName = "aerospike.template.conf"

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

// createConfigMapData create configMap data
func (r *SingleClusterReconciler) createConfigMapData(rack *asdbv1.Rack) (
	map[string]string, error,
) {
	// Add config template
	confTemp, err := r.buildConfigTemplate(rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build config template: %v", err)
	}

	// Add conf file
	confData, err := r.getBaseConfData(rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build config template: %v", err)
	}

	confData[aerospikeTemplateConfFileName] = confTemp

	// [Backward compatibility fix for AKO 3.3.0 upgrade]
	// rack-id was historically set to 0 for all namespaces, but since the AKO 3.3.0, it reflects actual values.
	// This change led to hash mismatches during the AKO 3.3.0 upgrade, triggering unnecessary warm restarts.
	// Solution: Replace real rack-id with 0 in hash calculations to avoid this issue.
	re := regexp.MustCompile(`rack-id.*\d+`)
	if rackStr := re.FindString(confTemp); rackStr != "" {
		confTemp = strings.ReplaceAll(confTemp, rackStr, "rack-id    0")
	}

	// [Backward Compatibility Fix]
	// Historically, all benchmark configurations were represented as single literal fields in the configuration.
	// The presence of these fields indicated that the corresponding benchmark was enabled.
	// However, AER-6767(https://aerospike.atlassian.net/browse/AER-6767) restricted a two-literal benchmark
	// configuration format (e.g., "enable-benchmarks-read true").
	// Handling this change leads to hash mismatches during AKO upgrades, leading to unnecessary warm restarts.
	// Solution: For hash calculations, replace the new benchmark configuration format with the old format
	// to maintain compatibility and prevent these issues.
	// Example: Convert "enable-benchmarks-read true" to "enable-benchmarks-read".
	for _, benchmarkConfig := range asconfig.BenchmarkConfigs {
		re := regexp.MustCompile(fmt.Sprintf(`%s.*true`, benchmarkConfig))
		if benchmarkStr := re.FindString(confTemp); benchmarkStr != "" {
			confTemp = strings.ReplaceAll(confTemp, benchmarkStr, benchmarkConfig)
		}
	}

	// Add conf hash
	confHash, err := utils.GetHash(confTemp)
	if err != nil {
		return nil, err
	}

	confData[aerospikeConfHashFileName] = confHash

	// Add networkPolicy hash
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

	// Add podSpec hash
	podSpec := createPodSpecForRack(r.aeroCluster, rack)

	podSpecStr, err := json.Marshal(podSpec)
	if err != nil {
		return nil, err
	}

	// [Backward compatibility fix for AKO 2.1.0 upgrade]
	// This is a newly introduced field in 2.1.0.
	// Ignore empty value from hash computation so that on upgrade clusters are
	// not rolling restarted.
	podSpecStr = []byte(strings.ReplaceAll(
		string(podSpecStr), "\"aerospikeInitContainer\":{},", "",
	))

	// [Backward compatibility fix for AKO 3.3.0 upgrade]
	// This field is changed from bool type to *bool type in 3.3.0
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

func (r *SingleClusterReconciler) buildConfigTemplate(rack *asdbv1.Rack) (
	string, error,
) {
	log := pkgLog.WithValues(
		"aerospikecluster", utils.ClusterNamespacedName(r.aeroCluster),
	)

	configMap := rack.AerospikeConfig.Value
	log.V(1).Info(
		"AerospikeConfig", "config", configMap, "image",
		r.aeroCluster.Spec.Image,
	)

	asConf, err := asconfig.NewMapAsConfig(r.Log, configMap)
	if err != nil {
		return "", fmt.Errorf("failed to load config map by lib: %v", err)
	}

	// No need for asConf version validation, it's already validated in admission webhook

	confFile := asConf.ToConfFile()
	log.V(1).Info("AerospikeConfig", "conf", confFile)

	return confFile, nil
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
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rackState.Rack.ID)

		for i := int32(0); i < size; i++ {
			fqdn := getFQDNForPod(r.aeroCluster, getSTSPodName(stsName.Name, i))
			podNameSet.Insert(fqdn)
		}
	}

	return podNameSet.List(), nil
}

func (r *SingleClusterReconciler) deleteRackConfigMap(namespacedName types.NamespacedName) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}

	if err := r.Client.Delete(context.TODO(), configMap); err != nil {
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
