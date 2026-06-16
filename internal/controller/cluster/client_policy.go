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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
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
	ctx context.Context, _ string, userSpec *asdbv1.AerospikeUserSpec,
) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := (*pp.k8sClient).Get(
		ctx,
		types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret,
	)
	if err != nil {
		return "", fmt.Errorf("could not get secret %s: %w", utils.NamespacedName(pp.namespace, secretName), err)
	}

	passBytes, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("missing %q key in secret %s", "password", utils.NamespacedName(pp.namespace, secretName))
	}

	return string(passBytes), nil
}

// GetDefaultPassword returns the default password for cluster using AerospikeClusterSpec.
func (pp fromSecretPasswordProvider) GetDefaultPassword(ctx context.Context, spec *asdbv1.AerospikeClusterSpec) string {
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

	password, err := pp.getPasswordFromSecret(ctx, secretName, passwordFileName)
	if err != nil {
		pkgLog.Error(err, "Failed to get password from secret",
			"secret", klog.KRef(pp.namespace, secretName),
		)

		return asdbv1.DefaultAdminPassword
	}

	return password
}

// GetPasswordFromSecret returns the password from the secret.
func (pp fromSecretPasswordProvider) getPasswordFromSecret(
	ctx context.Context, secretName string, passFileName string,
) (string, error) {
	secretNamespcedName := types.NamespacedName{Name: secretName, Namespace: pp.namespace}
	secret := &corev1.Secret{}

	err := (*pp.k8sClient).Get(ctx, secretNamespcedName, secret)
	if err != nil {
		return "", fmt.Errorf("could not get secret %s: %w", secretNamespcedName, err)
	}

	passBytes, ok := secret.Data[passFileName]
	if !ok {
		return "", fmt.Errorf("missing password file %q in secret %s", passFileName, secretNamespcedName)
	}

	return string(passBytes), nil
}

func (r *SingleClusterReconciler) getPasswordProvider() fromSecretPasswordProvider {
	return fromSecretPasswordProvider{
		k8sClient: &r.Client, namespace: r.aeroCluster.Namespace,
	}
}

func (r *SingleClusterReconciler) getClientPolicy(ctx context.Context) *as.ClientPolicy {
	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = r.aeroCluster.Name

	// tls config
	if tlsName, _ := r.getServiceTLSNameAndPortIfConfigured(); tlsName != "" {
		r.Log.V(1).Info("Set tls config in aerospike client policy")
		clientCertSpec := r.aeroCluster.Spec.OperatorClientCertSpec

		tlsConf := tls.Config{
			RootCAs: r.getClusterServerCAPool(
				ctx, clientCertSpec, r.aeroCluster.Namespace,
			),
			Certificates: []tls.Certificate{},
			// used only in testing
			// InsecureSkipVerify: true,
		}

		if clientCertSpec == nil || !asdbv1.IsClientCertConfigured(clientCertSpec) {
			// This is possible when tls-authenticate-client = false
			r.Log.Info(
				"Operator's client cert is not configured. Skip using client certs.",
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
				"clientCertSpec", clientCertSpec,
			)
		} else if cert, err := r.getClientCertificate(
			ctx, clientCertSpec, r.aeroCluster.Namespace,
		); err == nil {
			tlsConf.Certificates = append(tlsConf.Certificates, *cert)
		} else {
			r.Log.Error(
				err,
				"Failed to get client certificate, using basic clientPolicy",
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
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
		r.Log.Error(err, "Failed to copy status in spec",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		)
	}

	user, pass, err := AerospikeAdminCredentials(
		ctx, &r.aeroCluster.Spec, statusToSpec, r.getPasswordProvider(),
	)
	if err != nil {
		r.Log.Error(err, "Failed to get cluster auth info",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		)
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
	ctx context.Context,
	clientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
	clusterNamespace string,
) *x509.CertPool {
	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		r.Log.Info(
			"Warn: Failed to add system certificates to the pool, using empty pool",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			"err", err,
		)

		serverPool = x509.NewCertPool()
	}

	if clientCertSpec == nil {
		r.Log.Info("`operatorClientCertSpec` is not configured. Using default system CA certs",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		)

		return serverPool
	}

	switch {
	case clientCertSpec.CertPathInOperator != nil:
		return r.appendCACertFromFileOrPath(
			clientCertSpec.CertPathInOperator.CaCertsPath, serverPool,
		)
	case clientCertSpec.SecretCertSource != nil:
		return r.appendCACertFromSecret(
			ctx, clientCertSpec.SecretCertSource, clusterNamespace, serverPool,
		)
	default:
		r.Log.Error(
			fmt.Errorf("both `secretName` and `certPathInOperator` are not set"),
			"Returning empty certPool",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		)

		return serverPool
	}
}

func (r *SingleClusterReconciler) appendCACertFromFileOrPath(
	caPath string, serverPool *x509.CertPool,
) *x509.CertPool {
	if caPath == "" {
		r.Log.Info("CA path is not provided in `operatorClientCertSpec`. Using default system CA certs",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		)

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
				r.Log.Info("Loaded CA certs from file",
					"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
					"caPath", caPath,
					"file", path,
				)
			}

			return nil
		},
	)
	if err != nil {
		r.Log.Error(
			err, "Failed to load CA certs from dir",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			"caPath", caPath,
		)
	}

	return serverPool
}

