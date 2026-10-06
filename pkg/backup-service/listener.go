package backupservice

import (
	"errors"
	"fmt"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/model"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

// ListenerPort is the name and port of one enabled backup service listener.
type ListenerPort struct {
	Name string
	Port int32
}

// Listeners describes the backup service listeners and the one the operator connects to.
type Listeners struct {
	// Scheme and ContextPath describe the listener the operator connects to.
	Scheme      corev1.URIScheme
	ContextPath string

	// Ports holds every enabled listener, http before https.
	Ports []ListenerPort

	// Port is the port the operator connects to. It is one of the entries in Ports.
	Port int32
}

// GetListeners reads the service.http and service.https listeners from a backup service config and chooses
// the one the operator connects to: service.https when the operator has a client cert configured,
// service.http otherwise. It returns an error when the chosen listener is not enabled, or when the backup
// service requires client certificates and certSpec has none.
//
// Defaults come from the backup service's own model getters, the same ones it uses at startup to decide
// which listeners to run, so an omitted field resolves exactly as it does in the backup service.
func GetListeners(rawConfig []byte, certSpec *v1beta1.OperatorClientCertSpec) (*Listeners, error) {
	var config dto.Config

	if err := yaml.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal backup service config: %w", err)
	}

	svc := config.ServiceConfig.ToModel()
	httpConf := svc.GetServerHTTPOrDefault()
	httpsConf := svc.GetServerHTTPSOrDefault()

	listeners := &Listeners{}

	// Ports are appended in a fixed order, http then https. The container and Service ports are built in
	// this order, and a different order on a later reconcile would change the Deployment and restart the pod.
	if !httpConf.Disabled {
		listeners.Ports = append(listeners.Ports, ListenerPort{
			Name: v1beta1.HTTPKey, Port: toInt32(httpConf.GetPortOrDefault()),
		})
	}

	if !httpsConf.Disabled {
		listeners.Ports = append(listeners.Ports, ListenerPort{
			Name: v1beta1.HTTPSKey, Port: toInt32(httpsConf.GetPortOrDefault()),
		})
	}

	if certSpec == nil {
		if httpConf.Disabled {
			return nil, errors.New("service.http is disabled; set spec.operatorClientCert so that the operator " +
				"connects to service.https, or enable service.http")
		}

		listeners.Scheme = corev1.URISchemeHTTP
		listeners.Port = toInt32(httpConf.GetPortOrDefault())
		listeners.ContextPath = httpConf.GetContextPathOrDefault()

		return listeners, nil
	}

	if httpsConf.Disabled {
		return nil, errors.New("spec.operatorClientCert is set but service.https is not enabled")
	}

	if httpsConf.GetClientAuthOrDefault() == model.TLSClientAuthRequireAndVerify &&
		certSpec.SecretCertSource.ClientCertFilename == "" {
		return nil, errors.New("service.https.client-auth is require-and-verify; set clientCertFilename and " +
			"clientKeyFilename in spec.operatorClientCert.secretCertSource")
	}

	listeners.Scheme = corev1.URISchemeHTTPS
	listeners.Port = toInt32(httpsConf.GetPortOrDefault())
	listeners.ContextPath = httpsConf.GetContextPathOrDefault()

	return listeners, nil
}

func toInt32(port model.Port) int32 {
	return int32(port) //nolint:gosec // backup service config validation limits ports to 1-65535
}
