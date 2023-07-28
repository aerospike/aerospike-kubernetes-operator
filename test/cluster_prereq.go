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
		secretName := "jfrogcred"
		secretRefs := make([]corev1.LocalObjectReference, 0)
		secretRef := new(corev1.LocalObjectReference)
		secretRef.Name = secretName
		secretRefs = append(secretRefs, *secretRef)

		// Create service account for getting access in cluster specific namespaces
		if err := createServiceAccount(
			k8sClient, ctx, aeroClusterServiceAccountName, testNamespaces[idx], secretRefs,
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
	k8sClient client.Client, ctx goctx.Context, name string, namespace string, secretRef []corev1.LocalObjectReference,
) error {
	svcAct := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		ImagePullSecrets: secretRef,
	}

	err := k8sClient.Create(ctx, svcAct)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	} else if errors.IsAlreadyExists(err) {
		pkgLog.Info("service account already exist")
		errUpdate := k8sClient.Update(ctx, svcAct)
		if errUpdate != nil {
			return err
		}
	}

	return nil
}
