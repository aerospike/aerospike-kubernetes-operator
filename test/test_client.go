package test

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"
	as "github.com/ashishshinde/aerospike-client-go/v6"
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
	_ string, userSpec *asdbv1.AerospikeUserSpec,
) (string, error) {
	secret := &v1.Secret{}
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
	aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client,
) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{
		k8sClient: &k8sClient, namespace: aeroCluster.Namespace,
	}
}

func getClient(
	log logr.Logger, aeroCluster *asdbv1.AerospikeCluster,
	k8sClient client.Client,
) (*as.Client, error) {
	conns, err := newAllHostConn(log, aeroCluster, k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %v", err)
	}

	hosts := make([]*as.Host, 0, len(conns))
	for connIndex := range conns {
		hosts = append(
			hosts, &as.Host{
				Name:    conns[connIndex].ASConn.AerospikeHostName,
				TLSName: conns[connIndex].ASConn.AerospikeTLSName,
				Port:    conns[connIndex].ASConn.AerospikePort,
			},
		)
	}
	// Create policy using status, status has current connection info
	policy := getClientPolicy(
		aeroCluster, k8sClient,
	)
	aeroClient, err := as.NewClientWithPolicyAndHost(
		policy, hosts...,
	)

	if aeroClient == nil {
		return nil, fmt.Errorf(
			"failed to create aerospike cluster client: %v", err,
		)
	}

	return aeroClient, nil
}

// getClientExternalAuth returns an Aerospike client using external
// authentication user.
func getClientExternalAuth(
	log logr.Logger, aeroCluster *asdbv1.AerospikeCluster,
	k8sClient client.Client, ldapUser string, ldapPassword string,
) (*as.Client, error) {
	conns, err := newAllHostConn(log, aeroCluster, k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %v", err)
	}

	hosts := make([]*as.Host, 0, len(conns))
	for connIndex := range conns {
		hosts = append(
			hosts, &as.Host{
				Name:    conns[connIndex].ASConn.AerospikeHostName,
				TLSName: conns[connIndex].ASConn.AerospikeTLSName,
				Port:    conns[connIndex].ASConn.AerospikePort,
			},
		)
	}
	// Create policy using status, status has current connection info
	policy := getClientPolicy(
		aeroCluster, k8sClient,
	)
	policy.User = ldapUser
	policy.Password = ldapPassword
	policy.AuthMode = as.AuthModeExternal
	aeroClient, err := as.NewClientWithPolicyAndHost(
		policy, hosts...,
	)

	if aeroClient == nil {
		return nil, fmt.Errorf(
			"failed to create aerospike cluster client: %v", err,
		)
	}

	return aeroClient, nil
}

func getServiceTLSName(aeroCluster *asdbv1.AerospikeCluster) string {
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
	aeroCluster *asdbv1.AerospikeCluster, k8sClient client.Client,
) *as.ClientPolicy {
	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// Pod services take time to come up.
	// do not fail the client if not connected.
	policy.FailIfNotConnected = false

	tlsName := getServiceTLSName(aeroCluster)

	networkType := asdbv1.AerospikeNetworkType(*defaultNetworkType)
	if tlsName != "" {
		if aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType != networkType &&
			aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType == networkType {
			policy.UseServicesAlternate = true
		}
	} else {
		if aeroCluster.Spec.AerospikeNetworkPolicy.AccessType != networkType &&
			aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType == networkType {
			policy.UseServicesAlternate = true
		}
	}

	// tls config
	if tlsName != "" {
		logrus.Info("Set tls config in aerospike client policy")

		clientCertSpec := aeroCluster.Spec.OperatorClientCertSpec
		tlsConf := tls.Config{
			RootCAs: getClusterServerPool(
				clientCertSpec, aeroCluster.Namespace, k8sClient,
			),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			MinVersion:               tls.VersionTLS12,
			// used only in testing
			// InsecureSkipVerify: true,
		}

		if clientCertSpec != nil && clientCertSpec.IsClientCertConfigured() {
			if cert, err := getClientCertificate(
				clientCertSpec, aeroCluster.Namespace, k8sClient,
			); err == nil {
				tlsConf.Certificates = append(tlsConf.Certificates, *cert)
			} else {
				logrus.Error(err, "Failed to get client certificate. Using basic clientPolicy")
			}
		}

		policy.TlsConfig = &tlsConf
	}

	statusToSpec, err := asdbv1.CopyStatusToSpec(&aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		logrus.Error("Failed to copy spec in status", "err: ", err)
	}

	user, pass, err := aerospikecluster.AerospikeAdminCredentials(
		&aeroCluster.Spec, statusToSpec,
		getPasswordProvider(aeroCluster, k8sClient),
	)
	if err != nil {
		logrus.Error("Failed to get cluster auth info", "err: ", err)
	}
	// TODO: What should be the timeout, should make it configurable or just keep it default
	policy.Timeout = time.Minute * 1
	policy.User = user
	policy.Password = pass

	return policy
}

