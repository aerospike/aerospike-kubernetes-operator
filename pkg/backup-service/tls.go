package backupservice

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

// BuildOperatorTLSConfig builds the TLS configuration the operator uses to connect to the backup service
// HTTPS listener. The files are read from the Secret in certSpec on every call, so rotated certificates
// are picked up on the next request without restarting the operator.
//
// Unlike a misconfigured client certificate for an Aerospike cluster, any error here makes every call to the
// backup service fail, so errors are returned instead of being logged and skipped.
func BuildOperatorTLSConfig(
	ctx context.Context, k8sClient client.Client, namespace string,
	certSpec *v1beta1.OperatorClientCertSpec, defaultServerName string,
) (*tls.Config, error) {
	if certSpec == nil {
		return nil, errors.New("operatorClientCert is not set")
	}

	source := certSpec.SecretCertSource
	secretName := types.NamespacedName{Namespace: namespace, Name: source.SecretName}

	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, secretName, secret); err != nil {
		return nil, fmt.Errorf("get operator client cert Secret %s: %w", secretName, err)
	}

	tlsConfig := &tls.Config{
		// TLS 1.2 is the lowest version the backup service accepts. Refusing anything older means a server
		// or a middleman offering only TLS 1.0 or 1.1 fails the handshake instead of getting a weaker
		// connection. TLS 1.3 is still used whenever both sides support it.
		MinVersion: tls.VersionTLS12,
		ServerName: certSpec.ServerName,
	}

	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = defaultServerName
	}

	if source.CaCertsFilename != "" {
		caPool, err := caCertPool(secret, source.CaCertsFilename)
		if err != nil {
			return nil, fmt.Errorf("load CA certificate from Secret %s: %w", secretName, err)
		}

		tlsConfig.RootCAs = caPool
	}

	if source.ClientCertFilename != "" {
		cert, err := clientCertificate(secret, source.ClientCertFilename, source.ClientKeyFilename)
		if err != nil {
			return nil, fmt.Errorf("load client certificate from Secret %s: %w", secretName, err)
		}

		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// caCertPool returns a pool holding only the CA certificates stored under key in secret. The system CA
// certificates are deliberately left out: when the user names a CA, a certificate issued for the same name by
// any other CA, including a public one, must not be accepted.
func caCertPool(secret *corev1.Secret, key string) (*x509.CertPool, error) {
	caData, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}

	pool := x509.NewCertPool()

	if !pool.AppendCertsFromPEM(caData) {
		return nil, fmt.Errorf("no PEM certificate found under key %q", key)
	}

	return pool, nil
}

func clientCertificate(secret *corev1.Secret, certKey, keyKey string) (tls.Certificate, error) {
	certData, ok := secret.Data[certKey]
	if !ok {
		return tls.Certificate{}, fmt.Errorf("key %q not found", certKey)
	}

	keyData, ok := secret.Data[keyKey]
	if !ok {
		return tls.Certificate{}, fmt.Errorf("key %q not found", keyKey)
	}

	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse key pair %q, %q: %w", certKey, keyKey, err)
	}

	return cert, nil
}
