package v1beta1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v2/pkg/dto"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

func namespacedName(obj client.Object) string {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}.String()
}

func getK8sClient() (client.Client, error) {
	restConfig := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	utilRuntime.Must(asdbv1.AddToScheme(scheme))
	utilRuntime.Must(clientGoScheme.AddToScheme(scheme))
	utilRuntime.Must(AddToScheme(scheme))

	cl, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return cl, nil
}

func getBackupServiceFullConfig(name, namespace string) (*dto.Config, error) {
	var backupSvcConfigMap corev1.ConfigMap

	cl, gErr := getK8sClient()
	if gErr != nil {
		return nil, gErr
	}

	if err := cl.Get(context.TODO(),
		types.NamespacedName{Name: name, Namespace: namespace},
		&backupSvcConfigMap); err != nil {
		return nil, err
	}

	var backupSvcConfig dto.Config

	if err := yaml.UnmarshalStrict([]byte(backupSvcConfigMap.Data[BackupServiceConfigYAML]),
		&backupSvcConfig); err != nil {
		return nil, err
	}

	return &backupSvcConfig, nil
}
