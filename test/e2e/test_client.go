package e2e

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"

	log "github.com/inconshreveable/log15"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kubeClient "sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
// TODO duplicated from controller_helper
type FromSecretPasswordProvider struct {
	// Client to read secrets.
	client *kubeClient.Client

	// The secret namespace.
	namespace string
}

var pkglog = log.New(log.Ctx{"module": "test_aerospike_cluster"})

func (pp FromSecretPasswordProvider) Get(username string, userSpec *aerospikev1alpha1.AerospikeUserSpec) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := (*pp.client).Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret)
	if err != nil {
		return "", fmt.Errorf("Failed to get secret %s: %v", secretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("Failed to get password from secret. Please check your secret %s", secretName)
	}
	return string(passbyte), nil
}

func getPasswordProvider(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{client: client, namespace: aeroCluster.Namespace}
}

func getClient(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) (*as.Client, error) {
	pp := getPasswordProvider(aeroCluster, client)
	username, password, err := accessControl.AerospikeAdminCredentials(&aeroCluster.Spec, &aeroCluster.Status.AerospikeClusterSpec, &pp)

	if err != nil {
		return nil, err
	}

	return getClientForUser(username, password, aeroCluster, client)
}

// TODO: username, password not used. check the use of this function
func getClientForUser(username string, password string, aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) (*as.Client, error) {
	conns, err := newAllHostConn(aeroCluster, client)
	if err != nil {
		return nil, fmt.Errorf("Failed to get host info: %v", err)
	}
	var hosts []*as.Host
	for _, conn := range conns {
		hosts = append(hosts, &as.Host{
			Name:    conn.ASConn.AerospikeHostName,
			TLSName: conn.ASConn.AerospikeTLSName,
			Port:    conn.ASConn.AerospikePort,
		})
	}
	// Create policy using status, status has current connection info
	aeroClient, err := as.NewClientWithPolicyAndHost(getClientPolicy(aeroCluster, client), hosts...)
	if err != nil {
		return nil, fmt.Errorf("Failed to create aerospike cluster client: %v", err)
	}

	return aeroClient, nil
}

func getServiceTLSName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	networkConf := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})
	if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
		return tlsName.(string)
	}
	return ""
}

func getClientCertificate(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) (*tls.Certificate, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// get the tls info from secret
	found := &corev1.Secret{}
	err := (*client).Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("Failed to get secret certificates to the pool", log.Ctx{"err": err})
		return nil, err
	}

	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("Failed to get tlsName from aerospikeConfig", log.Ctx{"err": err})
		return nil, err
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"].([]interface{})
	for _, tlsConfInt := range tlsConfList {
		tlsConf := tlsConfInt.(map[string]interface{})
		if tlsConf["name"].(string) == tlsName {
			certFileName := filepath.Base(tlsConf["cert-file"].(string))
			keyFileName := filepath.Base(tlsConf["key-file"].(string))

			cert, err := tls.X509KeyPair(found.Data[certFileName], found.Data[keyFileName])
			if err != nil {
				return nil, fmt.Errorf("failed to load X509 key pair for cluster: %v", err)
			}
			return &cert, nil
		}
	}
	return nil, fmt.Errorf("Failed to get tls config for creating client certificate")
}

func getClusterServerPool(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) *x509.CertPool {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		logger.Warn("Failed to add system certificates to the pool", log.Ctx{"err": err})
		serverPool = x509.NewCertPool()
	}

	// get the tls info from secret
	found := &corev1.Secret{}
	err = (*client).Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("Failed to get secret certificates to the pool, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("Failed to get tlsName from aerospikeConfig, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"].([]interface{})
	for _, tlsConfInt := range tlsConfList {
		tlsConf := tlsConfInt.(map[string]interface{})
		if tlsConf["name"].(string) == tlsName {
			if cafile, ok := tlsConf["ca-file"]; ok {
				logger.Debug("Adding cert in tls serverpool", log.Ctx{"tlsConf": tlsConf})
				caFileName := filepath.Base(cafile.(string))
				serverPool.AppendCertsFromPEM(found.Data[caFileName])
			}
		}
	}
	return serverPool
}

func getClientPolicy(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := as.NewClientPolicy()

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// tls config
	if tlsName := getServiceTLSName(aeroCluster); tlsName != "" {
		logger.Debug("Set tls config in aeospike client policy")
		tlsConf := tls.Config{
			RootCAs:                  getClusterServerPool(aeroCluster, client),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			// InsecureSkipVerify: true,
		}

		cert, err := getClientCertificate(aeroCluster, client)
		if err != nil {
			logger.Error("Failed to get client certificate. Using basic clientPolicy", log.Ctx{"err": err})
			return policy
		}
		tlsConf.Certificates = append(tlsConf.Certificates, *cert)

		tlsConf.BuildNameToCertificate()
		policy.TlsConfig = &tlsConf
	}

	user, pass, err := accessControl.AerospikeAdminCredentials(&aeroCluster.Spec, &aeroCluster.Status.AerospikeClusterSpec, getPasswordProvider(aeroCluster, client))
	if err != nil {
		logger.Error("Failed to get cluster auth info", log.Ctx{"err": err})
	}

	policy.User = user
	policy.Password = pass
	return policy
}

func getServiceForPod(pod *corev1.Pod, client *kubeClient.Client) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := (*client).Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
	if err != nil {
		return nil, fmt.Errorf("Failed to get service for pod %s: %v", pod.Name, err)
	}
	return service, nil
}

func newAsConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod, client *kubeClient.Client) (*deployment.ASConn, error) {
	var port int32

	tlsName := ""

	if aeroCluster.Spec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, client)
		if err != nil {
			return nil, err
		}
		if tlsName == "" {
			port = svc.Spec.Ports[0].NodePort
		} else {
			for _, portInfo := range svc.Spec.Ports {
				if portInfo.Name == "tls" {
					port = portInfo.NodePort
					break
				}
			}
		}
	} else {
		if tlsName == "" {
			port = utils.ServicePort
		} else {
			port = utils.ServiceTlsPort
		}
	}
	host := pod.Status.HostIP
	asConn := &deployment.ASConn{
		AerospikeHostName: host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
	}

	return asConn, nil
}

func newHostConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod *corev1.Pod, client *kubeClient.Client) (*deployment.HostConn, error) {
	asConn, err := newAsConn(aeroCluster, pod, client)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(host, asConn, nil), nil
}

func getPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &kubeClient.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := (*client).List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func newAllHostConn(aeroCluster *aerospikev1alpha1.AerospikeCluster, client *kubeClient.Client) ([]*deployment.HostConn, error) {
	podList, err := getPodList(aeroCluster, client)
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("Pod list empty")
	}

	var hostConns []*deployment.HostConn
	for _, pod := range podList.Items {
		hostConn, err := newHostConn(aeroCluster, &pod, client)
		if err != nil {
			return nil, err
		}
		hostConns = append(hostConns, hostConn)
	}
	return hostConns, nil
}
