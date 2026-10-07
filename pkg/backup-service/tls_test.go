package backupservice

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const (
	testNamespace  = "aerospike"
	testSecretName = "operator-tls"
	testServerName = "abs.aerospike.svc"
)

// testCA is a CA generated for one test, with helpers to issue PEM-encoded certificates.
type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return &testCA{cert: cert, key: key, certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue returns a PEM certificate and key signed by the CA, valid for dnsNames and the given usage.
func (ca *testCA) issue(t *testing.T, usage x509.ExtKeyUsage, dnsNames ...string) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test"},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func fakeClientWithSecret(t *testing.T, data map[string][]byte) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       data,
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
}

// startHTTPSServer starts an HTTPS server with a certificate for testServerName. When clientCA is set, the
// server requires and verifies client certificates signed by it.
func startHTTPSServer(t *testing.T, serverCA, clientCA *testCA) *httptest.Server {
	t.Helper()

	certPEM, keyPEM := serverCA.issue(t, x509.ExtKeyUsageServerAuth, testServerName)
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	// Answer only the route the backup service uses for health, so a wrong URL fails the test.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12}

	if clientCA != nil {
		pool := x509.NewCertPool()
		pool.AddCert(clientCA.cert)
		server.TLS.ClientCAs = pool
		server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
	}

	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}

// healthCheck calls the server through a Client built from certSpec, the way the operator does.
func healthCheck(t *testing.T, server *httptest.Server, k8sClient client.Client,
	certSpec *v1beta1.OperatorClientCertSpec,
) error {
	t.Helper()

	tlsConfig, err := BuildOperatorTLSConfig(context.TODO(), k8sClient, testNamespace, certSpec, testServerName)
	require.NoError(t, err)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)

	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	return NewClientWithTLS(host, int32(port), "/", "", tlsConfig).CheckBackupServiceHealth()
}

func TestBuildOperatorTLSConfig_HTTPS(t *testing.T) {
	t.Parallel()

	ca := newTestCA(t)
	server := startHTTPSServer(t, ca, nil)
	k8sClient := fakeClientWithSecret(t, map[string][]byte{"ca.crt": ca.certPEM})

	err := healthCheck(t, server, k8sClient, certSpec(false))
	require.NoError(t, err)
}

func TestBuildOperatorTLSConfig_MutualTLS(t *testing.T) {
	t.Parallel()

	ca := newTestCA(t)
	server := startHTTPSServer(t, ca, ca)
	clientCertPEM, clientKeyPEM := ca.issue(t, x509.ExtKeyUsageClientAuth)

	k8sClient := fakeClientWithSecret(t, map[string][]byte{
		"ca.crt": ca.certPEM, "tls.crt": clientCertPEM, "tls.key": clientKeyPEM,
	})

	require.NoError(t, healthCheck(t, server, k8sClient, certSpec(true)))

	// Without a client certificate the server rejects the operator.
	require.Error(t, healthCheck(t, server, k8sClient, certSpec(false)))
}

func TestBuildOperatorTLSConfig_RejectsUntrustedServer(t *testing.T) {
	t.Parallel()

	server := startHTTPSServer(t, newTestCA(t), nil)
	otherCA := newTestCA(t)
	k8sClient := fakeClientWithSecret(t, map[string][]byte{"ca.crt": otherCA.certPEM})

	err := healthCheck(t, server, k8sClient, certSpec(false))
	require.ErrorContains(t, err, "certificate signed by unknown authority")
}

func TestBuildOperatorTLSConfig_ServerName(t *testing.T) {
	t.Parallel()

	ca := newTestCA(t)
	server := startHTTPSServer(t, ca, nil)
	k8sClient := fakeClientWithSecret(t, map[string][]byte{"ca.crt": ca.certPEM})

	spec := certSpec(false)
	spec.ServerName = "other.aerospike.svc"

	err := healthCheck(t, server, k8sClient, spec)
	require.ErrorContains(t, err, "not other.aerospike.svc")
}

func TestBuildOperatorTLSConfig_Errors(t *testing.T) {
	t.Parallel()

	ca := newTestCA(t)
	clientCertPEM, _ := ca.issue(t, x509.ExtKeyUsageClientAuth)

	tests := []struct {
		data     map[string][]byte
		certSpec *v1beta1.OperatorClientCertSpec
		name     string
		wantErr  string
	}{
		{name: "nil spec", certSpec: nil, wantErr: "operatorClientCert is not set"},
		{name: "missing CA key", data: map[string][]byte{}, certSpec: certSpec(false), wantErr: `key "ca.crt" not found`},
		{
			name: "CA is not PEM", data: map[string][]byte{"ca.crt": []byte("not a cert")},
			certSpec: certSpec(false), wantErr: "no PEM certificate",
		},
		{
			name: "missing client key", data: map[string][]byte{"ca.crt": ca.certPEM, "tls.crt": clientCertPEM},
			certSpec: certSpec(true), wantErr: `key "tls.key" not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := BuildOperatorTLSConfig(context.TODO(), fakeClientWithSecret(t, tt.data), testNamespace,
				tt.certSpec, testServerName)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}

	_, err := BuildOperatorTLSConfig(context.TODO(), fakeClientWithSecret(t, nil), "other-namespace",
		certSpec(false), testServerName)
	require.ErrorContains(t, err, "get operator client cert Secret")
}

func TestClientAPIScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "http://abs.aerospike.svc:8081/v1/config",
		NewClient("abs.aerospike.svc", 8081, "").API("/config"))
	require.Equal(t, "https://abs.aerospike.svc:8443/abs/v1/config",
		NewClientWithTLS("abs.aerospike.svc", 8443, "/abs", "", &tls.Config{MinVersion: tls.VersionTLS12}).API("/config"))

	// Health is served under the context path without the API version.
	require.Equal(t, "http://abs.aerospike.svc:8081/health", NewClient("abs.aerospike.svc", 8081, "").systemURL("health"))
	require.Equal(t, "https://abs.aerospike.svc:8443/abs/health",
		NewClientWithTLS("abs.aerospike.svc", 8443, "/abs", "", &tls.Config{MinVersion: tls.VersionTLS12}).
			systemURL("health"))

	// TLS clients are short-lived, so their connections must not idle in a keep-alive pool.
	tlsClient := NewClientWithTLS("abs.aerospike.svc", 8443, "/", "", &tls.Config{MinVersion: tls.VersionTLS12})
	transport, ok := tlsClient.client().Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.DisableKeepAlives)

	// A Client built as a struct literal, as some callers do, defaults to HTTP and still has an HTTP client.
	literal := &Client{Address: "abs.aerospike.svc", Port: 8081}
	require.Equal(t, "http://abs.aerospike.svc:8081/v1/config", literal.API("/config"))
	require.NotNil(t, literal.client())
}
