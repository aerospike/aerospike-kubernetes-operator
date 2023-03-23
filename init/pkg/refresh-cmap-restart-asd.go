package pkg

import (
	"fmt"
	"path/filepath"
)

func (initp *InitParams) QuickRestart(cmName, cmNamespace string) error {
	if cmNamespace == "" {
		return fmt.Errorf("kubernetes namespace required as an argument")
	}

	if cmName == "" {
		return fmt.Errorf("aerospike configmap required as an argument")
	}

	if err := initp.ExportK8sConfigmap(cmNamespace, cmName, filepath.Join(configVolume, "configmap")); err != nil {
		return err
	}

	if err := initp.restartASD(); err != nil {
		return err
	}

	if err := initp.manageVolumesAndUpdateStatus("quickRestart"); err != nil {
		return err
	}

	return nil
}
