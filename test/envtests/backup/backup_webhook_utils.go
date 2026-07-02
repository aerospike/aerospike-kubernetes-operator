package backup

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	name := fmt.Sprintf("envtests-%s", suffix)

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func namePrefix(nsNm types.NamespacedName) string {
	return asdbv1beta1.NamePrefix(nsNm)
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

func backupConfigMap(backupNsNm types.NamespacedName) map[string]interface{} {
	prefix := namePrefix(backupNsNm)
	clusterName := fmt.Sprintf("%s-test-cluster", prefix)
	routineName := fmt.Sprintf("%s-test-routine", prefix)

	return map[string]interface{}{
		asdbv1beta1.AerospikeClusterKey: map[string]interface{}{
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
		},
		asdbv1beta1.BackupRoutinesKey: map[string]interface{}{
			routineName: map[string]interface{}{
				"backup-policy":      "test-policy",
				"interval-cron":      "@daily",
				"incr-interval-cron": "@hourly",
				"namespaces":         []string{"test"},
				"source-cluster":     clusterName,
				"storage":            "local",
			},
		},
	}
}

func routineName(backupNsNm types.NamespacedName) string {
	return fmt.Sprintf("%s-test-routine", namePrefix(backupNsNm))
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

	return envtests.K8sClient.Create(ctx, configMap)
}

func newBackup(backupNsNm, absNsNm types.NamespacedName) *asdbv1beta1.AerospikeBackup {
	configBytes := marshalConfig(backupConfigMap(backupNsNm))

	return &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupNsNm.Name,
			Namespace: backupNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      absNsNm.Name,
				Namespace: absNsNm.Namespace,
			},
			Config: runtime.RawExtension{
				Raw: configBytes,
			},
		},
	}
}

func syncBackupStatus(ctx context.Context, backup *asdbv1beta1.AerospikeBackup) error {
	backup.Status = asdbv1beta1.AerospikeBackupStatus{
		BackupService: backup.Spec.BackupService,
		Config:        backup.Spec.Config,
	}

	return envtests.K8sClient.Status().Update(ctx, backup)
}

func deleteBackup(ctx context.Context, backupNsNm types.NamespacedName) {
	backup := &asdbv1beta1.AerospikeBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupNsNm.Name,
			Namespace: backupNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(envtests.K8sClient.Delete(ctx, backup))
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
