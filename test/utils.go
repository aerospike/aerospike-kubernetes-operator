package test

import (
	goctx "context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
)

func InitialiseClients(scheme *runtime.Scheme, cfg *rest.Config) (
	k8sClient client.Client, k8sClientSet *kubernetes.Clientset, err error) {
	utilRuntime.Must(clientgoscheme.AddToScheme(scheme))
	utilRuntime.Must(asdbv1.AddToScheme(scheme))
	utilRuntime.Must(admissionv1.AddToScheme(scheme))
	utilRuntime.Must(asdbv1beta1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(
		cfg, client.Options{Scheme: scheme},
	)
	if err != nil {
		return k8sClient, k8sClientSet, err
	}

	if k8sClient == nil {
		err = fmt.Errorf("k8sClient is nil")
		return k8sClient, k8sClientSet, err
	}

	k8sClientSet = kubernetes.NewForConfigOrDie(cfg)

	if k8sClientSet == nil {
		err = fmt.Errorf("k8sClientSet is nil")
		return k8sClient, k8sClientSet, err
	}

	return k8sClient, k8sClientSet, nil
}

func GetNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func StartTestEnvironment() (testEnv *envtest.Environment, cfg *rest.Config, err error) {
	t := true
	testEnv = &envtest.Environment{
		UseExistingCluster: &t,
	}

	cfg, err = testEnv.Start()
	if err != nil {
		return testEnv, cfg, err
	}

	if cfg == nil {
		err = fmt.Errorf("cfg is nil")
		return testEnv, cfg, err
	}

	return testEnv, cfg, nil
}

func SetNodeLabels(ctx goctx.Context, k8sClient client.Client, labels map[string]string) error {
	nodeList, err := GetNodeList(ctx, k8sClient)
	if err != nil {
		return err
	}

	for idx := range nodeList.Items {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			node := &nodeList.Items[idx]

			if err := k8sClient.Get(
				ctx, types.NamespacedName{Name: node.Name}, node); err != nil {
				return err
			}

			for key, val := range labels {
				node.Labels[key] = val
			}

			return k8sClient.Update(ctx, node)
		}); err != nil {
			return err
		}
	}

	return nil
}

func DeleteNodeLabels(ctx goctx.Context, k8sClient client.Client, keys []string) error {
	nodeList, err := GetNodeList(ctx, k8sClient)
	if err != nil {
		return err
	}

	for idx := range nodeList.Items {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			node := &nodeList.Items[idx]

			if err := k8sClient.Get(
				ctx, types.NamespacedName{Name: node.Name}, node); err != nil {
				return err
			}

			for _, key := range keys {
				delete(node.Labels, key)
			}

			return k8sClient.Update(ctx, node)
		}); err != nil {
			return err
		}
	}

	return nil
}

func GetNodeList(ctx goctx.Context, k8sClient client.Client) (
	*corev1.NodeList, error,
) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList); err != nil {
		return nil, err
	}

	return nodeList, nil
}

// GetContainerByName finds a container by name in a slice of containers.
// Returns the container pointer if found, nil otherwise.
func GetContainerByName(containers []corev1.Container, name string) *corev1.Container {
	for idx := range containers {
		if containers[idx].Name == name {
			return &containers[idx]
		}
	}

	return nil
}
