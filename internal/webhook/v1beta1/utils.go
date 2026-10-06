package v1beta1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto/decoder"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/redact"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

// validateNoSecretPlaceholder rejects the "[secret]" placeholder in any secret field of value.
// The backup service API shows literal secrets as "[secret]", and on its REST API that value means "keep the
// stored secret". The operator hands the config to the backup service as a file, which is loaded as written,
// so a "[secret]" copied from the API would become the actual password or key and fail at runtime.
// MergeSecrets against an empty value of the same type has no stored secret to keep, so it returns an error
// for any placeholder, in every secret field the backup service defines.
func validateNoSecretPlaceholder(value, empty any) error {
	if err := decoder.MergeSecrets(value, empty); err != nil {
		return fmt.Errorf("config contains %q, the placeholder the backup service API shows in place of "+
			"real secrets; set the real value or a secrets: reference", redact.Placeholder)
	}

	return nil
}

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
	utilRuntime.Must(asdbv1beta1.AddToScheme(scheme))

	cl, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return cl, nil
}

func getBackupServiceFullConfig(k8sClient client.Client, name, namespace string) (*dto.Config, error) {
	var backupSvcConfigMap corev1.ConfigMap

	if err := k8sClient.Get(context.TODO(),
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
