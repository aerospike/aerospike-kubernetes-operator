package backupconfig

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

// CreateStubBackupService creates a stub AerospikeBackupService and its ConfigMap.
// The ABS CR spec always uses the base config. When configMapConfig is nil, the ConfigMap uses
// the same base config; otherwise configMapConfig is written to the ConfigMap (e.g. merged config for restore).
func CreateStubBackupService(
	ctx context.Context,
	cl client.Client,
	absNsNm types.NamespacedName,
	configMapConfig map[string]interface{},
) error {
	baseConfig := BackupServiceBaseConfig()

	baseConfigBytes, err := MarshalConfig(baseConfig)
	if err != nil {
		return err
	}

	cmConfig := configMapConfig
	if cmConfig == nil {
		cmConfig = baseConfig
	}

	cmConfigBytes, err := MarshalConfig(cmConfig)
	if err != nil {
		return err
	}

	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeBackupServiceSpec{
			Image: BackupServiceImage,
			Config: runtime.RawExtension{
				Raw: baseConfigBytes,
			},
		},
	}

	if err := cl.Create(ctx, backupService); err != nil {
		return err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
		Data: map[string]string{
			asdbv1beta1.BackupServiceConfigYAML: string(cmConfigBytes),
		},
	}

	return cl.Create(ctx, configMap)
}

// DeleteStubBackupService deletes a stub AerospikeBackupService and its ConfigMap.
func DeleteStubBackupService(ctx context.Context, cl client.Client, absNsNm types.NamespacedName) {
	backupService := &asdbv1beta1.AerospikeBackupService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(cl.Delete(ctx, backupService))

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      absNsNm.Name,
			Namespace: absNsNm.Namespace,
		},
	}
	_ = client.IgnoreNotFound(cl.Delete(ctx, configMap))
}
