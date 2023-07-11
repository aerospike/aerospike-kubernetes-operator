package test

import (
	goctx "context"

	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"
	aeroClusterCRB                string = "aerospike-cluster"
	aeroClusterCR                 string = "aerospike-cluster"
)

func createClusterRBAC(k8sClient client.Client, ctx goctx.Context) error {
	subjects := make([]rbac.Subject, 0, len(testNamespaces))

	for idx := range testNamespaces {
		// Create service account for getting access in cluster specific namespaces
		if err := createServiceAccount(
			k8sClient, ctx, aeroClusterServiceAccountName, testNamespaces[idx],
		); err != nil {
			return err
		}

		// Create subjects to bind clusterRole to serviceAccounts
		subjects = append(subjects, rbac.Subject{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: testNamespaces[idx],
		})
	}

	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: aeroClusterCRB,
		},
		Subjects: subjects,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     aeroClusterCR,
		},
	}

	if err := k8sClient.Create(ctx, crb); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func createNamespace(
	k8sClient client.Client, ctx goctx.Context, name string,
) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	err := k8sClient.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func createServiceAccount(
	k8sClient client.Client, ctx goctx.Context, name string, namespace string,
) error {
	svcAct := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := k8sClient.Create(ctx, svcAct)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
