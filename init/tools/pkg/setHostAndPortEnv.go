package pkg

import (
	goctx "context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

func SetHostPortEnv(podName, namespace string, hostIP *string) error {
	cfg := ctrl.GetConfigOrDie()

	err := clientgoscheme.AddToScheme(clientgoscheme.Scheme)
	if err != nil {
		return err
	}

	err = asdbv1beta1.AddToScheme(clientgoscheme.Scheme)
	if err != nil {
		return err
	}

	k8sClient, err := client.New(
		cfg, client.Options{Scheme: clientgoscheme.Scheme},
	)

	if err != nil {
		return err
	}

	infoport, tlsport, err := getPortString(k8sClient, goctx.TODO(), namespace, podName)
	if err != nil {
		return err
	}

	internalIP, externalIP, err := getHostIPS(k8sClient, goctx.TODO(), *hostIP)
	if err != nil {
		return err
	}

	exportInfoPort := "export infoport=" + strconv.Itoa(int(infoport))
	exportTLSPort := "export tlsport=" + strconv.Itoa(int(tlsport))
	exportInternalIP := "export INTERNALIP=" + internalIP
	exportExternalIP := "export EXTERNALIP=" + externalIP

	fmt.Println(exportInfoPort)
	fmt.Println(exportTLSPort)
	fmt.Println(exportInternalIP)
	fmt.Println(exportExternalIP)

	return nil
}

func getPortString(k8sClient client.Client, ctx goctx.Context, namespace, podName string) (int32, int32, error) {
	var (
		infoport, tlsport int32
	)

	serviceList := &corev1.ServiceList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err := k8sClient.List(ctx, serviceList, listOps)
	if err != nil {
		return infoport, tlsport, err
	}

	for _, service := range serviceList.Items {
		if service.Name == podName {
			for _, port := range service.Spec.Ports {
				if port.Name == "service" {
					infoport = port.NodePort
				}
				if port.Name == "tls-service" {
					tlsport = port.NodePort
				}
			}
		}
	}

	return infoport, tlsport, err
}

func getHostIPS(k8sClient client.Client, ctx goctx.Context, hostIP string) (string, string, error) {
	var (
		internalIP = hostIP
		externalIP = hostIP
	)

	nodeList := &corev1.NodeList{}

	err := k8sClient.List(ctx, nodeList)
	if err != nil {
		return internalIP, externalIP, err
	}

	for _, node := range nodeList.Items {
		nodeInternalIP := ""
		nodeExternalIP := ""
		matchFound := false
		for _, add := range node.Status.Addresses {
			if add.Address == hostIP {
				matchFound = true
			}
			if add.Type == "InternalIP" {
				nodeInternalIP = add.Address
				continue
			}
			if add.Type == "ExternalIP" {
				nodeExternalIP = add.Address
				continue
			}
		}
		if matchFound {
			if nodeInternalIP != "" {
				internalIP = nodeInternalIP
			}
			if nodeExternalIP != "" {
				externalIP = nodeExternalIP
			}
			break
		}
	}

	return internalIP, externalIP, nil
}