func (r *SingleClusterReconciler) appendCACertFromSecret(
	ctx context.Context,
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string, serverPool *x509.CertPool,
) *x509.CertPool {
	if secretSource.CaCertsFilename == "" && secretSource.CaCertsSource == nil {
		r.Log.V(2).Info(
			"Neither caCertsFilename nor caCertSource is specified, using default CA certs",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			"secretSource", secretSource,
		)

		return serverPool
	}
	// get the tls info from secret
	r.Log.V(2).Info(
		"Trying to find an appropriate CA cert from the secret",
		"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		"secretSource", secretSource,
	)

	found := &corev1.Secret{}

	if secretSource.CaCertsSource != nil {
		//nolint:staticcheck // SA1019: must read deprecated SecretNamespace to resolve secret until field is removed
		secretName := namespacedSecret(secretSource.CaCertsSource.SecretNamespace,
			secretSource.CaCertsSource.SecretName, defaultNamespace)
		if err := r.Get(ctx, secretName, found); err != nil {
			r.Log.Error(
				err,
				"Failed to get CA certificates secret, returning empty certPool",
				"secret", klog.KRef(secretName.Namespace, secretName.Name),
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			)

			return serverPool
		}

		for file, caData := range found.Data {
			r.Log.V(1).Info(
				"Adding cert to tls server-pool from the secret",
				"secret", klog.KRef(secretName.Namespace, secretName.Name),
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
				"file", file,
			)
			serverPool.AppendCertsFromPEM(caData)
		}
	} else {
		//nolint:staticcheck // SA1019: must read deprecated SecretNamespace to resolve secret until field is removed
		secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)
		if err := r.Get(ctx, secretName, found); err != nil {
			r.Log.Error(
				err,
				"Failed to get secret certificates to the pool, returning empty certPool",
				"secret", klog.KRef(secretName.Namespace, secretName.Name),
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			)

			return serverPool
		}

		if caData, ok := found.Data[secretSource.CaCertsFilename]; ok {
			r.Log.V(1).Info(
				"Adding cert to tls server-pool from the secret",
				"secret", klog.KRef(secretName.Namespace, secretName.Name),
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			)
			serverPool.AppendCertsFromPEM(caData)
		} else {
			r.Log.V(1).Info(
				"CA file not found in secret, using default certPool",
				"secret", klog.KRef(secretName.Namespace, secretName.Name),
				"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
				"caFile", secretSource.CaCertsFilename,
			)
		}
	}

	return serverPool
}

func (r *SingleClusterReconciler) getClientCertificate(
	ctx context.Context,
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
			ctx, clientCertSpec.SecretCertSource, clusterNamespace,
		)
	default:
		return nil, fmt.Errorf("both `secretName` and `certPathInOperator` are not set")
	}
}

func (r *SingleClusterReconciler) loadCertAndKeyFromSecret(
	ctx context.Context,
	secretSource *asdbv1.AerospikeSecretCertSource,
	defaultNamespace string,
) (*tls.Certificate, error) {
	// get the tls info from secret
	found := &corev1.Secret{}

	//nolint:staticcheck // SA1019: must read deprecated SecretNamespace to resolve secret until field is removed
	secretName := namespacedSecret(secretSource.SecretNamespace, secretSource.SecretName, defaultNamespace)
	if err := r.Get(ctx, secretName, found); err != nil {
		r.Log.Info(
			"Failed to get secret for client certificate, continuing with error",
			"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
			"secret", klog.KRef(secretName.Namespace, secretName.Name),
			"err", err,
		)

		return nil, fmt.Errorf("could not get secret %+v: %w", secretName, err)
	}

	crtData, crtExists := found.Data[secretSource.ClientCertFilename]
	if !crtExists {
		return nil, fmt.Errorf(
			"can't find certificate `%q` in secret %+v",
			secretSource.ClientCertFilename, secretName,
		)
	}

	keyData, keyExists := found.Data[secretSource.ClientKeyFilename]
	if !keyExists {
		return nil, fmt.Errorf(
			"can't find client key `%q` in secret %+v",
			secretSource.ClientKeyFilename, secretName,
		)
	}

	cert, err := tls.X509KeyPair(crtData, keyData)
	if err != nil {
		return nil, fmt.Errorf(
			"could not load X509 key pair for cluster from secret %+v: %w",
			secretName, err,
		)
	}

	r.Log.Info(
		"Loading Aerospike Cluster client cert from secret",
		"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		"secret", klog.KRef(secretName.Namespace, secretName.Name),
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
		return nil, fmt.Errorf("could not read certificate file %s: %w", certPath, certErr)
	}

	keyData, keyErr := os.ReadFile(keyPath)
	if keyErr != nil {
		return nil, fmt.Errorf("could not read client key file %s: %w", keyPath, keyErr)
	}

	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, fmt.Errorf(
			"could not load X509 key pair for cluster (cert=%s, key=%s): %w",
			certPath, keyPath, err,
		)
	}

	r.Log.Info(
		"Loading Aerospike Cluster client cert from files",
		"aerospikeCluster", klog.KRef(r.aeroCluster.Namespace, r.aeroCluster.Name),
		"certPath", certPath,
		"keyPath", keyPath,
	)

	return &cert, nil
}
