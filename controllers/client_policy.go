package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"time"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
type FromSecretPasswordProvider struct {
	// Client to read secrets.
	client *client.Client

	// The secret namespace.
	namespace string
}

// Get returns the password for the username using userSpec.
func (pp FromSecretPasswordProvider) Get(username string, userSpec *asdbv1alpha1.AerospikeUserSpec) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := (*pp.client).Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %v", secretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("failed to get password from secret. Please check your secret %s", secretName)
	}
	return string(passbyte), nil
}

func (r *AerospikeClusterReconciler) getPasswordProvider(aeroCluster *asdbv1alpha1.AerospikeCluster) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{client: &r.Client, namespace: aeroCluster.Namespace}
}

func (r *AerospikeClusterReconciler) getClientPolicy(aeroCluster *asdbv1alpha1.AerospikeCluster) *as.ClientPolicy {

	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// tls config
	if tlsName := getServiceTLSName(aeroCluster); tlsName != "" {
		r.Log.V(1).Info("Set tls config in aeospike client policy")
		tlsConf := tls.Config{
			RootCAs:                  r.getClusterServerPool(aeroCluster),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			// InsecureSkipVerify: true,
		}

		cert, err := r.getClientCertificate(aeroCluster)
		if err != nil {
			r.Log.Error(err, "Failed to get client certificate. Using basic clientPolicy", "err", err)
			return policy
		}
		tlsConf.Certificates = append(tlsConf.Certificates, *cert)

		tlsConf.BuildNameToCertificate()
		policy.TlsConfig = &tlsConf
	}

	// TODO: FIXME: We are creating a spec object here so that it can be passed to reconcileAccessControl
	// reconcileAccessControl uses many helper func over spec object. So statusSpec to spec conversion
	// help in reusing those functions over statusSpec.
	// See if this can be done in better manner
	// statusSpec := asdbv1alpha1.AerospikeClusterSpec{}
	// if err := lib.DeepCopy(&statusSpec, &aeroCluster.Status.AerospikeClusterStatusSpec); err != nil {
	// 	r.Log.Error(err, "Failed to copy spec in status", "err", err)
	// }

	statusToSpec, err := asdbv1alpha1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		r.Log.Error(err, "Failed to copy spec in status", "err", err)
	}

	user, pass, err := AerospikeAdminCredentials(&aeroCluster.Spec, statusToSpec, r.getPasswordProvider(aeroCluster))
	if err != nil {
		r.Log.Error(err, "Failed to get cluster auth info", "err", err)
	}
	// TODO: What should be the timeout, should make it configurable or just keep it default
	policy.Timeout = time.Minute * 1
	policy.User = user
	policy.Password = pass
	return policy
}

func (r *AerospikeClusterReconciler) getClusterServerPool(aeroCluster *asdbv1alpha1.AerospikeCluster) *x509.CertPool {

	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		r.Log.Info("Warn: Failed to add system certificates to the pool", "err", err)
		serverPool = x509.NewCertPool()
	}

	// get the tls info from secret
	found := &v1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		r.Log.Info("Warn: Failed to get secret certificates to the pool, returning empty certPool", "err", err)
		return serverPool
	}
	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		r.Log.Info("Warn: Failed to get tlsName from aerospikeConfig, returning empty certPool", "err", err)
		return serverPool
	}
	// get ca-file and use as cacert
	aeroConf := aeroCluster.Spec.AerospikeConfig.Value

	tlsConfList := aeroConf["network"].(map[string]interface{})["tls"].([]interface{})
	for _, tlsConfInt := range tlsConfList {
		tlsConf := tlsConfInt.(map[string]interface{})
		if tlsConf["name"].(string) == tlsName {
			if cafile, ok := tlsConf["ca-file"]; ok {
				r.Log.V(1).Info("Adding cert in tls serverpool", "tlsConf", tlsConf)
				caFileName := filepath.Base(cafile.(string))
				serverPool.AppendCertsFromPEM(found.Data[caFileName])
			}
		}
	}
	return serverPool
}

func (r *AerospikeClusterReconciler) getClientCertificate(aeroCluster *asdbv1alpha1.AerospikeCluster) (*tls.Certificate, error) {

	// get the tls info from secret
	found := &v1.Secret{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		r.Log.Info("Warn: Failed to get secret certificates to the pool", "err", err)
		return nil, err
	}

	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		r.Log.Info("Warn: Failed to get tlsName from aerospikeConfig", "err", err)
		return nil, err
	}
	// get ca-file and use as cacert
	aeroConf := aeroCluster.Spec.AerospikeConfig.Value

	tlsConfList := aeroConf["network"].(map[string]interface{})["tls"].([]interface{})
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
