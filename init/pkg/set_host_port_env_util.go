package pkg

import (
	goctx "context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetHostPortEnv(k8sClient client.Client, podName, namespace, hostIP string) error {
	infoport, tlsport, err := getPortString(goctx.TODO(), k8sClient, namespace, podName)
	if err != nil {
		return err
	}

	internalIP, externalIP, err := getHostIPS(goctx.TODO(), k8sClient, hostIP)
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

func getPortString(ctx goctx.Context, k8sClient client.Client, namespace,
	podName string) (infoport, tlsport int32, err error) {
	serviceList := &corev1.ServiceList{}
	listOps := &client.ListOptions{Namespace: namespace}

	err = k8sClient.List(ctx, serviceList, listOps)
	if err != nil {
		return infoport, tlsport, err
	}

	for idx := range serviceList.Items {
		service := &serviceList.Items[idx]
		if service.Name == podName {
			for _, port := range service.Spec.Ports {
				if port.Name == "service" {
					infoport = port.NodePort
				}

				if port.Name == "tls-service" {
					tlsport = port.NodePort
				}
			}

			break
		}
	}

	return infoport, tlsport, err
}

func getHostIPS(ctx goctx.Context, k8sClient client.Client, hostIP string) (internalIP, externalIP string, err error) {
	internalIP = hostIP
	externalIP = hostIP
	nodeList := &corev1.NodeList{}

	err = k8sClient.List(ctx, nodeList)
	if err != nil {
		return internalIP, externalIP, err
	}

	for idx := range nodeList.Items {
		node := &nodeList.Items[idx]
		nodeInternalIP := ""
		nodeExternalIP := ""
		matchFound := false

		for _, add := range node.Status.Addresses {
			if add.Address == hostIP {
				matchFound = true
			}

			if add.Type == "InternalIP" {
				nodeInternalIP = add.Address
			} else if add.Type == "ExternalIP" {
				nodeExternalIP = add.Address
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
