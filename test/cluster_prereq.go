package test

import (
	goctx "context"

	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"
	aeroClusterRoleBindingName    string = "aerospike-operator-manager-rolebinding"
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

	// Create service account for getting access in cluster specific namespaces
	if err := createServiceAccount(k8sClient, ctx, aeroClusterServiceAccountName, multiClusterNs1); err != nil {
		return err
	}
	if err := createServiceAccount(k8sClient, ctx, aeroClusterServiceAccountName, multiClusterNs2); err != nil {
		return err
	}

	// Create clusterRoleBinding to bind clusterRole and accounts
	subjects := []rbac.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: operatorNs,
		},
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
	if err := createRoleBinding(k8sClient, ctx, aeroClusterRoleBindingName, subjects); err != nil {
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

func createRoleBinding(k8sClient client.Client, ctx goctx.Context, name string, subjects []rbac.Subject) error {
	crb := rbac.ClusterRoleBinding{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &crb); err != nil {
		return err
	}
	crb.Subjects = subjects

	return k8sClient.Update(ctx, &crb)
}
