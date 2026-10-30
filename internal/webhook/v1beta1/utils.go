package v1beta1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

func namespacedName(obj client.Object) string {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}.String()
}

func getBackupServiceFullConfig(reader client.Reader, name, namespace string) (*dto.Config, error) {
	var backupSvcConfigMap corev1.ConfigMap

	if err := reader.Get(context.TODO(),
		types.NamespacedName{Name: name, Namespace: namespace},
		&backupSvcConfigMap); err != nil {
		return nil, err
	}

	var backupSvcConfig dto.Config

	if err := yaml.Unmarshal([]byte(backupSvcConfigMap.Data[asdbv1beta1.BackupServiceConfigYAML]),
		&backupSvcConfig); err != nil {
		return nil, err
	}

	return &backupSvcConfig, nil
}
