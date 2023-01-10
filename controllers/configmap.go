package controllers

import (
	"bytes"
	"context"
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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	WorkDir          string
	MultiPodPerHost  bool
	NetworkPolicy    asdbv1beta1.AerospikeNetworkPolicy
	PodPort          int32
	PodTLSPort       int32
	HeartBeatPort    int32
	HeartBeatTlsPort int32
	FabricPort       int32
	FabricTlsPort    int32
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

// CreateConfigMapData create configMap data
func (r *SingleClusterReconciler) CreateConfigMapData(rack asdbv1beta1.Rack) (
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
	confData[AerospikeTemplateConfFileName] = confTemp

	// Add conf hash
	confHash, err := utils.GetHash(confTemp)
	if err != nil {
		return nil, err
	}
	confData[AerospikeConfHashFileName] = confHash

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
	confData[NetworkPolicyHashFileName] = policyHash

	// Add podSpec hash
	podSpec, err := createPodSpecForRack(r.aeroCluster, rack)
	if err != nil {
		return nil, err
	}
	podSpecStr, err := json.Marshal(podSpec)
	if err != nil {
		return nil, err
	}

	// This is a newly introduced field in 2.1.0.
	// Ignore empty value from hash computation so that on upgrade clusters are
	// not rolling restarted.
	podSpecStr = []byte(strings.ReplaceAll(
		string(podSpecStr), "\"aerospikeInitContainer\":{},", "",
	))

	podSpecHash, err := utils.GetHash(string(podSpecStr))
	if err != nil {
		return nil, err
	}
	confData[PodSpecHashFileName] = podSpecHash

	return confData, nil
}

func createPodSpecForRack(
	aeroCluster *asdbv1beta1.AerospikeCluster, rack asdbv1beta1.Rack,
) (*asdbv1beta1.AerospikePodSpec, error) {
	rackFullPodSpec := asdbv1beta1.AerospikePodSpec{}
	lib.DeepCopy(
		&rackFullPodSpec, &aeroCluster.Spec.PodSpec,
	)

	rackFullPodSpec.Affinity = rack.PodSpec.Affinity
	rackFullPodSpec.Tolerations = rack.PodSpec.Tolerations
	rackFullPodSpec.NodeSelector = rack.PodSpec.NodeSelector

	return &rackFullPodSpec, nil
}

func (r *SingleClusterReconciler) buildConfigTemplate(rack asdbv1beta1.Rack) (
	string, error,
) {
	log := pkgLog.WithValues(
		"aerospikecluster", utils.ClusterNamespacedName(r.aeroCluster),
	)

	version := strings.Split(r.aeroCluster.Spec.Image, ":")

	configMap := rack.AerospikeConfig.Value
	log.V(1).Info(
		"AerospikeConfig", "config", configMap, "image",
		r.aeroCluster.Spec.Image,
	)

	asConf, err := asconfig.NewMapAsConfig(r.Log, version[1], configMap)
	if err != nil {
		return "", fmt.Errorf("failed to load config map by lib: %v", err)
	}

	// No need for asConf version validation, it's already validated in admission webhook

	confFile := asConf.ToConfFile()
	log.V(1).Info("AerospikeConfig", "conf", confFile)

	return confFile, nil
}

// getBaseConfData returns the basic data to be used in the config map for input aeroCluster spec.
func (r *SingleClusterReconciler) getBaseConfData(rack asdbv1beta1.Rack) (
	map[string]string, error,
) {
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

	asConfig := r.aeroCluster.Spec.AerospikeConfig
	var serviceTlsPortParam int32
	if _, serviceTlsPort := asdbv1beta1.GetServiceTLSNameAndPort(asConfig); serviceTlsPort != nil {
		serviceTlsPortParam = int32(*serviceTlsPort)
	}

	var servicePortParam int32
	if servicePort := asdbv1beta1.GetServicePort(asConfig); servicePort != nil {
		servicePortParam = int32(*servicePort)
	}

	var hbTlsPortParam int32
	if _, hbTlsPort := asdbv1beta1.GetHeartbeatTLSNameAndPort(asConfig); hbTlsPort != nil {
		hbTlsPortParam = int32(*hbTlsPort)
	}

	var hbPortParam int32
	if hbPort := asdbv1beta1.GetHeartbeatPort(asConfig); hbPort != nil {
		hbPortParam = int32(*hbPort)
	}

	var fabricTlsPortParam int32
	if _, fabricTlsPort := asdbv1beta1.GetFabricTLSNameAndPort(asConfig); fabricTlsPort != nil {
		fabricTlsPortParam = int32(*fabricTlsPort)
	}

	var fabricPortParam int32
	if fabricPort := asdbv1beta1.GetFabricPort(asConfig); fabricPort != nil {
		fabricPortParam = int32(*fabricPort)
	}

	initializeTemplateInput := initializeTemplateInput{
		WorkDir:          workDir,
		MultiPodPerHost:  r.aeroCluster.Spec.PodSpec.MultiPodPerHost,
		NetworkPolicy:    r.aeroCluster.Spec.AerospikeNetworkPolicy,
		PodPort:          servicePortParam,
		PodTLSPort:       serviceTlsPortParam,
		HeartBeatPort:    hbPortParam,
		HeartBeatTlsPort: hbTlsPortParam,
		FabricPort:       fabricPortParam,
		FabricTlsPort:    fabricTlsPortParam,
		HostNetwork:      r.aeroCluster.Spec.PodSpec.HostNetwork,
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
	peers, err := r.getFQDNsForCluster()
	if err != nil {
		return nil, err
	}

	baseConfData["peers"] = strings.Join(peers, "\n")
	return baseConfData, nil
}

func (r *SingleClusterReconciler) getFQDNsForCluster() ([]string, error) {

	podNameMap := make(map[string]bool)

	// The default rack is not listed in config during switchover to rack aware state.
	// Use current pod names as well.
	pods, err := r.getClusterPodList()
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		fqdn := getFQDNForPod(r.aeroCluster, pod.Name)
		podNameMap[fqdn] = true
	}

	podNames := make([]string, 0)
	rackStateList := getConfiguredRackStateList(r.aeroCluster)

	// Use all pods running or to be launched for each rack.
	for _, rackState := range rackStateList {
		size := rackState.Size
		stsName := getNamespacedNameForSTS(r.aeroCluster, rackState.Rack.ID)
		for i := 0; i < size; i++ {
			fqdn := getFQDNForPod(
				r.aeroCluster,
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
