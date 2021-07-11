package test

// Aerospike client and info testing utilities.
//
// TODO refactor the code in aero_helper.go anc controller_helper.go so that it can be used here.
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	log "github.com/inconshreveable/log15"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	aerospikecluster "github.com/aerospike/aerospike-kubernetes-operator/controllers"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	"github.com/aerospike/aerospike-management-lib/info"
	as "github.com/ashishshinde/aerospike-client-go/v5"
)

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
// TODO duplicated from controller_helper
type FromSecretPasswordProvider struct {
	// Client to read secrets.
	k8sClient client.Client

	// The secret namespace.
	namespace string
}

var pkglog = log.New(log.Ctx{"module": "test_aerospike_cluster"})

// Get returns the password for the username using userSpec.
func (pp FromSecretPasswordProvider) Get(username string, userSpec *asdbv1alpha1.AerospikeUserSpec) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := pp.k8sClient.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %v", secretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("failed to get password from secret. Please check your secret %s", secretName)
	}
	return string(passbyte), nil
}

func getPasswordProvider(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{k8sClient: k8sClient, namespace: aeroCluster.Namespace}
}

func getClient(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) (*as.Client, error) {
	pp := getPasswordProvider(aeroCluster, k8sClient)
	statusToSpec, err := asdbv1alpha1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		return nil, err
	}

	username, password, err := aerospikecluster.AerospikeAdminCredentials(&aeroCluster.Spec, statusToSpec, &pp)

	if err != nil {
		return nil, err
	}

	return getClientForUser(username, password, aeroCluster, k8sClient)
}

// TODO: username, password not used. check the use of this function
func getClientForUser(username string, password string, aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) (*as.Client, error) {
	conns, err := newAllHostConn(aeroCluster, k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %v", err)
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
	aeroClient, err := as.NewClientWithPolicyAndHost(getClientPolicy(aeroCluster, k8sClient), hosts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create aerospike cluster client: %v", err)
	}

	return aeroClient, nil
}

func getServiceTLSName(aeroCluster *asdbv1alpha1.AerospikeCluster) string {
	if networkConfTmp, ok := aeroCluster.Spec.AerospikeConfig.Value["network"]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		if _, ok := networkConf["service"]; !ok {
			// Service section will be missing if the spec is not obtained from server but is generated locally from test code.
			return ""
		}
		if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
			return tlsName.(string)
		}
	}
	return ""
}

func getClientCertificate(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) (*tls.Certificate, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// get the tls info from secret
	found := &corev1.Secret{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("failed to get secret certificates to the pool", log.Ctx{"err": err})
		return nil, err
	}

	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("failed to get tlsName from aerospikeConfig", log.Ctx{"err": err})
		return nil, err
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})["tls"].([]interface{})
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
	return nil, fmt.Errorf("failed to get tls config for creating client certificate")
}

func getClusterServerPool(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) *x509.CertPool {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		logger.Warn("failed to add system certificates to the pool", log.Ctx{"err": err})
		serverPool = x509.NewCertPool()
	}

	// get the tls info from secret
	found := &corev1.Secret{}
	err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("failed to get secret certificates to the pool, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("failed to get tlsName from aerospikeConfig, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig.Value["network"].(map[string]interface{})["tls"].([]interface{})
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

func getClientPolicy(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := as.NewClientPolicy()

	// cluster name
	policy.ClusterName = aeroCluster.Name

	tlsName := getServiceTLSName(aeroCluster)

	if tlsName != "" {
		if aeroCluster.Spec.AerospikeNetworkPolicy.TLSAccessType != asdbv1alpha1.AerospikeNetworkTypeHostExternal && aeroCluster.Spec.AerospikeNetworkPolicy.TLSAlternateAccessType == asdbv1alpha1.AerospikeNetworkTypeHostExternal {
			policy.UseServicesAlternate = true
		}
	} else {
		if aeroCluster.Spec.AerospikeNetworkPolicy.AccessType != asdbv1alpha1.AerospikeNetworkTypeHostExternal && aeroCluster.Spec.AerospikeNetworkPolicy.AlternateAccessType == asdbv1alpha1.AerospikeNetworkTypeHostExternal {
			policy.UseServicesAlternate = true
		}
	}

	// tls config
	if tlsName != "" {
		logger.Debug("Set tls config in aeospike client policy")
		tlsConf := tls.Config{
			RootCAs:                  getClusterServerPool(aeroCluster, k8sClient),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			InsecureSkipVerify: true,
		}

		cert, err := getClientCertificate(aeroCluster, k8sClient)
		if err != nil {
			logger.Error("failed to get client certificate. Using basic clientPolicy", log.Ctx{"err": err})
			return policy
		}
		tlsConf.Certificates = append(tlsConf.Certificates, *cert)

		tlsConf.BuildNameToCertificate()
		policy.TlsConfig = &tlsConf
	}

	statusToSpec, err := asdbv1alpha1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		logger.Error("failed to get spec from status", log.Ctx{"err": err})
		return policy
	}
	user, pass, err := aerospikecluster.AerospikeAdminCredentials(&aeroCluster.Spec, statusToSpec, getPasswordProvider(aeroCluster, k8sClient))
	if err != nil {
		logger.Error("failed to get cluster auth info", log.Ctx{"err": err})
	}
	policy.Timeout = time.Minute * 3
	policy.User = user
	policy.Password = pass
	return policy
}

