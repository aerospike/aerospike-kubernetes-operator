//go:build !noac

// Tests Aerospike TLS authenticate client settings.

package test

import (
	goctx "context"
	"fmt"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"TlsAuthenticateClient", func() {
		ctx := goctx.TODO()
		Context(
			"When using tls-authenticate-client: Any", func() {
				doTestTLSAuthenticateClientAny(ctx)
				doTestTLSAuthenticateClientAnyWithCapath(ctx)
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
				doTestTLSClientNameMissing(ctx)
			},
		)
		Context(
			"When using tls-authenticate-client: False", func() {
				doTestTLSAuthenticateClientFalse(ctx)
			},
		)

	},
)

func getTLSAuthenticateClient(config *asdbv1.AerospikeCluster) (
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

	tlsAuthenticateClient, err := asdbv1.ReadTLSAuthenticateClient(serviceConf)
	if err != nil {
		return nil, err
	}

	return tlsAuthenticateClient, nil
}

func getAerospikeConfig(
	networkConf map[string]interface{},
	operatorClientCertSpec *asdbv1.AerospikeOperatorClientCertSpec,
) *asdbv1.AerospikeCluster {
	cascadeDelete := true

	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-auth-client",
			Namespace: "test",
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  1,
			Image: latestImage,
			Storage: asdbv1.AerospikeStorageSpec{
				FileSystemVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				BlockVolumePolicy: asdbv1.AerospikePersistentVolumePolicySpec{
					InputCascadeDelete: &cascadeDelete,
				},
				Volumes: []asdbv1.VolumeSpec{
					{
						Name: "workdir",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike",
						},
					},
					{
						Name: "ns",
						Source: asdbv1.VolumeSource{
							PersistentVolume: &asdbv1.PersistentVolumeSpec{
								Size:         resource.MustParse("1Gi"),
								StorageClass: storageClass,
								VolumeMode:   corev1.PersistentVolumeFilesystem,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/opt/aerospike/data",
						},
					},
					{
						Name: aerospikeConfigSecret,
						Source: asdbv1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: tlsSecretName,
							},
						},
						Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
							Path: "/etc/aerospike/secret",
						},
					},
				},
			},

			AerospikeAccessControl: &asdbv1.AerospikeAccessControlSpec{
				Users: []asdbv1.AerospikeUserSpec{
					{
						Name:       "admin",
						SecretName: authSecretNameForUpdate,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"migrate-threads":  1,
					},
					"network":  networkConf,
					"security": map[string]interface{}{},
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
			RackConfig: asdbv1.RackConfig{
				Racks: []asdbv1.Rack{
					{
						ID: 1,
						AerospikeConfig: asdbv1.AerospikeConfigSpec{
							Value: map[string]interface{}{
								"service": map[string]interface{}{
									"feature-key-file": "/etc/aerospike/secret/features.conf",
									"migrate-threads":  1,
								},
								"network":  networkConf,
								"security": map[string]interface{}{},
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
					},
				},
			},
		},
	}
}

func doTestTLSAuthenticateClientAny(ctx goctx.Context) {
	It(
		"TlsAuthenticateClientAny", func() {
			networkConf := getNetworkTLSConfig()
			networkConf["service"].(map[string]interface{})["tls-authenticate-client"] = "any"

			operatorClientCertSpec := getOperatorCert()

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTLSAuthenticateClient(aeroCluster)
			Expect(err).ToNot(HaveOccurred())

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

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func doTestTLSAuthenticateClientAnyWithCapath(ctx goctx.Context) {
	It(
		"TlsAuthenticateClientAny with capath", func() {
			networkConf := getNetworkTLSConfig()
			networkConf["service"].(map[string]interface{})["tls-authenticate-client"] = "any"
			tls := []interface{}{
				map[string]interface{}{
					"name":      "aerospike-a-0.test-runner",
					"cert-file": "/etc/aerospike/secret/server-cert.pem",
					"key-file":  "/etc/aerospike/secret/server_key.pem",
					"ca-path":   "/etc/aerospike/secret/cacerts",
				},
			}
			networkConf["tls"] = tls

			operatorClientCertSpec := getOperatorCert()
			operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.CaCertsFilename = ""
			operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.ClientCertFilename = "server-cert.pem"
			operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.ClientKeyFilename = "server_key.pem"
			cacertPath := &asdbv1.CaCertsSource{
				SecretName: tlsCacertSecretName,
			}
			operatorClientCertSpec.AerospikeOperatorCertSource.SecretCertSource.CaCertsSource = cacertPath

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			secretVolume := asdbv1.VolumeSpec{
				Name: tlsCacertSecretName,
				Source: asdbv1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: tlsCacertSecretName,
					},
				},
				Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
					Path: "/etc/aerospike/secret/cacerts",
				},
			}
			aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, secretVolume)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTLSAuthenticateClient(aeroCluster)
			Expect(err).ToNot(HaveOccurred())

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

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func doTestTLSAuthenticateClientEmptyString(ctx goctx.Context) {
	It(
		"TlsAuthenticateClientEmptyString", func() {
			networkConf := getNetworkTLSConfig()
			operatorClientCertSpec := getOperatorCert()
			networkConf["service"].(map[string]interface{})["tls-authenticate-client"] = ""

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			assertError(err, "config schema error")
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSNameMissing(ctx goctx.Context) {
	It(
		"TLSNameMissing", func() {
			networkConf := getNetworkTLSConfig()
			delete(
				networkConf["service"].(map[string]interface{}), "tls-name",
			)

			operatorClientCertSpec := getOperatorCert()

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			expectedError := "without specifying tls-name"
			assertError(err, expectedError)
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSMissing(ctx goctx.Context) {
	It(
		"TLSMissing", func() {
			networkConf := getNetworkTLSConfig()
			delete(networkConf, "tls")

			operatorClientCertSpec := getOperatorCert()

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			assertError(err, "is not configured")
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestOperatorClientCertSpecMissing(ctx goctx.Context) {
	It(
		"OperatorClientCertSpecMissing", func() {
			networkConf := getNetworkTLSConfig()
			aeroCluster := getAerospikeConfig(networkConf, nil)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			assertError(err, "operator client cert is not specified")
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSAuthenticateClientRandomString(ctx goctx.Context) {
	It(
		"TLSAuthenticateClientRandomString", func() {
			networkConf := getNetworkTLSConfig()
			operatorClientCertSpec := getOperatorCert()
			networkConf["service"].(map[string]interface{})["tls-authenticate"+
				"-client"] = "test"

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			assertError(err, "contains invalid value")
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSAuthenticateClientDomainList(ctx goctx.Context) {
	It(
		"TlsAuthenticateClientDomainList", func() {
			networkConf := getNetworkTLSConfig()
			operatorClientCertSpec := getOperatorCert()
			networkConf["service"].(map[string]interface{})["tls-authenticate"+
				"-client"] = []string{"aerospike-a-0.tls-client-name"}

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTLSAuthenticateClient(aeroCluster)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}

			Expect(
				reflect.DeepEqual(
					[]string{
						"aerospike-a-0.tls-client-name",
						"aerospike-a-0.test-runner",
					},
					tlsAuthenticateClient,
				),
			).To(
				BeTrue(),
				"TlsAuthenticateClientAny Validation Failed %v",
				tlsAuthenticateClient,
			)

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func doTestTLSClientNameMissing(ctx goctx.Context) {
	It(
		"TlsClientNameMissing", func() {
			networkConf := getNetworkTLSConfig()
			operatorClientCertSpec := getOperatorCert()
			networkConf["service"].(map[string]interface{})["tls-authenticate-client"] = []string{"aerospike-a-0.test-runner"}
			operatorClientCertSpec.TLSClientName = ""

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			assertError(err, "operator TLSClientName is not specified")
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
		},
	)
}

func doTestTLSAuthenticateClientFalse(ctx goctx.Context) {
	It(
		"TlsAuthenticateClientFalse", func() {
			networkConf := getNetworkTLSConfig()
			networkConf["service"].(map[string]interface{})["tls-authenticate"+
				"-client"] = "false"

			operatorClientCertSpec := getOperatorCert()

			aeroCluster := getAerospikeConfig(
				networkConf, operatorClientCertSpec,
			)
			err := aerospikeClusterCreateUpdate(k8sClient, aeroCluster, ctx)
			Expect(err).ToNot(HaveOccurred())
			tlsAuthenticateClient, err := getTLSAuthenticateClient(aeroCluster)
			Expect(err).ToNot(HaveOccurred())

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
			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)
}

func assertError(err error, expectedError string) {
	if err == nil {
		Fail(
			fmt.Sprintf(
				"Expected - %s, Actual - nil", expectedError,
			),
		)
	}

	if !strings.Contains(err.Error(), expectedError) {
		Fail(
			fmt.Sprintf(
				"Expected - %s, Actual - %v", expectedError,
				err.Error(),
			),
		)
	}
}
