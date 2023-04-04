package pkg

import (
	"fmt"
)

// QuickRestart refreshes Aerospike config map and tries to warm restart Aerospike.
func (initp *InitParams) QuickRestart(cmName, cmNamespace string) error {
	if cmNamespace == "" {
		return fmt.Errorf("kubernetes namespace required as an argument")
	}

	if cmName == "" {
		return fmt.Errorf("aerospike configmap required as an argument")
	}

	if err := initp.ExportK8sConfigmap(cmNamespace, cmName, configMapDir); err != nil {
		return err
	}

	if err := initp.restartASD(); err != nil {
		return err
	}

	// Update pod status in the k8s aerospike cluster object
	if err := initp.manageVolumesAndUpdateStatus("quickRestart"); err != nil {
		return err
	}

	return nil
}
