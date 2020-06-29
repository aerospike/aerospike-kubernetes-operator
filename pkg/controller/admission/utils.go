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
	"strconv"
	"strings"
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

// CompareVersions compares Aerospike Server versions
// if version1 == version2 returns 0
// else if version1 < version2 returns -1
// else returns 1
func compareVersions(version1, version2 string) (int, error) {
	if len(version1) == 0 || len(version2) == 0 {
		return 0, fmt.Errorf("Wrong versions to compare")
	}

	if version1 == version2 {
		return 0, nil
	}

	// Ignoring extra comment tag... found in git source code build
	v1 := strings.Split(version1, "-")[0]
	v2 := strings.Split(version2, "-")[0]

	if v1 == v2 {
		return 0, nil
	}

	verElems1 := strings.Split(v1, ".")
	verElems2 := strings.Split(v2, ".")

	minLen := len(verElems1)
	if len(verElems2) < minLen {
		minLen = len(verElems2)
	}

	for i := 0; i < minLen; i++ {
		ve1, err := strconv.Atoi(verElems1[i])
		if err != nil {
			return 0, fmt.Errorf("Wrong version to compare")
		}
		ve2, err := strconv.Atoi(verElems2[i])
		if err != nil {
			return 0, fmt.Errorf("Wrong version to compare")
		}

		if ve1 > ve2 {
			return 1, nil
		} else if ve1 < ve2 {
			return -1, nil
		}
	}

	if len(verElems1) > len(verElems2) {
		return 1, nil
	}

	if len(verElems1) < len(verElems2) {
		return -1, nil
	}

	return 0, nil
}

// Webhook example

// func main() {
// 	var disableWebhookConfigInstaller bool
// 	flag.BoolVar(&disableWebhookConfigInstaller, "disable-webhook-config-installer", false,
// 		"disable the installer in the webhook server, so it won't install webhook configuration resources during bootstrapping")

// 	flag.Parse()
// 	logf.SetLogger(logf.ZapLogger(false))
// 	entryLog := log.WithName("entrypoint")

// 	// Setup a Manager
// 	entryLog.Info("setting up manager")
// 	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
// 	if err != nil {
// 		entryLog.Error(err, "unable to set up overall controller manager")
// 		os.Exit(1)
// 	}

// 	// Setup a new controller to Reconciler ReplicaSets
// 	entryLog.Info("Setting up controller")
// 	c, err := controller.New("foo-controller", mgr, controller.Options{
// 		Reconciler: &reconcileReplicaSet{client: mgr.GetClient(), log: log.WithName("reconciler")},
// 	})
// 	if err != nil {
// 		entryLog.Error(err, "unable to set up individual controller")
// 		os.Exit(1)
// 	}

// 	// Watch ReplicaSets and enqueue ReplicaSet object key
// 	if err := c.Watch(&source.Kind{Type: &appsv1.ReplicaSet{}}, &handler.EnqueueRequestForObject{}); err != nil {
// 		entryLog.Error(err, "unable to watch ReplicaSets")
// 		os.Exit(1)
// 	}

// 	// Watch Pods and enqueue owning ReplicaSet key
// 	if err := c.Watch(&source.Kind{Type: &corev1.Pod{}},
// 		&handler.EnqueueRequestForOwner{OwnerType: &appsv1.ReplicaSet{}, IsController: true}); err != nil {
// 		entryLog.Error(err, "unable to watch Pods")
// 		os.Exit(1)
// 	}

// 	// Setup webhooks
// 	entryLog.Info("setting up webhooks")
// 	mutatingWebhook, err := builder.NewWebhookBuilder().
// 		Name("mutating.k8s.io").
// 		Mutating().
// 		Operations(admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update).
// 		WithManager(mgr).
// 		ForType(&corev1.Pod{}).
// 		Handlers(&podAnnotator{}).
// 		Build()
// 	if err != nil {
// 		entryLog.Error(err, "unable to setup mutating webhook")
// 		os.Exit(1)
// 	}

// 	validatingWebhook, err := builder.NewWebhookBuilder().
// 		Name("validating.k8s.io").
// 		Validating().
// 		Operations(admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update).
// 		WithManager(mgr).
// 		ForType(&corev1.Pod{}).
// 		Handlers(&podValidator{}).
// 		Build()
// 	if err != nil {
// 		entryLog.Error(err, "unable to setup validating webhook")
// 		os.Exit(1)
// 	}

// 	entryLog.Info("setting up webhook server")
// 	as, err := webhook.NewServer("foo-admission-server", mgr, webhook.ServerOptions{
// 		Port:                          9876,
// 		CertDir:                       "/tmp/cert",
// 		DisableWebhookConfigInstaller: &disableWebhookConfigInstaller,
// 		BootstrapOptions: &webhook.BootstrapOptions{
// 			Secret: &apitypes.NamespacedName{
// 				Namespace: "default",
// 				Name:      "foo-admission-server-secret",
// 			},

// 			Service: &webhook.Service{
// 				Namespace: "default",
// 				Name:      "foo-admission-server-service",
// 				// Selectors should select the pods that runs this webhook server.
// 				Selectors: map[string]string{
// 					"app": "foo-admission-server",
// 				},
// 			},
// 		},
// 	})
// 	if err != nil {
// 		entryLog.Error(err, "unable to create a new webhook server")
// 		os.Exit(1)
// 	}

// 	entryLog.Info("registering webhooks to the webhook server")
// 	err = as.Register(mutatingWebhook, validatingWebhook)
// 	if err != nil {
// 		entryLog.Error(err, "unable to register webhooks in the admission server")
// 		os.Exit(1)
// 	}

// 	entryLog.Info("starting manager")
// 	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
// 		entryLog.Error(err, "unable to run manager")
// 		os.Exit(1)
// 	}
// }
