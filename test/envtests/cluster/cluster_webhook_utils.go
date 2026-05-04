package cluster

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
	lib "github.com/aerospike/aerospike-management-lib"
)

const (
	aerospikeConfigVolName = "aerospike-config-secret"
	workDir                = "/opt/aerospike"
	otherStorageClass      = "other-storage-class"
)

func uniqueNamespacedName(suffix string) types.NamespacedName {
	name := fmt.Sprintf("envtests-%s", suffix)

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func getStorageSpecForDevice(devicePath string) asdbv1.AerospikeStorageSpec {
	initM := asdbv1.AerospikeVolumeMethodDeleteFiles

	return asdbv1.AerospikeStorageSpec{
		BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputCascadeDelete: ptr.To(false),
		},
		FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
			InputInitMethod:    &initM,
			InputCascadeDelete: ptr.To(false),
		},
		Volumes: []asdbv1.VolumeSpec{
			{
				Name: "ns",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeBlock,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: devicePath,
				},
			},
			{
				Name: "workdir",
				Source: asdbv1.VolumeSource{
					PersistentVolume: &asdbv1.PersistentVolumeSpec{
						Size:         resource.MustParse("1Gi"),
						StorageClass: testutil.StorageClass,
						VolumeMode:   corev1.PersistentVolumeFilesystem,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: workDir,
				},
			},
			{
				Name: aerospikeConfigVolName,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: test.AerospikeSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret",
				},
			},
		},
	}
}

// isYAMLFormatImage reports whether the server image requires the new YAML
// map-keyed aerospikeConfig format (server >= 8.1.1).
func isYAMLFormatImage(image string) bool {
	version, err := asdbv1.GetImageVersion(image)
	if err != nil {
		return false
	}

	cmp, err := lib.CompareVersions(version, testutil.MinYAMLServerVersion)
	if err != nil {
		return false
	}

	return cmp >= 0
}

// buildNSConfig assembles one or more namespace config maps into the value
// appropriate for aerospikeConfig["namespaces"] based on the server image:
//   - server < 8.1.1  →  []interface{} list  (each map must have a "name" key)
//   - server >= 8.1.1 →  map[string]interface{} keyed by namespace name
//
// The "name" key is stripped from each map in the new format.
func buildNSConfig(image string, nsMaps ...map[string]interface{}) interface{} {
	if isYAMLFormatImage(image) {
		result := make(map[string]interface{}, len(nsMaps))

		for _, nsMap := range nsMaps {
			name, _ := nsMap[asdbv1.ConfKeyName].(string)
			inner := make(map[string]interface{}, len(nsMap))

			for k, v := range nsMap {
				if k != asdbv1.ConfKeyName {
					inner[k] = v
				}
			}

			result[name] = inner
		}

		return result
	}

	list := make([]interface{}, len(nsMaps))
	for i, m := range nsMaps {
		list[i] = m
	}

	return list
}

// getFirstNSMutableRef returns the name and a mutable reference to the first
// namespace's inner config map, regardless of whether namespaces is stored as
// a list (legacy) or a map (new YAML format).
//
// For list format:  the returned map IS the slice element (a reference).
//
//	Field mutations propagate automatically to the parent config.
//
// For map format:   the returned map IS the value in the namespaces map.
//
//	Field mutations also propagate automatically.
//
// Use this when you only need to modify existing fields.  If you are replacing
// the entire namespace config, call setFirstNSConf instead.
func getFirstNSMutableRef(config map[string]interface{}) (nsName string, nsConf map[string]interface{}) {
	switch ns := config[asdbv1.ConfKeyNamespace].(type) {
	case []interface{}:
		if len(ns) > 0 {
			if m, ok := ns[0].(map[string]interface{}); ok {
				name, _ := m[asdbv1.ConfKeyName].(string)
				return name, m
			}
		}
	case map[string]interface{}:
		for k, v := range ns {
			if m, ok := v.(map[string]interface{}); ok {
				return k, m
			}
		}
	}

	return "", nil
}

// setFirstNSConf replaces the first namespace entry in config["namespaces"],
// handling both list and map formats.  nsConf must contain a "name" key.
func setFirstNSConf(config map[string]interface{}, nsConf map[string]interface{}) {
	name, _ := nsConf[asdbv1.ConfKeyName].(string)

	switch ns := config[asdbv1.ConfKeyNamespace].(type) {
	case []interface{}:
		if len(ns) > 0 {
			ns[0] = nsConf
		}
	case map[string]interface{}:
		inner := make(map[string]interface{}, len(nsConf))

		for k, v := range nsConf {
			if k != asdbv1.ConfKeyName {
				inner[k] = v
			}
		}

		ns[name] = inner
	}
}

