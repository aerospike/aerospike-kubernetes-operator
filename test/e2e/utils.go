package e2e

import (
	goctx "context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/test"
	framework "github.com/operator-framework/operator-sdk/pkg/test"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var schemas map[string]string
var secrets map[string][]byte

const schemaDir = "deploy/config-schemas"
const secretDir = "deploy/secrets"

const tlsSecretName = "aerospike-secret"
const authSecretName = "auth"
const authSecretNameForUpdate = "auth-update"

func cleanupOption(ctx *framework.TestCtx) *framework.CleanupOptions {
	return &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval}
}

func initConf(schemaDir string) error {
	schemas = make(map[string]string)

	fileInfo, err := ioutil.ReadDir(schemaDir)
	if err != nil {
		return err
	}

	if len(fileInfo) == 0 {
		return fmt.Errorf("no config schema file available in %s", schemaDir)
	}

	for _, file := range fileInfo {
		if file.IsDir() {
			// no need to check recursively
			continue
		}

		schema, err := ioutil.ReadFile(filepath.Join(schemaDir, file.Name()))
		if err != nil {
			return fmt.Errorf("wrong config schema file %s: %v", file.Name(), err)
		}

		schemas[file.Name()] = string(schema)
	}

	return nil
}

func initConfigSecret(secretDir string) error {
	secrets = make(map[string][]byte)

	fileInfo, err := ioutil.ReadDir(secretDir)
	if err != nil {
		return err
	}

	if len(fileInfo) == 0 {
		return fmt.Errorf("no secret file available in %s", secretDir)
	}

	for _, file := range fileInfo {
		if file.IsDir() {
			// no need to check recursively
			continue
		}

		secret, err := ioutil.ReadFile(filepath.Join(secretDir, file.Name()))
		if err != nil {
			return fmt.Errorf("wrong secret file %s: %v", file.Name(), err)
		}

		secrets[file.Name()] = secret
	}

	return nil
}

func setupByUser(f *framework.Framework, ctx *framework.TestCtx) error {
	// kubectl create configmap config-schemas --from-file=deploy/config-schemas
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("Could not get namespace: %v", err)
	}

	// Create configSecret
	if err := initConfigSecret(secretDir); err != nil {
		return fmt.Errorf("Failed to init secrets: %v", err)
	}
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsSecretName,
			Namespace: namespace,
		},
		Type: v1.SecretTypeOpaque,
		Data: secrets,
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), s, cleanupOption(ctx))
	if err != nil {
		return err
	}

	// Create authSecret
	pass := "admin123"
	as := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authSecretName,
			Namespace: namespace,
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(pass),
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), as, cleanupOption(ctx))
	if err != nil {
		return err
	}

	passUpdate := "admin321"
	asUpdate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authSecretNameForUpdate,
			Namespace: namespace,
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(passUpdate),
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), asUpdate, cleanupOption(ctx))
	if err != nil {
		return err
	}

	// // Create rbac
	// err = createServiceAccountWithPermission(f, ctx, namespace)
	// if err != nil {
	// 	return err
	// }
	return nil
}

// WaitForOperatorDeployment has the same functionality as WaitForDeployment but will no wait for the deployment if the
// test was run with a locally run operator (--up-local flag)
func waitForOperatorDeployment(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, replicas int, retryInterval, timeout time.Duration) error {
	return waitForDeployment(t, kubeclient, namespace, name, replicas, retryInterval, timeout, true)
}

func waitForDeployment(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, replicas int, retryInterval, timeout time.Duration, isOperator bool) error {
	if isOperator && test.Global.LocalOperator {
		t.Log("Operator is running locally; skip waitForDeployment")
		return nil
	}
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		deployment, err := kubeclient.AppsV1().Deployments(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s deployment\n", name)
				return false, nil
			}
			return false, err
		}

		if int(deployment.Status.AvailableReplicas) == replicas {
			return true, nil
		}
		t.Logf("Waiting for full availability of %s deployment (%d/%d)\n", name, deployment.Status.AvailableReplicas, replicas)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Logf("Deployment available (%d/%d)\n", replicas, replicas)
	return nil
}

func waitForAerospikeCluster(t *testing.T, f *framework.Framework, aeroCluster *aerospikev1alpha1.AerospikeCluster, replicas int, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Fetch the AerospikeCluster instance
		newCluster := &aerospikev1alpha1.AerospikeCluster{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
				return false, nil
			}
			return false, err
		}
		t.Logf("Waiting for full availability of %s AerospikeCluster (%d/%d)\n", aeroCluster.Name, aeroCluster.Status.Size, replicas)

		if int(newCluster.Status.Size) != replicas {
			t.Logf("Cluster size is not correct")
			return false, nil
		}
		if !reflect.DeepEqual(newCluster.Status.AerospikeClusterSpec, newCluster.Spec) {
			t.Logf("Cluster status not updated")
			//t.Logf("Cluster status not updated. cluster.Status.Spec %v, cluster.Spec %v", newCluster.Status.AerospikeClusterSpec, newCluster.Spec)
			return false, nil
		}
		if newCluster.Status.Nodes != nil && len(newCluster.Status.Nodes) == replicas {
			for _, node := range newCluster.Status.Nodes {
				if node.NodeID == "" {
					t.Logf("Cluster nodes nodeID is empty")
					return false, nil
				}
				specBuild := strings.Split(aeroCluster.Spec.Build, ":")[1]
				if node.Build != specBuild {
					t.Logf("Cluster node build %s not same as spec %s", node.Build, specBuild)
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	// make info call
	return nil
}

func getTimeout(nodes int32) time.Duration {
	return (3 * time.Minute * time.Duration(nodes))
}

func validateError(t *testing.T, err error, msg string) {
	if err == nil {
		t.Fatal(msg)
	} else {
		t.Log(err)
	}
}
