package restore

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

const backupServiceImage = "aerospike/aerospike-backup-service:3.5.0"

func uniqueNamespacedName(suffix string) types.NamespacedName {
	name := fmt.Sprintf("envtests-restore-%s", suffix)

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func backupServiceConfigMap() map[string]interface{} {
	return map[string]interface{}{
		asdbv1beta1.ServiceKey: map[string]interface{}{
			"http": map[string]interface{}{
				"port": 8081,
			},
		},
		asdbv1beta1.BackupPoliciesKey: map[string]interface{}{
			"test-policy": map[string]interface{}{
				"parallel": 3,
			},
		},
		asdbv1beta1.StorageKey: map[string]interface{}{
			"local": map[string]interface{}{
				"local-storage": map[string]interface{}{
					"path": "/tmp/localStorage",
				},
			},
		},
	}
}

func minimalRestoreConfigMap() map[string]interface{} {
	return map[string]interface{}{
		"destination": map[string]interface{}{
			"label": "destinationCluster",
			"credentials": map[string]interface{}{
				"password": "admin123",
				"user":     "admin",
			},
			"seed-nodes": []map[string]interface{}{
				{
					"host-name": "aerocluster.test.svc.cluster.local",
					"port":      3000,
				},
			},
		},
		"policy": map[string]interface{}{
			"parallel":      3,
			"no-generation": true,
			"no-indexes":    true,
		},
		"source": map[string]interface{}{
			"local-storage": map[string]interface{}{
				"path": "/tmp/localStorage",
			},
		},
		"backup-data-path": "/tmp/backup-data",
	}
}

func marshalConfig(config map[string]interface{}) []byte {
	configBytes, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}

	return configBytes
}

func createStubBackupService(ctx context.Context, absNsNm types.NamespacedName) error {
	configBytes := marshalConfig(backupServiceConfigMap())

	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupServiceSpec{
			Image: backupServiceImage,
			Config: runtime.RawExtension{
				Raw: configBytes,
			},
		},
	}

	if err := envtests.K8sClient.Create(ctx, backupService); err != nil {
		return err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
		Data: map[string]string{
			asdbv1beta1.BackupServiceConfigYAML: string(configBytes),
		},
	}

	if err := envtests.K8sClient.Create(ctx, configMap); err != nil {
		return err
	}

	// Restore webhook validation reads merged backup-service config from the ConfigMap.
	// Routines are normally added by the backup controller after a Backup CR is created.
	return patchBackupServiceConfigMapWithRoutine(ctx, absNsNm)
}

func patchBackupServiceConfigMapWithRoutine(ctx context.Context, absNsNm types.NamespacedName) error {
	configMap := &corev1.ConfigMap{}
	if err := envtests.K8sClient.Get(ctx, absNsNm, configMap); err != nil {
		return err
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configMap.Data[asdbv1beta1.BackupServiceConfigYAML]), &config); err != nil {
		return err
	}

	const clusterName = "test-cluster"

	config[asdbv1beta1.AerospikeClustersKey] = map[string]interface{}{
		clusterName: map[string]interface{}{
			"credentials": map[string]interface{}{
				"password": "admin123",
				"user":     "admin",
			},
			"seed-nodes": []map[string]interface{}{
				{
					"host-name": "aerocluster.test.svc.cluster.local",
					"port":      3000,
				},
			},
		},
	}
	config[asdbv1beta1.BackupRoutinesKey] = map[string]interface{}{
		"test-routine": map[string]interface{}{
			"backup-policy":      "test-policy",
			"interval-cron":      "@daily",
			"incr-interval-cron": "@hourly",
			"namespaces":         []string{"test"},
			"source-cluster":     clusterName,
			"storage":            "local",
		},
	}

	updatedConfigBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	configMap.Data[asdbv1beta1.BackupServiceConfigYAML] = string(updatedConfigBytes)

	return envtests.K8sClient.Update(ctx, configMap)
}

func minimalTimestampRestoreConfigMap() map[string]interface{} {
	base := minimalRestoreConfigMap()

	return map[string]interface{}{
		"destination":          base["destination"],
		"policy":               base["policy"],
		asdbv1beta1.RoutineKey: "test-routine",
		asdbv1beta1.TimeKey:    int64(1722408895094),
	}
}

func restoreConfigBytes(restoreType asdbv1beta1.RestoreType) []byte {
	switch restoreType {
	case asdbv1beta1.Timestamp:
		return marshalConfig(minimalTimestampRestoreConfigMap())
	default:
		return marshalConfig(minimalRestoreConfigMap())
	}
}

func newRestore(restoreNsNm, absNsNm types.NamespacedName, restoreType asdbv1beta1.RestoreType) *asdbv1beta1.AerospikeRestore {
	return &asdbv1beta1.AerospikeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreNsNm.Name,
			Namespace: restoreNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeRestoreSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      absNsNm.Name,
				Namespace: absNsNm.Namespace,
			},
			Type: restoreType,
			Config: runtime.RawExtension{
				Raw: restoreConfigBytes(restoreType),
			},
		},
	}
}

// createRestoreOmittingPollingPeriod creates a restore without pollingPeriod in the request.
// Typed clients marshal zero metav1.Duration as "0s", which prevents the CRD default from applying.
func createRestoreOmittingPollingPeriod(
	ctx context.Context,
	restoreNsNm, absNsNm types.NamespacedName,
	restoreType asdbv1beta1.RestoreType,
) error {
	var config interface{}
	if err := json.Unmarshal(restoreConfigBytes(restoreType), &config); err != nil {
		return err
	}

	restore := &unstructured.Unstructured{}
	restore.SetGroupVersionKind(asdbv1beta1.GroupVersion.WithKind("AerospikeRestore"))
	restore.SetName(restoreNsNm.Name)
	restore.SetNamespace(restoreNsNm.Namespace)
	restore.Object["spec"] = map[string]interface{}{
		"backupService": map[string]interface{}{
			"name":      absNsNm.Name,
			"namespace": absNsNm.Namespace,
		},
		"type":   string(restoreType),
		"config": config,
	}

	return envtests.K8sClient.Create(ctx, restore)
}

func deleteRestore(ctx context.Context, restoreNsNm types.NamespacedName) {
	restore := &asdbv1beta1.AerospikeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreNsNm.Name,
			Namespace: restoreNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(envtests.K8sClient.Delete(ctx, restore))
}

func deleteBackupService(ctx context.Context, absNsNm types.NamespacedName) {
	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(envtests.K8sClient.Delete(ctx, backupService))

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(envtests.K8sClient.Delete(ctx, configMap))
}
