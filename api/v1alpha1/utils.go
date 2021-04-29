package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
)

// var scheme *runtime.Scheme

var (
	aerospikeGroupName = "aerospike.com"
)

const (
	aerospikeCluster = "aerospikeclusters"
)

var (
	aerospikeClusterCRDName = fmt.Sprintf("%s.%s", aerospikeCluster, GroupVersion.Group)
)

var (
	// AerospikeClusterValidationWebhookPath  validation webhook path
	AerospikeClusterValidationWebhookPath = "/admission/reviews/aerospikeclusters/validating"
	// AerospikeClusterMutationWebhookPath mutation webhook path
	AerospikeClusterMutationWebhookPath = "/admission/reviews/aerospikeclusters/mutating"
	aerospikeOperatorWebhookName        = fmt.Sprintf("aerospike-cluster-webhook.%s", aerospikeGroupName)
	failurePolicy                       = admissionregistrationv1beta1.Fail
)

const (
	// serviceName is the name of the service used to expose the webhook.
	serviceName = "aerospike-cluster-webhook"
	// tlsSecretName is the name of the secret that will hold tls artifacts used
	// by the webhook.
	tlsSecretName = "aerospike-cluster-webhook-tls"
	// whReadyTimeout is the time to wait until the validating webhook service
	// endpoints are ready
	whReadyTimeout = time.Second * 30
)

const (
	// DefaultRackID is the ID for the default rack created when no racks are specified.
	DefaultRackID = 0
	MaxRackID     = 1000000
	MinRackID     = 1

	ServiceTLSPort     = 4333
	ServiceTLSPortName = "svc-tls-port"
	ServicePort        = 3000
	ServicePortName    = "service"

	HeartbeatTLSPort     = 3012
	HeartbeatTLSPortName = "hb-tls-port"
	HeartbeatPort        = 3002
	HeartbeatPortName    = "heartbeat"

	FabricTLSPort     = 3011
	FabricTLSPortName = "fb-tls-port"
	FabricPort        = 3001
	FabricPortName    = "fabric"

	InfoPort     = 3003
	InfoPortName = "info"
)

// var pkglog = log.New(log.Ctx{"module": "admissionWebhook"})

const baseVersion = "4.6.0.2"

// ContainsString check whether list contains given string
func ContainsString(list []string, ele string) bool {
	for _, listEle := range list {
		if ele == listEle {
			return true
		}
	}
	return false
}

// GetWorkDirectory returns the Aerospike work directory to use for aerospikeConfig.
func GetWorkDirectory(aerospikeConfig AeroConfMap) string {
	// Get namespace config.
	serviceTmp := aerospikeConfig[confKeyService]

	if serviceTmp != nil {
		serviceConf := serviceTmp.(map[string]interface{})
		workDir, ok := serviceConf[confKeyWorkDirectory]
		if ok {
			return workDir.(string)
		}
	}

	return defaultWorkDirectory
}

const (
	// Namespace keys.
	confKeyNamespace     = "namespaces"
	confKeyMemorySize    = "memory-size"
	confKeyStorageEngine = "storage-engine"
	confKeyFilesize      = "filesize"
	confKeyDevice        = "devices"
	confKeyFile          = "files"
	confKeyNetwork       = "network"
	confKeyTLS           = "tls"
	confKeySecurity      = "security"

	// XDR keys.
	confKeyXdr         = "xdr"
	confKeyXdrDlogPath = "xdr-digestlog-path"

	// Service section keys.
	confKeyService       = "service"
	confKeyWorkDirectory = "work-directory"

	// Defaults.
	defaultWorkDirectory = "/opt/aerospike"
)
const (
	AerospikeServerContainerName     string = "aerospike-server"
	AerospikeServerInitContainerName string = "aerospike-init"
)

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (registry string, name string, version string) {
	if tag == "" {
		return "", "", ""
	}
	r := regexp.MustCompile(`(?P<registry>[^/]+/)?(?P<image>[^:]+)(?P<version>:.+)?`)
	matches := r.FindStringSubmatch(tag)
	return matches[1], matches[2], strings.TrimPrefix(matches[3], ":")
}

// IsTLS tells if cluster is tls enabled
func IsTLS(aerospikeConfig AeroConfMap) bool {
	if confInterface, ok := aerospikeConfig[confKeyNetwork]; ok {
		if networkConf, ok := confInterface.(map[string]interface{}); ok {
			if _, ok := networkConf[confKeyTLS]; ok {
				return true
			}
		}
	}

	return false
}

// IsSecurityEnabled tells if security is enabled in cluster
// TODO: can a invalid map come here
func IsSecurityEnabled(aerospikeConfig AeroConfMap) (bool, error) {
	// security conf
	if confInterface, ok := aerospikeConfig[confKeySecurity]; ok {
		if secConf, ok := confInterface.(map[string]interface{}); ok {
			if enabled, ok := secConf["enable-security"]; ok {
				if _, ok := enabled.(bool); ok {
					return enabled.(bool), nil
				}
				return false, fmt.Errorf("Invalid aerospike.security conf. enable-security not valid %v", confInterface)

			}
			return false, fmt.Errorf("Invalid aerospike.security conf. enable-security key not present %v", confInterface)
		}
		return false, fmt.Errorf("Invalid aerospike.security conf. Not a valid map %v", confInterface)
	}
	return false, nil
}

