package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// fromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
type fromSecretPasswordProvider struct {
	// Client to read secrets.
	k8sClient *client.Client

	// The secret namespace.
	namespace string
}

// Get returns the password for the username using userSpec.
func (pp fromSecretPasswordProvider) Get(
	_ string, userSpec *asdbv1.AerospikeUserSpec,
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

// GetDefaultPassword returns the default password for cluster using AerospikeClusterSpec.
func (pp fromSecretPasswordProvider) GetDefaultPassword(spec *asdbv1.AerospikeClusterSpec) string {
	defaultPasswordFilePath := asdbv1.GetDefaultPasswordFilePath(spec.AerospikeConfig)

	// No default password file specified. Give default password.
	if defaultPasswordFilePath == nil {
		return asdbv1.DefaultAdminPassword
	}

	// Default password file specified. Get the secret name from the volume
	volume := asdbv1.GetVolumeForAerospikePath(&spec.Storage, *defaultPasswordFilePath)
	secretName := volume.Source.Secret.SecretName

	// Get the password from the secret.
	passwordFileName := filepath.Base(*defaultPasswordFilePath)

	password, err := pp.getPasswordFromSecret(secretName, passwordFileName)
	if err != nil {
		pkgLog.Error(err, "Failed to get password from secret")

		return asdbv1.DefaultAdminPassword
	}

	return password
}

// GetPasswordFromSecret returns the password from the secret.
func (pp fromSecretPasswordProvider) getPasswordFromSecret(
	secretName string, passFileName string,
) (string, error) {
	secretNamespcedName := types.NamespacedName{Name: secretName, Namespace: pp.namespace}
	secret := &corev1.Secret{}

	err := (*pp.k8sClient).Get(context.TODO(), secretNamespcedName, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %v", secretNamespcedName, err)
	}

	passBytes, ok := secret.Data[passFileName]
	if !ok {
		return "", fmt.Errorf(
			"failed to get password file in secret %s, fileName %s",
			secretNamespcedName, passFileName,
		)
	}

	return string(passBytes), nil
}

func (r *SingleClusterReconciler) getPasswordProvider() fromSecretPasswordProvider {
	return fromSecretPasswordProvider{
		k8sClient: &r.Client, namespace: r.aeroCluster.Namespace,
	}
}

func (r *SingleClusterReconciler) getClientPolicy() *as.ClientPolicy {
	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = r.aeroCluster.Name

	// tls config
	if tlsName, _ := r.getServiceTLSNameAndPortIfConfigured(); tlsName != "" {
		r.Log.V(1).Info("Set tls config in aerospike client policy")
		clientCertSpec := r.aeroCluster.Spec.OperatorClientCertSpec

		//nolint:gosec // This is a default TLS MinVersion
		tlsConf := tls.Config{
			RootCAs: r.getClusterServerCAPool(
				clientCertSpec, r.aeroCluster.Namespace,
			),
			Certificates: []tls.Certificate{},
			// used only in testing
			// InsecureSkipVerify: true,
		}

		if clientCertSpec == nil || !asdbv1.IsClientCertConfigured(clientCertSpec) {
			// This is possible when tls-authenticate-client = false
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

	// TODO: FIXME: We are creating a spec object here so that it can be passed to validateAndReconcileAccessControl
	// validateAndReconcileAccessControl uses many helper func over spec object. So statusSpec to spec conversion
	// help in reusing those functions over statusSpec.
	// See if this can be done in better manner
	// statusSpec := asdbv1.AerospikeClusterSpec{}
	// if err := lib.DeepCopy(&statusSpec, &aeroCluster.Status.AerospikeClusterStatusSpec); err != nil {
	// 	r.Log.Error(err, "Failed to copy spec in status", "err", err)
	// }

	statusToSpec, err := asdbv1.CopyStatusToSpec(&r.aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		r.Log.Error(err, "Failed to copy status in spec", "err", err)
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

	// If admin user is present in status, use its auth mode
	// Else if federal image, use PKI auth mode.
	// We can't use spec admin user because for EE the spec can have PKI or Internal but for new EE clusters,
	// the authMode must be Internal always for the first time.
	adminUser := asdbv1.GetAdminUserFromSpec(statusToSpec)
	if adminUser != nil {
		policy.AuthMode = asdbv1.GetClientAuthMode(adminUser.AuthMode)
	} else if asdbv1.IsFederal(r.aeroCluster.Spec.Image) {
		policy.AuthMode = as.AuthModePKI
	}

	return policy
}

func (r *SingleClusterReconciler) getClusterServerCAPool(
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
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
		r.Log.Info("`operatorClientCertSpec` is not configured. Using default system CA certs...")
		return serverPool
	}

	switch {
	case clientCertSpec.CertPathInOperator != nil:
		return r.appendCACertFromFileOrPath(
			clientCertSpec.CertPathInOperator.CaCertsPath, serverPool,
		)
	case clientCertSpec.SecretCertSource != nil:
		return r.appendCACertFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace, serverPool,
		)
	default:
		r.Log.Error(
			fmt.Errorf("both `secretName` and `certPathInOperator` are not set"),
			"Returning empty certPool.",
		)

		return serverPool
	}
}

func (r *SingleClusterReconciler) appendCACertFromFileOrPath(
	caPath string, serverPool *x509.CertPool,
) *x509.CertPool {
	if caPath == "" {
		r.Log.Info("CA path is not provided in `operatorClientCertSpec`. Using default system CA certs...")
		return serverPool
	}

	// caPath can be a file name as well as directory path containing cacert files.
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
				r.Log.Info("Loaded CA certs from file.", "ca-path", caPath,
					"file", path)
			}

			return nil
		},
	)
	if err != nil {
		r.Log.Error(
			err, "Failed to load CA certs from dir.", "ca-path", caPath,
		)
	}

	return serverPool
}

