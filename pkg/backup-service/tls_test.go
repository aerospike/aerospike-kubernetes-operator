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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const testNamespace = "aerospike"

// selfSignedClientCert returns a client certificate and key that also act as their own CA.
func selfSignedClientCert(t *testing.T) (cert *x509.Certificate, certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "operator"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err = x509.ParseCertificate(der)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	return cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// TestBuildOperatorTLSConfig builds the TLS config from a Secret, the way the operator does, and calls an HTTPS
// server that requires a client certificate and answers only the health route, so a wrong URL also fails.
func TestBuildOperatorTLSConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	clientCert, clientCertPEM, clientKeyPEM := selfSignedClientCert(t)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientCert)
	server.TLS = &tls.Config{
		ClientCAs: clientCAs, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portStr, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	serverCA := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	allFiles := map[string][]byte{"ca.crt": serverCA, "tls.crt": clientCertPEM, "tls.key": clientKeyPEM}

	tests := []struct {
		data      map[string][]byte
		spec      *v1beta1.OperatorClientCertSpec
		name      string
		namespace string
		wantErr   string
	}{
		{name: "trusted server with client certificate", data: allFiles, spec: certSpec(true)},
		{name: "no client certificate", data: allFiles, spec: certSpec(false), wantErr: "tls:"},
		{
			name: "untrusted server", data: map[string][]byte{"ca.crt": clientCertPEM, "tls.crt": clientCertPEM,
				"tls.key": clientKeyPEM}, spec: certSpec(true), wantErr: "certificate signed by unknown authority",
		},
		{
			name: "server name not in the certificate", data: allFiles,
			spec: func() *v1beta1.OperatorClientCertSpec {
				spec := certSpec(true)
				spec.TLSName = "other.aerospike.svc"

				return spec
			}(),
			wantErr: "not other.aerospike.svc",
		},
		{name: "nil spec", wantErr: "operatorClientCert is not set"},
		{name: "missing CA key", data: map[string][]byte{}, spec: certSpec(false), wantErr: `key "ca.crt" not found`},
		{
			name: "CA is not PEM", data: map[string][]byte{"ca.crt": []byte("not a cert")}, spec: certSpec(false),
			wantErr: "no PEM certificate",
		},
		{
			name: "missing client key", data: map[string][]byte{"ca.crt": serverCA, "tls.crt": clientCertPEM},
			spec: certSpec(true), wantErr: `key "tls.key" not found`,
		},
		{
			name: "Secret in another namespace", namespace: "other", spec: certSpec(false),
			wantErr: "get operator client cert Secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "operator-tls", Namespace: testNamespace},
				Data:       tt.data,
			}).Build()

			namespace := testNamespace
			if tt.namespace != "" {
				namespace = tt.namespace
			}

			tlsConfig, err := BuildOperatorTLSConfig(context.TODO(), k8sClient, namespace, tt.spec)
			if err == nil {
				//nolint:gosec // port was parsed with a 32-bit size
				err = NewClientWithTLS(host, int32(port), "/", "", tlsConfig).CheckBackupServiceHealth()
			}

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestClientAPIScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "http://abs.aerospike.svc:8081/v1/config",
		NewClient("abs.aerospike.svc", 8081, "").API("/config"))
	require.Equal(t, "https://abs.aerospike.svc:8443/abs/v1/config",
		NewClientWithTLS("abs.aerospike.svc", 8443, "/abs", "", &tls.Config{MinVersion: tls.VersionTLS12}).API("/config"))

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

func TestGetBackupServiceClientListener(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	clientURL := func(status v1beta1.AerospikeBackupServiceStatus) string {
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1beta1.AerospikeBackupService{
			ObjectMeta: metav1.ObjectMeta{Name: "abs", Namespace: testNamespace},
			Spec: v1beta1.AerospikeBackupServiceSpec{
				Image: "aerospike/aerospike-backup-service:3.7.0",
				Config: runtime.RawExtension{
					Raw: []byte(`{"service":{"http":{"port":8081,"context-path":"/abs"}}}`),
				},
			},
			Status: status,
		}).Build()

		serviceClient, err := GetBackupServiceClient(k8sClient,
			&v1beta1.BackupService{Name: "abs", Namespace: testNamespace})
		require.NoError(t, err)

		return serviceClient.API("/config")
	}

	// Status written: the listener the backup service runs with.
	require.Equal(t, "http://abs.aerospike.svc:9090/running/v1/config",
		clientURL(v1beta1.AerospikeBackupServiceStatus{Port: 9090, ContextPath: "/running"}))

	// Status not written yet: the listener in spec.
	require.Equal(t, "http://abs.aerospike.svc:8081/abs/v1/config", clientURL(v1beta1.AerospikeBackupServiceStatus{}))
}
