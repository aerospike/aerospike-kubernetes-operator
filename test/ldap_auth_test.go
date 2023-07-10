//go:build !noac

// Tests Aerospike ldap external authentication.

package test

import (
	goctx "context"
	"time"

	as "github.com/ashishshinde/aerospike-client-go/v6"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
)

var _ = Describe(
	"LDAP External Auth test", func() {
		ctx := goctx.TODO()
		It(
			"Validate LDAP user transactions", func() {
				By("DeployCluster with LDAP auth")
				clusterNamespacedName := getNamespacedName(
					"ldap-auth", namespace,
				)
				aeroCluster := getAerospikeClusterSpecWithLDAP(clusterNamespacedName)
				err := deployCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())

				By("Validate transactions for user01")
				err = validateTransactions(aeroCluster, "user01", "password01")
				Expect(err).ToNot(HaveOccurred())

				By("Validate transactions for user02")
				err = validateTransactions(aeroCluster, "user02", "password02")
				Expect(err).ToNot(HaveOccurred())

				By("Validate invalid user")
				err = validateTransactions(aeroCluster, "dne", "dne")
				Expect(err).To(HaveOccurred())

				err = deleteCluster(k8sClient, ctx, aeroCluster)
				Expect(err).ToNot(HaveOccurred())
			},
		)
	},
)

func validateTransactions(
	cluster *asdbv1.AerospikeCluster,
	ldapUser string, ldapPassword string,
) error {
	client, err := getClientExternalAuth(
		pkgLog, cluster, k8sClient, ldapUser,
		ldapPassword,
	)

	if err != nil {
		return err
	}

	defer client.Close()

	_, _ = client.WarmUp(-1)

	key, err := as.NewKey("test", "test", "key1")
	if err != nil {
		return err
	}

	binMap := map[string]interface{}{
		"testBin": "binValue",
	}

	// The k8s services take time to come up so the timeouts are on the
	// higher side. Try a few times
	for j := 0; j < 100; j++ {
		err = client.Put(nil, key, binMap)
		if err == nil {
			break
		}

		time.Sleep(time.Second * 1)
	}

	return err
}

// getAerospikeClusterSpecWithLDAP create a spec with LDAP security
// configuration
func getAerospikeClusterSpecWithLDAP(
	clusterNamespacedName types.NamespacedName,
) *asdbv1.AerospikeCluster {
	cascadeDelete := true
	networkConf := getNetworkTLSConfig()
	operatorClientCertSpec := getOperatorCert()

	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:  2,
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
						SecretName: authSecretName,
						Roles: []string{
							"sys-admin",
							"user-admin",
						},
					},
				},
			},
			PodSpec: asdbv1.AerospikePodSpec{
				MultiPodPerHost: true,
			},
			AerospikeConfig: &asdbv1.AerospikeConfigSpec{
				Value: map[string]interface{}{
					"service": map[string]interface{}{
						"feature-key-file": "/etc/aerospike/secret/features.conf",
						"migrate-threads":  4,
					},

					"network": networkConf,

					"security": map[string]interface{}{
						"ldap": map[string]interface{}{
							"query-base-dn": "dc=example,dc=org",
							"server":        "ldap://openldap.default.svc.cluster.local:1389",
							"disable-tls":   true,
							"query-user-dn": "cn=admin,dc=example,dc=org",
							"query-user-password-file": "/etc/aerospike/secret" +
								"/ldap-passwd.txt",
							"user-dn-pattern": "cn=${un},ou=users," +
								"dc=example,dc=org",
							"role-query-search-ou": true,
							"role-query-patterns": []string{
								"(&(objectClass=groupOfNames)(member=cn=${un},ou=users,dc=example,dc=org))",
								"(&(ou=db_groups)(uniqueMember=${dn}))",
							},
							"polling-period": 10,
						},
					},
					"namespaces": []interface{}{
						map[string]interface{}{
							"name":               "test",
							"replication-factor": 2,
							"memory-size":        3000000000,
							"migrate-sleep":      0,
							"storage-engine": map[string]interface{}{
								"type": "memory",
							},
						},
					},
				},
			},
			OperatorClientCertSpec: operatorClientCertSpec,
		},
	}
}
