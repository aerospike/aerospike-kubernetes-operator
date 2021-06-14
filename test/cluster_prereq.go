package test

import (
	goctx "context"

	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createClusterResource(k8sClient client.Client, ctx goctx.Context) error {
	// Create namespaces to test multicluster
	if err := createNamespace(k8sClient, ctx, multiClusterNs1); err != nil {
		return err
	}
	if err := createNamespace(k8sClient, ctx, multiClusterNs2); err != nil {
		return err
	}

	operatorNs := namespace

	if err := createServiceAccount(k8sClient, ctx, "aerospike-cluster", operatorNs); err != nil {
		return err
	}

	// Create service account for getting access in cluster specific namespaces
	if err := createServiceAccount(k8sClient, ctx, "aerospike-cluster", multiClusterNs1); err != nil {
		return err
	}
	if err := createServiceAccount(k8sClient, ctx, "aerospike-cluster", multiClusterNs2); err != nil {
		return err
	}

	// Create clusterRole for service accounts
	if err := createClusterRole(k8sClient, ctx, "aerospike-cluster"); err != nil {
		return err
	}

	// Create clusterRoleBinding to bind clusterRole and accounts
	subjects := []rbac.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "aerospike-cluster",
			Namespace: operatorNs,
		},
		{
			Kind:      "ServiceAccount",
			Name:      "aerospike-cluster",
			Namespace: multiClusterNs1,
		},
		{
			Kind:      "ServiceAccount",
			Name:      "aerospike-cluster",
			Namespace: multiClusterNs2,
		},
	}
	if err := createRoleBinding(k8sClient, ctx, "aerospike-cluster", subjects, "aerospike-cluster"); err != nil {
		return err
	}

	return nil
}

func createNamespace(k8sClient client.Client, ctx goctx.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return k8sClient.Create(ctx, ns)
}

func createServiceAccount(k8sClient client.Client, ctx goctx.Context, name string, namespace string) error {
	svcAct := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return k8sClient.Create(ctx, svcAct)
}

func createClusterRole(k8sClient client.Client, ctx goctx.Context, name string) error {
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
				APIGroups: []string{"asdb.aerospike.com"},
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
	return k8sClient.Create(ctx, cr)
}

func createRoleBinding(k8sClient client.Client, ctx goctx.Context, name string, subjects []rbac.Subject, roleRefName string) error {
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
	return k8sClient.Create(ctx, crb)
}