// appendNSConf appends a namespace config map to config["namespaces"],
// handling both list and map formats.  nsConf must contain a "name" key.
func appendNSConf(config map[string]interface{}, nsConf map[string]interface{}) {
	name, _ := nsConf[asdbv1.ConfKeyName].(string)

	switch ns := config[asdbv1.ConfKeyNamespace].(type) {
	case []interface{}:
		config[asdbv1.ConfKeyNamespace] = append(ns, nsConf)
	case map[string]interface{}:
		inner := make(map[string]interface{}, len(nsConf))

		for k, v := range nsConf {
			if k != asdbv1.ConfKeyName {
				inner[k] = v
			}
		}

		ns[name] = inner
	}
}

// setNSRFInConfig sets the replication-factor of a named namespace inside
// config["namespaces"], handling both list and map formats.
func setNSRFInConfig(config map[string]interface{}, nsName string, rf int) {
	switch ns := config[asdbv1.ConfKeyNamespace].(type) {
	case []interface{}:
		for i, item := range ns {
			m, ok := item.(map[string]interface{})
			if ok && m[asdbv1.ConfKeyName] == nsName {
				m[asdbv1.ConfKeyReplicationFactor] = rf
				ns[i] = m

				return
			}
		}
	case map[string]interface{}:
		if m, ok := ns[nsName].(map[string]interface{}); ok {
			m[asdbv1.ConfKeyReplicationFactor] = rf
		}
	}
}

// networkTLSConfigForImage builds a network config with TLS entries using the
// format appropriate for the server image:
//   - server < 8.1.1  →  "tls" is a []interface{} list (each entry has "name")
//   - server >= 8.1.1 →  "tls" is a map[string]interface{} keyed by TLS name
func networkTLSConfigForImage(image string) map[string]interface{} {
	const tlsName = "aerospike-a-0.test-runner"

	tlsFields := map[string]interface{}{
		"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
		"key-file":  "/etc/aerospike/secret/svc_key.pem",
		"ca-file":   "/etc/aerospike/secret/cacert.pem",
	}

	var tls interface{}

	if isYAMLFormatImage(image) {
		tls = map[string]interface{}{tlsName: tlsFields}
	} else {
		entry := make(map[string]interface{}, len(tlsFields)+1)
		entry["name"] = tlsName

		for k, v := range tlsFields {
			entry[k] = v
		}

		tls = []interface{}{entry}
	}

	return map[string]interface{}{
		"service": map[string]interface{}{
			"tls-name": tlsName,
			"tls-port": 4333,
			"port":     3000,
		},
		"fabric": map[string]interface{}{
			"tls-name": tlsName,
			"tls-port": 3011,
			"port":     3001,
		},
		"heartbeat": map[string]interface{}{
			"tls-name": tlsName,
			"tls-port": 3012,
			"port":     3002,
		},
		"tls": tls,
	}
}

// rackNSOverride returns an InputAerospikeConfig that overrides the "test"
// namespace for a rack, using the format appropriate for the server image.
func rackNSOverride(devicePath, image string) *asdbv1.AerospikeConfigSpec {
	nsMap := map[string]interface{}{
		asdbv1.ConfKeyName:   "test",
		"replication-factor": 2,
		"strong-consistency": true,
		asdbv1.ConfKeyStorageEngine: map[string]interface{}{
			"type":    "device",
			"devices": []interface{}{devicePath},
		},
	}

	return &asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			asdbv1.ConfKeyNamespace: buildNSConfig(image, nsMap),
		},
	}
}

func deleteCluster(ctx context.Context, nsName types.NamespacedName) {
	aeroCluster := &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsName.Name,
			Namespace: nsName.Namespace,
		},
	}
	// Delete the cluster after each test
	Expect(testCluster.DeleteCluster(envtests.K8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
}

func patchFirstPVStorageClassSpec(storage *asdbv1.AerospikeStorageSpec) {
	for i := range storage.Volumes {
		if storage.Volumes[i].Source.PersistentVolume != nil {
			storage.Volumes[i].Source.PersistentVolume.StorageClass = otherStorageClass

			return
		}
	}

	Fail("no PersistentVolume volume found in storage spec")
}
