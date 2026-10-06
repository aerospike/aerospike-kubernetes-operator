package backupservice

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

func certSpec(clientCert bool) *v1beta1.OperatorClientCertSpec {
	spec := &v1beta1.OperatorClientCertSpec{
		SecretCertSource: v1beta1.SecretCertSource{SecretName: "operator-tls", CaCertsFilename: "ca.crt"},
	}

	if clientCert {
		spec.SecretCertSource.ClientCertFilename = "tls.crt"
		spec.SecretCertSource.ClientKeyFilename = "tls.key"
	}

	return spec
}

func TestGetListeners(t *testing.T) {
	t.Parallel()

	const httpsBlock = `"https":{"port":9443,"context-path":"/abs","cert-file":"/c","key-file":"/k"}`

	tests := []struct {
		certSpec    *v1beta1.OperatorClientCertSpec
		name        string
		config      string
		wantErr     string
		wantScheme  corev1.URIScheme
		wantCtxPath string
		wantPorts   []ListenerPort
		wantPort    int32
	}{
		{
			name:        "no service section uses http defaults",
			config:      `{"backup-policies":{}}`,
			wantPorts:   []ListenerPort{{Name: "http", Port: 8080}},
			wantScheme:  corev1.URISchemeHTTP,
			wantPort:    8080,
			wantCtxPath: "/",
		},
		{
			name:        "http port and context path",
			config:      `{"service":{"http":{"port":8081,"context-path":"/api"}}}`,
			wantPorts:   []ListenerPort{{Name: "http", Port: 8081}},
			wantScheme:  corev1.URISchemeHTTP,
			wantPort:    8081,
			wantCtxPath: "/api",
		},
		{
			name:        "https enabled without operatorClientCert keeps operator on http",
			config:      `{"service":{"http":{"port":8081},` + httpsBlock + `}}`,
			wantPorts:   []ListenerPort{{Name: "http", Port: 8081}, {Name: "https", Port: 9443}},
			wantScheme:  corev1.URISchemeHTTP,
			wantPort:    8081,
			wantCtxPath: "/",
		},
		{
			name:        "operatorClientCert makes operator use https while http stays enabled",
			config:      `{"service":{"http":{"port":8081},` + httpsBlock + `}}`,
			certSpec:    certSpec(false),
			wantPorts:   []ListenerPort{{Name: "http", Port: 8081}, {Name: "https", Port: 9443}},
			wantScheme:  corev1.URISchemeHTTPS,
			wantPort:    9443,
			wantCtxPath: "/abs",
		},
		{
			name:        "https only with default port",
			config:      `{"service":{"http":{"disabled":true},"https":{"cert-file":"/c","key-file":"/k"}}}`,
			certSpec:    certSpec(false),
			wantPorts:   []ListenerPort{{Name: "https", Port: 8443}},
			wantScheme:  corev1.URISchemeHTTPS,
			wantPort:    8443,
			wantCtxPath: "/",
		},
		{
			name:    "http disabled without operatorClientCert",
			config:  `{"service":{"http":{"disabled":true},` + httpsBlock + `}}`,
			wantErr: "service.http is disabled",
		},
		{
			name:     "operatorClientCert without https",
			config:   `{"service":{"http":{"port":8081}}}`,
			certSpec: certSpec(false),
			wantErr:  "service.https is not enabled",
		},
		{
			name:     "operatorClientCert with disabled https",
			config:   `{"service":{"https":{"disabled":true,"cert-file":"/c","key-file":"/k"}}}`,
			certSpec: certSpec(false),
			wantErr:  "service.https is not enabled",
		},
		{
			name: "require-and-verify without client cert",
			config: `{"service":{"https":{"cert-file":"/c","key-file":"/k","client-ca-file":"/ca",` +
				`"client-auth":"require-and-verify"}}}`,
			certSpec: certSpec(false),
			wantErr:  "require-and-verify",
		},
		{
			name: "require-and-verify with client cert",
			config: `{"service":{"https":{"cert-file":"/c","key-file":"/k","client-ca-file":"/ca",` +
				`"client-auth":"Require-And-Verify"}}}`,
			certSpec:    certSpec(true),
			wantPorts:   []ListenerPort{{Name: "http", Port: 8080}, {Name: "https", Port: 8443}},
			wantScheme:  corev1.URISchemeHTTPS,
			wantPort:    8443,
			wantCtxPath: "/",
		},
		{
			name:    "malformed service section",
			config:  `{"service":{"http":{"port":"not-a-number"}}}`,
			wantErr: "unmarshal backup service config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			listeners, err := GetListeners([]byte(tt.config), tt.certSpec)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantPorts, listeners.Ports)
			require.Equal(t, tt.wantScheme, listeners.Scheme)
			require.Equal(t, tt.wantPort, listeners.Port)
			require.Equal(t, tt.wantCtxPath, listeners.ContextPath)
		})
	}
}
