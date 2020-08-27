package admission

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"time"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

var scheme *runtime.Scheme

var (
	aerospikeGroupName = "aerospike.com"
)

const (
	aerospikeCluster = "aerospikeclusters"
)

var (
	aerospikeClusterCRDName = fmt.Sprintf("%s.%s", aerospikeCluster, aerospikev1alpha1.SchemeGroupVersion.Group)
)

var (
	// AerospikeClusterValidationWebhookPath  validation webhook path
	AerospikeClusterValidationWebhookPath = "/admission/reviews/aerospikeclusters/validating"
	// AerospikeClusterMutationWebhookPath mutation webhook path
	AerospikeClusterMutationWebhookPath = "/admission/reviews/aerospikeclusters/mutating"
	aerospikeOperatorWebhookName        = fmt.Sprintf("aerospike-cluster-webhook.%s", aerospikeGroupName)
	failurePolicy                       = admissionregistrationv1beta1.Fail
)

const (
	// serviceName is the name of the service used to expose the webhook.
	serviceName = "aerospike-cluster-webhook"
	// tlsSecretName is the name of the secret that will hold tls artifacts used
	// by the webhook.
	tlsSecretName = "aerospike-cluster-webhook-tls"
	// whReadyTimeout is the time to wait until the validating webhook service
	// endpoints are ready
	whReadyTimeout = time.Second * 30
	// default rackID
	defaultRackID = 1000000
)

var pkglog = log.New(log.Ctx{"module": "admissionWebhook"})

const baseVersion = "4.6.0.2"

// SetupSecret setup secret, it doesn't create object
func SetupSecret(certDir, namespace string) error {
	// generate the certificate to use when registering and serving the webhook
	svc := fmt.Sprintf("%s.%s.svc", serviceName, namespace)
	now := time.Now()
	crt := x509.Certificate{
		Subject:               pkix.Name{CommonName: svc},
		NotBefore:             now,
		NotAfter:              now.Add(365 * 24 * time.Hour),
		SerialNumber:          big.NewInt(now.Unix()),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		DNSNames:              []string{svc},
	}
	// generate the private key to use when registering and serving the webhook
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	// pem-encode the private key
	keyBytes := pem.EncodeToMemory(&pem.Block{
		Type:  keyutil.RSAPrivateKeyBlockType,
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	// self-sign the generated certificate using the private key
	sig, err := x509.CreateCertificate(rand.Reader, &crt, &crt, key.Public(), key)
	if err != nil {
		return err
	}
	// pem-encode the signed certificate
	sigBytes := pem.EncodeToMemory(&pem.Block{
		Type:  cert.CertificateBlockType,
		Bytes: sig,
	})

	certFileName := filepath.Join(certDir, v1.TLSCertKey)
	keyFileName := filepath.Join(certDir, v1.TLSPrivateKeyKey)

	os.MkdirAll(filepath.Dir(certFileName), os.FileMode(0777))
	err = ioutil.WriteFile(certFileName, sigBytes, os.FileMode(0777))
	if err != nil {
		return err
	}

	os.MkdirAll(filepath.Dir(keyFileName), os.FileMode(0777))
	err = ioutil.WriteFile(keyFileName, keyBytes, os.FileMode(0777))
	if err != nil {
		return err
	}
	return nil
}
