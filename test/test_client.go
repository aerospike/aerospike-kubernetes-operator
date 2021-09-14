package test

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	"github.com/go-logr/logr"
	"io/ioutil"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
// TODO duplicated from controller_helper
type FromSecretPasswordProvider struct {
	// Client to read secrets.
	k8sClient *client.Client

	// The secret namespace.
	namespace string
}

// Get returns the password for the username using userSpec.
func (pp FromSecretPasswordProvider) Get(
	_ string, userSpec *asdbv1beta1.AerospikeUserSpec,
) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := (*pp.k8sClient).Get(
		context.TODO(),
		types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret,
	)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %v", secretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf(
			"failed to get password from secret. Please check your secret %s",
			secretName,
		)
	}
	return string(passbyte), nil
}

func getPasswordProvider(
	aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{
		k8sClient: &k8sClient, namespace: aeroCluster.Namespace,
	}
}

func getClient(
	log logr.Logger, aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) (*as.Client, error) {
	pp := getPasswordProvider(aeroCluster, k8sClient)
	statusToSpec, err := asdbv1beta1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		return nil, err
	}

	username, password, err := aerospikecluster.AerospikeAdminCredentials(
		&aeroCluster.Spec, statusToSpec, &pp,
	)

	if err != nil {
		return nil, err
	}

	return getClientForUser(log, username, password, aeroCluster, k8sClient)
}

// TODO: username, password not used. check the use of this function
func getClientForUser(log logr.Logger, username string, password string, aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client) (*as.Client, error) {
	conns, err := newAllHostConn(log, aeroCluster, k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %v", err)
	}
	var hosts []*as.Host
	for _, conn := range conns {
		hosts = append(
			hosts, &as.Host{
				Name:    conn.ASConn.AerospikeHostName,
				TLSName: conn.ASConn.AerospikeTLSName,
				Port:    conn.ASConn.AerospikePort,
			},
		)
	}
	// Create policy using status, status has current connection info
	aeroClient, err := as.NewClientWithPolicyAndHost(
		getClientPolicy(
			aeroCluster, k8sClient,
		), hosts...,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create aerospike cluster client: %v", err,
		)
	}

	return aeroClient, nil
}

func getServiceTLSName(aeroCluster *asdbv1beta1.AerospikeCluster) string {
	if networkConfTmp, ok := aeroCluster.Spec.AerospikeConfig.Value["network"]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		if _, ok := networkConf["service"]; !ok {
			// Service section will be missing if the spec is not obtained from server but is generated locally from test code.
			return ""
		}
		if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
			return tlsName.(string)
		}
	}
	return ""
}

func getClientPolicy(
	aeroCluster *asdbv1beta1.AerospikeCluster, k8sClient client.Client,
) *as.ClientPolicy {

	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// tls config
	if tlsName := getServiceTLSName(aeroCluster); tlsName != "" {
		// r.Log.V(1).Info("Set tls config in aerospike client policy")
		clientCertSpec := aeroCluster.Spec.OperatorClientCertSpec
		tlsConf := tls.Config{
			RootCAs: getClusterServerPool(
				clientCertSpec, aeroCluster.Namespace, k8sClient,
			),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			// InsecureSkipVerify: true,
		}
		if clientCertSpec == nil || !clientCertSpec.IsClientCertConfigured() {
			//This is possible when tls-authenticate-client = false
			// r.Log.Info("Operator's client cert is not configured. Skip using client certs.", "clientCertSpec", clientCertSpec)
		} else if cert, err := getClientCertificate(
			clientCertSpec, aeroCluster.Namespace, k8sClient,
		); err == nil {
			tlsConf.Certificates = append(tlsConf.Certificates, *cert)
		} else {
			// r.Log.Error(err, "Failed to get client certificate. Using basic clientPolicy")
		}

		policy.TlsConfig = &tlsConf
	}

	statusToSpec, err := asdbv1beta1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		// r.Log.Error(err, "Failed to copy spec in status", "err", err)
	}

	user, pass, err := aerospikecluster.AerospikeAdminCredentials(
		&aeroCluster.Spec, statusToSpec,
		getPasswordProvider(aeroCluster, k8sClient),
	)
	if err != nil {
		// r.Log.Error(err, "Failed to get cluster auth info", "err", err)
	}
	// TODO: What should be the timeout, should make it configurable or just keep it default
	policy.Timeout = time.Minute * 1
	policy.User = user
	policy.Password = pass
	return policy
}

func getClusterServerPool(
	clientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec,
	clusterNamespace string, k8sClient client.Client,
) *x509.CertPool {
	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		// r.Log.Info("Warn: Failed to add system certificates to the pool", "err", err)
		serverPool = x509.NewCertPool()
	}
	if clientCertSpec == nil {
		// r.Log.Info("OperatorClientCertSpec is not configured. Using default system CA certs...")
		return serverPool
	}

	if clientCertSpec.CertPathInOperator != nil {
		return appendCACertFromFile(
			clientCertSpec.CertPathInOperator.CaCertsPath, serverPool,
		)
	} else if clientCertSpec.SecretCertSource != nil {
		return appendCACertFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace, serverPool,
			k8sClient,
		)
	} else {
		// r.Log.Error(fmt.Errorf("both SecrtenName and CertPathInOperator are not set"), "Returning empty certPool.")
		return serverPool
	}
}