func getServiceForPod(pod *corev1.Pod, k8sClient client.Client) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
	if err != nil {
		return nil, fmt.Errorf("failed to get service for pod %s: %v", pod.Name, err)
	}
	return service, nil
}

func newAsConn(aeroCluster *asdbv1alpha1.AerospikeCluster, pod *corev1.Pod, k8sClient client.Client) (*deployment.ASConn, error) {
	// Use the Kubenetes serice port and IP since the test might run outside the Kubernetes cluster network.
	var port int32

	tlsName := getServiceTLSName(aeroCluster)

	if aeroCluster.Spec.MultiPodPerHost {
		svc, err := getServiceForPod(pod, k8sClient)
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
			port = asdbv1alpha1.ServicePort
		} else {
			port = asdbv1alpha1.ServiceTLSPort
		}
	}

	host, err := getNodeIP(pod, k8sClient)

	if err != nil {
		return nil, err
	}

	asConn := &deployment.ASConn{
		AerospikeHostName: *host,
		AerospikePort:     int(port),
		AerospikeTLSName:  tlsName,
	}

	return asConn, nil
}

func getNodeIP(pod *corev1.Pod, k8sClient client.Client) (*string, error) {
	ip := pod.Status.HostIP

	k8sNode := &corev1.Node{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName, pod.Name, err)
	}

	// TODO: when refactoring this to use this as main code, this might need to be the
	// internal hostIP instead of the external IP. Tests run outside the k8s cluster so
	// we should to use the external IP if present.

	// If externalIP is present than give external ip
	for _, add := range k8sNode.Status.Addresses {
		if add.Type == corev1.NodeExternalIP && add.Address != "" {
			ip = add.Address
		}
	}
	return &ip, nil
}

func newHostConn(aeroCluster *asdbv1alpha1.AerospikeCluster, pod *corev1.Pod, k8sClient client.Client) (*deployment.HostConn, error) {
	asConn, err := newAsConn(aeroCluster, pod, k8sClient)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", asConn.AerospikeHostName, asConn.AerospikePort)
	return deployment.NewHostConn(host, asConn, nil), nil
}

func getPodList(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := k8sClient.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func newAllHostConn(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) ([]*deployment.HostConn, error) {
	podList, err := getPodList(aeroCluster, k8sClient)
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("pod list empty")
	}

	var hostConns []*deployment.HostConn
	for _, pod := range podList.Items {
		hostConn, err := newHostConn(aeroCluster, &pod, k8sClient)
		if err != nil {
			return nil, err
		}
		hostConns = append(hostConns, hostConn)
	}
	return hostConns, nil
}

func getAeroClusterPVCList(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := k8sClient.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func getPodsPVCList(aeroCluster *asdbv1alpha1.AerospikeCluster, k8sClient client.Client, podNames []string) ([]corev1.PersistentVolumeClaim, error) {
	pvcListItems, err := getAeroClusterPVCList(aeroCluster, k8sClient)
	if err != nil {
		return nil, err
	}
	// https://github.com/kubernetes/kubernetes/issues/72196
	// No regex support in field-selector
	// Can not get pvc having matching podName. Need to check more.
	var newPVCItems []corev1.PersistentVolumeClaim
	for _, pvc := range pvcListItems {
		for _, podName := range podNames {
			// Get PVC belonging to pod only
			if strings.HasSuffix(pvc.Name, podName) {
				newPVCItems = append(newPVCItems, pvc)
			}
		}
	}
	return newPVCItems, nil
}

func getAsConfig(asinfo *info.AsInfo, cmd string) (lib.Stats, error) {
	var confs lib.Stats
	var err error
	for i := 0; i < 10; i++ {
		confs, err = asinfo.GetAsConfig(cmd)
		if err == nil {
			return confs, nil
		}
	}

	return nil, err
}

func runInfo(cp *as.ClientPolicy, asConn *deployment.ASConn, cmd string) (map[string]string, error) {
	var res map[string]string
	var err error
	for i := 0; i < 10; i++ {
		res, err = deployment.RunInfo(cp, asConn, cmd)
		if err == nil {
			return res, nil
		}

		time.Sleep(time.Second)
	}
	return nil, err
}
