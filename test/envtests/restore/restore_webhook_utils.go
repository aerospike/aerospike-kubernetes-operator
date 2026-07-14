package restore

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/fixtures/backupconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

func uniqueNamespacedName(suffix string) types.NamespacedName {
	name := fmt.Sprintf("envtests-restore-%s", suffix)

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
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
					"host-name": backupconfig.DefaultClusterHost,
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
	case asdbv1beta1.Full, asdbv1beta1.Incremental:
		return backupconfig.MustMarshalConfig(minimalRestoreConfigMap())
	case asdbv1beta1.Timestamp:
		return backupconfig.MustMarshalConfig(minimalTimestampRestoreConfigMap())
	default:
		return backupconfig.MustMarshalConfig(minimalRestoreConfigMap())
	}
}

func buildRestoreCR(
	restoreNsNm, absNsNm types.NamespacedName,
	restoreType asdbv1beta1.RestoreType,
) *asdbv1beta1.AerospikeRestore {
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
