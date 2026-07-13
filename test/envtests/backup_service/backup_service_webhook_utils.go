package backupservice

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

func buildBackupServiceCR(absNsNm types.NamespacedName) *asdbv1beta1.AerospikeBackupService {
	return &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupServiceSpec{
			Image: backupconfig.BackupServiceImage,
			Config: runtime.RawExtension{
				Raw: backupconfig.MustMarshalConfig(backupconfig.BackupServiceBaseConfig()),
			},
		},
	}
}

func deleteBackupService(ctx context.Context, absNsNm types.NamespacedName) {
	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(envtests.K8sClient.Delete(ctx, backupService))
}
