package pkg

import (
	"context"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ExportK8sConfigmap(k8sClient client.Client, namespace, cmName, toDir string) error {
	configMap := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.TODO(),
		types.NamespacedName{Name: cmName, Namespace: namespace}, configMap); err != nil {
		return err
	}

	if err := os.MkdirAll(toDir, 0644); err != nil { //nolint:gocritic // file permission
		return err
	}

	for key, value := range configMap.Data {
		f, err := os.Create(filepath.Join(toDir, key))
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
		f, err := os.Create(filepath.Join(toDir, key))
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