func getClusterServerPool(
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
	clusterNamespace string, k8sClient client.Client,
) *x509.CertPool {
	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		serverPool = x509.NewCertPool()
	}

	if clientCertSpec == nil {
		return serverPool
	}

	if clientCertSpec.CertPathInOperator != nil || clientCertSpec.SecretCertSource != nil {
		if clientCertSpec.CertPathInOperator != nil {
			return appendCACertFromFileOrPath(
				clientCertSpec.CertPathInOperator.CaCertsPath, serverPool,
			)
		}

		return appendCACertFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace, serverPool,
			k8sClient,
		)
	}

	return serverPool
}

func getClientCertificate(
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
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
	}

	return nil, fmt.Errorf("both SecrtenName and CertPathInOperator are not set")
}

func appendCACertFromFileOrPath(
	caPath string, serverPool *x509.CertPool,
) *x509.CertPool {
	if caPath == "" {
		return serverPool
	}

	err := filepath.WalkDir(
		caPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				var caData []byte
				if caData, err = os.ReadFile(path); err != nil {
					return err
				}
				serverPool.AppendCertsFromPEM(caData)
			}
			return nil
		},
	)

	if err != nil {
		logrus.Info("\"Failed to load CA certs from dir", "caPath: ", caPath)
	}

	return serverPool
}

func appendCACertFromSecret(
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string, serverPool *x509.CertPool, k8sClient client.Client,
) *x509.CertPool {
	if secretSource.CaCertsFilename == "" && secretSource.CaCertsSource == nil {
		return serverPool
	}
	// get the tls info from secret
	logrus.Info("Trying to find an appropriate CA cert from the secret...", "secret: ", secretSource)

	found := &v1.Secret{}

	if secretSource.CaCertsSource != nil {
		secretName := namespacedSecret(secretSource.CaCertsSource.SecretNamespace,
			secretSource.CaCertsSource.SecretName, defaultNamespace)
		if err := k8sClient.Get(context.TODO(), secretName, found); err != nil {
			return serverPool
		}

		for file, caData := range found.Data {
			logrus.Info(
				"Adding cert to tls server-pool from the secret.", "secret",
				secretName, "file", file,
			)
			serverPool.AppendCertsFromPEM(caData)
		}
	} else {
		secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)
		if err := k8sClient.Get(context.TODO(), secretName, found); err != nil {
			return serverPool
		}

		if caData, ok := found.Data[secretSource.CaCertsFilename]; ok {
			logrus.Info(
				"Adding cert to tls server-pool from the secret.", "secret",
				secretName,
			)
			serverPool.AppendCertsFromPEM(caData)
		} else {
			logrus.Info(
				"WARN: Can't find ca-file in the secret. using default certPool.",
				"secret", secretName, "ca-file", secretSource.CaCertsFilename,
			)
		}
	}

	return serverPool
}

func loadCertAndKeyFromSecret(
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string, k8sClient client.Client,
) (*tls.Certificate, error) {
	// get the tls info from secret
	found := &v1.Secret{}
	secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)

	if err := k8sClient.Get(context.TODO(), secretName, found); err != nil {
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
		return &cert, nil
	}
}

func namespacedSecret(
	secretNamespace, secretName,
	defaultNamespace string,
) types.NamespacedName {
	if secretNamespace == "" {
		return types.NamespacedName{
			Name: secretName, Namespace: defaultNamespace,
		}
	}

	return types.NamespacedName{
		Name:      secretName,
		Namespace: secretNamespace,
	}
}

func loadCertAndKeyFromFiles(certPath, keyPath string) (
	*tls.Certificate, error,
) {
	certData, certErr := os.ReadFile(certPath)
	if certErr != nil {
		return nil, certErr
	}

	keyData, keyErr := os.ReadFile(keyPath)
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

	return &cert, nil
}
