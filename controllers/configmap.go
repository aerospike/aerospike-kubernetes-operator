package controllers

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

var pkgLog = ctrl.Log.WithName("lib.asconfig")

const (
	// AerospikeTemplateConfFileName is the name of the aerospike conf template
	AerospikeTemplateConfFileName = "aerospike.template.conf"

	// NetworkPolicyHashFileName stores the network policy hash
	NetworkPolicyHashFileName = "networkPolicyHash"

	// PodSpecHashFileName stores the pod spec hash
	PodSpecHashFileName = "podSpecHash"

	// AerospikeConfHashFileName stores the Aerospike config hash
	AerospikeConfHashFileName = "aerospikeConfHash"
)

type initializeTemplateInput struct {
	WorkDir         string
	MultiPodPerHost bool
	NetworkPolicy   asdbv1beta1.AerospikeNetworkPolicy
	PodPort         int32
	PodTLSPort      int32
	HostNetwork     bool
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

// CreateConfigMapData create configMap data
func (r *AerospikeClusterReconciler) CreateConfigMapData(
	aeroCluster *asdbv1beta1.AerospikeCluster, rack asdbv1beta1.Rack,
) (map[string]string, error) {
	// Add config template
	confTemp, err := r.buildConfigTemplate(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build config template: %v", err)
	}

	// Add conf file
	confData, err := r.getBaseConfData(aeroCluster, rack)
	if err != nil {
		return nil, fmt.Errorf("failed to build config template: %v", err)
	}
	confData[AerospikeTemplateConfFileName] = confTemp

	// Add conf hash
	confHash, err := utils.GetHash(confTemp)
	if err != nil {
		return nil, err
	}
	confData[AerospikeConfHashFileName] = confHash

	// Add networkPolicy hash
	policy := aeroCluster.Spec.AerospikeNetworkPolicy
	policyStr, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	policyHash, err := utils.GetHash(string(policyStr))
	if err != nil {
		return nil, err
	}
	confData[NetworkPolicyHashFileName] = policyHash

	// Add podSpec hash
	podSpec, err := creatPodSpecForRack(aeroCluster, rack)
	if err != nil {
		return nil, err
	}
	podSpecStr, err := json.Marshal(podSpec)
	if err != nil {
		return nil, err
	}
	podSpecHash, err := utils.GetHash(string(podSpecStr))
	if err != nil {
		return nil, err
	}
	confData[PodSpecHashFileName] = podSpecHash

	return confData, nil
}

func creatPodSpecForRack(
	aeroCluster *asdbv1beta1.AerospikeCluster, rack asdbv1beta1.Rack,
) (*asdbv1beta1.AerospikePodSpec, error) {
	rackFullPodSpec := asdbv1beta1.AerospikePodSpec{}
	if err := lib.DeepCopy(
		&rackFullPodSpec, &aeroCluster.Spec.PodSpec,
	); err != nil {
		return nil, err
	}

	if rack.PodSpec.Affinity != nil {
		rackFullPodSpec.Affinity = rack.PodSpec.Affinity
	}
	if rack.PodSpec.Tolerations != nil {
		rackFullPodSpec.Tolerations = rack.PodSpec.Tolerations
	}
	if rack.PodSpec.NodeSelector != nil {
		rackFullPodSpec.NodeSelector = rack.PodSpec.NodeSelector
	}
	return &rackFullPodSpec, nil
}

func (r *AerospikeClusterReconciler) buildConfigTemplate(
	aeroCluster *asdbv1beta1.AerospikeCluster, rack asdbv1beta1.Rack,
) (string, error) {
	log := pkgLog.WithValues(
		"aerospikecluster", utils.ClusterNamespacedName(aeroCluster),
	)

	version := strings.Split(aeroCluster.Spec.Image, ":")

	configMap := rack.AerospikeConfig.Value
	log.V(1).Info(
		"AerospikeConfig", "config", configMap, "image", aeroCluster.Spec.Image,
	)

	asConf, err := asconfig.NewMapAsConfig(version[1], configMap)
	if err != nil {
		return "", fmt.Errorf("failed to load config map by lib: %v", err)
	}

	// No need for asConf version validation, it's already validated in admission webhook

	confFile := asConf.ToConfFile()
	log.V(1).Info("AerospikeConfig", "conf", confFile)

	return confFile, nil
}

// getBaseConfData returns the basic data to be used in the config map for input aeroCluster spec.
func (r *AerospikeClusterReconciler) getBaseConfData(
	aeroCluster *asdbv1beta1.AerospikeCluster, rack asdbv1beta1.Rack,
) (map[string]string, error) {
	workDir := asdbv1beta1.GetWorkDirectory(rack.AerospikeConfig)
	volume := rack.Storage.GetVolumeForAerospikePath(workDir)

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

	// Include initialization and restart scripts
	_, tlsPort := asdbv1beta1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig)
	initializeTemplateInput := initializeTemplateInput{
		WorkDir:         workDir,
		MultiPodPerHost: aeroCluster.Spec.PodSpec.MultiPodPerHost,
		NetworkPolicy:   aeroCluster.Spec.AerospikeNetworkPolicy,
		PodPort:         int32(asdbv1beta1.GetServicePort(aeroCluster.Spec.AerospikeConfig)),
		PodTLSPort:      int32(tlsPort),
		HostNetwork:     aeroCluster.Spec.PodSpec.HostNetwork,
	}

	baseConfData := map[string]string{}
	for path, scriptTemplate := range scriptTemplates {
		var script bytes.Buffer
		err := scriptTemplate.Execute(&script, initializeTemplateInput)
		if err != nil {
			return nil, err
		}

		baseConfData[path] = script.String()
	}

	// Include peer list.
	peers, err := r.getFQDNsForCluster(aeroCluster)
	if err != nil {
		return nil, err
	}

	baseConfData["peers"] = strings.Join(peers, "\n")
	return baseConfData, nil
}

func (r *AerospikeClusterReconciler) getFQDNsForCluster(aeroCluster *asdbv1beta1.AerospikeCluster) (
	[]string, error,
) {

	podNameMap := make(map[string]bool)

	// The default rack is not listed in config during switchover to rack aware state.
	// Use current pod names as well.
	pods, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		fqdn := getFQDNForPod(aeroCluster, pod.Name)
		podNameMap[fqdn] = true
	}

	podNames := make([]string, 0)
	rackStateList := getConfiguredRackStateList(aeroCluster)

	// Use all pods running or to be launched for each rack.
	for _, rackState := range rackStateList {
		size := rackState.Size
		stsName := getNamespacedNameForSTS(aeroCluster, rackState.Rack.ID)
		for i := 0; i < size; i++ {
			fqdn := getFQDNForPod(
				aeroCluster,
				getSTSPodName(stsName.Name, int32(i)),
			)
			podNameMap[fqdn] = true
		}
	}

	for fqdn := range podNameMap {
		podNames = append(podNames, fqdn)
	}

	return podNames, nil
}
