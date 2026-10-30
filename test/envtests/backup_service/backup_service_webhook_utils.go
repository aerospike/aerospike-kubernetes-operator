package backupservice

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

// backupServiceWithHTTPS returns a backup service that serves only HTTPS, with the given operator client cert.
// The webhook checks that the cert and key paths are in a mounted Secret; the files themselves are read by the
// backup service at startup, so the Secret need not exist.
func backupServiceWithHTTPS(absNsNm types.NamespacedName,
	certSpec *asdbv1beta1.OperatorClientCertSpec,
) *asdbv1beta1.AerospikeBackupService {
	config := backupconfig.BackupServiceBaseConfig()
	config[asdbv1beta1.ServiceKey] = map[string]interface{}{
		"http": map[string]interface{}{"disabled": true},
		"https": map[string]interface{}{
			"port": 8443, "cert-file": "/etc/abs-tls/tls.crt", "key-file": "/etc/abs-tls/tls.key",
		},
	}

	backupService := buildBackupServiceCR(absNsNm)
	backupService.Spec.Config = runtime.RawExtension{Raw: backupconfig.MustMarshalConfig(config)}
	backupService.Spec.OperatorClientCert = certSpec
	backupService.Spec.SecretMounts = []asdbv1beta1.SecretMount{{
		SecretName:  "abs-tls",
		VolumeMount: corev1.VolumeMount{Name: "abs-tls", MountPath: "/etc/abs-tls", ReadOnly: true},
	}}

	return backupService
}

func operatorCertSpec() *asdbv1beta1.OperatorClientCertSpec {
	return &asdbv1beta1.OperatorClientCertSpec{
		SecretCertSource: asdbv1beta1.SecretCertSource{SecretName: "operator-tls", CaCertsFilename: "ca.crt"},
	}
}