func getClientCertificate(
	clientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec,
	clusterNamespace string, k8sClient client.Client,
) (*tls.Certificate, error) {
	if clientCertSpec.CertPathInOperator != nil {
		return loadCertAndKeyFromFiles(
			clientCertSpec.CertPathInOperator.ClientCertPath,
			clientCertSpec.CertPathInOperator.ClientKeyPath,
		)
	} else if clientCertSpec.SecretCertSource != nil {
		return loadCertAndKeyFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace, k8sClient,
		)
	} else {
		return nil, fmt.Errorf("both SecrtenName and CertPathInOperator are not set")
	}
}

func appendCACertFromFile(
	caPath string, serverPool *x509.CertPool,
) *x509.CertPool {
	if caPath == "" {
		// r.Log.Info("CA path is not provided in \"operatorClientCertSpec\". Using default system CA certs...")
	} else if caData, err := ioutil.ReadFile(caPath); err != nil {
		// r.Log.Error(err, "Failed to load CA certs from file.", "ca-path", caPath)
	} else {
		serverPool.AppendCertsFromPEM(caData)
		// r.Log.Info("Loaded CA root certs from file.", "ca-path", caPath)
	}
	return serverPool
}

func appendCACertFromSecret(
	secretSource *asdbv1beta1.AerospikeSecretCertSource,
	defaultNamespace string, serverPool *x509.CertPool, k8sClient client.Client,
) *x509.CertPool {
	if secretSource.CaCertsFilename == "" {
		// r.Log.Info("CaCertsFilename is not specified. Using default CA certs...", "secret", secretSource)
		return serverPool
	}
	// get the tls info from secret
	// r.Log.Info("Trying to find an appropriate CA cert from the secret...", "secret", secretSource)
	found := &v1.Secret{}
	secretName := namespacedSecret(secretSource, defaultNamespace)
	if err := k8sClient.Get(context.TODO(), secretName, found); err != nil {
		// r.Log.Error(err, "Failed to get secret certificates to the pool, returning empty certPool", "secret", secretName)
		return serverPool
	}
	if caData, ok := found.Data[secretSource.CaCertsFilename]; ok {
		// r.Log.V(1).Info("Adding cert to tls serverpool from the secret.", "secret", secretName)
		serverPool.AppendCertsFromPEM(caData)
	} else {
		// r.Log.V(1).Info("WARN: Can't find ca-file in the secret. using default certPool.",
		// "secret", secretName, "ca-file", secretSource.CaCertsFilename)
	}
	return serverPool
}

func loadCertAndKeyFromSecret(
	secretSource *asdbv1beta1.AerospikeSecretCertSource,
	defaultNamespace string, k8sClient client.Client,
) (*tls.Certificate, error) {
	// get the tls info from secret
	found := &v1.Secret{}
	secretName := namespacedSecret(secretSource, defaultNamespace)
	if err := k8sClient.Get(context.TODO(), secretName, found); err != nil {
		// r.Log.Info("Warn: Failed to get secret certificates to the pool", "err", err)
		return nil, err
	}
	if crtData, crtExists := found.Data[secretSource.ClientCertFilename]; !crtExists {
		return nil, fmt.Errorf(
			"can't find certificate \"%s\" in secret %+v",
			secretSource.ClientCertFilename, secretName,
		)
	} else if keyData, keyExists := found.Data[secretSource.ClientKeyFilename]; !keyExists {
		return nil, fmt.Errorf(
			"can't find certificate \"%s\" in secret %+v",
			secretSource.ClientKeyFilename, secretName,
		)
	} else if cert, err := tls.X509KeyPair(crtData, keyData); err != nil {
		return nil, fmt.Errorf(
			"failed to load X509 key pair for cluster from secret %+v: %w",
			secretName, err,
		)
	} else {
		// r.Log.Info("Loading Aerospike Cluster client cert from secret", "secret", secretName)
		return &cert, nil
	}
}

func namespacedSecret(
	secretSource *asdbv1beta1.AerospikeSecretCertSource,
	defaultNamespace string,
) types.NamespacedName {
	if secretSource.SecretNamespace == "" {
		return types.NamespacedName{
			Name: secretSource.SecretName, Namespace: defaultNamespace,
		}
	} else {
		return types.NamespacedName{
			Name:      secretSource.SecretName,
			Namespace: secretSource.SecretNamespace,
		}
	}
}

func loadCertAndKeyFromFiles(certPath string, keyPath string) (
	*tls.Certificate, error,
) {
	certData, certErr := ioutil.ReadFile(certPath)
	if certErr != nil {
		return nil, certErr
	}
	keyData, keyErr := ioutil.ReadFile(keyPath)
	if keyErr != nil {
		return nil, keyErr
	}
	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load X509 key pair for cluster (cert=%s,key=%s): %w",
			certPath, keyPath, err,
		)
	}
	// r.Log.Info("Loading Aerospike Cluster client cert from files.", "cert-path", certPath, "key-path", keyPath)
	return &cert, nil
}
