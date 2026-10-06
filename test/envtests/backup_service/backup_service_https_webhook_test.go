package backupservice

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/fixtures/backupconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

// The cert and key paths are only checked by the backup service at startup, not by the webhook,
// so these paths do not need to exist.
func httpsListener(extra map[string]interface{}) map[string]interface{} {
	listener := map[string]interface{}{
		"port":      8443,
		"cert-file": "/etc/abs-tls/tls.crt",
		"key-file":  "/etc/abs-tls/tls.key",
	}

	for key, value := range extra {
		listener[key] = value
	}

	return listener
}

func backupServiceWithListeners(absNsNm types.NamespacedName, service map[string]interface{},
	certSpec *asdbv1beta1.OperatorClientCertSpec,
) *asdbv1beta1.AerospikeBackupService {
	config := backupconfig.BackupServiceBaseConfig()
	config[asdbv1beta1.ServiceKey] = service

	backupService := buildBackupServiceCR(absNsNm)
	backupService.Spec.Config = runtime.RawExtension{Raw: backupconfig.MustMarshalConfig(config)}
	backupService.Spec.OperatorClientCert = certSpec

	return backupService
}

func operatorCertSpec(clientCert bool) *asdbv1beta1.OperatorClientCertSpec {
	spec := &asdbv1beta1.OperatorClientCertSpec{
		SecretCertSource: asdbv1beta1.SecretCertSource{SecretName: "operator-tls", CaCertsFilename: "ca.crt"},
	}

	if clientCert {
		spec.SecretCertSource.ClientCertFilename = "tls.crt"
		spec.SecretCertSource.ClientKeyFilename = "tls.key"
	}

	return spec
}

var _ = Describe("AerospikeBackupService HTTPS listener validation", func() {
	ctx := context.TODO()

	var absNsNm types.NamespacedName

	BeforeEach(func() {
		absNsNm = uniqueNamespacedName("backup-service-https")
	})

	AfterEach(func() {
		deleteBackupService(ctx, absNsNm)
	})

	expectWebhookError := func(backupService *asdbv1beta1.AerospikeBackupService, substrings ...string) {
		GinkgoHelper()

		err := envtests.K8sClient.Create(ctx, backupService)
		Expect(err).To(HaveOccurred())
		envtests.NewStatusErrorMatcher().
			WithMessageSubstrings(append([]string{testutil.BackupServiceWebhookErrorPrefix}, substrings...)...).
			Validate(err)
	}

	Context("Deploy validation", func() {
		Context("spec.config.service", func() {
			Context("positive", func() {
				It("accepts service.https alongside service.http without operatorClientCert", func() {
					backupService := backupServiceWithListeners(absNsNm, map[string]interface{}{
						"http":  map[string]interface{}{"port": 8081},
						"https": httpsListener(nil),
					}, nil)
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())
				})
			})

			Context("negative", func() {
				DescribeTable("rejects invalid listener config",
					func(service map[string]interface{}, errSubstr string) {
						expectWebhookError(backupServiceWithListeners(absNsNm, service, nil), errSubstr)
					},
					Entry("http disabled without operatorClientCert",
						map[string]interface{}{
							"http":  map[string]interface{}{"disabled": true},
							"https": httpsListener(nil),
						}, "service.http is disabled"),
					Entry("both listeners disabled",
						map[string]interface{}{
							"http":  map[string]interface{}{"disabled": true},
							"https": httpsListener(map[string]interface{}{"disabled": true}),
						}, "cannot both be disabled"),
					Entry("both listeners on the same port",
						map[string]interface{}{
							"http":  map[string]interface{}{"port": 8443},
							"https": httpsListener(nil),
						}, "cannot use the same port"),
					Entry("https without key-file",
						map[string]interface{}{"https": map[string]interface{}{"cert-file": "/etc/abs-tls/tls.crt"}},
						"key-file"),
				)
			})
		})

		Context("spec.operatorClientCert", func() {
			Context("positive", func() {
				It("accepts HTTPS only with operatorClientCert", func() {
					backupService := backupServiceWithListeners(absNsNm, map[string]interface{}{
						"http":  map[string]interface{}{"disabled": true},
						"https": httpsListener(nil),
					}, operatorCertSpec(false))
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())
				})

				It("accepts require-and-verify when operatorClientCert has a client cert", func() {
					backupService := backupServiceWithListeners(absNsNm, map[string]interface{}{
						"https": httpsListener(map[string]interface{}{
							"client-ca-file": "/etc/abs-tls/ca.crt", "client-auth": "require-and-verify",
						}),
					}, operatorCertSpec(true))
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())
				})
			})

			Context("negative", func() {
				It("rejects operatorClientCert without service.https", func() {
					expectWebhookError(backupServiceWithListeners(absNsNm, map[string]interface{}{
						"http": map[string]interface{}{"port": 8081},
					}, operatorCertSpec(false)), "service.https is not enabled")
				})

				It("rejects require-and-verify without an operator client cert", func() {
					expectWebhookError(backupServiceWithListeners(absNsNm, map[string]interface{}{
						"https": httpsListener(map[string]interface{}{
							"client-ca-file": "/etc/abs-tls/ca.crt", "client-auth": "require-and-verify",
						}),
					}, operatorCertSpec(false)), "require-and-verify")
				})

				It("rejects a client cert without a client key", func() {
					certSpec := operatorCertSpec(false)
					certSpec.SecretCertSource.ClientCertFilename = "tls.crt"

					expectWebhookError(backupServiceWithListeners(absNsNm, map[string]interface{}{
						"https": httpsListener(nil),
					}, certSpec), "must be set together")
				})

				It("rejects empty secretName (MinLength=1)", func() {
					certSpec := operatorCertSpec(false)
					certSpec.SecretCertSource.SecretName = ""

					err := envtests.K8sClient.Create(ctx, backupServiceWithListeners(absNsNm, map[string]interface{}{
						"https": httpsListener(nil),
					}, certSpec))
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceCRDSchemaErrorPrefix, "secretName").
						Validate(err)
				})
			})
		})
	})

	Context("Status validation", func() {
		Context("status.scheme", func() {
			Context("negative", func() {
				It("rejects invalid scheme value (Enum)", func() {
					backupService := buildBackupServiceCR(absNsNm)
					Expect(envtests.K8sClient.Create(ctx, backupService)).To(Succeed())

					backupService.Status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted
					backupService.Status.Scheme = corev1.URIScheme("FTP")
					err := envtests.K8sClient.Status().Update(ctx, backupService)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.BackupServiceCRDSchemaErrorPrefix, "scheme", "Unsupported value").
						Validate(err)
				})
			})
		})
	})
})