func (r *SingleClusterReconciler) appendCACertFromSecret(
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string, serverPool *x509.CertPool,
) *x509.CertPool {
	if secretSource.CaCertsFilename == "" && secretSource.CaCertsSource == nil {
		r.Log.Info(
			"Neither `caCertsFilename` nor `caCertSource` is specified. Using default CA certs...",
			"secret", secretSource,
		)

		return serverPool
	}
	// get the tls info from secret
	r.Log.Info(
		"Trying to find an appropriate CA cert from the secret...", "secret",
		secretSource,
	)

	found := &corev1.Secret{}

	if secretSource.CaCertsSource != nil {
		secretName := namespacedSecret(secretSource.CaCertsSource.SecretNamespace,
			secretSource.CaCertsSource.SecretName, defaultNamespace)
		if err := r.Get(context.TODO(), secretName, found); err != nil {
			r.Log.Error(
				err,
				"Failed to get CA certificates secret, returning empty certPool",
				"secret", secretName,
			)

			return serverPool
		}

		for file, caData := range found.Data {
			r.Log.V(1).Info(
				"Adding cert to tls server-pool from the secret.", "secret",
				secretName, "file", file,
			)
			serverPool.AppendCertsFromPEM(caData)
		}
	} else {
		secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)
		if err := r.Get(context.TODO(), secretName, found); err != nil {
			r.Log.Error(
				err,
				"Failed to get secret certificates to the pool, returning empty certPool",
				"secret", secretName,
			)

			return serverPool
		}

		if caData, ok := found.Data[secretSource.CaCertsFilename]; ok {
			r.Log.V(1).Info(
				"Adding cert to tls server-pool from the secret.", "secret",
				secretName,
			)
			serverPool.AppendCertsFromPEM(caData)
		} else {
			r.Log.V(1).Info(
				"WARN: Can't find ca-file in the secret. using default certPool.",
				"secret", secretName, "ca-file", secretSource.CaCertsFilename,
			)
		}
	}

	return serverPool
}

func (r *SingleClusterReconciler) getClientCertificate(
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
	clusterNamespace string,
) (*tls.Certificate, error) {
	switch {
	case clientCertSpec.CertPathInOperator != nil:
		return r.loadCertAndKeyFromFiles(
			clientCertSpec.CertPathInOperator.ClientCertPath,
			clientCertSpec.CertPathInOperator.ClientKeyPath,
		)
	case clientCertSpec.SecretCertSource != nil:
		return r.loadCertAndKeyFromSecret(
			clientCertSpec.SecretCertSource, clusterNamespace,
		)
	default:
		return nil, fmt.Errorf("both `secretName` and `certPathInOperator` are not set")
	}
}

func (r *SingleClusterReconciler) loadCertAndKeyFromSecret(
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string,
) (*tls.Certificate, error) {
	// get the tls info from secret
	found := &corev1.Secret{}

	secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)
	if err := r.Get(context.TODO(), secretName, found); err != nil {
		r.Log.Info(
			"Warn: Failed to get secret certificates to the pool", "err", err,
		)

		return nil, err
	}

	crtData, crtExists := found.Data[secretSource.ClientCertFilename]
	if !crtExists {
		return nil, fmt.Errorf(
			"can't find certificate `%s` in secret %+v",
			secretSource.ClientCertFilename, secretName,
		)
	}

	keyData, keyExists := found.Data[secretSource.ClientKeyFilename]
	if !keyExists {
		return nil, fmt.Errorf(
			"can't find client key `%s` in secret %+v",
			secretSource.ClientKeyFilename, secretName,
		)
	}

	cert, err := tls.X509KeyPair(crtData, keyData)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load X509 key pair for cluster from secret %+v: %w",
			secretName, err,
		)
	}

	r.Log.Info(
		"Loading Aerospike Cluster client cert from secret", "secret",
		secretName,
	)

	return &cert, nil
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

func (r *SingleClusterReconciler) loadCertAndKeyFromFiles(
	certPath string, keyPath string,
) (*tls.Certificate, error) {
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

	r.Log.Info(
		"Loading Aerospike Cluster client cert from files.", "cert-path",
		certPath, "key-path", keyPath,
	)

	return &cert, nil
}
