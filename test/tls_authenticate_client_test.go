// +build !noac

// Tests Aerospike network policy settings.

package test

import (
	goctx "context"
	"fmt"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe(
	"TlsAuthenticateClient", func() {
		ctx := goctx.TODO()
		Context(
			"When using tls-authenticate-client: Any", func() {
				doTestTLSAuthenticateClientAny(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: Empty String", func() {
				doTestTLSAuthenticateClientEmptyString(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: TLS name missing", func() {
				doTestTLSNameMissing(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: TLS config missing", func() {
				doTestTLSMissing(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: OperatorClientCertSpec missing",
			func() {
				doTestOperatorClientCertSpecMissing(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: specified random string",
			func() {
				doTestTLSAuthenticateClientRandomString(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: Domain List", func() {
				doTestTLSAuthenticateClientDomainList(ctx)
			},
		)

		Context(
			"When using tls-authenticate-client: Client Name Missing", func() {
				doTestTlsClientNameMissing(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: False", func() {
				doTestTLSAuthenticateClientFalse(ctx)
			},
		)

	},
)

func getTlsAuthenticateClient(config *asdbv1beta1.AerospikeCluster) (
	[]string, error,
) {
	configSpec := config.Spec.AerospikeConfig.Value
	networkConf, ok := configSpec["network"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("network configuration not found")
	}
	serviceConf, ok := networkConf["service"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("service configuration not found")
	}
	tlsAuthenticateClient, err := asdbv1beta1.ReadTlsAuthenticateClient(serviceConf)
	if err != nil {
		return nil, err
	}
	return tlsAuthenticateClient, nil
}

func getAerospikeConfig(
	networkConf map[string]interface{},
	operatorClientCertSpec *asdbv1beta1.AerospikeOperatorClientCertSpec,
) *asdbv1beta1.AerospikeCluster {

	cascadeDelete := true
	return &asdbv1beta1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-auth-client",
			Namespace: "test",
		},
		Spec: asdbv1beta1.AerospikeClusterSpec{
			Size:  1,
			Image: "aerospike/aerospike-server-enterprise:5.6.0.7",
			Storage: asdbv1beta1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				BlockVolumePolicy: asdbv1beta1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1beta1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: "ns",
						Source: asdbv1beta1.VolumeSource{
							PersistentVolume: &asdbv1beta1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike/data",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1beta1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1beta1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},

			AerospikeAccessControl: &asdbv1beta1.AerospikeAccessControlSpec{
				Users: []asdbv1beta1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: "auth-update",
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
			AerospikeConfig: &asdbv1beta1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"migrate-threads":  1,
					},
					"network":  networkConf,
					"security": map[string]interface{}{"enable-security": true},
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"replication-factor": 1,
							"memory-size":        3000000000,
							"migrate-sleep":      0,
							"storage-engine": map[string]interface{}{
								"type":     "device",
								"files":    []interface{}{"/opt/aerospike/data/test.dat"},
								"filesize": 2000955200,
							},
						},
					},
				},
			},
			OperatorClientCertSpec: operatorClientCertSpec,
		},
	}
}

func doTestTLSAuthenticateClientAny(ctx goctx.Context) {

	It(
		"TlsAuthenticateClientAny", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "any",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				TLSClientName: "aerospike-a-0.test-runner",
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTlsAuthenticateClient(aeroCluster)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(
				reflect.DeepEqual(
					[]string{"any"}, tlsAuthenticateClient,
				),
			).To(
				BeTrue(),
				fmt.Sprintf(
					"TlsAuthenticateClientAny Validation Failed with following value: %v",
					tlsAuthenticateClient,
				),
			)

			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

}

func doTestTLSAuthenticateClientEmptyString(ctx goctx.Context) {

	It(
		"TlsAuthenticateClientEmptyString", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(err.Error(), "config schema error") {
				Fail("Error: %v should get config schema error")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSNameMissing(ctx goctx.Context) {

	It(
		"TLSNameMissing", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-authenticate-client": "any",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(
				err.Error(),
				"you can't specify tls-authenticate-client for network.service without specifying tls-name",
			) {
				Fail("you can't specify tls-authenticate-client for network.service without specifying tls-name")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSMissing(ctx goctx.Context) {

	It(
		"TLSMissing", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "any",
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(err.Error(), "is not configured") {
				Fail("is not configured")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestOperatorClientCertSpecMissing(ctx goctx.Context) {

	It(
		"OperatorClientCertSpecMissing", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "any",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(networkConf, nil)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(
				err.Error(), "operator client cert is not specified",
			) {
				Fail("operator client cert is not specified")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSAuthenticateClientRandomString(ctx goctx.Context) {

	It(
		"TLSAuthenticateClientRandomString", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "test",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(err.Error(), "contains invalid value") {
				Fail("operator client cert is not specified")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)

		},
	)

}

func doTestTLSAuthenticateClientDomainList(ctx goctx.Context) {

	It(
		"TlsAuthenticateClientDomainList", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": []string{"aerospike-a-0.test-runner"},
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				TLSClientName: "aerospike-a-0.tls-client-name",
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTlsAuthenticateClient(aeroCluster)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(
				reflect.DeepEqual(
					[]string{
						"aerospike-a-0.test-runner",
						"aerospike-a-0.tls-client-name",
					},
					tlsAuthenticateClient,
				),
			).To(
				BeTrue(),
				"TlsAuthenticateClientAny Validation Failed",
			)

			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

}

func doTestTlsClientNameMissing(ctx goctx.Context) {

	It(
		"TlsClientNameMissing", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": []string{"test"},
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			if !strings.Contains(
				err.Error(), "operator TLSClientName is not specified",
			) {
				Fail("operator client cert is not specified")
			}
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)

}

func doTestTLSAuthenticateClientFalse(ctx goctx.Context) {

	It(
		"TlsAuthenticateClientFalse", func() {
			networkConf := map[string]interface{}{
				"service": map[string]interface{}{
					"tls-name":                "aerospike-a-0.test-runner",
					"tls-authenticate-client": "false",
				},
				"tls": []interface{}{
					map[string]interface{}{
						"name":      "aerospike-a-0.test-runner",
						"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
						"key-file":  "/etc/aerospike/secret/svc_key.pem",
						"ca-file":   "/etc/aerospike/secret/cacert.pem",
					},
				},
			}
			operatorClientCertSpec := &asdbv1beta1.AerospikeOperatorClientCertSpec{
				AerospikeOperatorCertSource: asdbv1beta1.AerospikeOperatorCertSource{
					SecretCertSource: &asdbv1beta1.AerospikeSecretCertSource{
						SecretName:         "aerospike-secret",
						CaCertsFilename:    "cacert.pem",
						ClientCertFilename: "svc_cluster_chain.pem",
						ClientKeyFilename:  "svc_key.pem",
					},
				},
			}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTlsAuthenticateClient(aeroCluster)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(
				reflect.DeepEqual(
					[]string{"false"}, tlsAuthenticateClient,
				),
			).To(
				BeTrue(),
				fmt.Sprintf(
					"TlsAuthenticateClientAny Validation Failed with following value: %v",
					tlsAuthenticateClient,
				),
			)
			deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}
