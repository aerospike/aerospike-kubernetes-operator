package test

import (
	goctx "context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"
	aeroClusterCRB                string = "aerospike-cluster"
	aeroClusterCR                 string = "aerospike-cluster"
)

var secrets map[string][]byte
var cacertSecrets map[string][]byte

const secretDir = "../config/samples/secrets"               //nolint:gosec // for testing
const cacertSecretDir = "../config/samples/secrets/cacerts" //nolint:gosec // for testing
const awsCredentialPath = "$HOME/.aws/credentials"          //nolint:gosec // for testing

const AerospikeSecretName = "aerospike-secret"
const TLSCacertSecretName = "aerospike-cacert-secret" //nolint:gosec // for testing
const AuthSecretName = "auth-secret"
const AuthSecretNameForUpdate = "auth-update"
const AWSSecretName = "aws-secret"

const MultiClusterNs1 string = "test1"
const MultiClusterNs2 string = "test2"
const AerospikeNs string = "aerospike"
const namespace = "test"

// Namespaces is the list of all the namespaces used in test-suite
var Namespaces = []string{namespace, MultiClusterNs1, MultiClusterNs2, AerospikeNs}

func getLabels() map[string]string {
	return map[string]string{"app": "aerospike-cluster"}
}

func createClusterRBAC(k8sClient client.Client, ctx goctx.Context) error {
	subjects := make([]rbac.Subject, 0, len(Namespaces))

	for idx := range Namespaces {
		// Create service account for getting access in cluster specific namespaces
		if err := createServiceAccount(
			k8sClient, ctx, aeroClusterServiceAccountName, Namespaces[idx],
		); err != nil {
			return err
		}

		// Create subjects to bind clusterRole to serviceAccounts
		subjects = append(subjects, rbac.Subject{
			Kind:      "ServiceAccount",
			Name:      aeroClusterServiceAccountName,
			Namespace: Namespaces[idx],
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

func initConfigSecret(secretDirectory string) (map[string][]byte, error) {
	initSecrets := make(map[string][]byte)

	fileInfo, err := os.ReadDir(secretDirectory)
	if err != nil {
		return nil, err
	}

	if len(fileInfo) == 0 {
		return nil, fmt.Errorf("no secret file available in %s", secretDirectory)
	}

	for _, file := range fileInfo {
		if file.IsDir() {
			// no need to check recursively
			continue
		}

		secret, err := os.ReadFile(filepath.Join(secretDirectory, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("wrong secret file %s: %v", file.Name(), err)
		}

		initSecrets[file.Name()] = secret
	}

	return initSecrets, nil
}

func setupByUser(k8sClient client.Client, ctx goctx.Context) error {
	var err error
	// Create configSecret
	if secrets, err = initConfigSecret(secretDir); err != nil {
		return fmt.Errorf("failed to init secrets: %v", err)
	}

	// Create cacertSecret
	if cacertSecrets, err = initConfigSecret(cacertSecretDir); err != nil {
		return fmt.Errorf("failed to init secrets: %v", err)
	}

	// Create preReq for namespaces used for testing
	for idx := range Namespaces {
		if err := createClusterPreReq(k8sClient, ctx, Namespaces[idx]); err != nil {
			return err
		}
	}

	// Create another authSecret. Used in access-control tests
	passUpdate := "admin321"
	labels := getLabels()

	if err := createAuthSecret(
		k8sClient, ctx, namespace, labels, AuthSecretNameForUpdate, passUpdate,
	); err != nil {
		return err
	}

	return createClusterRBAC(k8sClient, ctx)
}

func createClusterPreReq(
	k8sClient client.Client, ctx goctx.Context, namespace string,
) error {
	labels := getLabels()

	if err := createNamespace(k8sClient, ctx, namespace); err != nil {
		return err
	}

	if err := createConfigSecret(
		k8sClient, ctx, namespace, labels,
	); err != nil {
		return err
	}

	if err := createCacertSecret(
		k8sClient, ctx, namespace, labels,
	); err != nil {
		return err
	}

	// Create authSecret
	pass := "admin123"

	return createAuthSecret(
		k8sClient, ctx, namespace, labels, AuthSecretName, pass,
	)
}

func createCacertSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string,
) error {
	// Create configSecret
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TLSCacertSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: cacertSecrets,
	}

	// Remove old object
	_ = k8sClient.Delete(ctx, s)

	// use test context's create helper to create the object and add a cleanup
	// function for the new object
	err := k8sClient.Create(ctx, s)
	if err != nil {
		return err
	}

	return nil
}

func createConfigSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string,
) error {
	// Create configSecret
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AerospikeSecretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: secrets,
	}

	// Remove old object
	_ = k8sClient.Delete(ctx, s)

	// use test context's create helper to create the object and add a cleanup
	// function for the new object
	return k8sClient.Create(ctx, s)
}

func createAuthSecret(
	k8sClient client.Client, ctx goctx.Context, namespace string,
	labels map[string]string, secretName, pass string,
) error {
	// Create authSecret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(pass),
		},
	}
	// use test context's create helper to create the object and add a cleanup function for the new object
	err := k8sClient.Create(ctx, secret)
	if !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func setupBackupServicePreReq(k8sClient client.Client, ctx goctx.Context, namespace string) error {
	// Create SA for aerospike backup service
	if err := createServiceAccount(k8sClient, goctx.TODO(), v1beta1.AerospikeBackupServiceKey, namespace); err != nil {
		return err
	}

	awsSecret := make(map[string][]byte)

	resolvePath := os.ExpandEnv(awsCredentialPath)

	data, err := os.ReadFile(resolvePath)
	if err != nil {
		return err
	}

	awsSecret["credentials"] = data

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AWSSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: awsSecret,
	}

	// Remove old object
	_ = k8sClient.Delete(ctx, secret)

	return k8sClient.Create(ctx, secret)
}
