package test

import (
	goctx "context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(
	"Backup Service Test", func() {

		It("Should setup user RBAC", func() {
			// Setup by user function
			// test creating resource
			// IN operator namespace
			// Create aerospike-secret
			// Create auth-secret (admin)
			// Create auth-update (admin123)

			// For test1
			// Create aerospike-secret
			// Create auth-secret (admin)

			// For test2
			// Create aerospike-secret
			// Create auth-secret (admin)

			// For aerospike
			// Create aerospike-secret
			// Create auth-secret (admin)

			// For common
			// Create namespace test1, test2, aerospike
			// ServiceAccount: aerospike-cluster (operatorNs, test1, test2, aerospike)
			// ClusterRole: aerospike-cluster
			// ClusterRoleBinding: aerospike-cluster

			err := SetupByUser(k8sClient, goctx.TODO())
			Expect(err).ToNot(HaveOccurred())

			// Set up AerospikeBackupService RBAC and AWS secret
			err = SetupBackupServicePreReq(k8sClient, goctx.TODO(), namespace)
			Expect(err).ToNot(HaveOccurred())
		})
	})
