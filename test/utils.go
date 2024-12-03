package test

import (
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

func BootStrapTestEnv(scheme *runtime.Scheme) (testEnv *envtest.Environment, cfg *rest.Config,
	k8sClient client.Client, k8sClientSet *kubernetes.Clientset, dynamicClient *dynamic.DynamicClient, err error) {
	t := true
	testEnv = &envtest.Environment{
		UseExistingCluster: &t,
	}

	cfg, err = testEnv.Start()

	if err != nil {
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	if cfg == nil {
		err = fmt.Errorf("cfg is nil")
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	utilRuntime.Must(clientgoscheme.AddToScheme(scheme))
	utilRuntime.Must(asdbv1.AddToScheme(scheme))
	utilRuntime.Must(admissionv1.AddToScheme(scheme))
	utilRuntime.Must(asdbv1beta1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(
		cfg, client.Options{Scheme: scheme},
	)

	if err != nil {
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	if k8sClient == nil {
		err = fmt.Errorf("k8sClient is nil")
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	k8sClientSet = kubernetes.NewForConfigOrDie(cfg)

	if k8sClientSet == nil {
		err = fmt.Errorf("k8sClientSet is nil")
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	dynamicClient = dynamic.NewForConfigOrDie(cfg)
	if dynamicClient == nil {
		err = fmt.Errorf("dynamicClient is nil")
		return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, err
	}

	return testEnv, cfg, k8sClient, k8sClientSet, dynamicClient, nil
}
