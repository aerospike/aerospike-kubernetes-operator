package pkg

import (
	"context"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (initp *InitParams) ExportK8sConfigmap(ctx context.Context, namespace, cmName, toDir string) error {
	configMap := &corev1.ConfigMap{}
	if err := initp.k8sClient.Get(ctx,
		types.NamespacedName{Name: cmName, Namespace: namespace}, configMap); err != nil {
		return err
	}

	if err := os.MkdirAll(toDir, 0755); err != nil { //nolint:gocritic // file permission
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

	initp.logger.Info("Created and populated config map directory", "cm-name", cmName, "dir", toDir)

	return nil
}
