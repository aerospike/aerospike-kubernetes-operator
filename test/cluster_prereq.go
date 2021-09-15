package test

import (
	goctx "context"

	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"
	aeroClusterRoleBindingName    string = "aerospike-cluster-rolebinding"
	aeroClusterRole               string = "aerospike-cluster-role"
)

func createClusterResource(k8sClient client.Client, ctx goctx.Context) error {
	// Create namespaces to test multicluster
	if err := createNamespace(k8sClient, ctx, multiClusterNs1); err != nil {
		return err
	}
	if err := createNamespace(k8sClient, ctx, multiClusterNs2); err != nil {
		return err
	}

	// Create service account for getting access in cluster specific namespaces
	if err := createServiceAccount(
		k8sClient, ctx, aeroClusterServiceAccountName, multiClusterNs1,
	); err != nil {
		return err
	}
	if err := createServiceAccount(
		k8sClient, ctx, aeroClusterServiceAccountName, multiClusterNs2,
	); err != nil {
		return err
	}
	// Create clusterRole for service accounts
	if err := createClusterRole(k8sClient, aeroClusterRole); err != nil {
		return err
	}
	// Create clusterRoleBinding to bind clusterRole and accounts
	subjects := []rbac.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: multiClusterNs1,
		},
		{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: multiClusterNs2,
		},
	}
	if err := createRoleBinding(
		k8sClient, aeroClusterRoleBindingName, subjects, aeroClusterRole,
	); err != nil {
		return err
	}

	return nil
}

func createClusterRole(k8sClient client.Client, name string) error {
	cr := &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "nodes", "services"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{"aerospike.com"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"*"},
			},
		},
	}
	return k8sClient.Create(goctx.TODO(), cr)
}

func createNamespace(
	k8sClient client.Client, ctx goctx.Context, name string,
) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return k8sClient.Create(ctx, ns)
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
	return k8sClient.Create(ctx, svcAct)
}

func createRoleBinding(
	k8sClient client.Client, name string, subjects []rbac.Subject,
	roleRefName string,
) error {
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: subjects,
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Name:     roleRefName,
			Kind:     "ClusterRole",
		},
	}
	return k8sClient.Create(goctx.TODO(), crb)
}
