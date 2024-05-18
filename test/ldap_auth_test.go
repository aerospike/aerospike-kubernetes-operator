//go:build !noac

// Tests Aerospike ldap external authentication.

package test

import (
	goctx "context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	as "github.com/aerospike/aerospike-client-go/v7"
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
	networkConf := getNetworkTLSConfig()
	operatorClientCertSpec := getOperatorCert()

	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterNamespacedName.Name,
			Namespace: clusterNamespacedName.Namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:    2,
			Image:   latestImage,
			Storage: getBasicStorageSpecObject(),

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
				MultiPodPerHost: ptr.To(true),
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
						getSCNamespaceConfig("test", "/test/dev/xvdf"),
					},
				},
			},
			OperatorClientCertSpec: operatorClientCertSpec,
		},
	}
}
