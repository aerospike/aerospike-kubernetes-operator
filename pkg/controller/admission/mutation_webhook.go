package admission

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"path/filepath"
	"reflect"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// MutatingAdmissionWebhook admission mutation webhook
type MutatingAdmissionWebhook struct {
	namespace      string
	client         client.Client
	tlsCertificate tls.Certificate
}

// NewMutatingAdmissionWebhook creates a MutatingAdmissionWebhook struct that will use the specified client to
// access the API.
func NewMutatingAdmissionWebhook(namespace string, mgr manager.Manager, cl client.Client) *MutatingAdmissionWebhook {
	scheme = mgr.GetScheme()
	return &MutatingAdmissionWebhook{
		namespace: namespace,
		client:    cl,
	}
}

// Register registers the validating admission webhook.
func (s *MutatingAdmissionWebhook) Register(certDir string) error {
	logger := pkglog.New(log.Ctx{"namespace": s.namespace})

	certPath := filepath.Join(certDir, v1.TLSCertKey)
	keyPath := filepath.Join(certDir, v1.TLSPrivateKeyKey)
	logger.Debug("Cert info", log.Ctx{"dir": certDir, "cert": certPath, "key": keyPath})

	certByte, err := ioutil.ReadFile(certPath)
	if err != nil {
		return err
	}
	keyByte, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return err
	}

	// parse the pem-encoded tls artifacts contained in the secret
	cert, err := tls.X509KeyPair(certByte, keyByte)
	if err != nil {
		return err
	}

	// store the tls certificate for later usage
	s.tlsCertificate = cert

	return s.ensureWebhookConfig(certByte)
}

func (s *MutatingAdmissionWebhook) ensureWebhookConfig(caBundle []byte) error {
	logger := pkglog.New(log.Ctx{"namespace": s.namespace})

	logger.Info("Creating mutation webhook")
	// create the webhook configuration object containing the target configuration
	vwConfig := &admissionregistrationv1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aerospikeOperatorWebhookName,
			Namespace: s.namespace,
		},
		Webhooks: []admissionregistrationv1beta1.MutatingWebhook{
			{
				Name: aerospikeClusterCRDName,
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups: []string{
								aerospikev1alpha1.SchemeGroupVersion.Group,
							},
							APIVersions: []string{
								aerospikev1alpha1.SchemeGroupVersion.Version,
							},
							Resources: []string{aerospikeCluster},
						},
					},
				},
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      serviceName,
						Namespace: s.namespace,
						Path:      &AerospikeClusterMutationWebhookPath,
					},
					CABundle: caBundle,
				},
				FailurePolicy: &failurePolicy,
			},
		},
	}

	// attempt to register the webhook
	err := s.client.Create(context.TODO(), vwConfig)
	if err == nil {
		return nil
	}

	if !errors.IsAlreadyExists(err) {
		// the webhook doesn't exist yet but we got an unexpected error while creating
		return err
	}

	// at this point the webhook config already exists but its spec may differ.
	// as such, we must do our best to update it.

	// fetch the latest version of the config
	currCfg := &admissionregistrationv1beta1.MutatingWebhookConfiguration{}
	err = s.client.Get(context.TODO(), types.NamespacedName{Name: aerospikeOperatorWebhookName, Namespace: s.namespace}, currCfg)
	if err != nil {
		// we've failed to fetch the latest version of the config
		return err
	}
	if reflect.DeepEqual(currCfg.Webhooks, vwConfig.Webhooks) {
		// if the specs match there's nothing to do
		return nil
	}

	// set the resulting object's spec according to the current spec
	currCfg.Webhooks = vwConfig.Webhooks

	// attempt to update the config
	if err := s.client.Update(context.TODO(), currCfg); err != nil {
		return err
	}

	return nil
}
