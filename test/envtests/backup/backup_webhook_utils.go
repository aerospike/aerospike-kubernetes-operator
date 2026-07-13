package backup

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	name := fmt.Sprintf("envtests-%s", suffix)

	return test.GetNamespacedName(name, testutil.DefaultNamespace)
}

func buildBackupCR(backupNsNm, absNsNm types.NamespacedName) *asdbv1beta1.AerospikeBackup {
	prefix := asdbv1beta1.NamePrefix(backupNsNm)
	configBytes := backupconfig.MustMarshalConfig(backupconfig.BackupCRConfig(
		prefix, backupconfig.DefaultClusterHost, backupconfig.EnvtestRoutineCrons))

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
