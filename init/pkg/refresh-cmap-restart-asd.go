package pkg

import (
	"fmt"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/sirupsen/logrus"
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

	if err := initp.createAerospikeConf(); err != nil {
		return err
	}

	processes, psErr := ps.Processes()
	if psErr != nil {
		return psErr
	}

	asdPid := -1

	for _, ps := range processes {
		if ps.Executable() == "asd" {
			asdPid = ps.Pid()
		}
	}

	if asdPid == -1 {
		return fmt.Errorf("error getting Aerospike server PID")
	}

	if err := syscall.Kill(1, syscall.SIGUSR1); err != nil {
		return err
	}

	for i := 1; i <= 60; i++ {
		asdProc, psErr := ps.FindProcess(asdPid)
		if psErr != nil {
			return psErr
		}

		if asdProc != nil {
			logrus.Info("Waiting for Aerospike server to terminate ...")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	asdProc, err := ps.FindProcess(asdPid)
	if err != nil {
		return err
	}

	if asdProc != nil {
		return fmt.Errorf("aborting warm start - Aerospike server did not terminate")
	}

	if err := initp.manageVolumesAndUpdateStatus("quickRestart"); err != nil {
		return err
	}

	return nil
}
