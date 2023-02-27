package pkg

import (
	"context"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func ExportK8sConfigmap(namespace string, toDir, cmName *string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), *cmName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = os.MkdirAll(*toDir, 0644) //nolint:gocritic // file permission
	if err != nil {
		return err
	}

	for key, value := range configMap.Data {
		f, err := os.Create(filepath.Join(*toDir, key))
		if err != nil {
			return err
		}

		if _, err = f.WriteString(value); err != nil {
			return err
		}

		if err := f.Sync(); err != nil {
			return err
		}

		if err := f.Close(); err != nil {
			return err
		}
	}

	for key, value := range configMap.BinaryData {
		f, err := os.Create(filepath.Join(*toDir, key))
		if err != nil {
			return err
		}

		if _, err = f.Write(value); err != nil {
			return err
		}

		if err := f.Sync(); err != nil {
			return err
		}

		if err := f.Close(); err != nil {
			return err
		}
	}

	return nil
}
