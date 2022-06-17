package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	as "github.com/ashishshinde/aerospike-client-go/v6"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
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

	passBytes, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf(
			"failed to get password from secret. Please check your secret %s",
			secretName,
		)
	}
	return string(passBytes), nil
}

func (r *SingleClusterReconciler) getPasswordProvider() FromSecretPasswordProvider {
	return FromSecretPasswordProvider{
		k8sClient: &r.Client, namespace: r.aeroCluster.Namespace,
	}
}

func (r *SingleClusterReconciler) getClientPolicy() *as.ClientPolicy {

	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = r.aeroCluster.Name

	// tls config
	if tlsName, _ := asdbv1beta1.GetServiceTLSNameAndPort(
		&r.aeroCluster.Spec.RackConfig.Racks[0].
			AerospikeConfig,
	); tlsName != "" {
		r.Log.V(1).Info("Set tls config in aerospike client policy")
		clientCertSpec := r.aeroCluster.Spec.OperatorClientCertSpec
		tlsConf := tls.Config{
			RootCAs: r.getClusterServerCAPool(
				clientCertSpec, r.aeroCluster.Namespace,
			),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			// InsecureSkipVerify: true,
		}
		if clientCertSpec == nil || !clientCertSpec.IsClientCertConfigured() {
			//This is possible when tls-authenticate-client = false
			r.Log.Info(
				"Operator's client cert is not configured. Skip using client certs.",
				"clientCertSpec", clientCertSpec,
			)
		} else if cert, err := r.getClientCertificate(
			clientCertSpec, r.aeroCluster.Namespace,
		); err == nil {
			tlsConf.Certificates = append(tlsConf.Certificates, *cert)
		} else {
			r.Log.Error(
				err,
				"Failed to get client certificate. Using basic clientPolicy",
			)
		}

		policy.TlsConfig = &tlsConf
	}

	// TODO: FIXME: We are creating a spec object here so that it can be passed to reconcileAccessControl
	// reconcileAccessControl uses many helper func over spec object. So statusSpec to spec conversion
	// help in reusing those functions over statusSpec.
	// See if this can be done in better manner
	// statusSpec := asdbv1beta1.AerospikeClusterSpec{}
	// if err := lib.DeepCopy(&statusSpec, &aeroCluster.Status.AerospikeClusterStatusSpec); err != nil {
	// 	r.Log.Error(err, "Failed to copy spec in status", "err", err)
	// }

	statusToSpec, err := asdbv1beta1.CopyStatusToSpec(
		r.aeroCluster.Status.
			AerospikeClusterStatusSpec,
	)
	if err != nil {
		r.Log.Error(err, "Failed to copy spec in status", "err", err)
	}

	user, pass, err := AerospikeAdminCredentials(
		&r.aeroCluster.Spec, statusToSpec, r.getPasswordProvider(),
	)
	if err != nil {
		r.Log.Error(err, "Failed to get cluster auth info", "err", err)
	}
	// TODO: What should be the timeout, should make it configurable or just keep it default
	policy.Timeout = time.Minute * 1
	policy.User = user
	policy.Password = pass
	return policy
}

func (r *SingleClusterReconciler) getClusterServerCAPool(
	clientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec,
	clusterNamespace string,
) *x509.CertPool {
	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		r.Log.Info(
			"Warn: Failed to add system certificates to the pool", "err", err,
		)
		serverPool = x509.NewCertPool()
	}
	if clientCertSpec == nil {
		r.Log.Info("OperatorClientCertSpec is not configured. Using default system CA certs...")
		return serverPool
	}

	if clientCertSpec.CertPathInOperator != nil {
		return r.appendCACertFromFile(
			clientCertSpec.CertPathInOperator.CaCertsPath, serverPool,
		)
	} else if clientCertSpec.SecretCertSource != nil {
		return r.appendCACertFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace, serverPool,
		)
	} else {
		r.Log.Error(
			fmt.Errorf("both SecrtenName and CertPathInOperator are not set"),
			"Returning empty certPool.",
		)
		return serverPool
	}
}

func (r *SingleClusterReconciler) appendCACertFromFile(
	caPath string, serverPool *x509.CertPool,
) *x509.CertPool {
	if caPath == "" {
		r.Log.Info("CA path is not provided in \"operatorClientCertSpec\". Using default system CA certs...")
	} else if caData, err := ioutil.ReadFile(caPath); err != nil {
		r.Log.Error(
			err, "Failed to load CA certs from file.", "ca-path", caPath,
		)
	} else {
		serverPool.AppendCertsFromPEM(caData)
		r.Log.Info("Loaded CA root certs from file.", "ca-path", caPath)
	}
	return serverPool
}

func (r *SingleClusterReconciler) appendCACertFromSecret(
	secretSource *asdbv1beta1.AerospikeSecretCertSource,
	defaultNamespace string, serverPool *x509.CertPool,
) *x509.CertPool {
	if secretSource.CaCertsFilename == "" {
		r.Log.Info(
			"CaCertsFilename is not specified. Using default CA certs...",
			"secret", secretSource,
		)
		return serverPool
	}
	// get the tls info from secret
	r.Log.Info(
		"Trying to find an appropriate CA cert from the secret...", "secret",
		secretSource,
	)
	found := &v1.Secret{}
	secretName := namespacedSecret(secretSource, defaultNamespace)
	if err := r.Client.Get(context.TODO(), secretName, found); err != nil {
		r.Log.Error(
			err,
			"Failed to get secret certificates to the pool, returning empty certPool",
			"secret", secretName,
		)
		return serverPool
	}
	if caData, ok := found.Data[secretSource.CaCertsFilename]; ok {
		r.Log.V(1).Info(
			"Adding cert to tls serverpool from the secret.", "secret",
			secretName,
		)
		serverPool.AppendCertsFromPEM(caData)
	} else {
		r.Log.V(1).Info(
			"WARN: Can't find ca-file in the secret. using default certPool.",
			"secret", secretName, "ca-file", secretSource.CaCertsFilename,
		)
	}
	return serverPool
}

func (r *SingleClusterReconciler) getClientCertificate(
	clientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec,
	clusterNamespace string,
) (*tls.Certificate, error) {
	if clientCertSpec.CertPathInOperator != nil {
		return r.loadCertAndKeyFromFiles(
			clientCertSpec.CertPathInOperator.ClientCertPath,
			clientCertSpec.CertPathInOperator.ClientKeyPath,
		)
	} else if clientCertSpec.SecretCertSource != nil {
		return r.loadCertAndKeyFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace,
		)
	} else {
		return nil, fmt.Errorf("both SecrtenName and CertPathInOperator are not set")
	}
}

func (r *SingleClusterReconciler) loadCertAndKeyFromSecret(
	secretSource *asdbv1beta1.AerospikeSecretCertSource,
	defaultNamespace string,
) (*tls.Certificate, error) {
	// get the tls info from secret
	found := &v1.Secret{}
	secretName := namespacedSecret(secretSource, defaultNamespace)
	if err := r.Client.Get(context.TODO(), secretName, found); err != nil {
		r.Log.Info(
			"Warn: Failed to get secret certificates to the pool", "err", err,
		)
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
		r.Log.Info(
			"Loading Aerospike Cluster client cert from secret", "secret",
			secretName,
		)
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

func (r *SingleClusterReconciler) loadCertAndKeyFromFiles(
	certPath string, keyPath string,
) (*tls.Certificate, error) {
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
	r.Log.Info(
		"Loading Aerospike Cluster client cert from files.", "cert-path",
		certPath, "key-path", keyPath,
	)
	return &cert, nil
}
