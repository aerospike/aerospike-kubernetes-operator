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

	httpOnly := func(port int32) []ListenerPort { return []ListenerPort{{Name: "http", Port: port}} }
	both := func(httpPort, httpsPort int32) []ListenerPort {
		return []ListenerPort{{Name: "http", Port: httpPort}, {Name: "https", Port: httpsPort}}
	}

	tests := []struct {
		certSpec *v1beta1.OperatorClientCertSpec
		want     *Listeners
		name     string
		config   string
		wantErr  string
	}{
		{
			name: "no service section uses http defaults", config: `{"backup-policies":{}}`,
			want: &Listeners{Scheme: corev1.URISchemeHTTP, ContextPath: "/", Ports: httpOnly(8080), Port: 8080},
		},
		{
			name: "http port and context path", config: `{"service":{"http":{"port":8081,"context-path":"/api"}}}`,
			want: &Listeners{Scheme: corev1.URISchemeHTTP, ContextPath: "/api", Ports: httpOnly(8081), Port: 8081},
		},
		{
			name:   "https enabled without operatorClientCert keeps operator on http",
			config: `{"service":{"http":{"port":8081},` + httpsBlock + `}}`,
			want:   &Listeners{Scheme: corev1.URISchemeHTTP, ContextPath: "/", Ports: both(8081, 9443), Port: 8081},
		},
		{
			name:   "operatorClientCert makes operator use https while http stays enabled",
			config: `{"service":{"http":{"port":8081},` + httpsBlock + `}}`, certSpec: certSpec(false),
			want: &Listeners{Scheme: corev1.URISchemeHTTPS, ContextPath: "/abs", Ports: both(8081, 9443), Port: 9443},
		},
		{
			name:     "https only with default port",
			config:   `{"service":{"http":{"disabled":true},"https":{"cert-file":"/c","key-file":"/k"}}}`,
			certSpec: certSpec(false),
			want: &Listeners{
				Scheme: corev1.URISchemeHTTPS, ContextPath: "/", Ports: []ListenerPort{{Name: "https", Port: 8443}},
				Port: 8443,
			},
		},
		{
			name: "require-and-verify with client cert, enum in another case",
			config: `{"service":{"https":{"cert-file":"/c","key-file":"/k","client-ca-file":"/ca",` +
				`"client-auth":"Require-And-Verify"}}}`,
			certSpec: certSpec(true),
			want:     &Listeners{Scheme: corev1.URISchemeHTTPS, ContextPath: "/", Ports: both(8080, 8443), Port: 8443},
		},
		{
			name: "http disabled without operatorClientCert", wantErr: "service.http is disabled",
			config: `{"service":{"http":{"disabled":true},` + httpsBlock + `}}`,
		},
		{
			name:     "operatorClientCert with disabled https",
			config:   `{"service":{"https":{"disabled":true,"cert-file":"/c","key-file":"/k"}}}`,
			certSpec: certSpec(false), wantErr: "service.https is not enabled",
		},
		{
			name: "require-and-verify without client cert",
			config: `{"service":{"https":{"cert-file":"/c","key-file":"/k","client-ca-file":"/ca",` +
				`"client-auth":"require-and-verify"}}}`,
			certSpec: certSpec(false), wantErr: "require-and-verify",
		},
		{
			name: "malformed service section", wantErr: "unmarshal backup service config",
			config: `{"service":{"http":{"port":"not-a-number"}}}`,
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
			require.Equal(t, tt.want, listeners)
		})
	}
}