// ListAerospikeNamespaces returns the list of namespaecs in the input aerospikeConfig.
// Assumes the namespace section is validated.
func ListAerospikeNamespaces(aerospikeConfig AeroConfMap) ([]string, error) {
	namespaces := make([]string, 5)
	// Get namespace config.
	if confs, ok := aerospikeConfig[confKeyNamespace].([]interface{}); ok {
		for _, nsConf := range confs {
			namespaceConf, ok := nsConf.(map[string]interface{})

			if !ok {
				// Should never happen
				return nil, fmt.Errorf("Invalid namespaces config: %v", nsConf)
			}
			namespaces = append(namespaces, namespaceConf["name"].(string))
		}
	}
	return namespaces, nil
}

// IsAerospikeNamespacePresent indicates if the namespace is present in aerospikeConfig.
// Assumes the namespace section is validated.
func IsAerospikeNamespacePresent(aerospikeConfig AeroConfMap, namespaceName string) bool {
	// Get namespace config.
	if confs, ok := aerospikeConfig[confKeyNamespace].([]interface{}); ok {
		for _, nsConf := range confs {
			namespaceConf, ok := nsConf.(map[string]interface{})

			if !ok {
				// Should never happen
				return false
			}

			if namespaceConf["name"] == namespaceName {
				return true
			}
		}
	}
	return false
}

// IsXdrEnabled indicates if XDR is enabled in aerospikeConfig.
func IsXdrEnabled(aerospikeConfig AeroConfMap) bool {
	xdrConf := aerospikeConfig[confKeyXdr]
	return xdrConf != nil
}

// GetDigestLogFile returns the xdr digest file path if configured.
func GetDigestLogFile(aerospikeConfig AeroConfMap) (*string, error) {
	if xdrConfTmp, ok := aerospikeConfig[confKeyXdr]; ok {
		xdrConf, ok := xdrConfTmp.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("aerospikeConfig.xdr not a valid map %v", aerospikeConfig[confKeyXdr])
		}
		dglog, ok := xdrConf[confKeyXdrDlogPath]
		if !ok {
			return nil, fmt.Errorf("%s is missing in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}
		if _, ok := dglog.(string); !ok {
			return nil, fmt.Errorf("%s is not a valid string in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}

		// "/opt/aerospike/xdr/digestlog 100G"
		if len(strings.Fields(dglog.(string))) != 2 {
			return nil, fmt.Errorf("%s is not in valid format (/opt/aerospike/xdr/digestlog 100G) in aerospikeConfig.xdr %v", confKeyXdrDlogPath, xdrConf)
		}

		return &strings.Fields(dglog.(string))[0], nil
	}

	return nil, fmt.Errorf("xdr not configured")
}

// // SetupSecret setup secret, it doesn't create object
// func SetupSecret(certDir, namespace string) error {
// 	// generate the certificate to use when registering and serving the webhook
// 	svc := fmt.Sprintf("%s.%s.svc", serviceName, namespace)
// 	now := time.Now()
// 	crt := x509.Certificate{
// 		Subject:               pkix.Name{CommonName: svc},
// 		NotBefore:             now,
// 		NotAfter:              now.Add(365 * 24 * time.Hour),
// 		SerialNumber:          big.NewInt(now.Unix()),
// 		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
// 		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
// 		IsCA:                  true,
// 		BasicConstraintsValid: true,
// 		DNSNames:              []string{svc},
// 	}
// 	// generate the private key to use when registering and serving the webhook
// 	key, err := rsa.GenerateKey(rand.Reader, 2048)
// 	if err != nil {
// 		return err
// 	}
// 	// pem-encode the private key
// 	keyBytes := pem.EncodeToMemory(&pem.Block{
// 		Type:  keyutil.RSAPrivateKeyBlockType,
// 		Bytes: x509.MarshalPKCS1PrivateKey(key),
// 	})
// 	// self-sign the generated certificate using the private key
// 	sig, err := x509.CreateCertificate(rand.Reader, &crt, &crt, key.Public(), key)
// 	if err != nil {
// 		return err
// 	}
// 	// pem-encode the signed certificate
// 	sigBytes := pem.EncodeToMemory(&pem.Block{
// 		Type:  cert.CertificateBlockType,
// 		Bytes: sig,
// 	})

// 	certFileName := filepath.Join(certDir, v1.TLSCertKey)
// 	keyFileName := filepath.Join(certDir, v1.TLSPrivateKeyKey)

// 	os.MkdirAll(filepath.Dir(certFileName), os.FileMode(0777))
// 	err = ioutil.WriteFile(certFileName, sigBytes, os.FileMode(0777))
// 	if err != nil {
// 		return err
// 	}

// 	os.MkdirAll(filepath.Dir(keyFileName), os.FileMode(0777))
// 	err = ioutil.WriteFile(keyFileName, keyBytes, os.FileMode(0777))
// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }
