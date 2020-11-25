package e2e

import (
	goctx "context"
	"fmt"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createClusterResource(f *framework.Framework, ctx *framework.TestCtx) error {
	// Create namespaces to test multicluster
	if err := createNamespace(f, ctx, multiClusterNs1); err != nil {
		return err
	}
	if err := createNamespace(f, ctx, multiClusterNs2); err != nil {
		return err
	}

	// Create service account for getting access in operator default namespace
	operatorNs, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("Could not get operator namespace: %v", err)
	}
	if err := createServiceAccount(f, ctx, "aerospike-cluster", operatorNs); err != nil {
		return err
	}

	// Create service account for getting access in cluster specific namespaces
	if err := createServiceAccount(f, ctx, "aerospike-cluster", multiClusterNs1); err != nil {
		return err
	}
	if err := createServiceAccount(f, ctx, "aerospike-cluster", multiClusterNs2); err != nil {
		return err
	}

	// Create clusterRole for service accounts
	if err := createClusterRole(f, ctx, "aerospike-cluster"); err != nil {
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
	if err := createRoleBinding(f, ctx, "aerospike-cluster", subjects, "aerospike-cluster"); err != nil {
		return err
	}

	return nil
}

func createNamespace(f *framework.Framework, ctx *framework.TestCtx, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return f.Client.Create(goctx.TODO(), ns, cleanupOption(ctx))
}

func createServiceAccount(f *framework.Framework, ctx *framework.TestCtx, name string, namespace string) error {
	svcAct := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return f.Client.Create(goctx.TODO(), svcAct, cleanupOption(ctx))
}

func createClusterRole(f *framework.Framework, ctx *framework.TestCtx, name string) error {
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
		},
	}
	return f.Client.Create(goctx.TODO(), cr, cleanupOption(ctx))
}

func createRoleBinding(f *framework.Framework, ctx *framework.TestCtx, name string, subjects []rbac.Subject, roleRefName string) error {
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
	return f.Client.Create(goctx.TODO(), crb, cleanupOption(ctx))
}
