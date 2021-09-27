package test

import (
	goctx "context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"
)

func createClusterRBAC(k8sClient client.Client, ctx goctx.Context) error {
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

	// Create clusterRoleBinding to bind clusterRole and accounts
	subjects := []rbac.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: namespace,
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

	return updateRoleBinding(k8sClient, ctx, subjects)
}

func getClusterRoleBinding(
	k8sClient client.Client, ctx goctx.Context,
) (*rbac.ClusterRoleBinding, error) {
	crbs := &rbac.ClusterRoleBindingList{}
	if err := k8sClient.List(ctx, crbs); err != nil {
		return nil, err
	}
	for _, crb := range crbs.Items {
		value, ok := crb.Labels["olm.owner"]
		if !ok {
			continue
		}

		if strings.HasPrefix(value, "aerospike-kubernetes-operator") {
			return &crb, nil
		}
	}
	return nil, fmt.Errorf("could not find cluster role binding for operator")
}

func updateRoleBinding(
	k8sClient client.Client, ctx goctx.Context,
	subjects []rbac.Subject,
) error {
	crb, err := getClusterRoleBinding(k8sClient, ctx)
	if err != nil {
		return err
	}

	crb.Subjects = subjects

	err = k8sClient.Update(ctx, crb)
	if err != nil && !errors.IsAlreadyExists(err) {
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
